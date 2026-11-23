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

/** What the list says of the interface: up while nothing is forwarded for it. */
async function forwardingOff(page: Page) {
  const answer = await bridgeCall(
    page,
    "wireguard.list_interfaces",
    {},
    randomUUID(),
  );
  expect(answer.error ?? "", "wireguard.list_interfaces").toBe("");
  const list = answer.data as { name: string; forwardingOff: boolean }[];
  return list.find((iface) => iface.name === IFACE)?.forwardingOff;
}
const ownForwarding = () =>
  sh(`cat /proc/sys/net/ipv4/conf/${IFACE}/forwarding`);
const linkIndex = () => sh(`cat /sys/class/net/${IFACE}/ifindex`);

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

// The connection check (docs/wireguard.md, "Connection check"): a throwaway
// peer on the running device and its side in an anonymous namespace, with
// nothing left behind afterwards.
const CHECK_LINK = "linuxio.check";
const CHECK_TITLES = [
  "Connects to the server",
  "Traffic passes through the tunnel",
  "Reaches the network behind the server",
  "Names resolve",
];

interface CheckStep {
  detail?: string;
  id: string;
  status: string;
  target?: string;
}

/** wireguard.check_interface's statuses by step, and the steps for a message. */
async function checkConnection(page: Page) {
  const answer = await bridgeCall(
    page,
    "wireguard.check_interface",
    { name: IFACE },
    randomUUID(),
  );
  expect(answer.error ?? "", "wireguard.check_interface").toBe("");
  const steps = (answer.data as { steps: CheckStep[] }).steps;
  return {
    statuses: Object.fromEntries(steps.map((step) => [step.id, step.status])),
    steps: JSON.stringify(steps),
  };
}

/** What a check must not leave: device peers, a link, a namespace, a listed peer. */
async function checkLeftovers(page: Page) {
  const listed = await bridgeCall(
    page,
    "wireguard.list_peers",
    { interfaceName: IFACE },
    randomUUID(),
  );
  expect(listed.error ?? "", "wireguard.list_peers").toBe("");
  return {
    devicePeers: await sh(`sudo wg show ${IFACE} peers | sort`),
    links: await sh(`ip -o link show | grep -cF '${CHECK_LINK}' || true`),
    namespaces: await sh("sudo ip netns list"),
    listedPeers: (listed.data as { public_key: string }[])
      .map((peer) => peer.public_key)
      .sort(),
  };
}

const cleanUp = async () => {
  await vmSsh(
    `sudo systemctl disable --now ${UNIT} >/dev/null 2>&1; sudo wg-quick down ${IFACE} >/dev/null 2>&1; ` +
      `sudo systemctl reset-failed ${UNIT} >/dev/null 2>&1; sudo rm -rf ${CONFIG} /etc/wireguard/${IFACE}; ` +
      `sudo ip netns del ${PEER_NS} 2>/dev/null; sudo ip link del ${PEER_LINK} 2>/dev/null; ` +
      `sudo rm -f ${PEER_KEYS}.key ${PEER_KEYS}.psk ${FORWARD_FILE}; true`,
  );
  await restoreDocker();
  // The spec turns forwarding off, on the host and for new interfaces, and
  // a run that failed there would leave Docker's containers without a network.
  await vmSsh(
    "systemctl is-active -q docker.service && sudo sysctl -qw net.ipv4.ip_forward=1 net.ipv4.conf.default.forwarding=1; true",
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

    // Through the page, as a user makes one: a name and one device shown,
    // the port, network and NIC under Advanced.
    await page.goto("/network/wireguard");
    await page.getByRole("button", { name: "Create New Interface" }).click();
    const create = page.getByRole("dialog", { name: "Create New Interface" });
    await create.getByLabel("Interface Name").fill(IFACE);
    await create.getByLabel("Devices").fill("1");
    await create.getByRole("button", { name: "Advanced" }).click();
    await create.getByLabel("Port").fill("51999");
    await create.getByLabel("CIDR").fill("10.99.0.1/24");
    const nic = create.getByRole("combobox", { name: "NIC" });
    await expect(nic).not.toHaveText(/Loading/);
    await nic.click();
    await page
      .getByRole("option", { name: new RegExp(`^${egressNic} \\(`) })
      .click();
    await expect(nic).toHaveText(new RegExp(`^${egressNic} \\(`));
    await expect(create.getByText(`through ${egressNic}`)).toBeVisible();
    await create.getByRole("button", { name: "Create Interface" }).click();

    // The first device's QR code opens by itself, carried in ?qr=.
    const qr = page.getByRole("dialog", { name: /^Connect / });
    await expect(qr).toBeVisible({ timeout: 60_000 });
    await expect(qr.getByRole("img", { name: /^QR code for / })).toBeVisible();
    await expect(page).toHaveURL(/[?&]iface=wge2e(&|$)/);
    await expect(page).toHaveURL(/[?&]qr=[^&]+/);
    await expect(page.getByText(`Clients for ${IFACE}`)).toBeVisible();
    await qr.getByRole("button", { name: "Done" }).click();
    await expect(qr).toHaveCount(0);
    await expect(page).not.toHaveURL(/[?&]qr=/);

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

  await test.step("a connection check proves the tunnel, finds forwarding off and leaves nothing", async () => {
    const before = await checkLeftovers(page);
    expect(before.links, "a probe link before any check").toBe("0");

    const ok = await checkConnection(page);
    expect(ok.statuses, ok.steps).toEqual({
      handshake: "ok",
      tunnel: "ok",
      beyond: "ok",
      dns: "ok",
    });
    expect(await checkLeftovers(page), "after a check").toEqual(before);

    await vmSsh("sudo sysctl -qw net.ipv4.ip_forward=0");
    const off = await checkConnection(page);
    expect(
      [off.statuses.handshake, off.statuses.tunnel, off.statuses.beyond],
      off.steps,
    ).toEqual(["ok", "ok", "failed"]);
    expect(await checkLeftovers(page), "after a failed check").toEqual(before);

    await call(page, "restore_forwarding", { name: IFACE });
    const again = await checkConnection(page);
    expect(again.statuses, again.steps).toEqual({
      handshake: "ok",
      tunnel: "ok",
      beyond: "ok",
      dns: "ok",
    });

    await page.goto("/network/wireguard");
    await page
      .getByRole("button", { name: "Check connection", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    for (const title of CHECK_TITLES) {
      await expect(
        dialog
          .getByRole("listitem")
          .filter({ hasText: title })
          .getByRole("img", { name: "Passed" }),
        title,
      ).toBeVisible({ timeout: 30_000 });
    }
    await dialog.getByRole("button", { name: "Close" }).click();
    await expect(dialog).toHaveCount(0);
    expect(await checkLeftovers(page), "after the page's check").toEqual(
      before,
    );
  });

  await test.step("the page says when forwarding is turned off, and turns it on again", async () => {
    expect(await forwardingOff(page), "forwardingOff while it forwards").toBe(
      false,
    );
    const link = await linkIndex();
    await vmSsh("sudo sysctl -qw net.ipv4.ip_forward=0");
    expect(await forwardingOff(page), "forwardingOff after the sysctl").toBe(
      true,
    );
    expect(await peerPingsAnswered(), "pings with forwarding off").toBe("0");

    await page.goto("/network/wireguard");
    await expect(page.getByText("Not forwarding")).toBeVisible();
    await page.getByRole("button", { name: "Turn on forwarding" }).click();
    await expect(page.getByText("Not forwarding")).toHaveCount(0);

    expect(await forwarding(), "forwarding after the fix").toBe("1");
    expect(await forwardingOff(page), "forwardingOff after the fix").toBe(
      false,
    );
    expect(await peerPingsAnswered(), "pings after the fix").toBe("2");
    // The same link: the interface was not restarted for it.
    expect(await linkIndex(), "the link's index").toBe(link);
  });

  await test.step("an interface that comes up with its own forwarding off is turned on", async () => {
    // A host that forwards, where new interfaces start with forwarding off.
    await call(page, "down_interface", { name: IFACE });
    await vmSsh("sudo sysctl -qw net.ipv4.conf.default.forwarding=0");
    await call(page, "up_interface", { name: IFACE });
    await vmSsh("sudo sysctl -qw net.ipv4.conf.default.forwarding=1");
    expect(await forwarding(), "the host's forwarding").toBe("1");
    expect(await ownForwarding(), "the interface's own forwarding").toBe("1");
    expect(await peerPingsAnswered(), "pings after that up").toBe("2");
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
