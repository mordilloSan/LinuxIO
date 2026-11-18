import { expect, test, type Page } from "@playwright/test";

import {
  clickInScheduleRow,
  createSchedule,
  enableSchedule,
  runNow,
  runsOf,
  type ScheduledRecord,
  scheduledOutput,
  signIn,
  signOutIfSignedIn,
  uniqueName,
  unitOf,
  vmSsh,
  waitForIdle,
  waitForRun,
} from "./vm";

// docs/scheduled-scripts.md, Status and logs: what LinuxIO shows for a
// scheduled script once journald no longer holds a run's lines. The journal's
// own retention takes days; here every journal file is archived and then
// removed, which takes the same lines at once. The schedule is enabled, with
// its calendar of 2099, as a real one would be: its timer keeps the service
// loaded, so systemd still names the latest run's invocation.

const STORE = "/var/lib/linuxio/jobs/scheduled";
const PROGRESS = "/run/linuxio/jobs/scheduled";

// The schedule this file made, until it is deleted, so that a failed check
// still removes it.
let made: { name: string; id: string } | undefined;

test.afterEach(async ({ page }) => {
  if (made && !page.isClosed()) {
    await vmSsh(`sudo systemctl stop ${unitOf(made.id)}`);
    await removeSchedule(page, made.name, made.id);
  }
  await signOutIfSignedIn(page);
});

/** The run's systemd invocation ID, as its record holds it. */
function invocationOf(run: ScheduledRecord) {
  const invocation = run.executor.invocation_id ?? "";
  expect(invocation, "the run's invocation ID").toMatch(/^[0-9a-f]{32}$/);
  return invocation;
}

/**
 * The messages the journal holds for invocation, on the three fields the Logs
 * page matches it on: the run's own processes, and systemd's lines about it.
 */
const journalOf = (invocation: string) =>
  vmSsh(
    `sudo journalctl -q --no-pager -o cat _SYSTEMD_INVOCATION_ID=${invocation} + INVOCATION_ID=${invocation} + OBJECT_SYSTEMD_INVOCATION_ID=${invocation}`,
  );

/**
 * Archives the active journal files and removes every archived one, as the
 * journal's retention removes old ones.
 */
const vacuumJournal = () =>
  vmSsh(
    "sudo journalctl --rotate && sleep 2 && sudo journalctl --vacuum-time=1s",
  );

/** The lines of run id's saved output without their journal prefix. */
const outputText = async (id: string) =>
  (await scheduledOutput(id))
    .toString("utf8")
    .split("\n")
    .filter(Boolean)
    .map((line) => line.replace(/^\S+ \S+ linuxio-schedule\[\d+\]: /, ""));

/** The open schedule's view of the latest run's journal lines. */
const latestOutput = (page: Page) =>
  page
    .locator(".frosted-card")
    .filter({ hasText: "Output of the latest run" })
    .last()
    .locator(".log-viewport__box");

/** The open schedule's past runs, from their records. */
const pastRuns = (page: Page) =>
  page
    .locator("div")
    .filter({ has: page.getByRole("heading", { name: "Past runs" }) })
    .last();

/**
 * Opens schedule id and follows its link to the Logs page, and returns the
 * scope the link gave.
 */
async function followLogsLink(
  page: Page,
  id: string,
  link: "Logs" | "Latest run logs",
) {
  await page.goto(`/services/schedules?schedule=${id}`);
  await page.getByRole("button", { name: link, exact: true }).click();
  await expect(page).toHaveURL(/\/logs\?/);
  const search = new URL(page.url()).searchParams;
  return { unit: search.get("unit"), invocation: search.get("invocationId") };
}

const logsTable = (page: Page) =>
  page.getByRole("table", { name: "General logs" });

/** A line the script printed, in the Logs page's table. */
const scriptLine = (page: Page, text: string) =>
  logsTable(page)
    .locator(".log-line")
    .filter({ hasText: new RegExp(`^${text}(\\s|$)`) });

const logsCount = (page: Page) => page.getByText(/^\d+ shown/);

/** The Logs page's line naming its scope. */
const logsScope = (unit: string, invocation?: string) =>
  `${unit} · ${invocation ? `invocation ${invocation}` : "all runs"} — logs are limited to the host’s journal retention.`;

/**
 * What the Logs page shows for a scope the journal holds nothing of: the
 * scope, no line, and an empty table that says neither that the script
 * succeeded nor that it failed.
 */
async function expectNoLines(page: Page, scope: string) {
  await expect(page.getByText(scope, { exact: true })).toBeVisible();
  await expect(logsTable(page)).toContainText(
    "No logs match the current filters.",
    { timeout: 30_000 },
  );
  await expect(logsCount(page)).toHaveText("0 shown");
  await expect(logsTable(page)).not.toContainText(
    /succe|complet|fail|error|exit/i,
  );
  await expect(page.getByRole("alert")).toHaveCount(0);
}

/**
 * Deletes schedule id on the Schedules page, then the records and saved
 * output of its runs, which a delete keeps.
 */
async function removeSchedule(page: Page, name: string, id: string) {
  await page.goto("/services/schedules");
  await clickInScheduleRow(page, name, (row) =>
    row.getByRole("button", { name: "Delete", exact: true }),
  );
  const confirm = page
    .getByRole("dialog")
    .filter({ hasText: "Delete scheduled task" });
  await confirm.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(confirm).toBeHidden({ timeout: 30_000 });
  made = undefined;
  expect(
    await vmSsh(
      `sudo test -e /etc/linuxio/schedules/definitions/${id}.json && echo kept || echo gone`,
    ),
    "the schedule's definition",
  ).toBe("gone\n");
  const files = (await runsOf(id)).flatMap((run) => [
    `${STORE}/${run.id}.json`,
    `${STORE}/${run.id}.log`,
    `${PROGRESS}/${run.id}.progress`,
  ]);
  await vmSsh(`sudo rm -f ${files.join(" ")}`);
  expect(await runsOf(id), "the schedule's runs").toEqual([]);
}

test("a run whose journal lines are gone keeps its record and output, and the Logs page shows none", async ({
  page,
}) => {
  test.setTimeout(300_000);
  await signIn(page);
  const name = uniqueName("journal");
  // Long enough for the second run to be vacuumed while it runs.
  const id = await createSchedule(
    page,
    name,
    "echo before; sleep 20; echo after",
  );
  made = { name, id };
  const unit = unitOf(id);
  await enableSchedule(page, name, id);

  // 1. A run that ends with known output, in the journal and saved.
  await runNow(page, name);
  const run = await waitForRun(id, "completed", { timeout: 90_000 });
  await waitForIdle(unit);
  const invocation = invocationOf(run);
  expect(await outputText(run.id)).toEqual(["before", "after"]);
  expect(await journalOf(invocation)).toContain("before\nafter\n");
  // The page reads the run from the journal while the journal has it.
  await page.goto(`/services/schedules?schedule=${id}`);
  await expect(latestOutput(page)).toContainText(/: after\b/, {
    timeout: 15_000,
  });
  expect(await followLogsLink(page, id, "Logs")).toEqual({
    unit,
    invocation: null,
  });
  await expect(page.getByText(logsScope(unit), { exact: true })).toBeVisible();
  await expect(scriptLine(page, "after")).toBeVisible({ timeout: 30_000 });
  expect(await followLogsLink(page, id, "Latest run logs")).toEqual({
    unit,
    invocation,
  });
  await expect(
    page.getByText(logsScope(unit, invocation), { exact: true }),
  ).toBeVisible();
  await expect(scriptLine(page, "after")).toBeVisible({ timeout: 30_000 });

  // 2. The journal no longer holds any line of the run, nor of the unit.
  await vacuumJournal();
  expect(await journalOf(invocation), "the run's journal lines").toBe("");
  expect(
    await vmSsh(`sudo journalctl -q --no-pager -o cat -u ${unit}`),
    "the unit's journal lines",
  ).toBe("");

  // 3. The record still reads as the run ended, and the output it saved
  // still holds the lines. The view of the latest run's journal lines, and
  // systemd's state beside it, are what they say: no lines, and success.
  expect((await runsOf(id)).at(-1)).toMatchObject({
    id: run.id,
    state: "completed",
  });
  expect(await outputText(run.id)).toEqual(["before", "after"]);
  await page.goto(`/services/schedules?schedule=${id}`);
  await expect(page.getByText(/^success · next /)).toBeVisible({
    timeout: 15_000,
  });
  await expect(latestOutput(page)).toHaveText("No logs available.", {
    timeout: 15_000,
  });
  const runs = pastRuns(page);
  const pastRun = runs.getByRole("button", { name: / · Completed( · |$)/ });
  await expect(pastRun).toHaveCount(1, { timeout: 15_000 });
  await pastRun.click();
  const saved = runs.locator(".log-viewport__box");
  await expect(saved).toContainText(/linuxio-schedule\[\d+\]: before\b/);
  await expect(saved).toContainText(/linuxio-schedule\[\d+\]: after\b/);

  // 4. The page's links to the Logs page, for the unit and for the run.
  expect(await followLogsLink(page, id, "Logs")).toEqual({
    unit,
    invocation: null,
  });
  await expectNoLines(page, logsScope(unit));
  expect(await followLogsLink(page, id, "Latest run logs")).toEqual({
    unit,
    invocation,
  });
  await expectNoLines(page, logsScope(unit, invocation));

  // 5. A run during which the journal is vacuumed: the page's live view keeps
  // following it, and it ends recorded as completed.
  await page.goto(`/services/schedules?schedule=${id}`);
  await runNow(page, name);
  const second = await waitForRun(id, "running", { count: 2 });
  const secondInvocation = invocationOf(second);
  const live = latestOutput(page);
  await expect(live).toContainText(/: before\b/, { timeout: 15_000 });
  expect(await journalOf(secondInvocation)).toContain("before\n");
  await vacuumJournal();
  expect(
    await journalOf(secondInvocation),
    "the run's journal lines so far",
  ).not.toContain("before");
  expect(
    (await runsOf(id)).at(-1),
    "the run once the journal was vacuumed",
  ).toMatchObject({ id: second.id, state: "running" });
  await expect(live).toContainText(/: after\b/, { timeout: 40_000 });
  await expect(live).toContainText(/: before\b/);
  const ended = await waitForRun(id, "completed", {
    count: 2,
    timeout: 60_000,
  });
  expect(ended.id).toBe(second.id);
  await waitForIdle(unit);
  // The output is saved from the journal when the run ends, so the line the
  // vacuum took is not in it.
  expect(await outputText(second.id)).toEqual(["after"]);

  // 6. The schedule, and the records and output of its runs, go.
  await removeSchedule(page, name, id);
});
