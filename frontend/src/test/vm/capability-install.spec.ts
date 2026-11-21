import { expect, test } from "@playwright/test";

import {
  jobIds,
  jobRecord,
  type JobRecord,
  newJob,
  openTray,
  signIn,
  signOutIfSignedIn,
  stopAptRuns,
  trayJob,
  vmDistro,
  vmSsh,
  waitForJob,
} from "./vm";

// Install from Capabilities, the way an admin gets zram: without a zram tool
// the Memory overflow dialog links to Capabilities, Install puts the
// distribution's package in through PackageKit as a job and leaves zram off,
// and the dialog then names that tool as the zram provider
// (docs/capabilities.md, "zram"). On Ubuntu zram-tools' package would start
// zram as it installs; the install must have kept that start from happening
// and left the service disabled. It is
// written for Ubuntu and, with DISTRO=fedora, Fedora; only the package, how
// it is taken out and put back, and the provider differ. On Fedora it runs
// with SELinux enforcing, against LinuxIO as its package installs it
// (docs/process-systemd-architecture.md, "SELinux").
//
// Before the test the tool is taken out with the distribution's package
// manager, with no zram left swapping, as on a host that never had one. The
// spec does not refresh PackageKit's cache: the install refreshes it once by
// itself when it does not find the package (docs/capabilities.md), and the
// messages the spec logs show whether it did. In the Fedora test VM some
// installs did, mostly the first of a boot.
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
  /**
   * The service the package enables and starts as it installs, which the
   * install must keep from starting and leave disabled; none where the
   * package starts nothing.
   */
  startedService?: string;
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
// dpkg's lock for a moment after its install ends. A hold of the install's
// that a killed job host left in /run would keep zramswap.service from
// starting for the specs after this one, so it goes too.
const UBUNTU_PURGE = String.raw`set -euo pipefail
if dpkg -s zram-tools >/dev/null 2>&1; then
	DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 purge -y -qq zram-tools >/dev/null
fi
hold=/run/systemd/system/zramswap.service.d
if [[ -e $hold/90-linuxio-memory.conf ]]; then
	rm -f "$hold/90-linuxio-memory.conf"
	rmdir "$hold" 2>/dev/null || true
	systemctl daemon-reload
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
    startedService: "zramswap.service",
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
  // The VM's clock, for the journal of the package's service.
  const installedSince = (await vmSsh("date +%s")).trim();
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
  const zramAfter = (await vmSsh(ZRAM_SWAPS)).trim();
  console.log(`zram swapping after the install: ${zramAfter || "none"}`);
  if (tool.startedService) {
    // The install held the service while the package went in, so its start
    // was skipped and it never ran, and the package's start at boot was
    // taken back.
    const unit = tool.startedService;
    const state = (
      await vmSsh(
        `echo "$(systemctl is-enabled ${unit} || true) $(systemctl is-active ${unit} || true)"`,
      )
    ).trim();
    const journal = (
      await vmSsh(
        `sudo journalctl --no-pager -o cat -b -u ${unit} --since @${installedSince}`,
      )
    ).trim();
    console.log(`${unit} after the install: ${state}; its journal: ${journal}`);
    expect(zramAfter, "zram swapping after the install").toBe("");
    expect(state, `${unit} at boot and now`).toBe("disabled inactive");
    // The package did ask for the start, and systemd skipped it.
    expect(journal, `${unit}'s skipped start`).toContain(
      "unmet condition check ConditionPathExists=!/",
    );
    expect(journal, `${unit} started during the install`).not.toMatch(
      /Starting|Started|Finished/,
    );
  }

  // The row links back to the dialog, whose zram row now names the tool that
  // sets zram up, read from the host by the memory status.
  await row.getByRole("link", { name: "Open Memory overflow" }).click();
  await expect(memoryDialog).toBeVisible();
  await expect(
    memoryDialog.getByText(`zram (${tool.provider})`, { exact: true }),
  ).toBeVisible({ timeout: 15_000 });
  await expect(capabilitiesLinks).toHaveCount(0);
});

// A cancel while dpkg runs (docs/capabilities.md, "Install Flow"): PackageKit
// says the transaction cannot be cancelled then (AllowCancel false, which
// apt's backend sets just before dpkg), so LinuxIO sends no Cancel, waits for
// the transaction's Finished, and only then lets go of the hold, which
// therefore covers the package's scripts. An apt drop-in makes apt sleep
// before it runs dpkg (DPkg::Pre-Invoke), holding the transaction in that
// phase long enough to cancel in it. The sleep's own duration names it in the
// process list.
const SLOW_DPKG = "/etc/apt/apt.conf.d/99linuxio-e2e-slow";
const DPKG_HOLD = "sleep 30.5";

/** What the guest shows of a cancelled install, read in this order. */
interface InstallProbe {
  /** The job's state in the job host's record. */
  state: string;
  /** Whether the DPkg::Pre-Invoke sleep still runs. */
  hold: boolean;
  /** PackageKit's install transaction (role 11, install-packages), if listed. */
  tx: string | null;
  allowCancel: boolean | null;
  /** The transaction's PackageKit status; 19 is Cancel. */
  status: number | null;
}

const PROBE = String.raw`set -uo pipefail
state=$(cat /var/lib/linuxio/jobs/0/*.json 2>/dev/null | jq -rs --arg id "$1" '.[] | select(.id == $id) | .state')
hold=false
if pgrep -f '${DPKG_HOLD.replace(".", "[.]")}' >/dev/null; then hold=true; fi
tx=null allow=null status=null
prop() {
	busctl --json=short get-property org.freedesktop.PackageKit "$1" org.freedesktop.PackageKit.Transaction "$2" 2>/dev/null | jq -c .data
}
for path in $(busctl --json=short call org.freedesktop.PackageKit /org/freedesktop/PackageKit org.freedesktop.PackageKit GetTransactionList | jq -r '.data[0][]'); do
	if [[ $(prop "$path" Role) == 11 ]]; then
		tx="\"$path\"" allow=$(prop "$path" AllowCancel) status=$(prop "$path" Status)
		[[ -n $allow ]] || allow=null
		[[ -n $status ]] || status=null
	fi
done
printf '{"state":"%s","hold":%s,"tx":%s,"allowCancel":%s,"status":%s}\n' "$state" "$hold" "$tx" "$allow" "$status"
`;

const probeInstall = async (jobId: string) =>
  JSON.parse(await vmSsh(`sudo bash -s -- ${jobId}`, PROBE)) as InstallProbe;

// PackageKit's apt backend reads apt's configuration once, as packagekitd
// starts (pkgInitConfig in pk_backend_initialize), so the drop-in reaches its
// transactions only once packagekitd has started again, and leaves them only
// the same way. packagekitd is stopped only while it runs no transaction,
// since stopping it kills apt with it; D-Bus starts it on the next call.
async function stopPackageKitWhenIdle() {
  await expect
    .poll(
      async () =>
        Number(
          (
            await vmSsh(
              "sudo busctl --json=short call org.freedesktop.PackageKit /org/freedesktop/PackageKit org.freedesktop.PackageKit GetTransactionList | jq '.data[0] | length'",
            )
          ).trim(),
        ),
      {
        intervals: [2_000],
        message: "PackageKit transactions running",
        timeout: 240_000,
      },
    )
    .toBe(0);
  await vmSsh("sudo systemctl stop packagekit.service");
}

test("a cancel while dpkg runs waits for PackageKit's transaction, and the hold covers the package's scripts", async ({
  page,
}) => {
  test.skip(
    vmDistro !== "ubuntu",
    "Ubuntu only: Fedora's dnf5 backend does nothing on a cancel and runs the whole install, and dnf has no DPkg::Pre-Invoke to hold the transaction in its install phase",
  );
  test.setTimeout(INSTALL_TIMEOUT + 480_000);
  // The test before this one put zram-tools in.
  await vmSsh("sudo bash -s", tool.remove);
  expect(await vmSsh(ZRAM_TOOLS), "zram tools installed").toBe("");
  try {
    await vmSsh(
      `sudo tee ${SLOW_DPKG} >/dev/null`,
      `DPkg::Pre-Invoke { "${DPKG_HOLD}"; };\n`,
    );
    await stopPackageKitWhenIdle();
    await signIn(page);
    await page.goto("/settings?tab=capabilities");
    const row = page.locator(".capability-manager__row").filter({
      has: page.getByRole("heading", { name: "zram", exact: true }),
    });
    await expect(row.getByText("Unavailable", { exact: true })).toBeVisible({
      timeout: 15_000,
    });

    const before = await jobIds();
    jobsBefore = before;
    const installedSince = Number((await vmSsh("date +%s")).trim());
    await row.getByRole("button", { name: "Install zram" }).click();
    const install = page.getByRole("dialog", { name: /^Installing zram/ });
    await expect(install).toBeVisible();
    // The install goes on with its dialog closed; the tray takes the cancel.
    await install
      .getByRole("button", { name: "Close capability installation dialog" })
      .click();
    await expect(install).toBeHidden();
    const job = await waitForJob(INSTALL_ROUTE, before, "running");

    // dpkg's phase has begun: apt sleeps before it runs dpkg, and PackageKit
    // says the transaction cannot be cancelled now.
    await expect
      .poll(
        async () => {
          const probe = await probeInstall(job.id);
          return { hold: probe.hold, allowCancel: probe.allowCancel };
        },
        {
          intervals: [500],
          message: "apt holding dpkg back, with AllowCancel false",
          timeout: INSTALL_TIMEOUT,
        },
      )
      .toEqual({ hold: true, allowCancel: false });

    const tray = await openTray(page);
    const item = trayJob(tray, "Installing zram");
    await item.getByRole("button", { name: "Cancel task" }).click();
    await expect(item).toContainText("Cancelling…");
    await expect
      .poll(
        async () => {
          const record = await jobRecord(job.id);
          return {
            cancel_requested: record?.cancel_requested,
            state: record?.state,
          };
        },
        { intervals: [500], timeout: 15_000 },
      )
      .toEqual({ cancel_requested: true, state: "running" });

    // Until apt's sleep ends the job runs on, and PackageKit's transaction
    // never reads Cancel (19), the status its Cancel handler and apt's
    // backend set as soon as a Cancel reaches them.
    const samples: InstallProbe[] = [];
    await expect
      .poll(
        async () => {
          const probe = await probeInstall(job.id);
          samples.push(probe);
          return probe.hold;
        },
        {
          intervals: [1_000],
          message: "apt still sleeping before dpkg",
          timeout: 60_000,
        },
      )
      .toBe(false);
    const held = samples.filter((sample) => sample.hold);
    console.log(
      `while apt slept after the cancel: ${held.map((s) => `${s.state}/${s.status}`).join(" ")}`,
    );
    expect(
      held.length,
      "probes while apt slept after the cancel",
    ).toBeGreaterThan(5);
    for (const sample of held) {
      expect(sample.state, "the job while apt sleeps before dpkg").toBe(
        "running",
      );
      expect(sample.status, "PackageKit's transaction status").not.toBe(19);
    }

    // The job ends cancelled, and only once dpkg has put the package in: the
    // cancel stopped nothing in PackageKit.
    const ended = (await waitForJob(INSTALL_ROUTE, before, "canceled", {
      timeout: 120_000,
    })) as JobRecord & { finished_at?: string };
    const dpkgInstalled = Number(
      (
        await vmSsh(
          String.raw`d=$(grep ' status installed zram-tools:' /var/log/dpkg.log | tail -1 | cut -d' ' -f1-2); if [ -n "$d" ]; then date -d "$d" +%s; else echo 0; fi`,
        )
      ).trim(),
    );
    expect(
      dpkgInstalled,
      "dpkg's install of zram-tools in this test",
    ).toBeGreaterThanOrEqual(installedSince);
    const jobEnded = Math.floor(Date.parse(ended.finished_at ?? "") / 1000);
    console.log(
      `dpkg installed zram-tools ${dpkgInstalled - installedSince} s after Install; the job ended ${jobEnded - dpkgInstalled} s after that`,
    );
    expect(jobEnded, "the job's end, after dpkg's").toBeGreaterThanOrEqual(
      dpkgInstalled,
    );
    expect(await packageInstalled(), "zram-tools installed").toBe(true);

    // The hold covered the package's start and was then let go of: the
    // package's start was skipped, its start at boot taken back, no zram
    // swaps, the hold is gone, and dpkg has nothing half done.
    const unit = "zramswap.service";
    expect(
      (await vmSsh(`systemctl is-enabled ${unit} || true`)).trim(),
      `${unit} at boot`,
    ).not.toBe("enabled");
    expect(await vmSsh(ZRAM_SWAPS), "zram swapping").toBe("");
    expect(
      (
        await vmSsh(
          "test -e /run/systemd/system/zramswap.service.d && echo present || echo gone",
        )
      ).trim(),
      "the install's hold",
    ).toBe("gone");
    expect(await vmSsh("sudo dpkg --audit"), "dpkg --audit").toBe("");
    const journal = (
      await vmSsh(
        `sudo journalctl --no-pager -o cat -b -u ${unit} --since @${installedSince}`,
      )
    ).trim();
    expect(journal, `${unit}'s skipped start`).toContain(
      "unmet condition check ConditionPathExists=!/",
    );
    expect(journal, `${unit} started during the install`).not.toMatch(
      /Starting|Started|Finished/,
    );
  } finally {
    // The afterAll's purge, and any apt run after it, would sleep too, and
    // so would PackageKit's until packagekitd starts again. A failure here
    // is logged, so it does not hide the test's own.
    await vmSsh(`sudo rm -f ${SLOW_DPKG}`);
    await stopPackageKitWhenIdle().catch((error: unknown) =>
      console.log(
        `packagekitd not restarted after the test, so it may still read the drop-in: ${String(error)}`,
      ),
    );
  }
});
