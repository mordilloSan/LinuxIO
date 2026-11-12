import { randomBytes } from "node:crypto";

import { expect, test, type Locator, type Page } from "@playwright/test";

import {
  bridgeCall,
  createSchedule,
  fillSchedule,
  jobIds,
  jobRecord,
  jobRecords,
  MiB,
  noSessions,
  openTray,
  rig,
  runNow,
  runsOf,
  SCRUB,
  type ScheduledRecord,
  scheduledOutput,
  scrubRunning,
  signIn,
  signOut,
  signOutIfSignedIn,
  startScrub,
  stopAptRuns,
  uniqueName,
  unitOf,
  vmSsh,
  waitForIdle,
  waitForJob,
  waitForRun,
} from "./vm";

// docs/jobs.md (Frontend, Updates): the history page
// the bell links to, each schedule's past runs, a saved output read in chunks,
// and the update banner. The scheduled scripts are schedules created on the
// Schedules page, disabled, with a calendar of 2099, and started with "Run
// now", as in jobs-phase4.spec.ts.
//
// The last check installs a second build over the first and leaves it there,
// with the apt timers stopped: run `make vm-reset` and `make vm-install`
// before the next run.

const COPY = "filebrowser.copy_batch";
const SCRUB_LABEL = "Scrubbing btrfs filesystem";
/** A user without sudo, created for the check that needs one. */
const PLAIN_USER = "e2e-plain";
/** What SavedOutput asks for at a time (frontend/src/components/tasks). */
const OUTPUT_CHUNK = 256 * 1024;

const vmUser = () => process.env.LINUXIO_VM_USER ?? "";

test.afterEach(({ page }) => signOutIfSignedIn(page));

/** The history page's rows, without its header. */
const historyRows = (page: Page) =>
  page
    .getByRole("table", { name: "Job history" })
    .getByRole("row")
    .filter({ has: page.getByRole("cell") });

/** Selects the history row of the job or run titled title. */
const selectHistoryRow = (page: Page, title: string) =>
  historyRows(page).getByText(title, { exact: true }).click();

/**
 * The numbers of the first and last `line NNN` of a saved output that the log
 * box shows without scrolling.
 */
const linesInView = (box: Locator) =>
  box.evaluate((el) => {
    const view = el.getBoundingClientRect();
    const seen: number[] = [];
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      for (const match of (node.textContent ?? "").matchAll(
        /: line (\d{3}) /g,
      )) {
        const range = document.createRange();
        range.setStart(node, match.index);
        range.setEnd(node, match.index + match[0].length);
        const rect = range.getBoundingClientRect();
        if (rect.bottom > view.top && rect.top < view.bottom) {
          seen.push(Number(match[1]));
        }
      }
    }
    // Both are undefined when no numbered line is in view.
    return { first: seen[0], last: seen[seen.length - 1] };
  });

test("a finished job shows in the history with its name", async ({ page }) => {
  test.setTimeout(300_000);
  const before = await jobIds();
  // Capped at 64 MiB/s per disk, the test pool's 1 GiB takes some 16 s to
  // scrub: a job that ends within a second leaves no record.
  const { job } = await startScrub(page, before, 64 * MiB);
  await waitForJob(SCRUB, before, "completed", { timeout: 120_000 });

  const tray = await openTray(page);
  await tray.getByRole("link", { name: "History", exact: true }).click();
  await expect(tray).toBeHidden();
  await expect(page).toHaveURL(/\/history$/);
  await expect(page.getByRole("table", { name: "Job history" })).toBeVisible();
  const first = historyRows(page).first();
  await expect(first).toBeVisible({ timeout: 15_000 });
  await expect(first).toContainText(SCRUB_LABEL);
  await expect(first).toContainText("Completed");
  await expect(first).toContainText(vmUser());
  // The first row is this scrub's job.
  await first.getByText(SCRUB_LABEL, { exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`[?&]job=${job.id}$`));
});

test("a failed scheduled script shows with its error and its output", async ({
  page,
}) => {
  test.setTimeout(180_000);
  await signIn(page);
  const name = uniqueName("failure");
  const id = await createSchedule(page, name, "echo boom; exit 3");
  await runNow(page, name);
  const run = await waitForRun(id, "failed", { anyReason: true });
  await waitForIdle(unitOf(id));

  // An admin's session is served the saved output, byte for byte; the check
  // of a user without sudo below is refused it.
  const saved = await scheduledOutput(run.id);
  expect(saved.toString("utf8")).toMatch(/: boom\n$/);
  expect(await bridgeCall(page, "tasks.output", { taskId: run.id })).toEqual({
    status: "ok",
    data: { content: saved.toString("utf8"), offset: 0, size: saved.length },
  });

  await page.goto("/history");
  const row = historyRows(page).filter({ hasText: name });
  await expect(row).toHaveCount(1, { timeout: 15_000 });
  await expect(row).toContainText("Failed");
  await selectHistoryRow(page, name);
  await expect(page).toHaveURL(new RegExp(`[?&]job=${run.id}$`));
  await expect(
    page.getByRole("alert").filter({ hasText: "exited with status 3" }),
  ).toBeVisible();
  await expect(page.locator(".log-viewport__box")).toContainText(": boom");
  await expect(
    page.getByRole("link", { name: "Open", exact: true }),
  ).toHaveAttribute(
    "href",
    new RegExp(`/services/schedules\\?schedule=${id}$`),
  );
});

test("a schedule lists its past runs", async ({ page }) => {
  test.setTimeout(240_000);
  await signIn(page);
  const name = uniqueName("past runs");
  // Each run prints its invocation ID, which its record's ID spells as a UUID.
  const id = await createSchedule(page, name, 'echo "run $INVOCATION_ID"');
  for (const count of [1, 2]) {
    await runNow(page, name);
    await waitForRun(id, "completed", { count, anyReason: true });
    await waitForIdle(unitOf(id));
  }
  const [older, newer] = await runsOf(id);
  const printed = (run: ScheduledRecord) =>
    `: run ${run.id.replaceAll("-", "")}`;
  expect((await scheduledOutput(older.id)).toString("utf8")).toContain(
    printed(older),
  );

  // Still on the schedule's page. Its past runs are listed newest first.
  const pastRuns = page
    .locator("div")
    .filter({
      has: page.getByRole("heading", { name: "Past runs", exact: true }),
    })
    .last();
  const runs = pastRuns.getByRole("button", { name: / · Completed/ });
  await expect(runs).toHaveCount(2, { timeout: 15_000 });
  await runs.nth(1).click();
  await expect(runs.nth(1)).toHaveAttribute("aria-pressed", "true");
  const output = pastRuns.locator(".log-viewport__box");
  await expect(output).toContainText(printed(older));
  await expect(output).not.toContainText(printed(newer));
});

test("a long output opens at its end and loads earlier parts", async ({
  page,
}) => {
  test.setTimeout(240_000);
  await signIn(page);
  const name = uniqueName("long output");
  // 600 numbered lines of 1000 characters, written whole by the shell (see
  // jobs-phase4.spec.ts): some 630 KB saved, more than two chunks.
  const id = await createSchedule(
    page,
    name,
    "x=$(head -c 991 /dev/zero | tr '\\0' x); for i in $(seq 600); do printf 'line %03d %s\\n' \"$i\" \"$x\"; done",
  );
  await runNow(page, name);
  const run = await waitForRun(id, "completed", {
    timeout: 120_000,
    anyReason: true,
  });
  await waitForIdle(unitOf(id));
  const size = (await scheduledOutput(run.id)).length;
  console.log(`saved output: ${size} bytes`);
  expect(size, "more than two chunks").toBeGreaterThan(2 * OUTPUT_CHUNK);

  // The run opened by its link (the row click is the checks' above).
  await page.goto(`/history?job=${run.id}`);
  await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();
  const box = page.locator(".log-viewport__box");
  await expect(box).toContainText(": line 600 ");
  expect(await box.textContent()).not.toContain(": line 001 ");
  const opened = await linesInView(box);
  console.log(`opened with lines ${opened.first} to ${opened.last} in view`);
  expect(opened.last, "the viewer opens at the last line").toBe(600);

  // Each earlier part joins above the text, which stays where it was in view.
  const earlier = page.getByRole("button", { name: "Show earlier" });
  await expect(earlier).toBeVisible();
  let presses = 0;
  while (await earlier.isVisible()) {
    expect(presses, "Show earlier stays after the start").toBeLessThan(10);
    const shown = (await box.textContent())!.length;
    const top = (await linesInView(box)).first;
    await earlier.click();
    presses++;
    await expect
      .poll(async () => (await box.textContent())!.length, {
        message: "an earlier part loaded",
      })
      .toBeGreaterThan(shown);
    const after = await linesInView(box);
    console.log(
      `press ${presses}: line ${top} was at the top; lines ${after.first} to ${after.last} in view`,
    );
    expect(top, "the line at the top before the press").toBeGreaterThanOrEqual(
      after.first,
    );
    expect(top, "the line at the top before the press").toBeLessThanOrEqual(
      after.last,
    );
  }
  expect(presses).toBe(Math.ceil(size / OUTPUT_CHUNK) - 1);
  // Every line once, whole and in order: no part lost or doubled where two
  // chunks meet.
  const lines = [
    ...(await box.textContent())!.matchAll(/: line (\d{3}) (x*)$/gm),
  ];
  expect(lines.map((m) => Number(m[1]))).toEqual(
    Array.from({ length: 600 }, (_, i) => i + 1),
  );
  expect(lines.filter((m) => m[2].length !== 991)).toEqual([]);
  await box.evaluate((el) => el.scrollTo({ top: 0, behavior: "instant" }));
  expect((await linesInView(box)).first, "line 1 at the top of the text").toBe(
    1,
  );
});

test("a user who is not an admin sees only their own jobs", async ({
  page,
}) => {
  test.setTimeout(300_000);
  // A scheduled run with a saved output, which only an admin may read.
  await signIn(page);
  const name = uniqueName("private");
  const id = await createSchedule(page, name, "echo private");
  await runNow(page, name);
  const run = await waitForRun(id, "completed", { anyReason: true });
  await waitForIdle(unitOf(id));
  expect((await scheduledOutput(run.id)).length).toBeGreaterThan(0);
  await signOut(page);

  // The VM is reset before each run; where the user exists already, it gets
  // a new password.
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
  const dir = `/home/${PLAIN_USER}/e2e-copy`;
  const host = `linuxio-jobs@${uid}.service`;
  // The user's job host reads at most 100 MB/s, so the 1 GiB copy takes
  // some 10 s: a job that ends within a second leaves no record.
  await vmSsh(
    `sudo -u ${PLAIN_USER} sh -c 'rm -rf ${dir} && mkdir -p ${dir}/dst && dd if=/dev/zero of=${dir}/big.bin bs=1M count=1024 status=none' && sync && ` +
      `echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null && ` +
      `sudo systemctl set-property --runtime ${host} 'IOReadBandwidthMax=/dev/vda 100M'`,
  );
  try {
    await signIn(page, user);
    const before = new Set((await jobRecords(COPY, uid)).map((r) => r.id));
    await page.goto(`/filebrowser${dir}`);
    await page.getByText("big.bin", { exact: true }).click();
    await page.keyboard.press("Control+c");
    await page.getByText("dst", { exact: true }).dblclick();
    await expect(page.getByText("This folder is empty.")).toBeVisible();
    await page.keyboard.press("Control+v");
    await expect
      .poll(
        async () =>
          (await jobRecords(COPY, uid))
            .filter((r) => !before.has(r.id))
            .map((r) => r.state),
        { intervals: [2_000], message: `${COPY} completed`, timeout: 120_000 },
      )
      .toEqual(["completed"]);
    expect(await bridgeCall(page, "tasks.output", { taskId: run.id })).toEqual(
      expect.objectContaining({ status: "error", code: 403 }),
    );

    await page.goto("/history");
    const rows = historyRows(page);
    await expect(rows).toHaveCount(1, { timeout: 15_000 });
    await expect(rows).toContainText("Copying big.bin");
    await expect(rows).toContainText("Completed");
    await expect(rows).toContainText(PLAIN_USER);
    await expect(page.getByText(name)).toHaveCount(0);
  } finally {
    await vmSsh(
      `sudo systemctl set-property --runtime ${host} IOReadBandwidthMax=`,
    );
  }
});

// The build in LINUXIO_VM_UPDATE_BUILD differs from the installed one only in
// jobs.Protocol, one higher: installed over it, it makes the running root job
// host an older one to every new bridge, without stopping it (installing over
// a running host is what an update does; docs/jobs.md, Updates). The test
// installs it with the rig's install, which `make vm-install` runs after its
// build. docs/jobs.md (Testing) gives the make command that builds it from
// this tree with a Go overlay of backend/common/jobs/record.go.
test("the update banner counts the jobs an update waits for", async ({
  page,
}) => {
  const build = process.env.LINUXIO_VM_UPDATE_BUILD ?? "";
  test.skip(
    build === "",
    "needs LINUXIO_VM_UPDATE_BUILD: a build directory of this tree with jobs.Protocol one higher (docs/jobs.md, Testing)",
  );
  // The bounds of the waits below: an apt run 600 s, the scrub's start (with
  // the pool on a fresh VM) about 120 s, the sessions 30 s, the install about
  // 60 s, the banner and the first refused job about 60 s, the reads 10 s,
  // the scrub's end 300 s, the second refused job 15 s, the two banner texts
  // 30 s and 150 s: some 1375 s.
  test.setTimeout(1_500_000);
  // After a dpkg run, apt runs needrestart, which restarted the job hosts
  // running a replaced binary and so interrupted their jobs (seen once in
  // this VM, during an unattended-upgrades run, on 2026-11-10;
  // docs/jobs.md, Updates). The installer's needrestart override
  // fixes that, and jobs-needrestart.spec.ts checks it. An apt run in the
  // middle of this check is still noise, so the apt timers stop until the
  // next boot and a run in progress is waited for.
  await stopAptRuns();
  const before = await jobIds();
  // 1 MiB/s per disk: the scrub outlasts the install and the checks of the
  // banner by minutes. The cap is lifted once they are done.
  const { uuid, job } = await startScrub(page, before, MiB);
  const speed = (bytesPerSecond: number) =>
    vmSsh(
      `for f in /sys/fs/btrfs/${uuid}/devinfo/*/scrub_speed_max; do echo ${bytesPerSecond} | sudo tee $f >/dev/null; done`,
    );
  const hostPid = async () =>
    (await vmSsh("systemctl show -P MainPID linuxio-jobs@0.service")).trim();
  try {
    const known = await jobIds();
    const olderHost = await hostPid();
    expect(olderHost, "the root job host runs").not.toBe("0");
    // A bridge still connected would keep the older host up after its jobs.
    await signOut(page);
    await noSessions();

    expect(
      await scrubRunning(),
      "the scrub ended before the update: lower its speed",
    ).toBe(true);
    console.log(await rig(["install", build]));
    expect(await hostPid(), "the install left the job host running").toBe(
      olderHost,
    );
    expect((await jobRecord(job.id))?.state).toBe("running");

    await signIn(page);
    const banner = page
      .getByRole("status")
      .filter({ hasText: "LinuxIO was updated." });
    await expect(banner).toContainText(
      "LinuxIO was updated. New jobs wait for 1 running job to finish.",
      { timeout: 30_000 },
    );
    // The older host refused the new bridge's hello on the protocol number.
    const refused = await vmSsh(
      "sudo journalctl -b -u linuxio-jobs@0.service -o cat --no-pager | grep -F 'job host refused a bridge hello' || true",
    );
    expect(refused).toMatch(/protocol \d+, want \d+/);
    const [, theirs, ours] = refused.match(/protocol (\d+), want (\d+)/) ?? [];
    expect(Number(theirs), refused).toBe(Number(ours) + 1);

    await banner.getByRole("button", { name: "Show running jobs" }).click();
    const listed = banner.getByRole("listitem");
    await expect(listed).toHaveCount(1);
    await expect(listed).toContainText(
      `${SCRUB_LABEL} · ${vmUser()} · started `,
    );
    console.log(`banner's job line: ${await listed.textContent()}`);
    // The scrub reports a percentage, which ends its line.
    await expect.soft(listed).toHaveText(/ · \d+%$/);
    await expect(
      banner.getByRole("button", { name: "Hide running jobs" }),
    ).toBeVisible();

    // A new job is refused with the count, and the toast drops the
    // `update_pending: ` of the error; nothing reached a job host.
    const refuse = async (toast: string) => {
      const dialog = await fillSchedule(page, uniqueName("refused"), "true");
      await dialog.getByRole("button", { name: "Save", exact: true }).click();
      await expect(page.getByText(toast, { exact: true })).toBeVisible({
        timeout: 15_000,
      });
      await page.keyboard.press("Escape");
      expect(await jobIds()).toEqual(known);
    };
    await refuse("LinuxIO update waits for 1 running job");

    // The reads that need the older host (docs/jobs.md, Updates): the
    // history answers empty at once, a job's own reads wait for a host that
    // does not come.
    expect
      .soft(await bridgeCall(page, "tasks.history", {}))
      .toEqual({ status: "ok", data: { entries: [], more: false } });
    expect
      .soft(await bridgeCall(page, "tasks.get", { taskId: job.id }))
      .toEqual(expect.objectContaining({ status: "error", code: 503 }));

    // The history page says it is unavailable rather than empty. It is a
    // second page of the signed-in session, so the first keeps the schedules
    // dialog the refusals above need.
    const historyPage = await page.context().newPage();
    await historyPage.goto("/history");
    await expect(
      historyPage.getByRole("table", { name: "Job history" }),
    ).toBeVisible();
    await expect(
      historyPage.getByText(
        "History is unavailable until the LinuxIO update finishes.",
      ),
    ).toBeVisible();
    await expect(historyPage.getByText("Nothing has ended yet.")).toHaveCount(
      0,
    );

    expect(await hostPid(), "the older host still runs").toBe(olderHost);
    expect(
      await scrubRunning(),
      "the scrub ended before the checks: lower its speed",
    ).toBe(true);
    await speed(0);
    await waitForJob(SCRUB, before, "completed", { timeout: 300_000 });
    await expect(banner).toContainText(
      "LinuxIO was updated. New jobs can start shortly.",
      { timeout: 30_000 },
    );
    // Within the older host's idle minute, a new job is refused without a
    // count.
    await refuse("LinuxIO was updated; new jobs can start shortly");
    expect(await hostPid(), "the older host refused it").toBe(olderHost);
    // The older host exits a minute after its last job and bridge (a refused
    // hello is no bridge); the relay tries again within 30 s (its backoff
    // ends at 30 s), and the banner asks every 15 s: about 110 s at most.
    await expect(banner).toBeHidden({ timeout: 150_000 });
    const newerHost = await hostPid();
    expect(newerHost, "a host of the new build runs").not.toBe("0");
    expect(newerHost).not.toBe(olderHost);
    // Without a reload, the history page loads the real history once the
    // banner's poll sees the wait end: the scrub's row replaces the sentence.
    await expect(
      historyRows(historyPage).filter({ hasText: SCRUB_LABEL }).first(),
    ).toBeVisible({ timeout: 30_000 });
    await expect(
      historyPage.getByText(
        "History is unavailable until the LinuxIO update finishes.",
      ),
    ).toHaveCount(0);
  } finally {
    await speed(0);
  }
});
