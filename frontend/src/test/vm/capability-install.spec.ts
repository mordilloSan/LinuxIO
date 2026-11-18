import { expect, test } from "@playwright/test";

import {
  jobIds,
  newJob,
  signIn,
  signOutIfSignedIn,
  stopAptRuns,
  vmDistro,
  vmSsh,
  waitForJob,
} from "./vm";

// Install from Capabilities, the way an admin gets zram: without a zram tool
// the Memory overflow dialog links to Capabilities, Install puts the
// distribution's package in through PackageKit as a job, and the dialog then
// names that tool as the zram provider (docs/capabilities.md, "zram"). It is
// written for Ubuntu and, with DISTRO=fedora, Fedora; only the package, how
// it is taken out and put back, and the provider differ. On Fedora, with
// SELinux enforcing, sign-in stops before the bridge starts, so the test fails
// there until the change in docs/TODO/backlog.md ("Visible bugs") is made.
//
// Before the test the tool is taken out with the distribution's package
// manager, with no zram left swapping, as on a host that never had one. The
// spec does not refresh PackageKit's cache: the install refreshes it once by
// itself when it does not find the package (docs/capabilities.md), and the
// messages the spec logs show whether it did. In the Fedora test VM the first
// install of one boot did; installs later in a boot did not.
//
// It leaves the VM signed out with the zram tool and swap as the clean image
// has them: on Ubuntu no zram tool and no swap (memory.spec.ts needs a VM
// without swap), with the apt timers stopped until the next boot; on Fedora
// zram-generator with its default zram0 swapping.

const INSTALL_ROUTE = "system.install_capability";
// PackageKit's refresh of a cold cache is bounded at two minutes
// (docs/capabilities.md), and one small package downloads and installs in
// seconds; five minutes leaves room for a slow mirror.
const INSTALL_TIMEOUT = 300_000;
const ZRAM_SWAPS = "grep '^/dev/zram' /proc/swaps || true";
// What LinuxIO looks for (memory.CheckZramAvailability): zram-tools'
// zramswap and systemd's zram-generator.
const ZRAM_TOOLS =
  "ls /usr/sbin/zramswap /usr/lib/systemd/system-generators/zram-generator 2>/dev/null || true";

interface ZramTool {
  /** The package Install puts in on this distribution. */
  package: string;
  /** The provider the memory status names once it is installed. */
  provider: string;
  /** A command that succeeds when the package is installed. */
  installed: string;
  /** Run before the removal. */
  prepare?: () => Promise<void>;
  /** A root script that takes the tool out with no zram left swapping. */
  remove: string;
  /** A root script that leaves the tool and zram as the clean image has them. */
  restore: string;
}

// Ubuntu's clean image has no zram tool, so the same purge takes it out
// before and puts the VM back after. The package's prerm stops
// zramswap.service, which takes zram0 down. PackageKit may still hold
// dpkg's lock for a moment after its install ends.
const UBUNTU_PURGE = String.raw`set -euo pipefail
if dpkg -s zram-tools >/dev/null 2>&1; then
	DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 purge -y -qq zram-tools >/dev/null
fi
if grep -q '^/dev/zram' /proc/swaps; then
	echo "zram still swaps with zram-tools purged:" >&2
	cat /proc/swaps >&2
	exit 1
fi
`;

// Fedora's clean image has zram-generator and zram-generator-defaults, whose
// vendor file swaps on zram0 from boot. It is switched off the way Fedora
// documents before the packages go, since while a configuration names zram0
// systemd starts it again as soon as it is reset (linuxio-vm.sh,
// zram_default_off): an empty /etc/systemd/zram-generator.conf, a reload, the
// swap stopped before its setup service. Removing zram-generator removes the
// defaults too. A device left set up is reset through sysfs, which fails
// while udev still holds it after the swapoff.
const FEDORA_REMOVE = String.raw`set -euo pipefail
zram0_set_up() { [[ -e /sys/block/zram0/disksize && $(</sys/block/zram0/disksize) != 0 ]]; }
if rpm -q zram-generator >/dev/null 2>&1; then
	: >/etc/systemd/zram-generator.conf
	systemctl daemon-reload
	if grep -q '^/dev/zram0[[:space:]]' /proc/swaps; then systemctl stop dev-zram0.swap; fi
	systemctl stop systemd-zram-setup@zram0.service
	dnf remove -y -q zram-generator >/dev/null
	rm -f /etc/systemd/zram-generator.conf
	systemctl daemon-reload
fi
for _ in {1..20}; do
	zram0_set_up || break
	echo 1 >/sys/block/zram0/reset 2>/dev/null || sleep 0.5
done
if zram0_set_up || grep -q '^/dev/zram' /proc/swaps; then
	echo "zram0 is still set up after zram-generator was removed:" >&2
	cat /proc/swaps >&2
	exit 1
fi
`;

// Install puts zram-generator in without zram-generator-defaults, so the
// defaults go back in (bringing zram-generator along if the install did not
// get that far) and zram0 is started from the vendor file, as at boot. A
// removal cut after it emptied /etc/systemd/zram-generator.conf leaves that
// file overriding the vendor one, so it goes first, as the rig's default_zram
// does.
const FEDORA_RESTORE = String.raw`set -euo pipefail
zram0_set_up() { [[ -e /sys/block/zram0/disksize && $(</sys/block/zram0/disksize) != 0 ]]; }
rpm -q zram-generator-defaults >/dev/null 2>&1 || dnf install -y -q zram-generator-defaults >/dev/null
rm -f /etc/systemd/zram-generator.conf
systemctl daemon-reload
if ! grep -q '^/dev/zram0[[:space:]]' /proc/swaps; then
	systemctl stop systemd-zram-setup@zram0.service
	for _ in {1..20}; do
		zram0_set_up || break
		echo 1 >/sys/block/zram0/reset 2>/dev/null || sleep 0.5
	done
	systemctl start dev-zram0.swap
fi
for _ in {1..60}; do
	if grep -q '^/dev/zram0[[:space:]]' /proc/swaps; then exit 0; fi
	sleep 1
done
echo "zram0 did not start from /usr/lib/systemd/zram-generator.conf" >&2
exit 1
`;

const TOOLS: Record<string, ZramTool> = {
  ubuntu: {
    package: "zram-tools",
    provider: "zram-tools",
    installed:
      "dpkg-query -W -f '${db:Status-Status}' zram-tools 2>/dev/null | grep -qx installed",
    // An apt run of the timers would hold dpkg's lock under the install.
    prepare: stopAptRuns,
    remove: UBUNTU_PURGE,
    restore: UBUNTU_PURGE,
  },
  fedora: {
    package: "zram-generator",
    provider: "zram-generator",
    installed: "rpm -q zram-generator >/dev/null 2>&1",
    remove: FEDORA_REMOVE,
    restore: FEDORA_RESTORE,
  },
};

const tool = TOOLS[vmDistro];
if (!tool) throw new Error(`no zram tool known for distribution ${vmDistro}`);

// The job IDs from before Install was clicked, for the cleanup to find the
// install's job.
let jobsBefore: Set<string> | undefined;

const packageInstalled = async () =>
  (await vmSsh(`${tool.installed} && echo yes || echo no`)).trim() === "yes";

test.beforeAll(async () => {
  // stopAptRuns waits up to ten minutes for an apt run in progress; the
  // removal takes seconds, or a minute where dnf loads its repositories.
  test.setTimeout(720_000);
  await tool.prepare?.();
  await vmSsh("sudo bash -s", tool.remove);
  expect(await vmSsh(ZRAM_TOOLS), "zram tools installed").toBe("");
  expect(await vmSsh(ZRAM_SWAPS), "zram swapping").toBe("");
});

test.afterEach(async ({ page }) => signOutIfSignedIn(page));

test.afterAll(async () => {
  // A test that failed mid-install leaves the job running in the job host,
  // and a package it put in after the restore would undo the restore: on
  // Ubuntu zram-tools would leave zram swapping for memory.spec.ts. The wait
  // is up to four minutes, the bound of PackageKit's refresh and then some;
  // dnf may fetch its repositories' metadata before it installs.
  test.setTimeout(540_000);
  const before = jobsBefore;
  if (before) {
    await expect
      .poll(async () => (await newJob(INSTALL_ROUTE, before))?.state, {
        intervals: [2_000],
        message: "the install job still running",
        timeout: 240_000,
      })
      .not.toBe("running");
  }
  await vmSsh("sudo bash -s", tool.restore);
});

test("installs zram from Capabilities and the Memory overflow dialog names the tool", async ({
  page,
}) => {
  test.setTimeout(INSTALL_TIMEOUT + 120_000);
  await signIn(page);

  // Without a zram tool the dialog's zram row offers Capabilities instead of
  // a choice. A host with no swap at all, as both VMs are by now, offers it
  // in the status line above the rows too.
  await page.goto("/?dialog=memory-overflow");
  const memoryDialog = page.getByRole("dialog", { name: "Memory overflow" });
  const capabilitiesLinks = memoryDialog.getByRole("link", {
    name: "Open Capabilities to install zram",
  });
  await memoryDialog
    .getByRole("radiogroup", { name: "When memory runs out" })
    .getByRole("link", { name: "Open Capabilities to install zram" })
    .click();
  await expect(page).toHaveURL(/\/settings\?tab=capabilities/);

  const row = page
    .locator(".capability-manager__row")
    .filter({ has: page.getByRole("heading", { name: "zram", exact: true }) });
  await expect(row.getByText("Unavailable", { exact: true })).toBeVisible();

  const before = await jobIds();
  jobsBefore = before;
  const started = Date.now();
  await row.getByRole("button", { name: "Install zram" }).click();
  const install = page.getByRole("dialog", { name: /^Installing zram/ });
  await expect(install).toBeVisible();

  // Once the job ends the caption under the title says how it ended. The
  // messages on the way are logged: one is "Refreshing the package lists"
  // where the install refreshed PackageKit's cache.
  const outcome = install.getByText(/^Capability installation/);
  const message = install.locator(".capability-install-dialog__phase + *");
  const messages: string[] = [];
  await expect
    .poll(
      async () => {
        const text = await message
          .innerText({ timeout: 1_000 })
          .catch(() => "");
        if (text && text !== messages.at(-1)) messages.push(text);
        return outcome.innerText({ timeout: 1_000 }).catch(() => "");
      },
      {
        intervals: [500],
        message: "the install's outcome",
        timeout: INSTALL_TIMEOUT,
      },
    )
    .toMatch(/^Capability installation/);
  console.log(
    `install ended after ${Math.round((Date.now() - started) / 1000)} s; messages: ${messages.join(" | ")}`,
  );
  const shown = await message.innerText({ timeout: 1_000 }).catch(() => "");
  await expect(outcome, shown).toHaveText("Capability installation completed");
  await expect(
    install.getByText("✓ zram installed", { exact: true }),
  ).toBeVisible();
  // The job host's record says the same.
  await waitForJob(INSTALL_ROUTE, before, "completed", {
    timeout: 30_000,
  });

  await install.getByRole("button", { name: "Close", exact: true }).click();
  await expect(install).toBeHidden();
  await expect(row.getByText("Available", { exact: true })).toBeVisible();
  await expect(row.getByRole("button", { name: "Install zram" })).toBeHidden();
  expect(await packageInstalled(), `${tool.package} installed`).toBe(true);
  console.log(
    `zram swapping after the install: ${(await vmSsh(ZRAM_SWAPS)).trim() || "none"}`,
  );

  // The row links back to the dialog, whose zram row now names the tool that
  // sets zram up, read from the host by the memory status.
  await row.getByRole("link", { name: "Open Memory overflow" }).click();
  await expect(memoryDialog).toBeVisible();
  await expect(
    memoryDialog.getByText(`zram (${tool.provider})`, { exact: true }),
  ).toBeVisible({ timeout: 15_000 });
  await expect(capabilitiesLinks).toHaveCount(0);
});
