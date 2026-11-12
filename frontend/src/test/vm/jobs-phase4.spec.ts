import { expect, test, type Locator } from "@playwright/test";

import {
  createSchedule,
  ensurePool,
  MiB,
  openTray,
  POOL_MOUNT,
  rig,
  runNow,
  runsOf,
  SCHEDULED_SCRIPT,
  type ScheduledRecord,
  scheduledOutput,
  scheduledRecords,
  signIn,
  signOut,
  signOutIfSignedIn,
  stopAptRuns,
  trayJob,
  uniqueName,
  unitOf,
  vmSsh,
  waitForIdle,
  waitForRun,
} from "./vm";

// docs/jobs.md (Scheduled runs): work a systemd timer runs
// writes its own record through two hooks in packaged drop-ins, shows in the
// bell while it runs and keeps a copy of its output. Each script is a schedule
// created on the Schedules page, disabled and with a calendar of 2099, and
// started with "Run now"; the snapshot and Docker update units are started
// over SSH, as their timers would.

const SNAPSHOT = "scheduled.btrfs_snapshot";
const DOCKER_UPDATE = "scheduled.docker_update";
const SYSTEM_UPDATE = "scheduled.system_update";
const SYSTEM_UPDATE_UNIT = "apt-daily-upgrade.service";
const SNAPSHOT_UNIT = "linuxio-btrfs-snapshot.service";
const DOCKER_UPDATE_UNIT = "linuxio-docker-update.service";
const SCRIPT_DROPIN = "linuxio-schedule-.service.d";
/**
 * A saved output line: the journal's short-iso time, host, identifier and PID.
 * systemd 259 writes the offset as +00:00; +0000 is accepted too.
 */
const outputLine = (text: string) =>
  new RegExp(
    `^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}[+-]\\d{2}:?\\d{2} \\S+ linuxio-schedule\\[\\d+\\]: ${text}$`,
  );

test.afterEach(({ page }) => signOutIfSignedIn(page));

/** Polls until the one run of route not in before reaches state, and returns it. */
async function waitForNewRun(
  route: string,
  before: Set<string>,
  state: string,
): Promise<ScheduledRecord> {
  let fresh: ScheduledRecord[] = [];
  await expect
    .poll(
      async () => {
        fresh = (await scheduledRecords(route)).filter(
          (r) => !before.has(r.id),
        );
        return fresh.map((r) => r.state);
      },
      { intervals: [1_000], message: `${route} ${state}`, timeout: 120_000 },
    )
    .toEqual([state]);
  return fresh[0];
}

const recordIds = async (route: string) =>
  new Set((await scheduledRecords(route)).map((r) => r.id));

const unitProperty = async (unit: string, property: string) =>
  (await vmSsh(`systemctl show -p ${property} --value ${unit}`)).trim();

/** The alert raised for scheduled run id, if one was. */
async function alertOf(id: string) {
  const out = await vmSsh(
    `sudo jq -c '[.alerts[] | select(.id == "jobs/${id}")][0]' /var/lib/linuxio/alerts.json`,
  );
  return JSON.parse(out) as { title: string; message: string } | null;
}

const trayAlert = (tray: Locator, title: string) =>
  tray.getByRole("listitem").filter({ hasText: title }).first();

/** The saved output of run id as text lines, without the final newline. */
const outputLines = async (id: string) =>
  (await scheduledOutput(id)).toString("utf8").replace(/\n$/, "").split("\n");

test("a drop-in alone creates no unit", async () => {
  for (const dir of [
    SCRIPT_DROPIN,
    `${SNAPSHOT_UNIT}.d`,
    `${DOCKER_UPDATE_UNIT}.d`,
  ]) {
    expect(
      await vmSsh(
        `test -f /etc/systemd/system/${dir}/10-linuxio-run.conf && echo installed || true`,
      ),
      dir,
    ).toBe("installed\n");
  }
  // The rest means something only on a VM where no snapshot rule and no
  // Docker update setting was ever saved: saving one writes the unit file.
  const written = (
    await vmSsh(
      `for u in ${SNAPSHOT_UNIT} ${DOCKER_UPDATE_UNIT}; do test -e /etc/systemd/system/$u && echo $u; done; true`,
    )
  ).trim();
  test.skip(
    written !== "",
    `${written.split("\n").join(" and ")} already written on this VM, so a missing unit cannot be checked; it needs a VM where none was (make vm-reset)`,
  );
  expect(await unitProperty(SNAPSHOT_UNIT, "LoadState")).toBe("not-found");
  expect(await unitProperty(DOCKER_UPDATE_UNIT, "LoadState")).toBe("not-found");
});

test("a running script shows in the bell and on its page, and leaves its output", async ({
  page,
}) => {
  test.setTimeout(180_000);
  await signIn(page);
  const name = uniqueName("output");
  const id = await createSchedule(
    page,
    name,
    "echo first; sleep 60; echo last >&2",
  );
  await runNow(page, name);
  const run = await waitForRun(id, "running");
  const output = page.locator(".log-viewport__box");
  await expect(output).toContainText(/: first\b/, { timeout: 15_000 });

  // Away from its page, the bell's row leads back to it.
  await page.goto("/");
  const tray = await openTray(page);
  const row = trayJob(tray, name);
  await expect(row).toBeVisible({ timeout: 15_000 });
  await expect(row.getByRole("button", { name: "Cancel task" })).toHaveCount(0);
  await row.getByRole("link", { name: `Open ${name}` }).click();
  await expect(page).toHaveURL(
    new RegExp(`/services/schedules\\?schedule=${id}$`),
  );
  await expect(output).toContainText(/: first\b/, { timeout: 15_000 });
  expect(
    (await runsOf(id)).at(-1)?.state,
    "the run ended before the bell was checked",
  ).toBe("running");

  const done = await waitForRun(id, "completed", { timeout: 90_000 });
  expect(done).toMatchObject({ id: run.id, route: SCHEDULED_SCRIPT });
  await waitForIdle(unitOf(id));
  const lines = await outputLines(run.id);
  expect(lines).toHaveLength(2);
  expect(lines[0]).toMatch(outputLine("first"));
  expect(lines[1]).toMatch(outputLine("last"));
});

test("a failing script raises an alert", async ({ page }) => {
  test.setTimeout(120_000);
  await signIn(page);
  const name = uniqueName("failure");
  const id = await createSchedule(page, name, "echo boom; exit 3");
  await runNow(page, name);
  const run = await waitForRun(id, "failed");
  expect(run.error?.message).toBe("exited with status 3");
  await expect
    .poll(() => alertOf(run.id), { timeout: 15_000 })
    .toMatchObject({
      title: `${name} failed`,
      message: "exited with status 3",
    });
  // The bell lists alerts once a minute; a reload reads them now.
  await page.reload();
  const tray = await openTray(page);
  await expect(trayAlert(tray, `${name} failed`)).toBeVisible({
    timeout: 30_000,
  });
});

test("a script stopped on its page ends canceled", async ({ page }) => {
  test.setTimeout(180_000);
  await signIn(page);
  const name = uniqueName("stop");
  const id = await createSchedule(page, name, "sleep 300");
  await runNow(page, name);
  const run = await waitForRun(id, "running");
  await page
    .getByRole("button", { name: "Stop", exact: true })
    .click({ timeout: 15_000 });
  const ended = await waitForRun(id, "canceled");
  expect(ended.cancel_requested).toBe(true);
  await waitForIdle(unitOf(id));
  expect(await alertOf(run.id)).toBeNull();
});

test("a long output keeps its start and its end", async ({ page }) => {
  test.setTimeout(300_000);
  await signIn(page);
  const name = uniqueName("long output");
  // 300 numbered lines of 45009 bytes: 13.5 MB, past the 10 MiB kept. Bash
  // writes each line whole: journald starts a new entry when another process
  // writes to the stream mid-line (seen: `printf 'line 001 '; head | tr`
  // saved "line 001" and the x's as two lines).
  const id = await createSchedule(
    page,
    name,
    "x=$(head -c 45000 /dev/zero | tr '\\0' x); for i in $(seq 300); do printf 'line %03d %s\\n' \"$i\" \"$x\"; done",
  );
  await runNow(page, name);
  const run = await waitForRun(id, "completed", { timeout: 180_000 });
  await waitForIdle(unitOf(id));
  const output = await scheduledOutput(run.id);
  console.log(`saved output: ${output.length} bytes`);
  expect(output.length).toBeGreaterThanOrEqual(10 * MiB);
  expect(output.length).toBeLessThanOrEqual(10 * MiB + 64);
  const lines = output.toString("utf8").replace(/\n$/, "").split("\n");
  expect(lines.filter((l) => l.includes("bytes left out"))).toHaveLength(1);
  // The true first and last lines, whole and in that order, with the line
  // saying what was left out between them.
  const first = lines.findIndex((l) => l.includes(": line 001 "));
  const gap = lines.findIndex((l) => l.includes("bytes left out"));
  const last = lines.findIndex((l) => l.includes(": line 300 "));
  expect(first, "the first numbered line").toBe(0);
  expect(last, "the last numbered line").toBe(lines.length - 1);
  expect(lines[first]).toMatch(outputLine("line 001 x{45000}"));
  expect(lines[last]).toMatch(outputLine("line 300 x{45000}"));
  expect(first).toBeLessThan(gap);
  expect(gap).toBeLessThan(last);
});

test("the last line before the exit is saved, run after run", async ({
  page,
}) => {
  test.setTimeout(300_000);
  await signIn(page);
  const name = uniqueName("last line");
  const id = await createSchedule(page, name, "echo almost; echo last");
  for (let count = 1; count <= 10; count++) {
    await runNow(page, name);
    await waitForRun(id, "completed", { count });
    await waitForIdle(unitOf(id));
  }
  const runs = await runsOf(id);
  expect(runs).toHaveLength(10);
  for (const run of runs) {
    expect((await outputLines(run.id)).at(-1), run.id).toMatch(/: last$/);
  }
});

test("control characters in the output are saved as written", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await signIn(page);
  const name = uniqueName("control characters");
  const id = await createSchedule(
    page,
    name,
    "printf 'a\\rb\\033[31mred\\033[0m\\n'",
  );
  await runNow(page, name);
  const run = await waitForRun(id, "completed");
  await waitForIdle(unitOf(id));
  const output = await scheduledOutput(run.id);
  expect(output.includes("a\rb"), "carriage return").toBe(true);
  expect(output.includes("\x1b[31mred\x1b[0m"), "colour codes").toBe(true);
  expect(output.includes("blob data")).toBe(false);
});

test("a broken hook does not stop the run", async ({ page }) => {
  test.setTimeout(120_000);
  const cli = "/usr/local/bin/linuxio";
  const aside = `${cli}.e2e-aside`;
  await signIn(page);
  const name = uniqueName("broken hook");
  const id = await createSchedule(page, name, "touch /tmp/phase4-ran");
  await vmSsh(`sudo rm -f /tmp/phase4-ran && sudo mv ${cli} ${aside}`);
  try {
    await runNow(page, name);
    await expect
      .poll(
        async () =>
          (await vmSsh("test -f /tmp/phase4-ran && echo ran || true")).trim(),
        { message: "the script ran", timeout: 30_000 },
      )
      .toBe("ran");
    await waitForIdle(unitOf(id));
  } finally {
    await vmSsh(`if sudo test -e ${aside}; then sudo mv ${aside} ${cli}; fi`);
  }
  // systemd unloads a script's unit once it stops, and a unit loaded again
  // reads Result=success whatever happened, so its journal tells the outcome.
  const journal = () =>
    vmSsh(`sudo journalctl -b -o cat --no-pager -u ${unitOf(id)}`);
  await expect
    .poll(journal, { message: "the run's end in the journal" })
    .toMatch(/Deactivated successfully|Failed with result/);
  const lines = await journal();
  console.log(lines);
  expect(
    lines.match(/Unable to locate executable '\/usr\/local\/bin\/linuxio'/g),
    "both hooks failed to start",
  ).toHaveLength(2);
  expect(lines).toContain("Deactivated successfully");
  expect(lines).not.toContain("Failed with result");
  // Neither hook could run, so the run left no record.
  expect(await runsOf(id)).toEqual([]);
});

test("the snapshot unit records its run", async ({ page }) => {
  test.setTimeout(240_000);
  await signIn(page);
  const uuid = await ensurePool(page);
  await vmSsh(
    `sudo test -d ${POOL_MOUNT}/e2e-data || sudo btrfs subvolume create ${POOL_MOUNT}/e2e-data >/dev/null`,
  );
  await page.goto(`/storage/subvolumes?view=snapshots&fs=${uuid}`);
  const rule = page.getByLabel(/^Automatic snapshots of \/?e2e-data$/);
  // The switch shows the saved rule, so it turns on once the rules reload.
  if (!(await rule.isChecked())) await rule.click();
  await expect(rule).toBeChecked({ timeout: 15_000 });
  await expect
    .poll(() => unitProperty(SNAPSHOT_UNIT, "LoadState"), { timeout: 30_000 })
    .toBe("loaded");

  const before = await recordIds(SNAPSHOT);
  await vmSsh(`sudo systemctl start ${SNAPSHOT_UNIT}`);
  const run = await waitForNewRun(SNAPSHOT, before, "completed");
  expect(run.executor).toMatchObject({ kind: "unit", unit: SNAPSHOT_UNIT });
  expect(run.metadata).toEqual({ label: "Snapshots and replication" });
});

test("the Docker update unit records its run", async ({ page }) => {
  test.setTimeout(240_000);
  const docker = (
    await vmSsh("systemctl is-active docker.service || true")
  ).trim();
  test.skip(docker !== "active", `Docker is ${docker} in the VM`);
  await signIn(page);
  await page.goto("/settings?tab=docker");
  const enable = page.getByLabel("Enable scheduled update checks");
  const save = page.getByRole("button", {
    name: "Save",
    exact: true,
    disabled: false,
  });
  await enable.check();
  await save.click();
  await expect
    .poll(() => unitProperty(DOCKER_UPDATE_UNIT, "LoadState"), {
      timeout: 30_000,
    })
    .toBe("loaded");
  try {
    const before = await recordIds(DOCKER_UPDATE);
    await vmSsh(`sudo systemctl start ${DOCKER_UPDATE_UNIT}`);
    const run = await waitForNewRun(DOCKER_UPDATE, before, "completed");
    expect(run.executor).toMatchObject({
      kind: "unit",
      unit: DOCKER_UPDATE_UNIT,
    });
    expect(run.metadata).toEqual({ label: "Container updates" });
  } finally {
    await enable.uncheck();
    await save.click();
    await expect
      .poll(
        async () =>
          (
            await vmSsh(
              "systemctl is-enabled linuxio-docker-update.timer || true",
            )
          ).trim(),
        { timeout: 30_000 },
      )
      .not.toBe("enabled");
  }
});

// The distribution's own automatic update is hooked like LinuxIO's units, so
// its runs show in the history. The unit's command is replaced for the test
// by a runtime drop-in (a real run would install packages and take minutes);
// the hooks are the installed ones.
test("a system update records its run and shows in the history", async ({
  page,
}) => {
  test.setTimeout(240_000);
  const loaded = await unitProperty(SYSTEM_UPDATE_UNIT, "LoadState");
  test.skip(
    loaded !== "loaded",
    `${SYSTEM_UPDATE_UNIT} is ${loaded} in the VM`,
  );
  expect(
    await vmSsh(
      `test -f /etc/systemd/system/${SYSTEM_UPDATE_UNIT}.d/10-linuxio-run.conf && echo installed || true`,
    ),
    "the run-record drop-in",
  ).toBe("installed\n");
  await stopAptRuns();
  const overrideDir = `/run/systemd/system/${SYSTEM_UPDATE_UNIT}.d`;
  const override = `${overrideDir}/zz-e2e.conf`;
  // ExecStart alone is reset: resetting ExecStartPre would drop the begin hook.
  const stub = async (command: string) => {
    await vmSsh(
      `sudo mkdir -p ${overrideDir} && sudo tee ${override} >/dev/null && sudo systemctl daemon-reload`,
      `[Service]\nExecStart=\nExecStart=${command}\n`,
    );
  };
  /** The jobs alerts about run id, or a run alert of the unit, in the store. */
  const jobsAlerts = async (id: string) =>
    JSON.parse(
      await vmSsh(
        `sudo sh -c 'cat /var/lib/linuxio/alerts.json 2>/dev/null || echo "{}"' | jq -c '[(.alerts // [])[] | select(.source == "jobs" and (.key == "${id}" or .key == "run-system-update"))]'`,
      ),
    ) as unknown[];
  try {
    await stub("/bin/echo e2e upgraded nothing");
    let before = await recordIds(SYSTEM_UPDATE);
    await vmSsh(`sudo systemctl start ${SYSTEM_UPDATE_UNIT}`);
    const run = await waitForNewRun(SYSTEM_UPDATE, before, "completed");
    expect(run.executor).toMatchObject({
      kind: "unit",
      unit: SYSTEM_UPDATE_UNIT,
    });
    expect(run.metadata).toEqual({ label: "System updates" });
    // Saved as the journal prints it: time, host and process before the text.
    expect(await outputLines(run.id)).toContainEqual(
      expect.stringMatching(/: e2e upgraded nothing$/),
    );

    await signIn(page);
    await page.goto(`/history?job=${run.id}`);
    await expect(
      page.getByRole("row").filter({ hasText: "System updates" }).first(),
    ).toBeVisible({ timeout: 30_000 });

    // A failed run is recorded as failed and raises no jobs alert. This VM
    // has no notify policy saved, so the provider's hook is not installed
    // and the run-record hook reports the failed update itself.
    await vmSsh(
      `sudo rm -f /etc/systemd/system/${SYSTEM_UPDATE_UNIT}.d/linuxio-alert.conf && sudo systemctl daemon-reload`,
    );
    await stub("/bin/false");
    before = await recordIds(SYSTEM_UPDATE);
    await vmSsh(`sudo systemctl start ${SYSTEM_UPDATE_UNIT} || true`);
    const failed = await waitForNewRun(SYSTEM_UPDATE, before, "failed");
    expect(failed.error?.message).toBe("exited with status 1");
    expect(await jobsAlerts(failed.id), "jobs alerts").toEqual([]);
    const reported = JSON.parse(
      await vmSsh(
        `sudo sh -c 'cat /var/lib/linuxio/alerts.json 2>/dev/null || echo "{}"' | jq -c '[(.alerts // [])[] | select(.id == "auto-update/run")][0]'`,
      ),
    ) as { severity: string; title: string; message: string; link: string };
    expect(reported).toMatchObject({
      severity: "error",
      title: "Automatic updates failed",
      message: "Service result: exit-code. The run's output is in the history.",
      link: `/history?job=${failed.id}`,
      // The run's invocation ID, as the provider's hook would name it.
      occurrence_id: failed.executor.invocation_id,
      occurrence_count: 1,
    });
  } finally {
    await vmSsh(
      `sudo rm -f ${override} && sudo systemctl daemon-reload && sudo systemctl reset-failed ${SYSTEM_UPDATE_UNIT} || true`,
    );
  }
});

test("a script cut by a power loss is marked interrupted after the boot", async ({
  page,
}) => {
  test.setTimeout(420_000);
  await signIn(page);
  const name = uniqueName("power cut");
  const id = await createSchedule(page, name, "sleep 300");
  await runNow(page, name);
  const run = await waitForRun(id, "running");
  await page.close();
  // Begin fsyncs its record, but the page cache of everything else is flushed
  // too, so the boot after the cut is the same on every run.
  await vmSsh("sync");

  await rig(["power-cut"]);
  // The root job host, which starts at boot, marks a run of an earlier boot.
  const page2 = await page.context().newPage();
  await signIn(page2);
  const after = await waitForRun(id, "interrupted", { reason: "reboot" });
  expect(after.id).toBe(run.id);
  await expect
    .poll(() => alertOf(run.id), { timeout: 15_000 })
    .toMatchObject({ title: `${name} was interrupted` });
  await page2.reload();
  const tray = await openTray(page2);
  await expect(trayAlert(tray, `${name} was interrupted`)).toBeVisible({
    timeout: 30_000,
  });
  await signOut(page2);
});

test("a script stopped by an orderly reboot ends interrupted by the shutdown", async ({
  page,
}) => {
  test.setTimeout(420_000);
  await signIn(page);
  const name = uniqueName("reboot");
  const id = await createSchedule(page, name, "sleep 300");
  await runNow(page, name);
  const run = await waitForRun(id, "running");
  await signOut(page);
  await page.close();

  await rig(["reboot"]);
  console.log(
    await vmSsh(
      `sudo journalctl -b -1 -u ${unitOf(id)} -o short-iso --no-pager`,
    ),
  );
  // The end hook wrote this while the system stopped. Had it written
  // nothing, the root job host would have marked the run interrupted by the
  // reboot at boot; had it not seen the shutdown, the run would read failed.
  expect((await runsOf(id)).at(-1)).toMatchObject({
    id: run.id,
    reason: "shutdown",
    state: "interrupted",
  });
  expect(await alertOf(run.id)).toMatchObject({
    title: `${name} was interrupted`,
  });
});
