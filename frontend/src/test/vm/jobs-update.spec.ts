import { createHash, randomBytes } from "node:crypto";
import { chmod, mkdir, readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test, type Locator, type Page } from "@playwright/test";

import {
  bridgeCall,
  createSchedule,
  fillSchedule,
  fixture,
  jobIds,
  jobRecord,
  jobRecords,
  type JobRecord,
  type Login,
  MiB,
  noSessions,
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
  vmSsh,
  waitForIdle,
  waitForJob,
  waitForRun,
} from "./vm";

// docs/jobs.md (Updates): a LinuxIO update never stops a job host. A host of
// an older protocol refuses the new bridge's hello, the update waits for its
// jobs, and new jobs are refused with `update_pending` until the older host
// exits. This file does it from a published release: the VM runs v0.53.0
// (which has job hosts), long jobs run in its hosts, and the build under test
// is installed over it.
//
// Two things the VM needs that nothing in a release gives:
//  - jobs.Protocol of v0.53.0 is 2, as it is in this tree, so a plain build of
//    this tree would be accepted by the older host and nothing would wait. The
//    build in LINUXIO_VM_UPDATE_BUILD must be this tree with jobs.Protocol one
//    higher (make vm-update-build builds it with a Go overlay; docs/jobs.md,
//    Testing). Without it the tests are skipped.
//  - the in-app updater (`control.app_update`) installs a published release
//    only, so the update is the installer script itself, which is what the
//    updater runs (the VM rig's install, as in jobs-phase5a.spec.ts).
//
// The release is downloaded on the host from GitHub into
// .cache/vm-release/<tag>, checked against the release's SHA256SUMS, and
// installed with the rig's install and the release's own installer and
// packaging (units, PAM, needrestart override), which the rig takes from the
// release's git tag. The update build is then installed with this checkout's,
// as an update does.
//
// The tests are one serial group sharing one update, because each update costs
// minutes (an install, then the older host's idle minute and the relay's
// backoff): the first test sets it up, the others read the wait from a
// different side, and the last ends the jobs. The jobs are two in the root
// host (a throttled btrfs scrub and a throttled copy) and one in the host of a
// user without sudo (a throttled copy). The copies read an ext4 on vdd that
// the VM rig throttles, so lifting the throttle ends both copies and leaves
// the scrub, which ends by raising its own speed cap.
//
// It runs alone, on a freshly reset VM that needs no `make vm-install`: the
// file is the Playwright project release-update, which `make test-vm-browser`
// runs after the other files, on its first VM reverted to clean. By hand:
//   make vm-reset vm-update-build
//   LINUXIO_VM_UPDATE_BUILD=.cache/vm-update-build/build make test-vm-browser-rerun VMS=1 PLAYWRIGHT_ARGS=--project=release-update
// The file needs vdd to hold nothing and changes the VM: it leaves the
// release's files in .cache, the update build installed, the apt timers
// stopped, the e2e pool on vdb and vdc, a user without sudo, an ext4 on vdd
// mounted at /mnt/e2e-slow and a schedule. Run `make vm-reset` (and
// `make vm-install` for the other files) before anything else.

const OLD_RELEASE = "v0.53.0";
const RELEASE_URL = `https://github.com/mordilloSan/LinuxIO/releases/download/${OLD_RELEASE}`;
/** The binaries of OLD_RELEASE, as that release named them. */
const BINARIES = [
  "linuxio",
  "linuxio-webserver",
  "linuxio-bridge",
  "linuxio-auth",
  "linuxio-docker-update",
  "linuxio-indexer",
  "linuxio-monitoring",
  "linuxio-btrfs-timer",
  "linuxio-claude-relay",
];
const RELEASE_DIR = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../../.cache/vm-release",
  OLD_RELEASE,
);

const COPY = "filebrowser.copy_batch";
/** A user without sudo: its job host is the older host of the second check. */
const PLAIN_USER = "e2e-plain";
const SLOW_DISK = "vdd";
const SLOW_MOUNT = "/mnt/e2e-slow";
/**
 * The cap of the disk both copies read and write, in bytes per second. Each
 * copy moves 2 GiB through it, so they outlast the checks by far.
 */
const SLOW_RATE = 2 * MiB;

const vmUser = () => process.env.LINUXIO_VM_USER ?? "";
const updateBuild = () => process.env.LINUXIO_VM_UPDATE_BUILD ?? "";

/** What the first test sets up and the others read. */
interface Waiting {
  /** The uuid of the test pool, whose scrub the root host runs. */
  uuid: string;
  scrub: JobRecord;
  copy: JobRecord;
  userCopy: JobRecord;
  /** A scheduled run that ended before the update (the older hooks wrote it). */
  run: ScheduledRecord;
  marker: string;
  user: Login;
  uid: number;
  /** The older hosts' main PIDs. */
  rootPid: string;
  userPid: string;
  /** The SHA-256 of linuxio-bridge in the release and in the update build. */
  oldSha: string;
  newSha: string;
}
let waiting: Waiting | undefined;

const current = () => {
  if (!waiting) throw new Error("the first test of the group did not finish");
  return waiting;
};

// Every click and locator check fails in seconds: the tests' own long waits
// (a host's idle minute, a scrub's end) carry their own timeouts.
test.use({ actionTimeout: 15_000 });
const ui = expect.configure({ timeout: 15_000 });

test.afterEach(({ page }) => signOutIfSignedIn(page));

const sha256 = async (file: string) =>
  createHash("sha256")
    .update(await readFile(file))
    .digest("hex");

/**
 * Downloads the binaries of OLD_RELEASE into RELEASE_DIR, each checked
 * against the release's SHA256SUMS, and returns the directory and the SHA-256
 * of its linuxio-bridge. A file already there with the right sum is kept.
 */
async function oldRelease() {
  const response = await fetch(`${RELEASE_URL}/SHA256SUMS`);
  expect(response.ok, `GET ${RELEASE_URL}/SHA256SUMS`).toBe(true);
  const sums = new Map<string, string>();
  for (const line of (await response.text()).split("\n")) {
    const [sum, name] = line.trim().split(/\s+/);
    if (sum && name) sums.set(name, sum);
  }
  await mkdir(RELEASE_DIR, { recursive: true });
  for (const name of BINARIES) {
    const file = path.join(RELEASE_DIR, name);
    const want = sums.get(name);
    expect(want, `${name} in the release's SHA256SUMS`).toBeTruthy();
    if ((await sha256(file).catch(() => "")) === want) continue;
    const asset = await fetch(`${RELEASE_URL}/${name}`);
    expect(asset.ok, `GET ${RELEASE_URL}/${name}`).toBe(true);
    await writeFile(`${file}.part`, Buffer.from(await asset.arrayBuffer()));
    expect(await sha256(`${file}.part`), `${name} against SHA256SUMS`).toBe(
      want,
    );
    await chmod(`${file}.part`, 0o755);
    await rename(`${file}.part`, file);
  }
  return { dir: RELEASE_DIR, bridgeSha: sums.get("linuxio-bridge")! };
}

const hostPid = async (uid: number) =>
  (await vmSsh(`systemctl show -P MainPID linuxio-jobs@${uid}.service`)).trim();

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

/**
 * Puts the release in the VM's place: no session holds a host, the job hosts
 * are stopped so that the next login starts one from the release, and the
 * release is installed over whatever build is there.
 */
async function installOldRelease() {
  const release = await oldRelease();
  await noSessions();
  expect(
    (await jobRecords()).filter((r) => r.state === "running"),
    "jobs running in the root job host: a fresh VM has none",
  ).toEqual([]);
  await vmSsh("sudo systemctl stop 'linuxio-jobs@*.service'");
  // With the release's own installer and packaging: this checkout's units
  // start programs and words the release does not have.
  console.log(await rig(["install", release.dir, OLD_RELEASE]));
  return release.bridgeSha;
}

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

test.describe.serial("an update from a release with job hosts", () => {
  test.skip(
    updateBuild() === "",
    "needs LINUXIO_VM_UPDATE_BUILD: make vm-update-build builds this tree with jobs.Protocol one higher (docs/jobs.md, Testing)",
  );

  test.afterAll(async () => {
    // Nothing may keep the disk or the scrub slow for the next file.
    try {
      await throttle(0, [SLOW_DISK]);
      if (waiting) await scrubSpeed(waiting.uuid, 0);
    } catch (error) {
      console.warn(`could not lift the caps: ${String(error)}`);
    }
  });

  test("the banner counts the two jobs the root host runs", async ({
    page,
  }) => {
    // The bounds: an apt run 600 s; the release's download 120 s; the two
    // installs 2 x 90 s; the pool on a fresh VM 120 s; the files on vdd 60 s;
    // the scrub, the schedule and the two copies 180 s; the banner 30 s; the
    // reads 60 s: some 1350 s.
    test.setTimeout(1_500_000);
    await stopAptRuns();

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
    expect(await vmSsh(`id -nG ${PLAIN_USER}`)).not.toMatch(/\bsudo\b/);
    const uid = Number((await vmSsh(`id -u ${PLAIN_USER}`)).trim());

    const oldSha = await installOldRelease();
    const newSha = await sha256(path.join(updateBuild(), "linuxio-bridge"));
    expect(newSha, "the update build is not the release").not.toBe(oldSha);
    await prepareSlowDisk();

    // The root host's jobs, started in the release's UI: a scrub at 1 MiB/s
    // per disk, and a copy that reads the throttled disk.
    const beforeScrub = await jobIds();
    const { uuid, job: scrub } = await startScrub(page, beforeScrub, MiB);
    expect(await hostSha(0), "the root host runs the release").toBe(oldSha);

    // A scheduled run that ends before the update: the release's hooks write
    // its record and its saved output, which the new build reads afterwards.
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
    expect(await hostSha(uid), "the user's host runs the release").toBe(oldSha);
    await signOut(page);

    // A bridge still connected would keep an older host up after its jobs.
    await noSessions();
    expect(await scrubRunning(), "the scrub ended: lower its speed").toBe(true);
    const rootPid = await hostPid(0);
    const userPid = await hostPid(uid);
    waiting = {
      uuid,
      scrub,
      copy,
      userCopy: userCopy!,
      run,
      marker,
      user,
      uid,
      rootPid,
      userPid,
      oldSha,
      newSha,
    };

    // The update. Installing over running hosts is what an update does; the
    // hosts keep their (replaced) binaries and their jobs.
    console.log(await rig(["install", updateBuild()]));
    expect(await hostPid(0), "the install left the root host").toBe(rootPid);
    expect(await hostPid(uid), "the install left the user's host").toBe(
      userPid,
    );
    expect(await hostSha(0), "the root host still runs the release").toBe(
      oldSha,
    );
    for (const id of [scrub.id, copy.id]) {
      expect((await jobRecord(id))?.state).toBe("running");
    }

    await signIn(page);
    const waitingBanner = banner(page);
    await ui(waitingBanner).toContainText(
      "LinuxIO was updated. New jobs wait for 2 running jobs to finish.",
      { timeout: 30_000 },
    );
    // The older host refused the new bridge's hello on the protocol number:
    // the release's protocol is 2.
    const refused = await vmSsh(
      "sudo journalctl -b -u linuxio-jobs@0.service -o cat --no-pager | grep -F 'job host refused a bridge hello' || true",
    );
    const [, theirs, ours] = refused.match(/protocol (\d+), want (\d+)/) ?? [];
    expect(Number(ours), refused).toBe(2);
    expect(Number(theirs), refused).toBe(Number(ours) + 1);

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

    const status = await taskHost(page);
    expect(status.update_pending).toBe(true);
    expect(status.running).toBe(1);
    expect(status.jobs?.map((j) => j.id)).toEqual([w.userCopy.id]);

    // The user's host refused the hello as the root host did, and still runs
    // the release with the job.
    const refused = await vmSsh(
      `sudo journalctl -b -u linuxio-jobs@${w.uid}.service -o cat --no-pager | grep -F 'job host refused a bridge hello' || true`,
    );
    expect(refused).toMatch(/protocol \d+, want 2/);
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
          await sha256(fixture(`upload/${name}`)),
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

  test("the update proceeds after the last job ends and the records survive it", async ({
    page,
  }) => {
    // The bounds: the copies' end 120 s, the banner 45 s, the scrub's end
    // 300 s, the older host's idle minute and the relay's backoff 150 s, the
    // reads 60 s: some 700 s.
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
    await ui(banner(page)).toContainText(
      "LinuxIO was updated. New jobs can start shortly.",
      { timeout: 30_000 },
    );
    // The older host exits a minute after its last job and bridge (a refused
    // hello is no bridge); the relay tries again within 30 s, and the banner
    // asks every 15 s.
    await ui(banner(page)).toBeHidden({ timeout: 150_000 });
    const newerPid = await hostPid(0);
    expect(newerPid, "a host of the new build runs").not.toBe("0");
    expect(newerPid).not.toBe(w.rootPid);
    expect(await hostSha(0), "the new host runs the update build").toBe(
      w.newSha,
    );

    // The records the release's host wrote are read by the new one.
    const scrub = await bridgeCall(page, "tasks.get", { taskId: w.scrub.id });
    expect(scrub).toEqual(
      expect.objectContaining({
        status: "ok",
        data: expect.objectContaining({ id: w.scrub.id, state: "completed" }),
      }),
    );
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
      expect.arrayContaining([w.scrub.id, w.copy.id, w.run.id]),
    );
    for (const id of [w.scrub.id, w.copy.id]) {
      expect((await jobRecord(id))?.state).toBe("completed");
    }
    await signOut(page);

    // The user's host exited a minute after its copy; the next sign-in of the
    // user starts one of the new build, which lists the copy.
    await signIn(page, w.user);
    await ui(page.getByRole("button", { name: "Account" })).toBeVisible();
    expect(await historyIds(page)).toContain(w.userCopy.id);
    expect(await hostPid(w.uid), "a new host of the user").not.toBe(w.userPid);
    expect(await hostSha(w.uid), "the user's host runs the update build").toBe(
      w.newSha,
    );
    await ui(banner(page)).toHaveCount(0);
  });
});
