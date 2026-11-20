import { expect, test, type Page } from "@playwright/test";

import {
  ensurePool,
  exclusiveOperation,
  jobIds,
  jobRecord,
  loggedJobs,
  MiB,
  openTray,
  POOL_MOUNT,
  rig,
  SCRUB,
  SCRUB_LABEL,
  scheduledRecords,
  scrubbedBytes,
  scrubRunning,
  signIn,
  signOut,
  signOutIfSignedIn,
  startScrub,
  throttle,
  trayJob,
  vmSsh,
  waitForIdle,
  waitForJob,
} from "./vm";

// docs/jobs.md (Job lifecycle): the resumable storage routes across host
// stops, reboots, power cuts, crashes and cancels, a pool resync's scrub
// continued after a power cut, the pool create rollback and the Docker
// storage switch. Jobs start in the browser; the host events go through
// packaging/vm/linuxio-vm.sh. Each check fails, rather than passing
// vacuously, when its operation ended before the event.

const BALANCE = "storage.btrfs_balance";
const REPLACE = "storage.btrfs_device_replace";
const POOL_CREATE = "storage.btrfs_pool_create";
const DOCKER_STORAGE = "docker.apply_daemon_storage";
/** The job's label, which names it in its alerts. */
const DOCKER_STORAGE_LABEL = "Docker storage change";
/** A disk rate at which a canceled balance needs seconds to stop. */
const CANCEL_CRAWL = 2 * MiB;

test.afterEach(({ page }) => signOutIfSignedIn(page));

/** The test pool's member disks, such as /dev/vdb. */
const poolDisks = async () =>
  (
    await vmSsh(
      `sudo btrfs filesystem show ${POOL_MOUNT} | awk '/ path /{print $NF}'`,
    )
  )
    .split("\n")
    .filter(Boolean)
    .sort();

/** The kernel log lines of this boot that match pattern. */
const kernelLog = (pattern: string) =>
  vmSsh(`sudo journalctl -k -b -o cat | grep -E '${pattern}' || true`);

/** The stored alert id (source/key), if it is raised. */
async function storedAlert(id: string) {
  const out = await vmSsh(
    `sudo jq -c --arg id '${id}' '[.alerts[] | select(.id == $id)][0]' /var/lib/linuxio/alerts.json`,
  );
  return JSON.parse(out) as {
    title: string;
    message: string;
    dismissed_at?: string;
  } | null;
}

/** The job host's alert for job id, if it raised one. */
const alertOf = (id: string) => storedAlert(`jobs/${id}`);

/**
 * Waits until no session bridge runs: a bridge reconnects to a stopped job
 * host within a second, and a host started again in this boot gives the
 * jobs it finds interrupted up for resume.
 */
async function noSessions() {
  await expect
    .poll(
      async () =>
        (
          await vmSsh(
            "systemctl list-units --no-legend --plain --state=active 'linuxio-auth@*' | wc -l",
          )
        ).trim(),
      { intervals: [1_000], message: "linuxio-auth sessions", timeout: 30_000 },
    )
    .toBe("0");
}

/**
 * Signs in, gives the test pool 1 GiB more in 256 MiB chunks and picks the
 * data profile a balance converts it to. A pause or a cancel waits for the
 * chunk in hand, and the kernel relocates these chunks first (newest first).
 */
async function prepareBalance(page: Page) {
  await signIn(page);
  const uuid = await ensurePool(page);
  await vmSsh(
    `echo ${256 * MiB} | sudo tee /sys/fs/btrfs/${uuid}/allocation/data/chunk_size >/dev/null && ` +
      `sudo rm -f ${POOL_MOUNT}/balance.bin && head -c 1G /dev/urandom | sudo tee ${POOL_MOUNT}/balance.bin >/dev/null && sync`,
  );
  const from = (
    await vmSsh(
      `sudo btrfs filesystem df ${POOL_MOUNT} | sed -n 's/^Data, \\([A-Z0-9]*\\):.*/\\1/p'`,
    )
  ).trim();
  return { to: from === "RAID1" ? "raid0" : "raid1", uuid };
}

/** Starts the balance to data profile `to` in the browser and returns its running job. */
async function startBalance(
  page: Page,
  uuid: string,
  to: string,
  before: Set<string>,
) {
  await page.goto(`/storage?pool=${uuid}`);
  await page.getByRole("button", { name: "Change profile" }).click();
  const dialog = page.getByRole("dialog", { name: "Change profile" });
  await dialog
    .getByRole("combobox", { name: "Data profile", exact: true })
    .click();
  await page.getByRole("option", { name: to, exact: true }).click();
  await dialog.getByRole("button", { name: "Balance", exact: true }).click();
  return waitForJob(BALANCE, before, "running");
}

test("a balance a host stop paused is resumed by the kernel at boot and completes", async ({
  page,
}) => {
  test.setTimeout(600_000);
  const { to, uuid } = await prepareBalance(page);
  // About 12 s per 256 MiB chunk, 1.5 min for the whole balance.
  await throttle(32 * MiB);
  try {
    const before = await jobIds();
    const job = await startBalance(page, uuid, to, before);

    await signOut(page);
    await page.close();
    await noSessions();
    expect(
      await exclusiveOperation(uuid),
      "the balance ended before the host stopped: raise the data size",
    ).toBe("balance");
    await vmSsh("sudo systemctl stop linuxio-jobs@0.service");
    const stopped = await jobRecord(job.id);
    expect(stopped).toMatchObject({
      checkpoint: { paused: true },
      reason: "shutdown",
      resumable: true,
      state: "interrupted",
    });
    expect(await exclusiveOperation(uuid)).toBe("balance paused");

    await rig(["reboot"]);
    await mountedAtBoot(uuid);
    expect(await kernelLog("balance: resume")).toMatch(/balance: resume/);
    const done = await waitForJob(BALANCE, before, "completed", {
      timeout: 300_000,
    });
    expect((await loggedJobs("job resumed")).map((j) => j.id)).toContain(
      job.id,
    );
    const { allocation } = done.result as {
      allocation: { kind: string; profile: string }[];
    };
    expect(
      allocation.filter((a) => a.kind === "data").map((a) => a.profile),
    ).toEqual([to]);
    expect(await exclusiveOperation(uuid)).toBe("none");
  } finally {
    await throttle(0);
  }
});

// A cancel is recorded before the handler is told to stop, and the job ends
// when the handler returns. A host stop in between must leave the job to end
// canceled: a suspend would pause the balance and the next boot resume it.
test("a balance canceled just before the host stops ends canceled and is not resumed", async ({
  page,
}) => {
  test.setTimeout(600_000);
  const { to, uuid } = await prepareBalance(page);
  await throttle(32 * MiB);
  try {
    const before = await jobIds();
    const job = await startBalance(page, uuid, to, before);
    expect(
      await exclusiveOperation(uuid),
      "the balance ended before the cancel: raise the data size",
    ).toBe("balance");
    // The kernel stops a canceled balance once the writes in flight are
    // done. At this rate that takes long enough to stop the host meanwhile.
    await throttle(CANCEL_CRAWL);
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Cancelling…" }),
    ).toBeDisabled();
    const requested = () =>
      jobRecord(job.id).then((r) => ({
        cancel_requested: r?.cancel_requested,
        state: r?.state,
      }));
    await expect
      .poll(requested, { intervals: [500], timeout: 15_000 })
      .toEqual({ cancel_requested: true, state: "running" });
    const tray = await openTray(page);
    await expect(trayJob(tray, /Balancing/)).toContainText("Cancelling…");

    await vmSsh("sudo systemctl stop --no-block linuxio-jobs@0.service");
    await expect
      .poll(
        async () =>
          (
            await vmSsh("systemctl is-active linuxio-jobs@0.service || true")
          ).trim(),
        { intervals: [500], message: "the job host stopping", timeout: 15_000 },
      )
      .toBe("deactivating");
    expect(
      await requested(),
      "the balance stopped before the host did: lower CANCEL_CRAWL",
    ).toEqual({ cancel_requested: true, state: "running" });

    await throttle(0);
    await expect
      .poll(async () => (await jobRecord(job.id))?.state, {
        intervals: [1_000],
        message: `${BALANCE} canceled`,
        timeout: 120_000,
      })
      .toBe("canceled");
    expect(await exclusiveOperation(uuid)).toBe("none");
    await page.close();

    await rig(["reboot"]);
    await mountedAtBoot(uuid);
    expect(await jobRecord(job.id)).toMatchObject({
      cancel_requested: true,
      state: "canceled",
    });
    expect((await loggedJobs("job resumed")).map((j) => j.id)).not.toContain(
      job.id,
    );
    expect(await kernelLog("balance: resume")).toBe("");
    expect(await exclusiveOperation(uuid)).toBe("none");
  } finally {
    await throttle(0);
  }
});

/** LinuxIO's scrub record of a pool (docs/btrfs.md, Scrub progress). */
interface ScrubRecord {
  status: string;
  startedAt: string;
  bytesScrubbed: number;
  correctedErrors: number;
  uncorrectableErrors: number;
  run?: string;
  bootId?: string;
  devices?: Record<
    string,
    { offset: number; bytesScrubbed?: number; correctedErrors?: number }
  >;
}

const scrubRecord = async (uuid: string) =>
  JSON.parse(
    await vmSsh(
      `sudo jq -c --arg u ${uuid} '.[$u] // null' /var/lib/linuxio/btrfs-scrub.json 2>/dev/null || echo null`,
    ),
  ) as ScrubRecord | null;

/** What a scrub record counted: its status and each device's running call. */
const recordedBytes = (rec: ScrubRecord) =>
  rec.bytesScrubbed +
  Object.values(rec.devices ?? {}).reduce(
    (sum, d) => sum + (d.bytesScrubbed ?? 0),
    0,
  );

/** What a scrub record corrected, counted as recordedBytes counts. */
const recordedCorrections = (rec: ScrubRecord) =>
  rec.correctedErrors +
  Object.values(rec.devices ?? {}).reduce(
    (sum, d) => sum + (d.correctedErrors ?? 0),
    0,
  );

/** The raw bytes in use, every copy of data and metadata: a full scrub's. */
const usedBytes = async (uuid: string) =>
  (
    await vmSsh(
      `cat /sys/fs/btrfs/${uuid}/allocation/data/disk_used /sys/fs/btrfs/${uuid}/allocation/metadata/disk_used`,
    )
  )
    .split("\n")
    .filter(Boolean)
    .reduce((sum, n) => sum + Number(n), 0);

test("a scrub cut by a power loss continues from its saved progress at boot", async ({
  page,
}) => {
  test.setTimeout(780_000);
  const before = await jobIds();
  try {
    // 2 GiB more in 256 MiB chunks: what the job saved before the cut then
    // outweighs the dev extent per disk the resume reads again (1 GiB at most).
    const { uuid, job } = await startScrub(page, before, 4 * MiB, (pool) =>
      vmSsh(
        `echo ${256 * MiB} | sudo tee /sys/fs/btrfs/${pool}/allocation/data/chunk_size >/dev/null && ` +
          `sudo rm -f ${POOL_MOUNT}/scrub.bin && head -c 2G /dev/urandom | sudo tee ${POOL_MOUNT}/scrub.bin >/dev/null && sync`,
      ),
    );
    await page.close();
    const used = await usedBytes(uuid);
    // A whole scrub then takes about 200 s; the job saves every minute, so its
    // second save holds more than half.
    const rate = Math.max(4 * MiB, Math.ceil(used / 2 / 200));
    await vmSsh(
      `for f in /sys/fs/btrfs/${uuid}/devinfo/*/scrub_speed_max; do echo ${rate} | sudo tee $f >/dev/null; done`,
    );
    const cut: { saved?: ScrubRecord } = {};
    await expect
      .poll(
        async () => {
          const rec = await scrubRecord(uuid);
          if (rec?.run !== job.id || rec.status !== "running") return false;
          cut.saved = rec;
          return recordedBytes(rec) >= 0.55 * used;
        },
        {
          intervals: [2_000],
          message: "the scrub saving more than half of its progress",
          timeout: 420_000,
        },
      )
      .toBe(true);
    // A job's first record write is not fsynced (docs/jobs.md, Store and
    // records); the scrub record's writes are.
    await vmSsh("sync");
    expect(
      await scrubRunning(),
      "the scrub ended before the power cut: lower its rate",
    ).toBe(true);

    await rig(["power-cut"]);
    const done = await waitForJob(SCRUB, before, "completed", {
      timeout: 300_000,
    });
    expect(done.id).toBe(job.id);
    expect(await loggedJobs("job marked interrupted")).toContainEqual({
      id: job.id,
      reason: "reboot",
    });
    expect((await loggedJobs("job resumed")).map((j) => j.id)).toContain(
      job.id,
    );
    // No Suspend ran, so no checkpoint: the job's own scrub record gave each
    // disk's start.
    expect(done.checkpoint).toBeUndefined();
    const saved = cut.saved!;
    const { scrub } = done.result as {
      scrub: { bytesScrubbed: number; errorCount: number; startedAt: string };
    };
    // A save after the last poll only makes this an overestimate.
    const own = scrub.bytesScrubbed - recordedBytes(saved);
    console.log(
      `${used} bytes in use; saved ${recordedBytes(saved)} before the cut, the resumed run read ${own}, ${scrub.bytesScrubbed} in all`,
    );
    expect(Date.parse(scrub.startedAt)).toBe(Date.parse(saved.startedAt));
    expect(scrub.errorCount).toBe(0);
    expect(scrub.bytesScrubbed).toBeGreaterThanOrEqual(0.99 * used);
    expect(own, "the resumed run scrubbed the whole pool again").toBeLessThan(
      used,
    );
  } finally {
    // Later tests were sized for the pool without it.
    await vmSsh(`sudo rm -f ${POOL_MOUNT}/scrub.bin && sync`);
  }
});

/** How many kernel log lines of this boot match pattern. */
const kernelLines = async (pattern: string) =>
  (await kernelLog(pattern)).split("\n").filter(Boolean).length;

/** A scrub call that ended interrupted (EINTR), and any scrub call started. */
const INTERRUPTED = "scrub: not finished on devid [0-9]+ with status: -4";
const STARTED = "scrub: started on devid";

/**
 * The job host's cgroup is frozen (`systemctl freeze`) while it scrubs. Since
 * 6.19 the freeze ends each scrub call with EINTR, and the job issues it
 * again after the thaw and finishes, which the kernel log shows. Before, the
 * kernel does not interrupt the call and the freeze waits until the scrub
 * ends, so there is nothing to check.
 */
test("a scrub in a frozen job host goes on by itself and finishes", async ({
  page,
}) => {
  test.setTimeout(600_000);
  const release = (await vmSsh("uname -r")).trim();
  const [major, minor] = release.split(".").map(Number);
  test.skip(
    major < 6 || (major === 6 && minor < 19),
    `kernel ${release}: before 6.19 a frozen cgroup does not interrupt a scrub call, and the freeze waits for the scrub to end`,
  );
  const unit = "linuxio-jobs@0.service";
  const before = await jobIds();
  const { uuid, job } = await startScrub(page, before, 16 * MiB);
  await page.close();
  const uncap = () =>
    vmSsh(
      `for f in /sys/fs/btrfs/${uuid}/devinfo/*/scrub_speed_max; do echo 0 | sudo tee $f >/dev/null; done`,
    );
  try {
    await expect
      .poll(scrubbedBytes, {
        intervals: [1_000],
        message: "the scrub reading 256 MiB",
        timeout: 120_000,
      })
      .toBeGreaterThan(256 * MiB);
    const interrupted = await kernelLines(INTERRUPTED);
    const started = await kernelLines(STARTED);
    try {
      await vmSsh(`sudo systemctl freeze ${unit}`);
      expect(
        (await vmSsh(`systemctl show -P FreezerState ${unit}`)).trim(),
      ).toBe("frozen");
      await new Promise((resolve) => setTimeout(resolve, 20_000));
    } finally {
      // A freeze that failed half way must not hold the later tests.
      await vmSsh(
        `sudo systemctl thaw ${unit} || test "$(systemctl show -P FreezerState ${unit})" = running`,
      );
    }
    const interruptedDuring = (await kernelLines(INTERRUPTED)) - interrupted;
    expect(
      interruptedDuring,
      "the freeze interrupted no scrub call",
    ).toBeGreaterThan(0);
    // Each call is issued again after the thaw, behind a save of the record.
    await expect
      .poll(async () => (await kernelLines(STARTED)) - started, {
        intervals: [1_000],
        message: "the interrupted calls issued again",
        timeout: 60_000,
      })
      .toBeGreaterThanOrEqual(interruptedDuring);
    await uncap();
    const done = await waitForJob(SCRUB, before, "completed", {
      timeout: 300_000,
    });
    expect(done.id).toBe(job.id);
    const used = await usedBytes(uuid);
    const { scrub } = done.result as {
      scrub: { status: string; bytesScrubbed: number; errorCount: number };
    };
    console.log(
      `kernel ${release}: ${interruptedDuring} scrub calls ended with EINTR during the freeze; ` +
        `${used} bytes in use, ${scrub.bytesScrubbed} scrubbed`,
    );
    expect(scrub.status).toBe("finished");
    expect(scrub.errorCount).toBe(0);
    expect(scrub.bytesScrubbed).toBeGreaterThanOrEqual(0.99 * used);
  } finally {
    await uncap();
  }
});

test("a replace cut by a power loss is resumed by the kernel and followed to the end", async ({
  page,
}) => {
  test.setTimeout(600_000);
  await signIn(page);
  const uuid = await ensurePool(page);
  const members = await poolDisks();
  // vdc onto vde on a fresh VM; back again on a later run.
  const target = members.includes("/dev/vde") ? "/dev/vdc" : "/dev/vde";
  const source = members.find((d) => d !== "/dev/vdb")!;
  // The cap is in the domain definition too, so the kernel's resumed
  // replace is still running when the host starts after the power cut.
  await throttle(16 * MiB, [target.slice("/dev/".length)]);
  try {
    const before = await jobIds();
    await page.goto(`/storage?pool=${uuid}`);
    await page
      .getByRole("row")
      .filter({ hasText: source })
      .getByRole("button", { name: "Replace" })
      .click();
    const dialog = page.getByRole("dialog", { name: /^Replace / });
    await dialog.getByRole("combobox", { name: "New device" }).click();
    await page.getByRole("option", { name: new RegExp(`^${target} `) }).click();
    await dialog.getByRole("button", { name: "Replace", exact: true }).click();
    const job = await waitForJob(REPLACE, before, "running");
    await page.close();
    await vmSsh("sync");
    expect(
      await exclusiveOperation(uuid),
      "the replace ended before the power cut: raise the data size",
    ).toBe("device replace");

    await rig(["power-cut"]);
    // udev never calls the pool ready, because the kernel counts the replace
    // target as a third disk of two. linuxio-btrfs-degraded mounts it after
    // its grace, and the kernel resumes the replace at mount.
    await expect
      .poll(
        async () =>
          (await vmSsh(`findmnt -no UUID ${POOL_MOUNT} || true`)).trim(),
        {
          intervals: [2_000],
          message:
            "the boot check mounts the pool its replace target held back",
          timeout: 60_000,
        },
      )
      .toBe(uuid);
    console.log(
      await vmSsh(
        "sudo journalctl -b -o short-monotonic --no-pager --grep 'continue a device replace|continuing dev_replace'",
      ),
    );
    expect(await kernelLog("continuing dev_replace")).toMatch(
      new RegExp(`continuing dev_replace from .* to target ${target}`),
    );
    // Whether the job found the kernel's replace still running when it
    // resumed (it follows it) or already finished (it trusts it).
    let followed = false;
    await expect
      .poll(
        async () => {
          const record = await jobRecord(job.id);
          if (record?.state === "running") {
            followed ||= (await exclusiveOperation(uuid)) === "device replace";
          }
          return record && { state: record.state, reason: record.reason };
        },
        {
          intervals: [2_000],
          message: `${REPLACE} completed`,
          timeout: 300_000,
        },
      )
      .toEqual({ state: "completed", reason: undefined });
    console.log(
      `resumed replace: ${followed ? "followed the running kernel replace" : "found it finished"}`,
    );
    expect(await loggedJobs("job marked interrupted")).toContainEqual({
      id: job.id,
      reason: "reboot",
    });
    expect((await loggedJobs("job resumed")).map((j) => j.id)).toContain(
      job.id,
    );
    expect(await poolDisks()).toEqual(["/dev/vdb", target].sort());
  } finally {
    await throttle(0, [target.slice("/dev/".length)]);
    // Cleanup only: a pool the boot check left unmounted is mounted by hand,
    // and the replace the kernel then resumes is waited for, so the checks
    // after this one run.
    await vmSsh(`findmnt ${POOL_MOUNT} >/dev/null || sudo mount ${POOL_MOUNT}`);
    await expect
      .poll(() => exclusiveOperation(uuid), { timeout: 180_000 })
      .toBe("none");
  }
});

test("a scrub canceled before a reboot is not resumed", async ({ page }) => {
  test.setTimeout(300_000);
  const before = await jobIds();
  const { job, uuid } = await startScrub(page, before, 4 * MiB);
  // The banner's Stop check cancels the scrub's job, as the tray's cancel does.
  const stop = page.getByRole("button", { name: "Stop check" });
  await expect(stop).toBeVisible({ timeout: 30_000 });
  expect(
    await scrubRunning(),
    "the scrub ended before the cancel: raise the data size",
  ).toBe(true);
  await stop.click();
  await expect
    .poll(async () => (await jobRecord(job.id))?.state, {
      intervals: [1_000],
      message: `${SCRUB} canceled`,
      timeout: 30_000,
    })
    .toBe("canceled");
  await page.close();

  await rig(["reboot"]);
  await mountedAtBoot(uuid);
  expect(await jobRecord(job.id)).toMatchObject({ state: "canceled" });
  expect((await loggedJobs("job resumed")).map((j) => j.id)).not.toContain(
    job.id,
  );
  expect(await scrubRunning()).toBe(false);
});

test("a scrub whose host crashed is not resumed by the next reboot", async ({
  page,
}) => {
  test.setTimeout(300_000);
  const before = await jobIds();
  const { job, uuid } = await startScrub(page, before, 4 * MiB);
  expect(
    await scrubRunning(),
    "the scrub ended before the kill: raise the data size",
  ).toBe(true);
  await vmSsh(
    'sudo kill -9 "$(systemctl show -P MainPID linuxio-jobs@0.service)"',
  );
  // The browser's bridge reconnects; the next host marks it crashed.
  await waitForJob(SCRUB, before, "interrupted", {
    reason: "crash",
    timeout: 90_000,
  });
  await page.close();

  await rig(["reboot"]);
  await mountedAtBoot(uuid);
  const after = await jobRecord(job.id);
  expect(after).toMatchObject({ reason: "crash", state: "interrupted" });
  expect(after?.resumable).toBeFalsy();
  expect((await loggedJobs("job resumed")).map((j) => j.id)).not.toContain(
    job.id,
  );
  expect(await scrubRunning()).toBe(false);
  expect(await alertOf(job.id)).toMatchObject({
    message: expect.stringContaining("(crash)"),
    title: `${SCRUB_LABEL} was interrupted`,
  });
  const page2 = await page.context().newPage();
  await signIn(page2);
  const tray = await openTray(page2);
  await expect(
    tray
      .getByRole("listitem")
      .filter({ hasText: `${SCRUB_LABEL} was interrupted` })
      .filter({ hasText: "(crash)" })
      .first(),
  ).toBeVisible({ timeout: 30_000 });
  await signOut(page2);
});

const RESYNC = "scheduled.btrfs_resync";
const HEALTH_TIMER = "linuxio-btrfs-health.timer";
const HEALTH_SERVICE = "linuxio-btrfs-health.service";
const DEGRADED_SERVICE = "linuxio-btrfs-degraded.service";
/** The resync check's files on the test pool. */
const RESYNC_DIR = `${POOL_MOUNT}/resync-e2e`;

/** LinuxIO's state of a pool (docs/btrfs.md, Missing disks). */
interface PoolState {
  degraded: boolean;
  present?: number[];
  resyncRun?: string;
}

const poolState = async (uuid: string) =>
  JSON.parse(
    await vmSsh(
      `sudo jq -c --arg u ${uuid} '.[$u] // null' /var/lib/linuxio/btrfs-pools.json 2>/dev/null || echo null`,
    ),
  ) as PoolState | null;

/** The btrfs device UUID on disk, read from the disk; "" without one. */
const memberUuid = async (disk: string) =>
  (
    await vmSsh(
      `sudo blkid -p -s UUID_SUB -o value /dev/${disk} 2>/dev/null || true`,
    )
  ).trim();

/** The btrfs device ID on disk, read from its superblock. */
const devidOf = async (disk: string) =>
  Number(
    await vmSsh(
      `sudo btrfs inspect-internal dump-super /dev/${disk} | awk '$1 == "dev_item.devid" {print $2}'`,
    ),
  );

/**
 * Takes disk off the guest's PCI bus, as a disk that drops out. The domain
 * keeps it, and a rescan brings it back under its name: virtio_blk names a
 * disk by the lowest free index.
 */
async function pullDisk(disk: string) {
  const fn = (
    await vmSsh(`basename "$(readlink -f /sys/block/${disk}/device/..)"`)
  ).trim();
  expect(fn, `the PCI function of ${disk}`).toMatch(
    /^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$/,
  );
  await vmSsh(
    `echo 1 | sudo tee /sys/bus/pci/devices/${fn}/remove >/dev/null && sudo udevadm settle`,
  );
  expect(await memberUuid(disk), `${disk} gone`).toBe("");
}

/** Rescans the guest's PCI bus and waits until disk is back as member. */
async function rescanFor(disk: string, member: string) {
  await vmSsh(
    "echo 1 | sudo tee /sys/bus/pci/rescan >/dev/null && sudo udevadm settle",
  );
  await expect
    .poll(() => memberUuid(disk), {
      intervals: [1_000],
      message: `${disk} back on the bus`,
      timeout: 30_000,
    })
    .toBe(member);
}

/** Unmounts the test pool, waiting out a moment of use. */
const unmountPool = () =>
  vmSsh(
    `for i in $(seq 30); do sudo umount ${POOL_MOUNT} 2>/dev/null && exit 0; sleep 1; done; sudo umount ${POOL_MOUNT}`,
  );

/**
 * Stops the health timer until the next boot or start and waits out a check
 * in progress, so no check starts a resync, or records a mount the test
 * makes by hand.
 */
async function holdHealthChecks() {
  await vmSsh(`sudo systemctl stop ${HEALTH_TIMER}`);
  await waitForIdle(HEALTH_SERVICE);
}

/**
 * Waits until systemd mounted the test pool at boot, and fails when it gave
 * up on it. udev can leave a complete pool unready when it scans both disks
 * at once; the boot check then has udev evaluate them again 10 s after it
 * starts, and systemd mounts the pool a moment later (docs/btrfs.md, Missing
 * disks), so a check that reads the pool right after a boot waits for this.
 * The boot check's line is printed when it did so.
 */
async function mountedAtBoot(uuid: string) {
  let seen = "";
  await expect
    .poll(
      async () => {
        seen = (
          await vmSsh(
            `findmnt -no UUID ${POOL_MOUNT} || systemctl list-jobs --no-legend --plain | grep -c ' mnt-e2e.mount ' || true`,
          )
        ).trim();
        return seen === uuid || seen === "0";
      },
      {
        intervals: [2_000],
        message: "systemd mounting the pool at boot, or giving up",
        timeout: 150_000,
      },
    )
    .toBe(true);
  const broughtUp = await vmSsh(
    "sudo journalctl -b -o short-monotonic --no-pager -u linuxio-btrfs-degraded.service | grep 'udev evaluated its disks again' || true",
  );
  if (broughtUp.trim()) console.log(broughtUp.trim());
  expect(seen, "systemd gave up mounting the pool at boot").toBe(uuid);
}

/** The SHA-256 of each file of the check, or the error reading them. */
const hashFiles = () =>
  vmSsh(`sudo sh -c 'cd ${RESYNC_DIR} && sha256sum -- *.bin' 2>&1 || true`);

/** The lines of `btrfs device stats` for disk whose counter is not zero. */
const raisedCounters = async (disk: string) =>
  (await vmSsh(`sudo btrfs device stats /dev/${disk}`))
    .trim()
    .split("\n")
    .filter((line) => !/\s0$/.test(line));

// docs/btrfs.md (Missing disks): a pool that ran with a disk missing is
// resynced once the disk is back, and a power cut mid-scrub is continued by
// the next resync, which the health check starts two minutes after the boot.
// The disk drops out on the guest's PCI bus and the boot check mounts the
// pool degraded. The writes made meanwhile fill holes in every data chunk, so
// the returned disk holds stale copies on both sides of the cut; the check
// fails when the next run scrubs from the start, or leaves a stale copy.
test("a pool resync cut by a power loss continues its scrub in the next run", async ({
  page,
}) => {
  test.setTimeout(1_500_000);
  await signIn(page);
  const uuid = await ensurePool(page);
  // A session's listing enables and starts the health timer, which then
  // starts at every boot, and only once per bridge.
  await page.goto(`/storage?pool=${uuid}`);
  await expect
    .poll(
      async () =>
        (await vmSsh(`systemctl is-enabled ${HEALTH_TIMER} || true`)).trim(),
      { message: HEALTH_TIMER, timeout: 30_000 },
    )
    .toBe("enabled");
  await signOut(page);
  await page.close();
  await noSessions();

  const members = await poolDisks();
  // vdc on a fresh pool; the replace check moves it to vde and back.
  const stale = members.find((d) => d !== "/dev/vdb")!.slice("/dev/".length);
  const goodUuid = await memberUuid("vdb");
  const staleUuid = await memberUuid(stale);
  const goodDevid = await devidOf("vdb");
  const unit = `linuxio-btrfs-resync@${uuid}.service`;
  const earlier = new Set((await scheduledRecords(RESYNC)).map((r) => r.id));
  const resyncRuns = async () =>
    (await scheduledRecords(RESYNC)).filter(
      (r) => r.executor.unit === unit && !earlier.has(r.id),
    );
  try {
    await holdHealthChecks();
    // Every data chunk raid1 and 256 MiB, then 32 MiB files with every
    // other one deleted, and fill.bin with them, so every data chunk left
    // has holes. The writes made without the disk fill them first.
    console.log(
      await vmSsh(
        "sudo bash -s",
        `set -euo pipefail
mnt=${POOL_MOUNT} dir=${RESYNC_DIR}
rm -rf "$dir" "$mnt/balance.bin"
sync
echo ${256 * MiB} >/sys/fs/btrfs/${uuid}/allocation/data/chunk_size
btrfs balance start -dconvert=raid1 -mconvert=raid1,soft "$mnt" >/dev/null
mkdir "$dir"
for i in $(seq -w 0 63); do
  head -c 32M /dev/urandom | dd of="$dir/before-$i.bin" bs=4M iflag=fullblock conv=fsync status=none
done
for i in $(seq -w 1 2 63); do rm "$dir/before-$i.bin"; done
rm -f "$mnt/fill.bin"
sync
btrfs balance start -dusage=0 "$mnt" >/dev/null
sync
btrfs filesystem df "$mnt"
`,
      ),
    );

    // The disk drops out, and the boot check mounts the pool degraded after
    // its grace, as at a boot without it, and records the episode.
    await unmountPool();
    await pullDisk(stale);
    await vmSsh(
      `sudo btrfs device scan --forget && sudo systemctl restart ${DEGRADED_SERVICE}`,
    );
    expect(await vmSsh(`findmnt -no UUID,OPTIONS ${POOL_MOUNT}`)).toMatch(
      new RegExp(`^${uuid}\\s+\\S*\\bdegraded\\b`),
    );
    expect(await poolState(uuid)).toMatchObject({
      degraded: true,
      present: [goodDevid],
    });
    const written = await vmSsh(
      "sudo bash -s",
      `set -euo pipefail
data=/sys/fs/btrfs/${uuid}/allocation/data
free=$(( $(cat $data/total_bytes) - $(cat $data/bytes_used) ))
n=$(( free / ${16 * MiB} + 8 ))
for i in $(seq -w 1 $n); do
  head -c 16M /dev/urandom | dd of="${RESYNC_DIR}/during-$i.bin" bs=4M iflag=fullblock conv=fsync status=none
done
sync
echo "$free bytes free in the data chunks; $n files of 16 MiB written"
btrfs filesystem df ${POOL_MOUNT}
`,
    );
    console.log(written);
    expect(
      written,
      "the writes without the disk filled the holes, then new chunks",
    ).toMatch(/^Data, single:/m);
    const hashes = await hashFiles();
    expect(
      hashes.trim().split("\n"),
      "a hash of each file written",
    ).toHaveLength(32 + Number(/(\d+) files of 16 MiB/.exec(written)?.[1]));
    expect(hashes).toMatch(/^([0-9a-f]{64} {2}\S+\.bin\n)+$/);

    // The disk is back; the pool is mounted whole, as at the next boot.
    await unmountPool();
    await rescanFor(stale, staleUuid);
    await vmSsh(`findmnt ${POOL_MOUNT} >/dev/null || sudo mount ${POOL_MOUNT}`);
    expect(await poolDisks()).toEqual(members);
    expect(await vmSsh(`findmnt -no OPTIONS ${POOL_MOUNT}`)).not.toMatch(
      /\bdegraded\b/,
    );

    const used = await usedBytes(uuid);
    // A whole scrub then takes about 200 s; the scrub saves every minute,
    // so its second save holds more than half.
    const rate = Math.max(4 * MiB, Math.ceil(used / 2 / 200));
    await vmSsh(
      `for f in /sys/fs/btrfs/${uuid}/devinfo/*/scrub_speed_max; do echo ${rate} | sudo tee $f >/dev/null; done && ` +
        `sudo systemctl start ${HEALTH_SERVICE}`,
    );
    const cut: { saved?: ScrubRecord } = {};
    await expect
      .poll(
        async () => {
          const run = (await poolState(uuid))?.resyncRun;
          const rec = await scrubRecord(uuid);
          if (!run || rec?.run !== run || rec.status !== "running") {
            return false;
          }
          cut.saved = rec;
          return (
            recordedBytes(rec) >= 0.55 * used && recordedCorrections(rec) > 0
          );
        },
        {
          intervals: [2_000],
          message:
            "the resync's scrub saving more than half of its progress, with corrections",
          timeout: 420_000,
        },
      )
      .toBe(true);
    // The scrub record's writes are fsynced; this flushes the rest.
    await vmSsh("sync");
    expect(
      await scrubRunning(),
      "the resync's scrub ended before the power cut: lower its rate",
    ).toBe(true);
    expect((await resyncRuns()).map((r) => r.state)).toEqual(["running"]);
    const cutBoot = (await vmSsh("cat /proc/sys/kernel/random/boot_id")).trim();

    await rig(["power-cut"]);
    // The record as the cut left it: what the next run starts from.
    const saved = (await scrubRecord(uuid))!;
    expect(
      saved,
      "the record the cut left, read before the next run, two minutes after the boot",
    ).toMatchObject({
      bootId: cutBoot,
      run: cut.saved!.run,
      status: "running",
    });
    expect(recordedBytes(saved)).toBeGreaterThanOrEqual(
      recordedBytes(cut.saved!),
    );
    await mountedAtBoot(uuid);
    await expect
      .poll(
        async () =>
          (await resyncRuns()).map((r) => ({
            state: r.state,
            reason: r.reason,
            message: (r.result as { message?: string } | undefined)?.message,
          })),
        {
          intervals: [5_000],
          message: `${RESYNC} runs: the cut one, then the next`,
          timeout: 480_000,
        },
      )
      .toEqual([
        { state: "interrupted", reason: "reboot", message: undefined },
        { state: "completed", reason: undefined, message: "Resynced" },
      ]);
    const done = (await scrubRecord(uuid))!;
    const own = done.bytesScrubbed - recordedBytes(saved);
    const corrected = done.correctedErrors - recordedCorrections(saved);
    console.log(
      `${used} bytes in use; saved ${recordedBytes(saved)} with ${recordedCorrections(saved)} corrections before the cut, ` +
        `the next run read ${own} and corrected ${corrected}, ${done.bytesScrubbed} and ${done.correctedErrors} in all`,
    );
    expect(done).toMatchObject({
      run: saved.run,
      status: "finished",
      uncorrectableErrors: 0,
    });
    expect(Date.parse(done.startedAt)).toBe(Date.parse(saved.startedAt));
    expect(done.bytesScrubbed).toBeGreaterThanOrEqual(0.99 * used);
    // The cut lies past half, and the next run reads again only the dev
    // extent each device stopped in (256 MiB): starting over reads it all.
    expect(own, "the next run scrubbed the whole pool again").toBeLessThan(
      0.9 * used,
    );
    expect(
      corrected,
      "no stale copy lay past the cut, so a skipped one would go unseen",
    ).toBeGreaterThan(0);
    const state = await poolState(uuid);
    expect(state?.degraded).toBe(false);
    expect(state?.resyncRun).toBeUndefined();
    expect(await vmSsh(`sudo btrfs filesystem df ${POOL_MOUNT}`)).not.toMatch(
      /^(Data|Metadata|System), single:/m,
    );
    expect(await storedAlert(`btrfs/resync-${uuid}`)).toBeNull();
    const resynced = await storedAlert(`btrfs/resynced-${uuid}`);
    console.log(`btrfs/resynced-${uuid}: ${JSON.stringify(resynced)}`);
    expect(resynced).toMatchObject({
      message: expect.stringContaining(`corrected ${done.correctedErrors} `),
    });
    // The scrub counted the stale copies it rewrote as errors of the
    // returned disk; the resync that made the pool whole reset them there,
    // so no health alert is left and nobody resets them by hand.
    expect(await storedAlert(`btrfs/health-${uuid}`)).toBeNull();
    expect(await raisedCounters(stale), `${stale}'s error counters`).toEqual(
      [],
    );

    // The returned disk alone holds every file, and every copy on it checks.
    await holdHealthChecks();
    await unmountPool();
    await pullDisk("vdb");
    await vmSsh(
      `sudo btrfs device scan --forget && sudo mount -o degraded,ro /dev/${stale} ${POOL_MOUNT}`,
    );
    expect(
      await raisedCounters(stale),
      `${stale}'s error counters as the mount read them from the disk, the resync having committed their reset`,
    ).toEqual([]);
    expect(await hashFiles(), `${stale} alone holds every file`).toBe(hashes);
    const check = await vmSsh(
      `sudo btrfs scrub start -B -R -r /dev/${stale} 2>&1; echo "exit $?"`,
    );
    console.log(check);
    expect(check).toMatch(/^\s*csum_errors: \d+$/m);
    expect(check).toMatch(/^exit 0$/m);
    expect(
      [...check.matchAll(/^\s*(\w+_errors): (\d+)$/gm)]
        .filter(([, , count]) => count !== "0")
        .map(([, name, count]) => `${name} ${count}`),
      `the scrub of ${stale} alone`,
    ).toEqual([]);
    expect(
      Number(/^\s*data_bytes_scrubbed: (\d+)$/m.exec(check)?.[1]),
    ).toBeGreaterThan(256 * MiB);
    await unmountPool();
    await rescanFor("vdb", goodUuid);
    await vmSsh(
      `sudo btrfs device scan >/dev/null && { findmnt ${POOL_MOUNT} >/dev/null || sudo mount ${POOL_MOUNT}; }`,
    );
    expect(await poolDisks()).toEqual(members);
  } catch (error) {
    console.log(
      await vmSsh(
        `sudo journalctl -b -o short-monotonic --no-pager -u ${HEALTH_SERVICE} -u ${unit} -u ${DEGRADED_SERVICE} | tail -n 60`,
      ).catch((e: unknown) => String(e)),
    );
    throw error;
  } finally {
    // Every disk back, the pool mounted whole as the later checks want it,
    // with its fill.bin, and the health checks running again. A run that
    // failed before the resync reset the returned disk's error counters
    // leaves them raised, which the next run's episode would keep as the
    // disk's own; they are reset here, a no-op after a run that passed. A
    // resync left running, or one that health check starts after a failure
    // while the pool still reads degraded, ends before the later checks
    // scrub. A cleanup that fails is logged, so it does not hide the check's
    // own failure.
    await vmSsh(
      "sudo bash -s",
      `mnt=${POOL_MOUNT}
echo 1 >/sys/bus/pci/rescan
udevadm settle
for f in /sys/fs/btrfs/${uuid}/devinfo/*/scrub_speed_max; do echo 0 >"$f"; done
if findmnt -no OPTIONS "$mnt" | grep -Eq '(^|,)(ro|degraded)(,|$)'; then umount "$mnt"; fi
btrfs device scan >/dev/null
findmnt "$mnt" >/dev/null || mount "$mnt"
if [ "$(findmnt -no UUID "$mnt")" = ${uuid} ]; then
  rm -rf ${RESYNC_DIR}
  [ -e "$mnt/fill.bin" ] || head -c 1G /dev/urandom >"$mnt/fill.bin"
  btrfs device stats -z "$mnt" >/dev/null
  sync
fi
systemctl start ${HEALTH_TIMER} ${HEALTH_SERVICE}
for i in $(seq 150); do
  case "$(systemctl show -P ActiveState ${unit})" in
  active | activating | deactivating) sleep 2 ;;
  *) break ;;
  esac
done
`,
    ).catch((e: unknown) => console.log(`cleanup failed: ${String(e)}`));
  }
});

test("a pool create whose mount fails removes its partitions and superblocks", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const name = "e2e-rollback";
  const mountpoint = `/mnt/${name}`;
  const disk = "/dev/vdd";
  // The btrfs superblock copies from 64 KiB to 80 MiB (a partition at 1 MiB
  // has two there), read past the page cache. Direct reads below 64 KiB of
  // a mounted pool disk fail with EINVAL, and hold no superblock.
  const magics = (device: string) =>
    vmSsh(
      `sudo dd if=${device} iflag=direct bs=64K skip=1 count=1279 status=none | grep -a -c -F _BHRfS_M || true`,
    ).then((n) => Number(n.trim()));
  expect(await vmSsh(`lsblk -rno NAME ${disk}`), `${disk} is blank`).toBe(
    "vdd\n",
  );
  expect(
    await magics("/dev/vdb"),
    "the scan finds a pool's superblocks",
  ).toBeGreaterThan(0);
  // A file where the mountpoint goes: mkfs succeeds, the mount fails.
  await vmSsh(`sudo rm -rf ${mountpoint} && sudo touch ${mountpoint}`);
  try {
    const before = await jobIds();
    await signIn(page);
    await page.goto("/storage");
    await page.getByRole("button", { name: "Create pool" }).click();
    const dialog = page.getByRole("dialog", { name: "Create pool" });
    await dialog.getByLabel("Name", { exact: true }).fill(name);
    await dialog.getByRole("checkbox", { name: /^\/dev\/vdd · / }).check();
    await dialog.getByLabel(/^Partition the whole disks/).check();
    await dialog.getByRole("button", { name: "Create pool" }).click();
    const failed = await waitForJob(POOL_CREATE, before, "failed");
    expect(failed.error?.message).toMatch(/mount/);
    expect(failed.error?.message).not.toMatch(/undo/);

    expect(
      await vmSsh(
        "sudo journalctl -b -u linuxio-jobs@0.service -o json | " +
          `jq -r 'select(.MESSAGE == "rolled back failed btrfs pool create") | [.LINUXIO_LABEL, .LINUXIO_PARTITIONS] | join(" ")'`,
      ),
    ).toMatch(new RegExp(`^"?${name}"? 1$`, "m"));
    expect(await vmSsh(`lsblk -rno NAME ${disk}`)).toBe("vdd\n");
    expect(await magics(disk)).toBe(0);
    expect(
      await vmSsh(`sudo blkid -p -o export ${disk} | grep '^TYPE=' || true`),
    ).toBe("");
    expect(
      await vmSsh(
        `sudo btrfs filesystem show 2>/dev/null | grep -c "Label: '${name}'" || true`,
      ),
    ).toBe("0\n");
  } finally {
    // The rollback keeps the partition table it made; the next run wants a blank disk.
    await vmSsh(`sudo rm -f ${mountpoint} && sudo wipefs -aq ${disk}`);
  }
});

test("killing the host mid Docker storage switch never leaves Docker stopped", async ({
  page,
}) => {
  test.setTimeout(300_000);
  const dataRoot = "/var/lib/docker-e2e";
  const backup = "/root/daemon.json.e2e";
  // A container that ignores SIGTERM takes 10 s to stop, so the job is
  // seen running before the containers are down.
  await vmSsh(
    `sudo docker rm -f e2e-slowstop >/dev/null 2>&1; sudo docker pull -q busybox:1.37 >/dev/null && ` +
      `sudo docker run -d --name e2e-slowstop busybox:1.37 sleep 3600 >/dev/null && ` +
      `sudo rm -f ${backup} && { ! sudo test -f /etc/docker/daemon.json || sudo cp -a /etc/docker/daemon.json ${backup}; }`,
  );
  const oldRoot = (
    await vmSsh("sudo docker info -f '{{.DockerRootDir}}'")
  ).trim();
  try {
    const before = await jobIds();
    await signIn(page);
    await page.goto("/settings?tab=docker");
    await page.getByRole("textbox", { name: "Data root" }).fill(dataRoot);
    await page
      .getByRole("button", { name: "Save", exact: true, disabled: false })
      .click();
    const confirm = page.getByRole("dialog", {
      name: "Stop all containers to change Docker storage?",
    });
    await confirm.getByRole("button", { name: "Change storage" }).click();
    const job = await waitForJob(DOCKER_STORAGE, before, "running");

    // Killed from inside the VM as soon as no container runs.
    await vmSsh(
      'for i in $(seq 600); do [ -z "$(sudo docker ps -q 2>/dev/null)" ] && break; sleep 0.1; done; ' +
        'sudo kill -9 "$(systemctl show -P MainPID linuxio-jobs@0.service)"',
    );
    expect(
      (await jobRecord(job.id))?.state,
      "the switch finished before the kill",
    ).not.toBe("completed");
    // The browser's bridge reconnects; the next host marks it crashed.
    await waitForJob(DOCKER_STORAGE, before, "interrupted", {
      reason: "crash",
      timeout: 90_000,
    });
    // systemd runs the restart the job queued, or Docker was never stopped.
    await expect
      .poll(
        async () =>
          (await vmSsh("systemctl is-active docker.service || true")).trim(),
        { message: "docker.service", timeout: 180_000 },
      )
      .toBe("active");
    const root = (
      await vmSsh("sudo docker info -f '{{.DockerRootDir}}'")
    ).trim();
    console.log(
      `Docker runs on ${root === oldRoot ? "the old" : "the new"} data root (${root})`,
    );
    expect([oldRoot, dataRoot]).toContain(root);
    expect(await alertOf(job.id)).toMatchObject({
      message: expect.stringContaining("(crash)"),
      title: `${DOCKER_STORAGE_LABEL} was interrupted`,
    });
    await page.reload();
    const tray = await openTray(page);
    await expect(
      tray
        .getByRole("listitem")
        .filter({ hasText: `${DOCKER_STORAGE_LABEL} was interrupted` })
        .first(),
    ).toBeVisible({ timeout: 30_000 });
  } finally {
    await vmSsh(
      `sudo rm -f /etc/systemd/system/docker.service.d/linuxio-data-root.conf && ` +
        `{ if sudo test -f ${backup}; then sudo mv ${backup} /etc/docker/daemon.json; else sudo rm -f /etc/docker/daemon.json; fi; } && ` +
        `sudo systemctl daemon-reload && sudo systemctl restart docker.service && ` +
        `sudo docker rm -f e2e-slowstop >/dev/null && sudo rm -rf ${dataRoot}`,
    );
  }
});
