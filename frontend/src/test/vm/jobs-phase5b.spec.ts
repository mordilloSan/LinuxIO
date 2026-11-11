import { expect, test, type Locator, type Page } from "@playwright/test";

import {
  bridgeCall,
  createSchedule,
  openTray,
  rig,
  runNow,
  runsOf,
  scheduleRow,
  signIn,
  signOut,
  signOutIfSignedIn,
  stopAptRuns,
  uniqueName,
  unitOf,
  vmSsh,
  waitForIdle,
  waitForRun,
} from "./vm";

// Plan 5b of docs/TODO/jobs.md (section 4.5; docs/alerts.md): what each kind
// of work reports besides its failures, the grid of level by channel at
// delivery and in the bell, a schedule's own choice, and the notice at sign-in.
// The scheduled scripts are schedules created on the Schedules page, disabled,
// with a calendar of 2099, and started with "Run now", as in
// jobs-phase4.spec.ts.
//
// Each check starts from settings of its own: it deletes notifications.json
// (the defaults) or writes the file it needs, and marks every alert seen
// before it counts unseen ones, so one check failing leaves the next its own
// starting point.
//
// State left behind, for `make vm-reset` to undo: the schedules and alerts
// the checks create; notifications.json as the last check left it, and the
// notifications.json.unreadable a save over an unreadable file keeps; a webhook
// listener (e2e-webhook.socket and e2e-webhook.service, enabled, recording to
// /var/lib/e2e-webhook); the APT drop-in "Report failures" writes; two power
// cuts behind it.

const NOTIFICATIONS = "/var/lib/linuxio/notifications.json";
const ALERTS = "/var/lib/linuxio/alerts.json";
/** The listener runs in the VM and the URL is a loopback one: nothing leaves the VM. */
const HOOK_PORT = 18089;
const HOOK_URL = `http://127.0.0.1:${HOOK_PORT}/hook`;
const HOOK_RECORD = "/var/lib/e2e-webhook/posts.jsonl";

// The listener: systemd holds its socket from sockets.target on, so it takes
// posts as soon as the root job host can send any after a boot, and the
// service records the body of each POST as one JSON line, synced to disk
// before it answers, so a power cut loses none.
const LISTENER = `import http.server, os, socket, sys

record = sys.argv[1]


class Hook(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
        with open(record, "ab") as f:
            f.write(body.replace(b"\\n", b" ") + b"\\n")
            f.flush()
            os.fsync(f.fileno())
        self.send_response(204)
        self.end_headers()

    def log_message(self, *args):
        pass


server = http.server.HTTPServer(("127.0.0.1", ${HOOK_PORT}), Hook, bind_and_activate=False)
server.socket.close()
server.socket = socket.socket(fileno=3)
server.serve_forever()
`;
const LISTENER_SOCKET = `[Unit]
Description=Webhook listener for jobs-phase5b.spec.ts

[Socket]
ListenStream=127.0.0.1:${HOOK_PORT}

[Install]
WantedBy=sockets.target
`;
const LISTENER_SERVICE = `[Unit]
Description=Webhook listener for jobs-phase5b.spec.ts

[Service]
ExecStart=/usr/bin/python3 -I /usr/local/lib/e2e-webhook.py ${HOOK_RECORD}
StateDirectory=e2e-webhook
`;
const PROBE = `import json, sys, urllib.request

body = json.dumps({"title": sys.argv[2]}).encode()
request = urllib.request.Request(sys.argv[1], data=body, headers={"Content-Type": "application/json"})
urllib.request.urlopen(request, timeout=10)
`;

test.afterEach(({ page }) => signOutIfSignedIn(page));

interface StoredSettings {
  report: Record<string, string>;
  levels: Record<
    string,
    { browser: boolean; email: boolean; webhook: boolean }
  >;
  scripts?: Record<string, string> | null;
  minSeverity: string;
  dockerUpdates: string;
  webhookUrl: string;
}

/** The members of notifications.json the checks read; never the SMTP password. */
const storedSettings = async () =>
  JSON.parse(
    await vmSsh(
      `sudo jq -c '{report, levels, scripts, minSeverity, dockerUpdates, webhookUrl}' ${NOTIFICATIONS}`,
    ),
  ) as StoredSettings;

/** Back to the defaults: no settings file and no lock beside it. */
const resetNotifications = () =>
  vmSsh(`sudo rm -f ${NOTIFICATIONS} ${NOTIFICATIONS}.lock`);

/** The settings file and its lock are root's alone, whoever wrote them. */
async function expectRootOnly() {
  const modes = await vmSsh(
    `sudo stat -c '%n %a %U:%G' ${NOTIFICATIONS} ${NOTIFICATIONS}.lock`,
  );
  expect(modes.trim().split("\n")).toEqual([
    `${NOTIFICATIONS} 600 root:root`,
    `${NOTIFICATIONS}.lock 600 root:root`,
  ]);
}

interface StoredAlert {
  id: string;
  severity: string;
  title: string;
  message?: string;
  link?: string;
  occurrence_count: number;
  last_occurrence: string;
  seen?: Record<string, number>;
}

/** The record of alert id in the VM's alert store, if there is one. */
async function storedAlert(id: string) {
  const out = await vmSsh(
    `if sudo test -f ${ALERTS}; then sudo jq -c --arg id '${id}' '[.alerts[] | select(.id == $id)][0]' ${ALERTS}; else echo null; fi`,
  );
  return JSON.parse(out) as StoredAlert | null;
}

interface AlertList {
  alerts: {
    id: string;
    severity: string;
    title: string;
    seen: boolean;
    occurrenceCount: number;
  }[];
  unseen: number;
}

/** The signed-in session's list, as alerts.list answers the bell. */
async function alertList(page: Page): Promise<AlertList> {
  const answer = await bridgeCall(page, "alerts.list", {});
  expect(answer.status, answer.error).toBe("ok");
  return answer.data as AlertList;
}

const bell = (page: Page) =>
  page.getByRole("button", { name: "Notifications", exact: true });

const badge = (page: Page) => bell(page).locator(".navbar-notifications-badge");

/**
 * Marks every alert the bell lists seen the way a user does: by opening the
 * tray once the badge shows them. (alerts.mark_all_seen is a mutation, which
 * the bridge takes only with a job ID that bridgeCall does not send.)
 */
async function markAllSeen(page: Page) {
  const { unseen } = await alertList(page);
  if (unseen > 0) {
    // The bell lists alerts once a minute; a reload reads them now.
    await page.reload();
    await expect(badge(page)).toHaveText(String(unseen), { timeout: 30_000 });
    await openTray(page);
    await page.keyboard.press("Escape");
  }
  await expect
    .poll(async () => (await alertList(page)).unseen, {
      message: "every listed alert seen",
    })
    .toBe(0);
}

/** The tray's line of the alert titled title. */
const trayLine = (tray: Locator, title: string) =>
  tray
    .getByRole("listitem")
    .filter({ has: tray.page().getByText(title, { exact: true }) });

const vmUid = async () => (await vmSsh("id -u")).trim();

const vmNow = async () => (await vmSsh("date +%s")).trim();

/** The journal lines in range (such as `-b`) whose message contains text. */
async function journalLines(range: string, text: string) {
  const out = await vmSsh(
    `sudo journalctl ${range} -o json --no-pager | jq -c --arg t '${text}' ` +
      `'select((.MESSAGE // "") | tostring | contains($t)) | {unit: ._SYSTEMD_UNIT, message: .MESSAGE}'`,
  );
  return out.split("\n").filter(Boolean);
}

const unitProperty = async (unit: string, property: string) =>
  (await vmSsh(`systemctl show -p ${property} --value ${unit}`)).trim();

/**
 * The schedule's unit files, definition and script (inode, size, time and
 * content) and its timer's state, to compare before and after a save.
 */
const scheduleFiles = (id: string) =>
  vmSsh(
    `sudo sh -c 'for f in /etc/systemd/system/linuxio-schedule-${id}.service /etc/systemd/system/linuxio-schedule-${id}.timer /etc/linuxio/schedules/definitions/${id}.json /etc/linuxio/schedules/scripts/${id}-*.sh; do stat -c "%n %i %s %y" "$f" && sha256sum < "$f"; done' && ` +
      `systemctl show -p LoadState,ActiveState,SubState,UnitFileState linuxio-schedule-${id}.timer`,
  );

async function openNotificationSettings(page: Page) {
  await page.goto("/settings?tab=notifications");
  await expect(
    page.getByRole("region", { name: "What to hear about" }),
  ).toBeVisible({ timeout: 15_000 });
}

/** Picks option in the select labelled label within scope. */
async function choose(
  page: Page,
  scope: Page | Locator,
  label: string,
  option: string,
) {
  const select = scope.getByRole("combobox", { name: label, exact: true });
  await select.click();
  await page.getByRole("option", { name: option, exact: true }).click();
  await expect(select).toHaveText(option);
}

const box = (page: Page, name: string) =>
  page.getByRole("checkbox", { name, exact: true });

/** Presses the settings page's Save and waits for its answer. */
async function saveSettings(page: Page) {
  const save = page.getByRole("button", { name: "Save", exact: true });
  await save.click();
  await expect(
    page.getByText("Notification settings saved").last(),
  ).toBeVisible({ timeout: 15_000 });
  await expect(save).toBeDisabled();
}

/** Opens the schedule's dialog, picks choice under "Notify me" and saves. */
async function setNotifyChoice(page: Page, name: string, choice: string) {
  await scheduleRow(page, name)
    .getByRole("button", { name: "Edit", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "Edit scheduled task" });
  await choose(page, dialog, "Notify me", choice);
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(dialog).toBeHidden({ timeout: 30_000 });
}

/** Runs the script of schedule id once and waits until its unit has stopped. */
async function runOnce(
  page: Page,
  name: string,
  id: string,
  state: string,
  count = 1,
) {
  await runNow(page, name);
  const run = await waitForRun(id, state, { count });
  await waitForIdle(unitOf(id));
  return run;
}

interface Post {
  title: string;
  severity?: string;
  source?: string;
  message?: string;
  link?: string;
}

const webhookPosts = async () =>
  JSON.parse(
    await vmSsh(`sudo cat ${HOOK_RECORD} 2>/dev/null | jq -cs .`),
  ) as Post[];

const postsTitled = async (title: string) =>
  (await webhookPosts()).filter((post) => post.title === title);

/** Posts a probe to the listener and waits until it has recorded it. */
async function probeListener() {
  const title = uniqueName("probe");
  await vmSsh(`python3 -I - ${HOOK_URL} '${title}'`, PROBE);
  await expect
    .poll(async () => (await postsTitled(title)).length, {
      message: "the listener recorded the probe",
      timeout: 15_000,
    })
    .toBe(1);
}

/** Installs and starts the webhook listener (again, if it is there). */
async function startListener() {
  await vmSsh("sudo tee /usr/local/lib/e2e-webhook.py >/dev/null", LISTENER);
  await vmSsh(
    "sudo tee /etc/systemd/system/e2e-webhook.socket >/dev/null",
    LISTENER_SOCKET,
  );
  await vmSsh(
    "sudo tee /etc/systemd/system/e2e-webhook.service >/dev/null",
    LISTENER_SERVICE,
  );
  await vmSsh(
    "sudo systemctl daemon-reload && sudo systemctl enable --now e2e-webhook.socket",
  );
  await probeListener();
}

/**
 * Runs a `sleep 300` script, cuts the power while it runs and, signed in again
 * on a new page, waits until the root job host has marked the run interrupted
 * by the reboot. The interruption is then raised at boot by the job host,
 * which starts after sockets.target, so after the listener's socket exists; an
 * orderly reboot would raise it while units stop, the listener among them.
 */
async function interruptByPowerCut(page: Page, name: string) {
  const id = await createSchedule(page, name, "sleep 300");
  await runNow(page, name);
  const run = await waitForRun(id, "running");
  await page.close();
  await vmSsh("sync");
  await rig(["power-cut"]);
  // A fresh boot starts the apt timers again; an apt run in the middle of a
  // check restarts services (jobs-phase5a.spec.ts).
  await stopAptRuns();
  const next = await page.context().newPage();
  await signIn(next);
  await waitForRun(id, "interrupted", { reason: "reboot" });
  const key = `jobs/${run.id}`;
  await expect
    .poll(() => storedAlert(key), {
      message: "the interruption's alert",
      timeout: 30_000,
    })
    .toMatchObject({
      severity: "warning",
      title: `${name} was interrupted`,
      message: "The system stopped before the run finished.",
    });
  return { page: next, title: `${name} was interrupted` };
}

test("the grid is a table of nine boxes, and a save leaves the file root's alone", async ({
  page,
}) => {
  // A sign-in, one settings save and a few reads: 5 s in run 3.
  test.setTimeout(120_000);
  await resetNotifications();
  await signIn(page);
  await openNotificationSettings(page);

  const grid = page.getByRole("table", { name: "Channels for each level" });
  await expect(grid).toBeVisible();
  expect(await grid.evaluate((el) => getComputedStyle(el).display)).toBe(
    "table",
  );
  await expect(grid.getByRole("columnheader")).toHaveText([
    "Browser",
    "Email",
    "Webhook",
  ]);
  await expect(grid.getByRole("rowheader")).toHaveText([
    "Notices",
    "Warnings",
    "Alerts",
  ]);
  await expect(grid.getByRole("checkbox")).toHaveCount(9);

  // With no file, the defaults: every level in the browser, alerts also by
  // email and webhook.
  const rows = ["Notices", "Warnings", "Alerts"];
  const columns = ["in the browser", "by email", "by webhook"];
  const centres: { x: number; y: number }[][] = [];
  for (const row of rows) {
    const line: { x: number; y: number }[] = [];
    for (const column of columns) {
      const name = `${row} ${column}`;
      const checkbox = grid.getByRole("checkbox", { name, exact: true });
      await expect(checkbox, name).toBeChecked({
        checked: column === "in the browser" || row === "Alerts",
      });
      const bounds = await checkbox.boundingBox();
      expect(bounds, name).not.toBeNull();
      line.push({
        x: bounds!.x + bounds!.width / 2,
        y: bounds!.y + bounds!.height / 2,
      });
    }
    centres.push(line);
  }
  // Laid out as a grid: a column's boxes one above the other, a row's side by
  // side, in the order of the headers.
  for (let r = 0; r < rows.length; r++) {
    for (let c = 0; c < columns.length; c++) {
      expect(Math.abs(centres[r][c].x - centres[0][c].x)).toBeLessThan(1);
      expect(Math.abs(centres[r][c].y - centres[r][0].y)).toBeLessThan(1);
      if (c > 0) expect(centres[r][c].x).toBeGreaterThan(centres[r][c - 1].x);
      if (r > 0) expect(centres[r][c].y).toBeGreaterThan(centres[r - 1][c].y);
    }
  }
  const locked = grid.getByRole("checkbox", {
    name: "Alerts in the browser",
    exact: true,
  });
  await expect(locked).toBeChecked();
  await expect(locked).toBeDisabled();
  await expect(locked).toHaveAccessibleDescription(
    "Alerts always show in the bell.",
  );

  // The page's save writes the file and its lock, root's alone.
  await choose(
    page,
    page,
    "Jobs started in the browser",
    "Also long jobs that finish",
  );
  await saveSettings(page);
  expect((await storedSettings()).report.browser_jobs).toBe("long_jobs");
  await expectRootOnly();
});

test("a script that reports every run keeps one line, made unseen by each run", async ({
  page,
}) => {
  // Three runs of a short script, each waited for with its stop hooks, and
  // two settings saves: 11 s in run 3; the bound leaves room for a slow VM.
  test.setTimeout(300_000);
  await resetNotifications();
  await signIn(page);
  await openNotificationSettings(page);
  await choose(page, page, "Scheduled scripts", "Every run");
  await saveSettings(page);
  expect((await storedSettings()).report.scheduled_scripts).toBe("every_run");

  const name = uniqueName("every run");
  const id = await createSchedule(page, name, "echo done");
  const key = `jobs/finished-${id}`;
  const title = `${name} finished`;
  await markAllSeen(page);

  await runOnce(page, name, id, "completed");
  expect(await storedAlert(key)).toMatchObject({
    severity: "info",
    title,
    link: `/services/schedules?schedule=${id}`,
    occurrence_count: 1,
  });
  // The bell lists alerts once a minute; a reload reads them now.
  await page.reload();
  await expect(badge(page)).toHaveText("1", { timeout: 30_000 });
  let tray = await openTray(page);
  await expect(trayLine(tray, title)).toHaveCount(1);
  await expect(trayLine(tray, title)).toContainText(/It ran for /);
  // Opening the tray marked it seen.
  await expect(badge(page)).toHaveCount(0);
  await page.keyboard.press("Escape");

  // A second run adds no line: the same one becomes unseen again.
  await runOnce(page, name, id, "completed", 2);
  expect(await storedAlert(key)).toMatchObject({ title, occurrence_count: 2 });
  await page.reload();
  await expect(badge(page)).toHaveText("1", { timeout: 30_000 });
  tray = await openTray(page);
  await expect(trayLine(tray, title)).toHaveCount(1);
  await expect(trayLine(tray, title)).toContainText("×2");
  await expect(badge(page)).toHaveCount(0);
  await page.keyboard.press("Escape");

  // Back to failures only, a further run changes nothing.
  await openNotificationSettings(page);
  await choose(page, page, "Scheduled scripts", "Failures only");
  await saveSettings(page);
  expect((await storedSettings()).report.scheduled_scripts).toBe("failures");
  const before = await storedAlert(key);
  await page.goto(`/services/schedules?schedule=${id}`);
  await runOnce(page, name, id, "completed", 3);
  expect(await storedAlert(key)).toEqual(before);
  const list = await alertList(page);
  expect(list.unseen).toBe(0);
  expect(list.alerts.filter((a) => a.id === key)).toMatchObject([
    { occurrenceCount: 2, seen: true },
  ]);

  // Deleting the schedule takes its notice out of the bell, without a
  // reload: the delete resolves it and refetches the list, which the bell
  // would otherwise read only on its next poll, a minute later.
  tray = await openTray(page);
  await expect(trayLine(tray, title)).toHaveCount(1);
  await page.keyboard.press("Escape");
  await scheduleRow(page, name)
    .getByRole("button", { name: "Delete", exact: true })
    .click();
  const confirm = page
    .getByRole("dialog")
    .filter({ hasText: "Delete scheduled task" });
  await confirm.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(confirm).toBeHidden({ timeout: 30_000 });
  expect(await storedAlert(key)).toBeNull();
  tray = await openTray(page);
  await expect(trayLine(tray, title)).toHaveCount(0, { timeout: 10_000 });
});

test("a schedule's own choice wins, and saving only it leaves the unit alone", async ({
  page,
}) => {
  // Two short runs, one of a minute, three dialog saves and the reads of the
  // unit files between them: 1.3 min in run 3.
  test.setTimeout(420_000);
  await resetNotifications();
  await signIn(page);
  const chosen = uniqueName("own choice");
  const other = uniqueName("kind default");
  const chosenId = await createSchedule(page, chosen, "echo chosen");
  const otherId = await createSchedule(page, other, "echo other");
  // The kind's line is failures only (the defaults, no file).
  await openNotificationSettings(page);
  await expect(
    page.getByRole("combobox", { name: "Scheduled scripts", exact: true }),
  ).toHaveText("Failures only");

  await page.goto(`/services/schedules?schedule=${chosenId}`);
  await scheduleRow(page, chosen)
    .getByRole("button", { name: "Edit", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "Edit scheduled task" });
  await expect(
    dialog.getByRole("combobox", { name: "Notify me", exact: true }),
  ).toHaveText("Use the default");
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();

  const before = await scheduleFiles(chosenId);
  await setNotifyChoice(page, chosen, "After every run");
  expect(await scheduleFiles(chosenId), "the unit files and timer").toBe(
    before,
  );
  const stored = await storedSettings();
  expect(stored.scripts).toEqual({ [chosenId]: "every_run" });
  expect(stored.report.scheduled_scripts).toBe("failures");
  // The dialog's write created the file: root's alone, as the page's.
  await expectRootOnly();
  // The dialog shows the stored choice.
  await scheduleRow(page, chosen)
    .getByRole("button", { name: "Edit", exact: true })
    .click();
  await expect(
    dialog.getByRole("combobox", { name: "Notify me", exact: true }),
  ).toHaveText("After every run");
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();

  await markAllSeen(page);
  await runOnce(page, chosen, chosenId, "completed");
  await runOnce(page, other, otherId, "completed");
  expect(await storedAlert(`jobs/finished-${chosenId}`)).toMatchObject({
    severity: "info",
    title: `${chosen} finished`,
  });
  expect(await storedAlert(`jobs/finished-${otherId}`)).toBeNull();
  await page.reload();
  const tray = await openTray(page);
  await expect(trayLine(tray, `${chosen} finished`)).toBeVisible({
    timeout: 30_000,
  });
  await expect(trayLine(tray, `${other} finished`)).toHaveCount(0);
  await page.keyboard.press("Escape");

  // While the script runs, the choice alone is still accepted, the run goes
  // on, and its end follows the new choice.
  const running = uniqueName("choice while running");
  const runningId = await createSchedule(page, running, "sleep 60");
  await runNow(page, running);
  await waitForRun(runningId, "running");
  const invocation = await unitProperty(unitOf(runningId), "InvocationID");
  const whileRunning = await scheduleFiles(runningId);
  // The schedule stays selected, so its live output is shown under the table.
  // In run 2 that output took the page's height (1280×720, the project's
  // viewport) and the table rendered no rows; since the fix the details are
  // at most half the page and the table keeps its rows.
  expect(new URL(page.url()).searchParams.get("schedule")).toBe(runningId);
  await expect(
    page.getByText("Output of the latest run", { exact: true }),
  ).toBeVisible();
  await expect(
    scheduleRow(page, running).getByText(running, { exact: true }),
  ).toBeInViewport();
  await setNotifyChoice(page, running, "After every run");
  expect(await scheduleFiles(runningId), "the unit files and timer").toBe(
    whileRunning,
  );
  expect(await unitProperty(unitOf(runningId), "InvocationID")).toBe(
    invocation,
  );
  expect((await runsOf(runningId)).at(-1)?.state).toBe("running");
  expect((await storedSettings()).scripts).toEqual({
    [chosenId]: "every_run",
    [runningId]: "every_run",
  });
  // The script sleeps 60 s from its start.
  await waitForRun(runningId, "completed", { timeout: 120_000 });
  await waitForIdle(unitOf(runningId));
  expect(await storedAlert(`jobs/finished-${runningId}`)).toMatchObject({
    severity: "info",
    title: `${running} finished`,
  });
});

test("a level not ticked for the browser is neither listed nor counted", async ({
  page,
}) => {
  // Three short runs and two settings saves: 10 s in run 3.
  test.setTimeout(300_000);
  await resetNotifications();
  await signIn(page);
  await openNotificationSettings(page);
  await choose(page, page, "Scheduled scripts", "Every run");
  await box(page, "Notices in the browser").uncheck();
  await saveSettings(page);
  const saved = await storedSettings();
  expect(saved.report.scheduled_scripts).toBe("every_run");
  expect(saved.levels.info).toEqual({
    browser: false,
    email: false,
    webhook: false,
  });

  const notice = uniqueName("hidden notice");
  const failing = uniqueName("shown failure");
  const noticeId = await createSchedule(page, notice, "true");
  const failingId = await createSchedule(page, failing, "exit 4");
  const noticeKey = `jobs/finished-${noticeId}`;
  await markAllSeen(page);
  await runOnce(page, notice, noticeId, "completed");
  const failed = await runOnce(page, failing, failingId, "failed");
  // The notice is raised and stored, only kept from the bell.
  expect(await storedAlert(noticeKey)).toMatchObject({
    severity: "info",
    occurrence_count: 1,
  });
  expect(await storedAlert(`jobs/${failed.id}`)).toMatchObject({
    severity: "error",
    title: `${failing} failed`,
  });
  const hidden = await alertList(page);
  expect(hidden.alerts.map((a) => a.id)).not.toContain(noticeKey);
  expect(hidden.unseen).toBe(1);
  // The badge counts the failure alone, and the tray lists it without the
  // notice.
  await page.reload();
  await expect(badge(page)).toHaveText("1", { timeout: 30_000 });
  let tray = await openTray(page);
  await expect(trayLine(tray, `${failing} failed`)).toBeVisible();
  await expect(trayLine(tray, `${notice} finished`)).toHaveCount(0);
  await page.keyboard.press("Escape");

  // Ticked again, the notice shows; marking all seen skipped it while it was
  // hidden, so it comes back unseen.
  await openNotificationSettings(page);
  await box(page, "Notices in the browser").check();
  await saveSettings(page);
  expect((await storedSettings()).levels.info.browser).toBe(true);
  expect(
    (await alertList(page)).alerts.filter((a) => a.id === noticeKey),
  ).toMatchObject([{ seen: false, occurrenceCount: 1 }]);
  // The next run shows and is counted.
  await markAllSeen(page);
  await page.goto(`/services/schedules?schedule=${noticeId}`);
  await runOnce(page, notice, noticeId, "completed", 2);
  await page.reload();
  await expect(badge(page)).toHaveText("1", { timeout: 30_000 });
  tray = await openTray(page);
  await expect(trayLine(tray, `${notice} finished`)).toContainText("×2");
});

test("an older settings file shows the behaviour it had", async ({ page }) => {
  // A sign-in and the page's reads, no run: 3 s in run 3.
  test.setTimeout(120_000);
  // What an older build wrote: one least severity, a webhook and the Docker
  // policy, none of the new maps.
  const older = JSON.stringify({
    minSeverity: "warning",
    webhookUrl: HOOK_URL,
    dockerUpdates: "on_change",
  });
  await vmSsh(
    `sudo install -m 600 /dev/null ${NOTIFICATIONS} && sudo tee ${NOTIFICATIONS} >/dev/null`,
    `${older}\n`,
  );
  const hash = await vmSsh(`sudo sha256sum ${NOTIFICATIONS}`);
  await signIn(page);
  await openNotificationSettings(page);
  // minSeverity drove email and webhook alike, and every level reached the
  // bell.
  for (const [row, sent] of [
    ["Notices", false],
    ["Warnings", true],
    ["Alerts", true],
  ] as const) {
    await expect(box(page, `${row} in the browser`)).toBeChecked();
    await expect(box(page, `${row} by webhook`)).toBeChecked({
      checked: sent,
    });
    await expect(box(page, `${row} by email`)).toBeChecked({ checked: sent });
  }
  await expect(
    page.getByRole("combobox", { name: "Docker updates", exact: true }),
  ).toHaveText("Also when something changed");
  await expect(page.getByLabel("Webhook URL", { exact: true })).toHaveValue(
    HOOK_URL,
  );
  // Reading it rewrote nothing.
  expect(await vmSsh(`sudo sha256sum ${NOTIFICATIONS}`)).toBe(hash);
});

test("an unreadable settings file is replaced by a save from the page", async ({
  page,
}) => {
  // A sign-in, one save, a journal read and a reload: 4 s in run 3.
  test.setTimeout(120_000);
  await vmSsh(
    `sudo rm -f ${NOTIFICATIONS}.unreadable && sudo install -m 600 /dev/null ${NOTIFICATIONS} && printf '{not json' | sudo tee ${NOTIFICATIONS} >/dev/null`,
  );
  const since = await vmNow();
  await signIn(page);
  await openNotificationSettings(page);
  const warning = page.getByText(
    "The stored notification settings could not be read. Saving replaces them.",
  );
  await expect(warning).toBeVisible();
  const sendTest = page.getByRole("button", { name: "Send test", exact: true });
  await expect(sendTest).toBeDisabled();
  await expect(sendTest).toHaveAttribute("title", "Save first");

  // Save is offered without an edit, and writes the defaults the page shows.
  await saveSettings(page);
  const saved = await storedSettings();
  expect(saved.levels).toEqual({
    info: { browser: true, email: false, webhook: false },
    warning: { browser: true, email: false, webhook: false },
    error: { browser: true, email: true, webhook: true },
  });
  expect(saved.report.scheduled_scripts).toBe("failures");
  await expectRootOnly();
  const logged = await journalLines(
    `--since @${since}`,
    "notification settings unreadable, starting from the defaults",
  );
  console.log(logged.join("\n"));
  expect(logged, "the save's one warning").toHaveLength(1);
  // The unreadable file is kept beside the new one, root's alone.
  expect(
    await vmSsh(
      `sudo stat -c '%a %U:%G' ${NOTIFICATIONS}.unreadable && sudo cat ${NOTIFICATIONS}.unreadable`,
    ),
  ).toBe("600 root:root\n{not json");
  await expect(warning).toBeHidden({ timeout: 15_000 });
  await expect(sendTest).toBeEnabled();
  await page.reload();
  await expect(
    page.getByRole("region", { name: "What to hear about" }),
  ).toBeVisible({ timeout: 15_000 });
  await expect(warning).toHaveCount(0);
});

test("the grid picks the channel: only alerts are posted to the webhook", async ({
  page,
}) => {
  // The listener's install, two short runs and a settings save: 12 s in run 3.
  test.setTimeout(300_000);
  await startListener();
  await resetNotifications();
  const since = await vmNow();
  await signIn(page);
  await openNotificationSettings(page);
  await page.getByLabel("Webhook URL", { exact: true }).fill(HOOK_URL);
  await choose(page, page, "Scheduled scripts", "Every run");
  await box(page, "Notices by webhook").setChecked(false);
  await box(page, "Warnings by webhook").setChecked(false);
  await box(page, "Alerts by webhook").setChecked(true);
  await saveSettings(page);
  const saved = await storedSettings();
  expect(saved.webhookUrl).toBe(HOOK_URL);
  expect(saved.report.scheduled_scripts).toBe("every_run");
  expect(
    Object.fromEntries(
      Object.entries(saved.levels).map(([level, c]) => [level, c.webhook]),
    ),
  ).toEqual({ info: false, warning: false, error: true });

  const failing = uniqueName("posted failure");
  const quiet = uniqueName("unposted notice");
  const failingId = await createSchedule(page, failing, "exit 5");
  const quietId = await createSchedule(page, quiet, "true");
  await runOnce(page, failing, failingId, "failed");
  await runOnce(page, quiet, quietId, "completed");
  // The end hook delivers before its unit stops, so what was posted for
  // these runs is all there is.
  expect(await postsTitled(`${failing} failed`)).toMatchObject([
    { severity: "error", source: "jobs", message: "exited with status 5" },
  ]);
  expect(await storedAlert(`jobs/finished-${quietId}`)).toMatchObject({
    severity: "info",
    title: `${quiet} finished`,
  });
  expect(await postsTitled(`${quiet} finished`)).toEqual([]);
  expect(
    await journalLines(`--since @${since}`, "alert delivery failed"),
  ).toEqual([]);
});

test("an interrupted script is a warning, posted only where warnings go", async ({
  page,
}) => {
  // Two power cuts, each after a run is seen running and followed by a boot,
  // a wait for an apt run the boot started (at most 600 s each, as
  // stopAptRuns bounds it), a sign-in, the 20 s wait below and the reads
  // after it: 1.1 min in run 3, without the apt waits.
  test.setTimeout(1_800_000);
  await startListener();
  await resetNotifications();
  await signIn(page);
  await openNotificationSettings(page);
  await page.getByLabel("Webhook URL", { exact: true }).fill(HOOK_URL);
  await box(page, "Warnings by webhook").setChecked(false);
  await box(page, "Alerts by webhook").setChecked(true);
  await saveSettings(page);
  expect((await storedSettings()).levels.warning.webhook).toBe(false);

  // Only alerts are ticked for the webhook: the interruption is not posted.
  const first = await interruptByPowerCut(
    page,
    uniqueName("interrupted unposted"),
  );
  // The job host delivers right after it stores the alert, within 15 s
  // (deliveryTimeout), and logs nothing when it sends nothing, so only time
  // tells that nothing was sent.
  await first.page.waitForTimeout(20_000);
  expect(await postsTitled(first.title)).toEqual([]);
  expect(await journalLines("-b", "alert delivery failed")).toEqual([]);
  // The listener takes posts in this boot.
  await probeListener();

  // Warnings ticked for the webhook: the next interruption is posted.
  await openNotificationSettings(first.page);
  await box(first.page, "Warnings by webhook").check();
  await saveSettings(first.page);
  expect((await storedSettings()).levels.warning.webhook).toBe(true);
  const second = await interruptByPowerCut(
    first.page,
    uniqueName("interrupted posted"),
  );
  await expect
    .poll(() => postsTitled(second.title), {
      message: "the interruption posted",
      timeout: 30_000,
    })
    .toMatchObject([{ severity: "warning", source: "jobs" }]);
  expect(await postsTitled(first.title)).toEqual([]);
  expect(await journalLines("-b", "alert delivery failed")).toEqual([]);

  // In the bell both are warnings.
  const tray = await openTray(second.page);
  for (const title of [first.title, second.title]) {
    const line = trayLine(tray, title);
    await expect(line, title).toBeVisible({ timeout: 30_000 });
    await expect(
      line.locator(".app-navbar-notifications__icon"),
      title,
    ).toHaveAttribute(
      "style",
      /--app-tile-color:\s*var\(--app-palette-warning-main\)/,
    );
  }
  await signOut(second.page);
});

test("the notice at sign-in counts the unseen alert and Show opens the bell", async ({
  page,
}) => {
  // Two short runs, a sign-out and sign-in, and up to a minute for the bell's
  // own poll to show the second failure: under 2 min.
  test.setTimeout(300_000);
  await resetNotifications();
  await signIn(page);
  await markAllSeen(page);
  const name = uniqueName("while away");
  const id = await createSchedule(page, name, "exit 6");
  const run = await runOnce(page, name, id, "failed");
  const key = `jobs/${run.id}`;
  expect(await storedAlert(key)).toMatchObject({
    severity: "error",
    occurrence_count: 1,
  });
  expect((await alertList(page)).unseen).toBe(1);

  await signOut(page);
  await signIn(page);
  const notice = page
    .locator("[data-sonner-toast]")
    .filter({ hasText: "While you were away" });
  await expect(
    notice.getByText("While you were away: 1 new notification", {
      exact: true,
    }),
  ).toBeVisible({ timeout: 30_000 });
  // From the keyboard, as jsdom showed: focus ends on the bell's button,
  // beside its open tray.
  await notice.getByRole("button", { name: "Show", exact: true }).focus();
  await page.keyboard.press("Enter");
  const tray = page.getByRole("dialog", { name: "Notifications" });
  await expect(tray).toBeVisible();
  await expect(bell(page)).toBeFocused();
  await expect(trayLine(tray, `${name} failed`)).toBeVisible();
  // Showing it was seeing it.
  const uid = await vmUid();
  await expect
    .poll(async () => (await storedAlert(key))?.seen?.[uid], {
      message: "the alert seen",
    })
    .toBe(1);
  expect((await alertList(page)).unseen).toBe(0);
  await expect(badge(page)).toHaveCount(0);
  await page.keyboard.press("Escape");

  // A reload in the same session says nothing, even with a new unseen alert:
  // the sign-in's mark was taken. A second failure, seen by the bell's own
  // poll (once a minute) before the reload and again after it, so the list
  // the notice would count has loaded with something unseen in it.
  await page.goto(`/services/schedules?schedule=${id}`);
  const second = await runOnce(page, name, id, "failed", 2);
  expect(await storedAlert(`jobs/${second.id}`)).toMatchObject({
    severity: "error",
  });
  await expect(badge(page)).toHaveText("1", { timeout: 90_000 });
  await page.reload();
  await expect(badge(page)).toHaveText("1", { timeout: 30_000 });
  await expect(
    page
      .locator("[data-sonner-toast]")
      .filter({ hasText: "While you were away" }),
  ).toHaveCount(0);
});

test("a stored never for system updates is reported and fixed in one click", async ({
  page,
}) => {
  // A sign-in, two reads of the state and one save: 6 s in run 3.
  test.setTimeout(120_000);
  // A stored "never" is no drop-in: remove it, whatever an earlier run of
  // this check or the image left there.
  const dropIn =
    "/etc/systemd/system/apt-daily-upgrade.service.d/linuxio-alert.conf";
  await vmSsh(`sudo rm -f ${dropIn} && sudo systemctl daemon-reload`);
  await signIn(page);
  const read = async () => {
    const answer = await bridgeCall(page, "updates.get_auto_updates", {});
    expect(answer.status, answer.error).toBe("ok");
    return answer.data as {
      can_configure: boolean;
      failures_unreported?: boolean;
      options: { enabled: boolean; notify: string };
      support: { notify_policies: string[] };
    };
  };
  const state = await read();
  console.log(
    `system updates: ${JSON.stringify({
      enabled: state.options.enabled,
      notify: state.options.notify,
      failures_unreported: state.failures_unreported,
      can_configure: state.can_configure,
      offered: state.support.notify_policies,
    })}`,
  );
  // Only facts of the machine skip: no automatic-update tool to configure,
  // or automatic updates off, where the page rightly shows no warning.
  test.skip(
    !state.can_configure || !state.options.enabled,
    `this VM reads can_configure ${state.can_configure}, enabled ${state.options.enabled}`,
  );
  expect(state.options.notify).toBe("never");
  expect(state.failures_unreported).toBe(true);
  expect(state.support.notify_policies).toEqual([
    "on_failure",
    "on_change",
    "always",
  ]);

  await openNotificationSettings(page);
  const warning = page.getByText(
    "System update failures are not reported on this machine.",
  );
  await expect(warning).toBeVisible({ timeout: 15_000 });
  const system = page.getByRole("combobox", {
    name: "System updates",
    exact: true,
  });
  await expect(system).toHaveText("Not reported");
  await system.click();
  await expect(page.getByRole("option")).toHaveText([
    "Failures only",
    "Also when something changed",
    "Every run",
  ]);
  await page.keyboard.press("Escape");

  await page
    .getByRole("button", { name: "Report failures", exact: true })
    .click();
  await expect(
    page.getByText("System update notifications saved").last(),
  ).toBeVisible({ timeout: 30_000 });
  await expect(warning).toBeHidden({ timeout: 15_000 });
  await expect(system).toHaveText("Failures only");
  console.log(
    await vmSsh(`sudo stat -c '%n %a %U:%G' $(dirname ${dropIn}) ${dropIn}`),
  );
  expect(await vmSsh(`sudo cat ${dropIn}`)).toContain("--policy on_failure");
  const after = await read();
  expect(after.options.notify).toBe("on_failure");
  expect(after.failures_unreported ?? false).toBe(false);
});
