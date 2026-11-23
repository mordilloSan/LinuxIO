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

const cleanUp = () =>
  vmSsh(
    `sudo systemctl disable --now ${UNIT} >/dev/null 2>&1; sudo wg-quick down ${IFACE} >/dev/null 2>&1; ` +
      `sudo systemctl reset-failed ${UNIT} >/dev/null 2>&1; sudo rm -rf ${CONFIG} /etc/wireguard/${IFACE}; true`,
  );

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
    await call(page, "enable_interface", { name: IFACE });
    expect(await unitEnabled(), UNIT).toBe("enabled");

    await rig(["reboot"]);
    expect(await linkExists(), "the interface after the reboot").toBe(true);
    expect(await unitState(), UNIT).toBe("active");

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
  });
});
