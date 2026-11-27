import { randomBytes } from "node:crypto";
import path from "node:path";

import { expect, test, type Locator, type Page } from "@playwright/test";

import {
  activeSince,
  bridgeCall,
  checkoutVersion,
  createSchedule,
  fillSchedule,
  fixture,
  hostSha256,
  INSTALL_FROM_REPO,
  installedVersion,
  jobIds,
  jobRecord,
  jobRecords,
  type JobRecord,
  type Login,
  mainPid,
  MiB,
  newJob,
  nextVersion,
  noSessions,
  packageKitVersion,
  plainBuild,
  publish,
  rig,
  runNow,
  SCRUB_LABEL,
  type ScheduledRecord,
  scheduledOutput,
  scrubRunning,
  signIn,
  signOut,
  signOutIfSignedIn,
  startScrub,
  stopAptRuns,
  throttle,
  uniqueName,
  unitOf,
  updateBuild,
  vmDistro,
  vmSsh,
  waitForIdle,
  waitForJob,
  waitForRun,
} from "./vm";

// docs/packaging.md (LinuxIO's own update) and docs/jobs.md (Updates):
// LinuxIO's own update is the package `linuxio` on the Updates page. The VM
// installs package A from its signed repository (linuxio-vm.sh repo-publish,
// then apt or dnf, as an administrator does), long jobs run in A's job
// hosts, and package B is published. A sudo session's banner names B and opens the Updates page,
// where the administrator updates it: `packages.update`, a job in the root job
// host, PackageKit verifies the repository and the package, and the package's
// own step restarts linuxio.target. The job hosts are outside the target and
// run on: the update job runs to its end and is recorded, and the jobs that
// were running keep the new build waiting.
//
// B is the update build (LINUXIO_VM_UPDATE_BUILD, `make vm-update-build`):
// this tree with a version of its own, which `linuxio --version` reports. It
// keeps jobs.Protocol, as most releases do, so A's hosts refuse B's bridge
// because the update replaced their binary, and it waits for their jobs
// (docs/jobs.md, Testing). On Fedora B also carries a
// changed SELinux module: the rig appends a comment to the copy of
// linuxio.cil B is assembled from. A is the plain build (LINUXIO_VM_BUILD_DIR)
// at this checkout's package version; B is that version with +e2e1. Without
// the two builds the tests are skipped.
//
// The tests are one serial group sharing one update, because each update costs
// minutes (two packages assembled and installed, then the relay's backoff
// once the older host exits): the first test sets it up, the others read
// the wait from a different side, the fifth ends the jobs, and on Fedora the
// last checks the SELinux module across a reboot. The jobs are two in the root
// host (a throttled btrfs scrub and a throttled copy) and one in the host of a
// user without sudo (a throttled copy). The copies read an ext4 on vdd that
// the VM rig throttles, so lifting the throttle ends both copies and leaves
// the scrub, which ends by raising its own speed cap.
//
// It runs alone, on a freshly reset VM with no LinuxIO: the file is the
// Playwright project release-update, which `make test-vm-browser` runs after
// the other files, on its first VM reverted to clean. By hand:
//   make vm-reset vm-update-build
//   LINUXIO_VM_UPDATE_BUILD=.cache/vm-update-build/build make test-vm-browser-rerun VMS=1 PLAYWRIGHT_ARGS=--project=release-update
// (DISTRO=fedora on each for Fedora). The file needs vdd and, on Ubuntu, vde
// to hold nothing and changes the VM: it leaves B installed from the VM's
// repository, the apt timers stopped, the e2e pool on vdb and vdc, a user
// without sudo, an ext4 on vdd mounted at /mnt/e2e-slow, on Ubuntu an ext4 on
// vde, and a schedule. Run `make vm-reset` (and `make vm-install` for the
// other files) before anything else.

const COPY = "filebrowser.copy_batch";
const PACKAGE_UPDATE = "packages.update";
/** A user without sudo: its job host is the older host of the second check. */
const PLAIN_USER = "e2e-plain";
const SLOW_DISK = "vdd";
const SLOW_MOUNT = "/mnt/e2e-slow";
/**
 * The cap of the disk both copies read and write, in bytes per second. Each
 * copy moves 2 GiB through it, so they outlast the checks by far.
 */
const SLOW_RATE = 2 * MiB;
/**
 * The scrub's per-disk cap: half a MiB a second takes over half an hour on
 * the 1 GiB the pool holds, longer than the two publishes, the two updates
 * that do not go through, the update and the checks before the fifth test
 * lifts it.
 */
const SCRUB_RATE = MiB / 2;
/** What B's SELinux module carries, and A's does not (Fedora). */
const MODULE_NOTE = `linuxio e2e update ${randomBytes(6).toString("hex")}`;
/** The suffix vm-update-build gives the update build's version. */
const UPDATE_BUILD_VERSION = /\S+\+update\b/;

const vmUser = () => process.env.LINUXIO_VM_USER ?? "";

/** What the first test sets up and the others read. */
interface Waiting {
  /** The uuid of the test pool, whose scrub the root host runs. */
  uuid: string;
  scrub: JobRecord;
  copy: JobRecord;
  userCopy: JobRecord;
  /** The Updates page's update of the linuxio package. */
  update: JobRecord;
  /** A scheduled run that ended before the update (A's hooks wrote it). */
  run: ScheduledRecord;
  marker: string;
  user: Login;
  uid: number;
  /** A's hosts' main PIDs. */
  rootPid: string;
  userPid: string;
  /** The SHA-256 of linuxio-bridge in A and in B. */
  oldSha: string;
  newSha: string;
  /** B's package version. */
  version: string;
}
let waiting: Waiting | undefined;

const current = () => {
  if (!waiting) throw new Error("the first test of the group did not finish");
  return waiting;
};

// Every click and locator check fails in seconds: the tests' own long waits
// (the relay's backoff, a scrub's end) carry their own timeouts.
test.use({ actionTimeout: 15_000 });
const ui = expect.configure({ timeout: 15_000 });

test.afterEach(({ page }) => signOutIfSignedIn(page));

const hostPid = (uid: number) => mainPid(`linuxio-jobs@${uid}.service`);

/** The SHA-256 of the binary uid's job host runs (a replaced one still counts). */
async function hostSha(uid: number) {
  const pid = await hostPid(uid);
  expect(pid, `the job host of uid ${uid} runs`).not.toBe("0");
  return (await vmSsh(`sudo sha256sum /proc/${pid}/exe`)).split(" ")[0];
}

/** Raises or lifts the scrub's per-disk cap (0 lifts it). */
const scrubSpeed = (uuid: string, bytesPerSecond: number) =>
  vmSsh(
    `for f in /sys/fs/btrfs/${uuid}/devinfo/*/scrub_speed_max; do echo ${bytesPerSecond} | sudo tee $f >/dev/null; done`,
  );

/** An ext4 on vdd with a 1 GiB file for each of the root and the plain user. */
async function prepareSlowDisk() {
  const used = await vmSsh(
    `lsblk -nro FSTYPE,MOUNTPOINT /dev/${SLOW_DISK} | tr -d ' \\n'`,
  );
  expect(used, `/dev/${SLOW_DISK} must hold nothing`).toBe("");
  const dd = (as: string, dir: string) =>
    `${as} dd if=/dev/zero of=${SLOW_MOUNT}/${dir}/big.bin bs=1M count=1024 status=none`;
  await vmSsh(
    `sudo mkfs.ext4 -q -F /dev/${SLOW_DISK} && sudo mkdir -p ${SLOW_MOUNT} && sudo mount /dev/${SLOW_DISK} ${SLOW_MOUNT} && ` +
      `sudo mkdir -p ${SLOW_MOUNT}/root/dst ${SLOW_MOUNT}/plain/dst && sudo chown -R ${PLAIN_USER}: ${SLOW_MOUNT}/plain && ` +
      `${dd("sudo", "root")} && ${dd(`sudo -u ${PLAIN_USER}`, "plain")} && sync && ` +
      "echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null",
  );
}

/** Copies big.bin of dir into its dst folder in the file browser. */
async function pasteCopy(page: Page, dir: string) {
  await page.goto(`/filebrowser${dir}`);
  await page.getByText("big.bin", { exact: true }).click();
  await page.keyboard.press("Control+c");
  await page.getByText("dst", { exact: true }).dblclick();
  await ui(page.getByText("This folder is empty.")).toBeVisible();
  await page.keyboard.press("Control+v");
}

const banner = (page: Page) =>
  page.getByRole("status").filter({ hasText: "LinuxIO was updated." });

/** The banner that names a LinuxIO update PackageKit lists (AppUpdateBanner). */
const available = (page: Page) =>
  page.getByRole("status").filter({ hasText: /LinuxIO \S+ is available\./ });

/** The tasks.host answer of the signed-in session. */
interface HostStatus {
  update_pending: boolean;
  running: number;
  jobs?: { id: string }[];
}
const taskHost = async (page: Page) => {
  const result = await bridgeCall(page, "tasks.host", {});
  expect(result.status, JSON.stringify(result)).toBe("ok");
  return result.data as HostStatus;
};

/** The ids of the ended jobs tasks.history lists for the signed-in session. */
const historyIds = async (page: Page) => {
  const result = await bridgeCall(page, "tasks.history", { limit: 500 });
  expect(result.status, JSON.stringify(result)).toBe("ok");
  const { entries } = result.data as { entries: { task: { id: string } }[] };
  return entries.map((entry) => entry.task.id);
};

/**
 * A new job is refused with the count; the toast drops the `update_pending: `
 * of the error.
 */
async function refuse(page: Page, toast: string) {
  const known = await jobIds();
  const dialog = await fillSchedule(page, uniqueName("refused"), "true");
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await ui(page.getByText(toast, { exact: true })).toBeVisible({
    timeout: 15_000,
  });
  await page.keyboard.press("Escape");
  expect(await jobIds()).toEqual(known);
}

/** Uploads files through the context menu's Upload dialog. */
async function upload(page: Page, rightClick: Locator, files: string[]) {
  await rightClick.click({ button: "right" });
  await page.getByRole("menuitem", { name: "Upload" }).click();
  const dialog = page.getByRole("dialog", { name: "Upload files or folders" });
  const chooser = page.waitForEvent("filechooser", { timeout: 15_000 });
  await dialog.getByRole("button", { name: "Select files" }).click();
  await (await chooser).setFiles(files);
  await dialog.getByRole("button", { name: "Upload", exact: true }).click();
  await ui(dialog).toBeHidden();
}

/** Where the VM's repository holds LinuxIO version v (repo-apt.sh, repo-rpm.sh). */
const packageFile = (v: string) =>
  vmDistro === "fedora"
    ? `/srv/linuxio-repo/rpm/stable/x86_64/linuxio-${v}-1.x86_64.rpm`
    : `/srv/linuxio-repo/apt/pool/main/l/linuxio/linuxio_${v}_amd64.deb`;

/**
 * The disk the cancelled update reads the package from, at a megabyte a
 * second: half a minute for the package's 30 MB.
 */
const PACKAGE_DISK = "vde";
const PACKAGE_MOUNT = "/mnt/e2e-package";
const PACKAGE_RATE = MiB;

/**
 * The status and AllowCancel of PackageKit's update transaction (role 22,
 * update-packages), nulls while it lists none. With apt the status goes
 * from loading the cache (27) to Download (8) while apt reads the package,
 * and to Install (9) when dpkg starts, where AllowCancel turns false.
 */
const UPDATE_TRANSACTION = String.raw`set -uo pipefail
prop() {
	busctl --json=short get-property org.freedesktop.PackageKit "$1" org.freedesktop.PackageKit.Transaction "$2" 2>/dev/null | jq -c .data
}
status=null allow=null
for path in $(busctl --json=short call org.freedesktop.PackageKit /org/freedesktop/PackageKit org.freedesktop.PackageKit GetTransactionList | jq -r '.data[0][]'); do
	if [[ $(prop "$path" Role) == 22 ]]; then
		status=$(prop "$path" Status) allow=$(prop "$path" AllowCancel)
		[[ -n $status ]] || status=null
		[[ -n $allow ]] || allow=null
	fi
done
printf '{"status":%s,"allowCancel":%s}\n' "$status" "$allow"
`;
const PK_STATUS_DOWNLOAD = 8;

const updateTransaction = async () =>
  JSON.parse(await vmSsh("sudo bash -s", UPDATE_TRANSACTION)) as {
    status: number | null;
    allowCancel: boolean | null;
  };

/** What an installed update changes, and a restart of LinuxIO with it. */
const installedAndRunning = async () => ({
  installed: await installedVersion(),
  version: (await vmSsh("linuxio --version")).trim(),
  targetSince: await activeSince("linuxio.target"),
  webserver: await mainPid("linuxio-webserver.service"),
  rootHost: await mainPid("linuxio-jobs@0.service"),
});

/** The SHA-256 of what the web server's process runs. */
const webserverSha = async () => {
  const pid = await mainPid("linuxio-webserver.service");
  expect(pid, "the web server runs").not.toBe("0");
  return (await vmSsh(`sudo sha256sum /proc/${pid}/exe`)).split(" ")[0];
};

test.describe
  .serial("an update from package A to package B on the Updates page", () => {
  test.skip(
    updateBuild() === "" || plainBuild() === "",
    "needs LINUXIO_VM_BUILD_DIR and LINUXIO_VM_UPDATE_BUILD: make test-vm-browser passes both, make vm-update-build builds the second (docs/jobs.md, Testing)",
  );

  test.afterAll(async () => {
    // Nothing may keep the disk or the scrub slow for the next file.
    try {
      await throttle(0, [SLOW_DISK, PACKAGE_DISK]);
      if (waiting) await scrubSpeed(waiting.uuid, 0);
    } catch (error) {
      console.warn(`could not lift the caps: ${String(error)}`);
    }
  });

  test("the banner names B, the update runs as a job across the restart, and two root jobs keep B waiting", async ({
    page,
  }) => {
    // The bounds: an apt run 600 s; A assembled and published 180 s and
    // installed 180 s; the pool on a fresh VM 120 s; the files on vdd 60 s;
    // the scrub, the schedule and the two copies 180 s; B assembled and
    // published 180 s; the refresh and the banner 240 s; the failed update
    // 360 s and, on Ubuntu, the cancelled one 420 s; the update 600 s; the
    // reads 120 s: some 3240 s.
    test.setTimeout(3_480_000);
    await stopAptRuns();
    expect(await installedVersion(), "a VM reverted to clean").toBe("");

    // The user without sudo, whose job host is the second older host. The VM
    // is reset before each run; where the user exists, it gets a new password.
    const user = {
      username: PLAIN_USER,
      password: randomBytes(18).toString("base64url"),
    };
    await vmSsh(
      `id -u ${PLAIN_USER} >/dev/null 2>&1 || sudo useradd -m -s /bin/bash ${PLAIN_USER}`,
    );
    await vmSsh("sudo chpasswd", `${user.username}:${user.password}\n`);
    expect(await vmSsh(`id -nG ${PLAIN_USER}`)).not.toMatch(/\b(sudo|wheel)\b/);
    const uid = Number((await vmSsh(`id -u ${PLAIN_USER}`)).trim());

    // A, installed from the VM's signed repository.
    const versionA = await checkoutVersion();
    const version = nextVersion(versionA);
    await publish(plainBuild(), versionA);
    console.log(
      await vmSsh(`set -o pipefail; ${INSTALL_FROM_REPO} 2>&1 | tail -n 5`),
    );
    expect(await installedVersion()).toBe(versionA);
    expect(
      (await vmSsh("systemctl is-active linuxio.target || true")).trim(),
    ).toBe("active");
    expect(await vmSsh("linuxio --version")).not.toMatch(UPDATE_BUILD_VERSION);
    const oldSha = await hostSha256(path.join(plainBuild(), "linuxio-bridge"));
    const newSha = await hostSha256(path.join(updateBuild(), "linuxio-bridge"));
    expect(newSha, "the update build is not the plain one").not.toBe(oldSha);
    await prepareSlowDisk();

    // The root host's jobs, started in A's UI: a capped scrub and a copy
    // that reads the throttled disk.
    const beforeScrub = await jobIds();
    const { uuid, job: scrub } = await startScrub(
      page,
      beforeScrub,
      SCRUB_RATE,
    );
    expect(await hostSha(0), "the root host runs A").toBe(oldSha);

    // A scheduled run that ends before the update: A's hooks write its
    // record and its saved output, which B reads afterwards.
    const marker = `update-marker-${randomBytes(6).toString("hex")}`;
    const name = uniqueName("before the update");
    const scheduleId = await createSchedule(page, name, `echo ${marker}`);
    await runNow(page, name);
    const run = await waitForRun(scheduleId, "completed", { anyReason: true });
    await waitForIdle(unitOf(scheduleId));
    expect((await scheduledOutput(run.id)).toString()).toContain(marker);

    await throttle(SLOW_RATE, [SLOW_DISK]);
    const beforeCopy = await jobIds();
    await pasteCopy(page, `${SLOW_MOUNT}/root`);
    const copy = await waitForJob(COPY, beforeCopy, "running");
    await signOut(page);

    // The plain user's copy runs in that user's host.
    const userRecords = async () => jobRecords(COPY, uid);
    const beforeUser = new Set((await userRecords()).map((r) => r.id));
    await signIn(page, user);
    await pasteCopy(page, `${SLOW_MOUNT}/plain`);
    let userCopy: JobRecord | undefined;
    await expect
      .poll(
        async () => {
          userCopy = (await userRecords()).find((r) => !beforeUser.has(r.id));
          return userCopy?.state;
        },
        { intervals: [2_000], message: `${COPY} of ${PLAIN_USER}` },
      )
      .toBe("running");
    expect(await hostSha(uid), "the user's host runs A").toBe(oldSha);
    await signOut(page);
    await noSessions();

    // B, published. PackageKit lists it once its lists are refreshed, which
    // control.version never does by itself: the Updates page's refresh.
    await publish(updateBuild(), version, MODULE_NOTE);
    await signIn(page);
    await page.goto("/updates");
    await page.getByRole("button", { name: "Refresh Sources" }).click();
    await ui(page.getByText("Refreshing", { exact: true })).toBeHidden({
      timeout: 240_000,
    });
    // The banner names B as PackageKit does, and its button opens the
    // Updates page.
    await page.goto("/");
    const offer = available(page);
    await ui(offer).toContainText(
      `LinuxIO ${packageKitVersion(version)} is available.`,
      { timeout: 30_000 },
    );
    await offer.getByText("Open Updates", { exact: true }).click();
    await ui(page).toHaveURL(/\/updates$/);
    // Opening the card shows it with its Update action beside the changelog,
    // and keeps the open update in the address.
    await page
      .getByRole("button", { name: "Open update linuxio details", exact: true })
      .click();
    await ui(page).toHaveURL(/\/updates\?package=/);
    const focusedUpdates = page.url();
    // The card names the package with its summary below; the details
    // panel's header names the version on offer.
    await ui(
      page.getByText(packageKitVersion(version), { exact: true }),
    ).toBeVisible();
    const card = page
      .locator("div")
      .filter({ has: page.getByText("linuxio", { exact: true }) })
      .filter({
        has: page.getByRole("button", { name: "Update", exact: true }),
      })
      .last();
    const updateButton = card.getByRole("button", {
      name: "Update",
      exact: true,
    });
    // docs/packaging.md (Changelog): B's changelog, the fixture release
    // notes the VM packages carry, read by PackageKit from the repository:
    // the apt Changelogs field on Ubuntu, updateinfo on Fedora.
    const changelog = page.getByText(
      "updates: show the package's changes on the Updates page (abc1234)",
    );
    await ui(changelog).toBeVisible({ timeout: 60_000 });
    // As written: the spec's %changelog takes the text without expanding it.
    await ui(changelog).toContainText(
      "memory: keep 50% of %{name} free (def5678)",
    );

    // An update that does not go through leaves A installed and running, and
    // this session signed in: nothing but the package's own step restarts
    // LinuxIO, and the transaction ended before that step.
    const file = packageFile(version);
    const before = await installedAndRunning();
    expect(before.installed).toBe(versionA);
    /** Clicks Update and answers with the job once it has ended. */
    const endedUpdate = async (act?: () => Promise<void>) => {
      const known = await jobIds();
      await updateButton.click();
      await act?.();
      let job: JobRecord | undefined;
      await expect
        .poll(
          async () => {
            job = await newJob(PACKAGE_UPDATE, known);
            return job?.state;
          },
          {
            intervals: [2_000],
            message: "the package update job ends",
            timeout: 300_000,
          },
        )
        .toMatch(/^(completed|failed|canceled|interrupted)$/);
      return job!;
    };
    const stillA = async (what: string) => {
      expect(await installedAndRunning(), what).toEqual(before);
      // The session lives, and the page offers B again.
      await page.goto(focusedUpdates);
      await ui(updateButton).toBeVisible({ timeout: 60_000 });
    };

    const seconds = (since: number) => Math.round((Date.now() - since) / 1000);
    await test.step("a failed update leaves A installed and running", async () => {
      const began = Date.now();
      // The package no longer is what the repository's signed index says.
      await vmSsh(
        `set -e; test -f ${file}; mv ${file} ${file}.good; head -c 4096 ${file}.good > ${file}`,
      );
      try {
        const failed = await endedUpdate();
        expect(failed, JSON.stringify(failed)).toMatchObject({
          state: "failed",
        });
        await ui(
          page.getByRole("button", { name: "Clear update error" }),
        ).toBeVisible();
        await stillA("after a failed update");
      } finally {
        // Only what the step changed, so that its own error is the one seen.
        await vmSsh(`[ ! -e ${file}.good ] || mv -f ${file}.good ${file}`);
      }
      console.log(`the failed update took ${seconds(began)} s`);
    });

    // Fedora's dnf5 backend does nothing on a cancel and installs the update
    // (docs/jobs.md, Known limits), so only apt has a cancelled transaction.
    if (vmDistro === "ubuntu") {
      await test.step("an update cancelled while it downloads leaves A installed and running", async () => {
        const began = Date.now();
        // apt reads the whole package to check it before it hands it to
        // dpkg, and the transaction can be cancelled until dpkg runs. The
        // package is read from a disk of its own, capped only now, so that
        // the read takes about half a minute and the cancel is in no race
        // with dpkg's start; the filesystem is mounted again after the copy,
        // so that the read goes to the disk and not to the page cache.
        const used = await vmSsh(
          `lsblk -nro FSTYPE,MOUNTPOINT /dev/${PACKAGE_DISK} | tr -d ' \\n'`,
        );
        expect(used, `/dev/${PACKAGE_DISK} must hold nothing`).toBe("");
        // What PackageKit's transaction went through and when, in seconds
        // from the click, for a failure's message.
        const seen: string[] = [];
        try {
          await vmSsh(
            `set -e; sudo mkfs.ext4 -q -F /dev/${PACKAGE_DISK}; sudo mkdir -p ${PACKAGE_MOUNT}; sudo mount /dev/${PACKAGE_DISK} ${PACKAGE_MOUNT}; ` +
              `sudo cp ${file} ${PACKAGE_MOUNT}/linuxio-package; sudo umount ${PACKAGE_MOUNT}; sudo mount /dev/${PACKAGE_DISK} ${PACKAGE_MOUNT}; ` +
              `mv ${file} ${file}.good; ln -s ${PACKAGE_MOUNT}/linuxio-package ${file}`,
          );
          await throttle(PACKAGE_RATE, [PACKAGE_DISK]);
          const cancelled = await endedUpdate(async () => {
            const clicked = Date.now();
            try {
              await expect
                .poll(
                  async () => {
                    const tx = await updateTransaction();
                    const now = `${tx.status}/${tx.allowCancel}`;
                    if (seen.at(-1)?.split("@")[0] !== now) {
                      seen.push(`${now}@${seconds(clicked)}`);
                    }
                    return tx;
                  },
                  {
                    intervals: [300],
                    message: "PackageKit downloading the update, cancellable",
                    timeout: 120_000,
                  },
                )
                .toEqual({ status: PK_STATUS_DOWNLOAD, allowCancel: true });
            } finally {
              console.log(
                `the update transaction's status/AllowCancel@seconds: ${seen.join(" ")}`,
              );
            }
            await page.getByRole("button", { name: "Cancel update" }).click();
          });
          expect(cancelled, JSON.stringify(cancelled)).toMatchObject({
            state: "canceled",
          });
          await stillA("after a cancelled update");
        } finally {
          // Only what the step changed, and an unmount that a read still
          // going cannot refuse, so that the step's own error is the one seen.
          await throttle(0, [PACKAGE_DISK]);
          await vmSsh(
            `[ ! -e ${file}.good ] || { rm -f ${file}; mv -f ${file}.good ${file}; }; ` +
              `! mountpoint -q ${PACKAGE_MOUNT} || sudo umount -l ${PACKAGE_MOUNT}`,
          );
        }
        console.log(`the cancelled update took ${seconds(began)} s`);
      });
    }

    // The update, on the Updates page, while the three jobs run.
    expect(await scrubRunning(), "the scrub ended: lower its speed").toBe(true);
    const rootPid = await hostPid(0);
    const userPid = await hostPid(uid);
    const targetSince = await activeSince("linuxio.target");
    const beforeUpdate = await jobIds();
    await updateButton.click();
    // The package restarts linuxio.target, and this session with it; the job
    // runs on in the root host, which is outside the target.
    let update: JobRecord | undefined;
    await expect
      .poll(
        async () => {
          update = await newJob(PACKAGE_UPDATE, beforeUpdate);
          return update?.state;
        },
        {
          intervals: [2_000],
          message: "the package update job ends",
          timeout: 600_000,
        },
      )
      .toMatch(/^(completed|failed|canceled|interrupted)$/);
    expect(update, JSON.stringify(update)).toMatchObject({
      state: "completed",
    });
    expect(await installedVersion(), "the host has B").toBe(version);
    expect(await vmSsh("linuxio --version")).toMatch(UPDATE_BUILD_VERSION);
    expect(
      await activeSince("linuxio.target"),
      "the package restarted linuxio.target",
    ).toBeGreaterThan(targetSince);
    expect(await hostPid(0), "the update left the root host").toBe(rootPid);
    expect(await hostPid(uid), "the update left the user's host").toBe(userPid);
    expect(await hostSha(0), "the root host still runs A").toBe(oldSha);
    for (const id of [scrub.id, copy.id]) {
      expect((await jobRecord(id))?.state).toBe("running");
    }
    waiting = {
      uuid,
      scrub,
      copy,
      userCopy: userCopy!,
      update: update!,
      run,
      marker,
      user,
      uid,
      rootPid,
      userPid,
      oldSha,
      newSha,
      version,
    };

    // Signed in again: B's bridge, and B's web server.
    await signIn(page);
    expect(await webserverSha()).toBe(
      await hostSha256(path.join(updateBuild(), "linuxio-webserver")),
    );
    const waitingBanner = banner(page);
    await ui(waitingBanner).toContainText(
      "LinuxIO was updated. New jobs wait for 2 running jobs to finish.",
      { timeout: 30_000 },
    );
    // PackageKit lists no newer LinuxIO now.
    await ui(available(page)).toHaveCount(0);
    // The older host refused the new bridge's hello because the update
    // replaced its binary, not on the protocol, which the update kept.
    const refused = await vmSsh(
      "sudo journalctl -b -u linuxio-jobs@0.service -o cat --no-pager | grep -F 'job host refused a bridge hello' || true",
    );
    expect(refused).toContain(
      "job host refused a bridge hello: binary replaced",
    );
    expect(refused).not.toMatch(/protocol \d+, want \d+/);

    await waitingBanner
      .getByRole("button", { name: "Show running jobs" })
      .click();
    const listed = waitingBanner.getByRole("listitem");
    await ui(listed).toHaveCount(2);
    await ui(listed.filter({ hasText: SCRUB_LABEL })).toHaveCount(1);
    await ui(listed.filter({ hasText: /Copying/ })).toHaveCount(1);
    for (const item of await listed.all()) {
      await ui(item).toContainText(` · ${vmUser()} · started `);
    }

    // tasks.host names the same two jobs, read from the older host's records.
    const status = await taskHost(page);
    expect(status.update_pending).toBe(true);
    expect(status.running).toBe(2);
    expect(status.jobs?.map((j) => j.id).sort()).toEqual(
      [scrub.id, copy.id].sort(),
    );

    await refuse(page, "LinuxIO update waits for 2 running jobs");
    expect(await hostPid(0), "the older host refused it").toBe(rootPid);
  });

  test("a job of a user's own host keeps the update waiting for that host", async ({
    page,
  }) => {
    test.setTimeout(180_000);
    const w = current();
    await signIn(page, w.user);
    const waitingBanner = banner(page);
    await ui(waitingBanner).toContainText(
      "LinuxIO was updated. New jobs wait for 1 running job to finish.",
      { timeout: 30_000 },
    );
    // Only the user's own host's job: the root host's two are not theirs.
    await waitingBanner
      .getByRole("button", { name: "Show running jobs" })
      .click();
    const listed = waitingBanner.getByRole("listitem");
    await ui(listed).toHaveCount(1);
    await ui(listed).toContainText(/Copying/);
    await ui(listed).toContainText(` · ${PLAIN_USER} · started `);
    await ui(waitingBanner).not.toContainText(SCRUB_LABEL);
    // A session without sudo is not offered LinuxIO's update.
    await ui(available(page)).toHaveCount(0);

    const status = await taskHost(page);
    expect(status.update_pending).toBe(true);
    expect(status.running).toBe(1);
    expect(status.jobs?.map((j) => j.id)).toEqual([w.userCopy.id]);

    // The user's host refused the hello as the root host did, and still runs
    // A with the job.
    const refused = await vmSsh(
      `sudo journalctl -b -u linuxio-jobs@${w.uid}.service -o cat --no-pager | grep -F 'job host refused a bridge hello' || true`,
    );
    expect(refused).toContain(
      "job host refused a bridge hello: binary replaced",
    );
    expect(await hostPid(w.uid)).toBe(w.userPid);
    expect(await hostSha(w.uid)).toBe(w.oldSha);
    expect(
      (await jobRecords(COPY, w.uid)).find((r) => r.id === w.userCopy.id),
    ).toMatchObject({ state: "running" });
  });

  test("tasks.watch, tasks.cancel and tasks.output wait 10 s and fail with 503, and cancel ends nothing", async ({
    page,
  }) => {
    test.setTimeout(180_000);
    const w = current();
    await signIn(page);
    await ui(banner(page)).toContainText("New jobs wait for 2 running jobs");

    // docs/jobs.md, Updates: the reads of a host job wait 10 s for a host
    // that does not come, then answer 503; there is no way to cancel a job
    // an update waits for. The output is of the run that ended before the
    // update, which the root host keeps.
    const timed = async (call: Promise<unknown>) => {
      const start = Date.now();
      const result = await call;
      return { result, seconds: (Date.now() - start) / 1000 };
    };
    const answers = await Promise.all([
      timed(bridgeCall(page, "tasks.watch", { taskId: w.scrub.id })),
      timed(bridgeCall(page, "tasks.cancel", { taskId: w.scrub.id })),
      timed(bridgeCall(page, "tasks.output", { taskId: w.run.id })),
    ]);
    for (const { result, seconds } of answers) {
      expect(result).toEqual(
        expect.objectContaining({ status: "error", code: 503 }),
      );
      expect.soft(seconds, "waits for the host").toBeGreaterThan(8);
    }
    console.log(
      `answered after ${answers.map((a) => a.seconds.toFixed(1)).join(", ")} s`,
    );

    // The cancel changed nothing: neither job was asked to stop, and the
    // older host and the kernel still run the scrub.
    for (const id of [w.scrub.id, w.copy.id]) {
      const record = await jobRecord(id);
      expect(record?.state).toBe("running");
      expect(record?.cancel_requested ?? false).toBe(false);
    }
    expect(await scrubRunning()).toBe(true);
    expect(await hostPid(0)).toBe(w.rootPid);
    // The history answers empty at once rather than waiting.
    expect(await bridgeCall(page, "tasks.history", {})).toEqual({
      status: "ok",
      data: { entries: [], more: false },
    });
  });

  test("uploads run in the bridge and are not refused during the wait", async ({
    page,
  }) => {
    test.setTimeout(180_000);
    const w = current();
    const dir = `/home/${vmUser()}/e2e-update-upload`;
    const names = ["single.txt", "batch-a.txt", "batch-b.txt"];
    const sums = Object.fromEntries(
      await Promise.all(
        names.map(async (name) => [
          name,
          await hostSha256(fixture(`upload/${name}`)),
        ]),
      ),
    );
    await vmSsh(`sudo rm -rf ${dir} && mkdir -p ${dir}/batch`);

    await signIn(page);
    await ui(banner(page)).toContainText("New jobs wait for 2 running jobs");
    await page.goto(`/filebrowser${dir}`);
    // docs/jobs.md, Updates: the three browser-bound mutations are not
    // affected. filebrowser.upload, then filebrowser.upload_batch.
    await upload(page, page.getByText("batch", { exact: true }), [
      fixture("upload/single.txt"),
    ]);
    await ui(page.getByText(`Uploaded 1 item to ${dir}`)).toBeVisible();
    await page.getByText("batch", { exact: true }).dblclick();
    await upload(page, page.getByText("This folder is empty."), [
      fixture("upload/batch-a.txt"),
      fixture("upload/batch-b.txt"),
    ]);
    await ui(page.getByText(`Uploaded 2 items to ${dir}/batch`)).toBeVisible();
    await ui(page.getByText(/update_pending|LinuxIO update waits/)).toHaveCount(
      0,
    );

    expect(
      await vmSsh(
        `cd ${dir} && sha256sum single.txt batch/batch-a.txt batch/batch-b.txt`,
      ),
    ).toBe(
      `${sums["single.txt"]}  single.txt\n` +
        `${sums["batch-a.txt"]}  batch/batch-a.txt\n` +
        `${sums["batch-b.txt"]}  batch/batch-b.txt\n`,
    );
    // The wait is as it was: both jobs run in the same older host.
    await ui(banner(page)).toContainText("New jobs wait for 2 running jobs");
    expect(await hostPid(0)).toBe(w.rootPid);
    expect((await jobRecord(w.scrub.id))?.state).toBe("running");
  });

  test("the update proceeds after the last job ends, and the update job and the records survive it", async ({
    page,
  }) => {
    // The bounds: the copies' end 120 s, the banner 45 s, the scrub's end
    // 300 s, the older host's exit 30 s, the relay's backoff and the banner's
    // poll 90 s, the reads 60 s: some 645 s.
    test.setTimeout(900_000);
    const w = current();
    await signIn(page);
    await ui(banner(page)).toContainText("New jobs wait for 2 running jobs");

    // Lifting the disk's cap ends both copies; the scrub is capped on its own.
    await throttle(0, [SLOW_DISK]);
    await expect
      .poll(async () => (await jobRecord(w.copy.id))?.state, {
        intervals: [2_000],
        message: "the root copy ends",
        timeout: 120_000,
      })
      .toBe("completed");
    await expect
      .poll(
        async () =>
          (await jobRecords(COPY, w.uid)).find((r) => r.id === w.userCopy.id)
            ?.state,
        {
          intervals: [2_000],
          message: "the user's copy ends",
          timeout: 120_000,
        },
      )
      .toBe("completed");

    // One job is left, so the update still waits for it, and the older host
    // is the same process.
    await ui(banner(page)).toContainText(
      "LinuxIO was updated. New jobs wait for 1 running job to finish.",
      { timeout: 45_000 },
    );
    await refuse(page, "LinuxIO update waits for 1 running job");
    expect(await hostPid(0), "the older host runs the last job").toBe(
      w.rootPid,
    );
    expect((await jobRecord(w.scrub.id))?.state).toBe("running");
    expect(await scrubRunning()).toBe(true);

    await scrubSpeed(w.uuid, 0);
    await expect
      .poll(async () => (await jobRecord(w.scrub.id))?.state, {
        intervals: [2_000],
        message: "the scrub ends",
        timeout: 300_000,
      })
      .toBe("completed");
    // With its last job recorded and no bridge of its own build (a refused
    // hello is no bridge), the older host exits within a second, without
    // waiting out its idle minute.
    await expect
      .poll(() => hostPid(0), {
        intervals: [1_000],
        message: "the older host exits after its last job",
        timeout: 30_000,
      })
      .not.toBe(w.rootPid);
    // The relay tries again within 30 s (its backoff ends at 30 s), which
    // starts a host of the new build, and the banner asks every 15 s.
    await ui(banner(page)).toBeHidden({ timeout: 90_000 });
    const newerPid = await hostPid(0);
    expect(newerPid, "a host of the new build runs").not.toBe("0");
    expect(newerPid).not.toBe(w.rootPid);
    expect(await hostSha(0), "the new host runs B").toBe(w.newSha);

    // The records A's host wrote are read by B's: the update job that ran
    // across the restart is in the history as finished, with the jobs it
    // ran beside.
    for (const id of [w.update.id, w.scrub.id]) {
      expect(await bridgeCall(page, "tasks.get", { taskId: id })).toEqual(
        expect.objectContaining({
          status: "ok",
          data: expect.objectContaining({ id, state: "completed" }),
        }),
      );
    }
    // Cancelling an ended job finds it ended; watching it answers its end.
    expect(
      await bridgeCall(page, "tasks.cancel", { taskId: w.scrub.id }),
    ).toEqual(
      expect.objectContaining({
        status: "ok",
        data: expect.objectContaining({ state: "completed" }),
      }),
    );
    expect(
      await bridgeCall(page, "tasks.watch", { taskId: w.scrub.id }),
    ).toEqual(expect.objectContaining({ status: "ok" }));
    // The run that ended before the update keeps its saved output.
    const output = await bridgeCall(page, "tasks.output", { taskId: w.run.id });
    expect(output).toEqual(expect.objectContaining({ status: "ok" }));
    expect((output.data as { content: string }).content).toContain(w.marker);
    expect(await historyIds(page)).toEqual(
      expect.arrayContaining([w.update.id, w.scrub.id, w.copy.id, w.run.id]),
    );
    for (const id of [w.scrub.id, w.copy.id]) {
      expect((await jobRecord(id))?.state).toBe("completed");
    }
    // The history page shows the update finished, and the tries before it
    // that did not go through: the failed one and, on Ubuntu, the cancelled.
    await page.goto("/history");
    const updateRow = page
      .getByRole("table", { name: "Job history" })
      .getByRole("row")
      .filter({ hasText: "Updating packages" });
    await ui(updateRow).toHaveCount(vmDistro === "ubuntu" ? 3 : 2);
    await ui(updateRow.filter({ hasText: "Completed" })).toHaveCount(1);
    await ui(updateRow.filter({ hasText: "Failed" })).toHaveCount(1);
    await ui(updateRow.filter({ hasText: "Cancelled" })).toHaveCount(
      vmDistro === "ubuntu" ? 1 : 0,
    );
    await signOut(page);

    // The user's host exited once its copy ended; the next sign-in of the
    // user starts one of the new build, which lists the copy.
    await signIn(page, w.user);
    await ui(page.getByRole("button", { name: "Account" })).toBeVisible();
    expect(await historyIds(page)).toContain(w.userCopy.id);
    expect(await hostPid(w.uid), "a new host of the user").not.toBe(w.userPid);
    expect(await hostSha(w.uid), "the user's host runs B").toBe(w.newSha);
    await ui(banner(page)).toHaveCount(0);
  });

  test("Fedora: B's SELinux module is loaded, and sign-in works after the update and after a reboot", async ({
    page,
  }) => {
    test.skip(
      vmDistro !== "fedora",
      "the DEB carries no SELinux module; Ubuntu runs AppArmor, not SELinux",
    );
    // The bounds: two checks 60 s each, the reboot 300 s: some 420 s.
    test.setTimeout(600_000);
    const w = current();
    expect((await vmSsh("getenforce")).trim()).toBe("Enforcing");
    const check = async (when: string) => {
      // semodule -E writes the module as the store holds it, linuxio.cil,
      // into the folder it runs in.
      const module = await vmSsh(
        'd="$(mktemp -d)" && cd "$d" && sudo semodule -E linuxio >/dev/null 2>&1 && cat linuxio.cil; rm -rf "$d"',
      );
      expect(module, `B's module ${when}`).toContain(`; ${MODULE_NOTE}`);
      expect(
        await vmSsh("ls -Z /usr/libexec/linuxio/linuxio-auth"),
        `linuxio-auth's type ${when}`,
      ).toContain(":linuxio_auth_exec_t:");
      await signIn(page);
      await ui(page.getByRole("button", { name: "Account" })).toBeVisible();
      await signOut(page);
      const denials = await vmSsh(
        "sudo ausearch -m AVC,USER_AVC,SELINUX_ERR -ts boot -i 2>/dev/null | grep -i linuxio || true",
      );
      expect(denials, `SELinux denials for LinuxIO ${when}`).toBe("");
    };
    expect(await installedVersion()).toBe(w.version);
    await check("after the update");
    await rig(["reboot"]);
    await check("after a reboot");
  });
});
