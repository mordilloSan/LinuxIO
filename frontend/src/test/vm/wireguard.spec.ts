import { randomUUID } from "node:crypto";

import { expect, type Page, test } from "@playwright/test";

import {
  bridgeCall,
  rig,
  signIn,
  signOutIfSignedIn,
  stopAptRuns,
  vmDistro,
  vmSsh,
} from "./vm";

// A WireGuard interface goes up and down through wg-quick@<name>.service
// (docs/wireguard.md). On Ubuntu wg-quick is confined by an AppArmor profile
// that runs ip and wg under profiles of their own, which a process started
// from a unit with NoNewPrivileges=yes cannot do: the root job host ran
// wg-quick itself, and every interface failed with "Operation not permitted".
// The job hosts keep the flag, so the one thing still run from there, the
// down of an interface its unit does not hold, is refused on Ubuntu.
const IFACE = "wge2e";
const UNIT = `wg-quick@${IFACE}.service`;
const CONFIG = `/etc/wireguard/${IFACE}.conf`;

const sh = async (command: string) => (await vmSsh(command)).trim();

const linkExists = async () =>
  (await sh(
    `ip -o link show ${IFACE} >/dev/null 2>&1 && echo yes || echo no`,
  )) === "yes";
const unitState = () => sh(`systemctl is-active ${UNIT} || true`);
const unitEnabled = () =>
  sh(`systemctl is-enabled ${UNIT} 2>/dev/null || true`);

/** What AppArmor refused wg-quick in this boot for want of new privileges. */
const refusedProfileChanges = () =>
  sh(
    `sudo journalctl -k -b --no-pager | grep 'profile="wg-quick' | grep -c 'no new privs' || true`,
  );

async function call(page: Page, route: string, request: unknown) {
  const answer = await bridgeCall(
    page,
    `wireguard.${route}`,
    request,
    randomUUID(),
  );
  // The error first, so a failure says what the bridge answered.
  expect(answer.error ?? "", `wireguard.${route}`).toBe("");
  expect(answer.status, `wireguard.${route}`).toBe("ok");
}

// IPv4 forwarding (docs/wireguard.md, "Forwarding"): LinuxIO turns it on
// before the unit starts and keeps it on at boot with this file. A sysctl
// line in PostUp is refused by wg-quick's profile on Ubuntu, without an
// error, and the peers of such an interface reached nothing behind the VM.
const FORWARD_FILE = "/etc/sysctl.d/90-linuxio-wireguard.conf";
const forwarding = () => sh("cat /proc/sys/net/ipv4/ip_forward");
const forwardFileExists = async () =>
  (await sh(`test -e ${FORWARD_FILE} && echo yes || echo no`)) === "yes";

// Docker turns forwarding on when it starts, which would hide a boot that
// LinuxIO's file did not cover, so the reboot runs without it.
// The units it disabled are listed in a file on the VM, so that the clean-up
// of a later run enables them again after a run that was cut short.
const DOCKER_UNITS = "docker.service docker.socket";
const DOCKER_DISABLED = "/var/tmp/wge2e-docker-disabled";
const disableDocker = () =>
  vmSsh(
    `for unit in ${DOCKER_UNITS}; do systemctl is-enabled -q "$unit" 2>/dev/null && echo "$unit"; done | sudo tee ${DOCKER_DISABLED} >/dev/null; ` +
      `sudo systemctl disable $(cat ${DOCKER_DISABLED}) >/dev/null 2>&1; true`,
  );
const restoreDocker = () =>
  vmSsh(
    `test -s ${DOCKER_DISABLED} && sudo systemctl enable $(cat ${DOCKER_DISABLED}) >/dev/null 2>&1 && sudo systemctl start docker.service; ` +
      `sudo rm -f ${DOCKER_DISABLED}; true`,
  );

// A peer of the interface, in a network namespace of the VM: it has the
// interface's first exported client, sends everything through the tunnel and
// pings the VM's own gateway, which only a forwarded and masqueraded packet
// reaches. The key file is under /etc/wireguard because wg's profile on
// Ubuntu reads keys nowhere else. Prints how many of two pings came back.
const PEER_NS = "wge2e-peer";
const PEER_LINK = "wge2epeer";
const PEER_KEYS = "/etc/wireguard/wge2e-peer";
const PEER_PROBE = `
set -eu
ip netns del ${PEER_NS} 2>/dev/null || true
ip link del ${PEER_LINK} 2>/dev/null || true
conf=$(ls /etc/wireguard/${IFACE}/*.conf | head -n 1)
value() { awk -v key="$1" 'sub("^" key " *= *", "") { print; exit }' "$conf"; }
umask 077
value PrivateKey >${PEER_KEYS}.key
psk=$(value PresharedKey)
gateway=$(ip -4 route show default | awk '{ print $3; exit }')
ip netns add ${PEER_NS}
ip link add ${PEER_LINK} type wireguard
ip link set ${PEER_LINK} netns ${PEER_NS}
set -- peer "$(wg show ${IFACE} public-key)" endpoint 127.0.0.1:51999 allowed-ips 0.0.0.0/0
if [ -n "$psk" ]; then
  echo "$psk" >${PEER_KEYS}.psk
  set -- "$@" preshared-key ${PEER_KEYS}.psk
fi
ip netns exec ${PEER_NS} wg set ${PEER_LINK} private-key ${PEER_KEYS}.key "$@"
ip -n ${PEER_NS} address add "$(value Address | cut -d, -f1)" dev ${PEER_LINK}
ip -n ${PEER_NS} link set ${PEER_LINK} up
ip -n ${PEER_NS} route add default dev ${PEER_LINK}
ip netns exec ${PEER_NS} ping -c 2 -W 3 -q "$gateway" | awk '/received/ { print $4 }' || true
ip netns del ${PEER_NS}
rm -f ${PEER_KEYS}.key ${PEER_KEYS}.psk
`;
const peerPingsAnswered = async () =>
  (await vmSsh("sudo bash -s", PEER_PROBE)).trim();

const cleanUp = async () => {
  await vmSsh(
    `sudo systemctl disable --now ${UNIT} >/dev/null 2>&1; sudo wg-quick down ${IFACE} >/dev/null 2>&1; ` +
      `sudo systemctl reset-failed ${UNIT} >/dev/null 2>&1; sudo rm -rf ${CONFIG} /etc/wireguard/${IFACE}; ` +
      `sudo ip netns del ${PEER_NS} 2>/dev/null; sudo ip link del ${PEER_LINK} 2>/dev/null; ` +
      `sudo rm -f ${PEER_KEYS}.key ${PEER_KEYS}.psk ${FORWARD_FILE}; true`,
  );
  await restoreDocker();
  // The spec turns forwarding off twice, and a run that failed there would
  // leave Docker's containers without a network.
  await vmSsh(
    "systemctl is-active -q docker.service && sudo sysctl -qw net.ipv4.ip_forward=1; true",
  );
};

test.beforeAll(async () => {
  test.setTimeout(600_000);
  if (vmDistro === "fedora") {
    await vmSsh(
      "command -v wg-quick >/dev/null || sudo dnf install -y -q wireguard-tools",
    );
  } else {
    await stopAptRuns();
    await vmSsh(
      "command -v wg-quick >/dev/null || sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq wireguard-tools >/dev/null",
    );
  }
  await cleanUp();
});

test.afterAll(cleanUp);

test.afterEach(async ({ page }) => signOutIfSignedIn(page));

test("an interface goes up, down and away through its unit, also after a reboot", async ({
  page,
}) => {
  test.setTimeout(900_000);
  await signIn(page);
  const egressNic = await sh(
    "ip -o -4 route show default | awk '{ print $5; exit }'",
  );

  if (vmDistro !== "fedora") {
    // Without the profile the checks below would prove nothing about it.
    expect(
      await sh(
        "sudo grep -c '^wg-quick ' /sys/kernel/security/apparmor/profiles || true",
      ),
      "wg-quick's AppArmor profile is loaded",
    ).not.toBe("0");
  }

  await test.step("create brings it up through the unit", async () => {
    // A host that forwards nothing yet, as one without Docker is.
    await vmSsh("sudo sysctl -qw net.ipv4.ip_forward=0");
    expect(await forwarding(), "forwarding before the create").toBe("0");
    await call(page, "add_interface", {
      name: IFACE,
      addresses: "10.99.0.1/24",
      listenPort: "51999",
      egressNic,
      numPeers: "1",
    });
    expect(await linkExists(), "the interface").toBe(true);
    expect(await unitState(), UNIT).toBe("active");
    expect(await refusedProfileChanges(), "refused profile changes").toBe("0");
  });

  await test.step("its peer reaches what is behind the server", async () => {
    expect(await forwarding(), "forwarding after the create").toBe("1");
    expect(await forwardFileExists(), FORWARD_FILE).toBe(true);
    expect(await peerPingsAnswered(), "pings through the tunnel").toBe("2");

    // Turned off behind LinuxIO's back: the next up turns it on again.
    await call(page, "down_interface", { name: IFACE });
    await vmSsh("sudo sysctl -qw net.ipv4.ip_forward=0");
    await call(page, "up_interface", { name: IFACE });
    expect(await forwarding(), "forwarding after the up").toBe("1");
    expect(await peerPingsAnswered(), "pings after the up").toBe("2");
  });

  await test.step("down and up", async () => {
    await call(page, "down_interface", { name: IFACE });
    expect(await linkExists(), "the interface after down").toBe(false);
    expect(await unitState(), UNIT).toBe("inactive");

    await call(page, "up_interface", { name: IFACE });
    expect(await linkExists(), "the interface after up").toBe(true);
    expect(await unitState(), UNIT).toBe("active");
  });

  await test.step("up clears a unit left active by a hand-run down", async () => {
    await vmSsh(`sudo wg-quick down ${IFACE}`);
    expect(await unitState(), "the unit after wg-quick down by hand").toBe(
      "active",
    );
    await call(page, "up_interface", { name: IFACE });
    expect(await linkExists(), "the interface").toBe(true);
    expect(await unitState(), UNIT).toBe("active");
  });

  await test.step("a failed start says why, and an interface outside its unit", async () => {
    await vmSsh(`sudo systemctl stop ${UNIT} && sudo wg-quick up ${IFACE}`);
    expect(await unitState(), UNIT).toBe("inactive");

    const failed = await bridgeCall(
      page,
      "wireguard.up_interface",
      { name: IFACE },
      randomUUID(),
    );
    expect(failed.status, "up of an interface that exists").toBe("error");
    // wg-quick's own words, from the unit's journal.
    expect(failed.error).toContain("already exists");

    // Nothing was refused so far: every wg-quick ran under systemd.
    expect(await refusedProfileChanges(), "refused profile changes").toBe("0");

    // wg-quick run by the root job host itself. Where it is confined the
    // host's NoNewPrivileges makes its profile refuse wg, and the job says
    // what to run; elsewhere it takes the interface down.
    const down = await bridgeCall(
      page,
      "wireguard.down_interface",
      { name: IFACE },
      randomUUID(),
    );
    if (vmDistro === "fedora") {
      expect(down.error ?? "", "down outside the unit").toBe("");
    } else {
      expect(down.status, "down outside the unit").toBe("error");
      expect(down.error).toContain(`run "wg-quick down ${IFACE}" as root`);
      expect(await linkExists(), "the interface").toBe(true);
      await vmSsh(`sudo wg-quick down ${IFACE}`);
    }
    expect(await linkExists(), "the interface").toBe(false);

    await call(page, "up_interface", { name: IFACE });
    expect(await unitState(), "the unit after its failed start").toBe("active");
  });

  await test.step("an enabled interface is up after a reboot and still answers", async () => {
    // As for an interface that was up before this LinuxIO, or started with
    // systemctl: start at boot is then what writes the file.
    await vmSsh(`sudo rm -f ${FORWARD_FILE}`);
    await call(page, "enable_interface", { name: IFACE });
    expect(await unitEnabled(), UNIT).toBe("enabled");
    expect(await forwardFileExists(), FORWARD_FILE).toBe(true);

    await disableDocker();

    await rig(["reboot"]);
    expect(await linkExists(), "the interface after the reboot").toBe(true);
    expect(await unitState(), UNIT).toBe("active");
    // Nothing but the sysctl.d file turned forwarding on in this boot.
    expect(
      await sh("systemctl is-active docker.service || true"),
      "Docker after the reboot",
    ).not.toBe("active");
    expect(await forwarding(), "forwarding after the reboot").toBe("1");
    expect(await peerPingsAnswered(), "pings after the reboot").toBe("2");
    await restoreDocker();

    await signIn(page);
    await call(page, "down_interface", { name: IFACE });
    expect(await linkExists(), "the interface after down").toBe(false);
    await call(page, "up_interface", { name: IFACE });
    expect(await linkExists(), "the interface after up").toBe(true);
    // The kernel log of this boot: nothing refused since the reboot.
    expect(await refusedProfileChanges(), "refused profile changes").toBe("0");
  });

  await test.step("remove takes it down, disables its unit and deletes its config", async () => {
    await call(page, "remove_interface", { name: IFACE });
    expect(await linkExists(), "the interface").toBe(false);
    expect(await unitState(), UNIT).toBe("inactive");
    expect(await unitEnabled(), UNIT).toBe("disabled");
    expect(
      await sh(`sudo test -e ${CONFIG} && echo yes || echo no`),
      CONFIG,
    ).toBe("no");
    // The last interface that needed it: the boot setting goes, and the
    // running value stays for whatever else counts on it.
    expect(await forwardFileExists(), FORWARD_FILE).toBe(false);
    expect(await forwarding(), "forwarding after the removal").toBe("1");
  });
});
