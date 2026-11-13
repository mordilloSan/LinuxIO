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
  scrubRunning,
  signIn,
  signOut,
  signOutIfSignedIn,
  startScrub,
  throttle,
  trayJob,
  vmSsh,
  waitForJob,
} from "./vm";

// docs/jobs.md (Job lifecycle): the resumable storage routes across host
// stops, reboots, power cuts, crashes and cancels, the pool create rollback
// and the Docker storage switch. Jobs start in the browser; the host events go
// through packaging/vm/linuxio-vm.sh. Each check fails, rather than passing
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

/** The job host's alert for job id, if it raised one. */
async function alertOf(id: string) {
  const out = await vmSsh(
    `sudo jq -c '[.alerts[] | select(.id == "jobs/${id}")][0]' /var/lib/linuxio/alerts.json`,
  );
  return JSON.parse(out) as {
    title: string;
    message: string;
    dismissed_at?: string;
  } | null;
}

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

test("a scrub cut by a power loss is scrubbed again in full at boot", async ({
  page,
}) => {
  test.setTimeout(420_000);
  const before = await jobIds();
  const { uuid, job } = await startScrub(page, before, 4 * MiB);
  await page.close();
  // A job's first record write is not fsynced (docs/jobs.md, Store and
  // records).
  await vmSsh("sync");
  expect(
    await scrubRunning(),
    "the scrub ended before the power cut: raise the data size",
  ).toBe(true);

  await rig(["power-cut"]);
  const done = await waitForJob(SCRUB, before, "completed", {
    timeout: 300_000,
  });
  expect(await loggedJobs("job marked interrupted")).toContainEqual({
    id: job.id,
    reason: "reboot",
  });
  expect((await loggedJobs("job resumed")).map((j) => j.id)).toContain(job.id);
  // No Suspend ran, so no checkpoint: every device is scrubbed from 0.
  expect(done.checkpoint).toBeUndefined();
  const used = (
    await vmSsh(
      `cat /sys/fs/btrfs/${uuid}/allocation/data/disk_used /sys/fs/btrfs/${uuid}/allocation/metadata/disk_used`,
    )
  )
    .split("\n")
    .filter(Boolean)
    .reduce((sum, n) => sum + Number(n), 0);
  const { scrub } = done.result as { scrub: { bytesScrubbed: number } };
  console.log(`full rescan: ${scrub.bytesScrubbed} of ${used} bytes in use`);
  expect(scrub.bytesScrubbed).toBeGreaterThanOrEqual(0.99 * used);
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
  const { job } = await startScrub(page, before, 4 * MiB);
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
  const { job } = await startScrub(page, before, 4 * MiB);
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
