import { execFile } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { expect, type Locator, type Page } from "@playwright/test";

// Helpers for the specs in this directory. They reach the VM only through
// packaging/vm/linuxio-vm.sh, which acts on the domain linuxio-e2e alone.
const here = path.dirname(fileURLToPath(import.meta.url));
const rigPath = path.resolve(here, "../../../../packaging/vm/linuxio-vm.sh");
const execFileAsync = promisify(execFile);

export const fixture = (name: string) => path.join(here, "fixtures", name);

/** Runs a rig subcommand (ssh, reboot, power-cut, throttle) and returns its stdout. */
export async function rig(
  args: string[],
  input?: string | Buffer,
): Promise<string> {
  const run = execFileAsync(rigPath, args, {
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
  });
  run.child.stdin?.end(input);
  return (await run).stdout;
}

/** Runs a shell command in the VM as the test user, who can sudo. */
export const vmSsh = (command: string, input?: string | Buffer) =>
  rig(["ssh", command], input);

/**
 * Stops the apt timers until the next boot and waits for an apt run in
 * progress to end. A fresh VM runs unattended-upgrades minutes after boot, and
 * an apt run in the middle of a check is noise: it restarts services.
 */
export async function stopAptRuns() {
  await vmSsh("sudo systemctl stop apt-daily.timer apt-daily-upgrade.timer");
  await expect
    .poll(
      async () =>
        vmSsh(
          "systemctl is-active apt-daily.service apt-daily-upgrade.service || true",
        ),
      { intervals: [5_000], message: "apt runs in progress", timeout: 600_000 },
    )
    .not.toMatch(/^(active|activating|deactivating|reloading)$/m);
}

export interface JobRecord {
  id: string;
  route: string;
  state: string;
  reason?: string;
  resumable?: boolean;
  cancel_requested?: boolean;
  checkpoint?: unknown;
  error?: { message: string };
  result?: unknown;
}

/** The records of uid's job host (by default root's), read from its store in the VM. */
export async function jobRecords(
  route?: string,
  uid = 0,
): Promise<JobRecord[]> {
  const out = await vmSsh(
    `sudo sh -c 'cat /var/lib/linuxio/jobs/${uid}/*.json 2>/dev/null' | jq -cs .`,
  );
  const records = JSON.parse(out) as JobRecord[];
  return route ? records.filter((r) => r.route === route) : records;
}

/** The one record of route created after `before` (ids seen earlier). */
export async function newJob(
  route: string,
  before: Set<string>,
): Promise<JobRecord | undefined> {
  const fresh = (await jobRecords(route)).filter((r) => !before.has(r.id));
  expect(fresh.length, `new ${route} jobs`).toBeLessThanOrEqual(1);
  return fresh[0];
}

/** The record of job id in the root job host's store. */
export async function jobRecord(id: string): Promise<JobRecord | undefined> {
  return (await jobRecords()).find((r) => r.id === id);
}

/**
 * The jobs the root job host logged message for in this boot, with the
 * reason it logged (the journal holds it JSON-quoted).
 */
export async function loggedJobs(
  message: string,
): Promise<{ id: string; reason: string }[]> {
  const out = await vmSsh(
    `sudo journalctl -b -u linuxio-jobs@0.service -o json | jq -c --arg m '${message}' ` +
      `'select(.MESSAGE == $m) | {id: .LINUXIO_JOB_ID, reason: ((.LINUXIO_REASON // "") | ltrimstr("\\"") | rtrimstr("\\""))}'`,
  );
  return out
    .split("\n")
    .filter(Boolean)
    .map((line) => JSON.parse(line) as { id: string; reason: string });
}

export async function jobIds(): Promise<Set<string>> {
  return new Set((await jobRecords()).map((r) => r.id));
}

/** Polls until the route's new job reaches state (and reason), and returns it. */
export async function waitForJob(
  route: string,
  before: Set<string>,
  state: string,
  { reason, timeout = 60_000 }: { reason?: string; timeout?: number } = {},
): Promise<JobRecord> {
  let job: JobRecord | undefined;
  await expect
    .poll(
      async () => {
        job = await newJob(route, before);
        return job && { state: job.state, reason: job.reason };
      },
      { intervals: [2_000], message: `${route} ${state}`, timeout },
    )
    .toEqual({ state, reason });
  return job!;
}

/** The record of a timer-started run (docs/jobs.md, Scheduled runs). */
export interface ScheduledRecord extends JobRecord {
  created_at: string;
  target?: string[];
  metadata?: { label?: string; identity?: string[] };
  executor: { kind: string; unit?: string; invocation_id?: string };
}

const SCHEDULED_STORE = "/var/lib/linuxio/jobs/scheduled";
const RUN_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/** The scheduled runs' records, read from their store in the VM, oldest first. */
export async function scheduledRecords(
  route?: string,
): Promise<ScheduledRecord[]> {
  const out = await vmSsh(
    `sudo sh -c 'cat ${SCHEDULED_STORE}/*.json 2>/dev/null' | jq -cs .`,
  );
  const records = (JSON.parse(out) as ScheduledRecord[]).sort(
    (a, b) => Date.parse(a.created_at) - Date.parse(b.created_at),
  );
  return route ? records.filter((r) => r.route === route) : records;
}

/** The output scheduled run id saved, byte for byte; empty when it saved none. */
export async function scheduledOutput(id: string): Promise<Buffer> {
  expect(id, "scheduled run ID").toMatch(RUN_ID);
  const out = await vmSsh(
    `sudo sh -c 'base64 -w0 ${SCHEDULED_STORE}/${id}.log 2>/dev/null' || true`,
  );
  return Buffer.from(out.trim(), "base64");
}

export interface Login {
  username: string;
  password: string;
}

/** Signs in as login, by default the VM's test user, who can sudo. */
export async function signIn(page: Page, login?: Login) {
  const username = login?.username ?? process.env.LINUXIO_VM_USER ?? "";
  const password = login?.password ?? process.env.LINUXIO_VM_PASSWORD ?? "";
  expect(username, "LINUXIO_VM_USER").not.toBe("");
  expect(password, "LINUXIO_VM_PASSWORD").not.toBe("");

  await page.goto("/sign-in");
  await page.locator('input[autocomplete="username"]').fill(username);
  await page.locator('input[autocomplete="current-password"]').fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).not.toHaveURL(/\/sign-in/);
}

/** Signs out unless the page is closed or already on the sign-in page. */
export async function signOutIfSignedIn(page: Page) {
  const url = page.isClosed() ? "" : page.url();
  if (url.startsWith("http") && !url.includes("/sign-in")) {
    await signOut(page);
  }
}

export async function signOut(page: Page) {
  await page.keyboard.press("Escape"); // an open tray or menu
  await page.getByRole("button", { name: "Account" }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/sign-in/);
}

/**
 * Waits until no session bridge runs, so none holds a job host up: a host
 * exits a minute after its last bridge and its last job.
 */
export async function noSessions() {
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

/** A bridge's answer to one call, as its result frame carries it. */
export interface BridgeResult {
  status: "ok" | "error";
  data?: unknown;
  error?: string;
  code?: number | string;
}

/**
 * Calls route of the signed-in session's bridge on a WebSocket of its own,
 * the way the app's call() does, for an answer the app never asks for (such
 * as a privileged route from a session without sudo).
 */
export function bridgeCall(
  page: Page,
  route: string,
  request: unknown,
): Promise<BridgeResult> {
  return page.evaluate(
    async ({ route, request }) => {
      // The framing of frontend/src/api/StreamMultiplexer.ts: a WebSocket
      // frame [stream:4][flags:1] around a bridge frame [opcode:1][stream:4]
      // [length:4][payload]. SYN 0x01, DATA 0x04; open 0x80, result 0x85.
      const ws = new WebSocket(`wss://${location.host}/ws`);
      ws.binaryType = "arraybuffer";
      try {
        return await new Promise<BridgeResult>((resolve, reject) => {
          const timer = setTimeout(
            () => reject(new Error(`no answer to ${route} in 30 s`)),
            30_000,
          );
          let pending = new Uint8Array(0);
          ws.onerror = () => reject(new Error("WebSocket error"));
          ws.onclose = (event) =>
            reject(
              new Error(`WebSocket closed (${event.code}) before a result`),
            );
          ws.onopen = () => {
            const body = new TextEncoder().encode(
              JSON.stringify({ route, request }),
            );
            const frame = new Uint8Array(14 + body.length);
            const view = new DataView(frame.buffer);
            view.setUint32(0, 1);
            frame[4] = 0x01;
            frame[5] = 0x80;
            view.setUint32(6, 1);
            view.setUint32(10, body.length);
            frame.set(body, 14);
            ws.send(frame);
          };
          ws.onmessage = ({ data }) => {
            const bytes = new Uint8Array(data as ArrayBuffer);
            const outer = new DataView(bytes.buffer);
            if (outer.getUint32(0) !== 1 || !(bytes[4] & 0x04)) return;
            const joined = new Uint8Array(pending.length + bytes.length - 5);
            joined.set(pending);
            joined.set(bytes.subarray(5), pending.length);
            pending = joined;
            while (pending.length >= 9) {
              const length = new DataView(
                pending.buffer,
                pending.byteOffset,
              ).getUint32(5);
              if (pending.length < 9 + length) return;
              const opcode = pending[0];
              const payload = pending.slice(9, 9 + length);
              pending = pending.slice(9 + length);
              if (opcode === 0x85) {
                clearTimeout(timer);
                resolve(JSON.parse(new TextDecoder().decode(payload)));
                return;
              }
            }
          };
        });
      } finally {
        ws.close();
      }
    },
    { route, request },
  );
}

/** Opens the footer's notifications tray and returns it. */
export async function openTray(page: Page) {
  const tray = page.getByRole("dialog", { name: "Notifications" });
  if (!(await tray.isVisible())) {
    await page
      .getByRole("button", { name: "Notifications", exact: true })
      .click();
  }
  await expect(tray).toBeVisible();
  return tray;
}

/** A running job's row in the tray (alerts, which can be dismissed, are not). */
export const trayJob = (tray: Locator, text: string | RegExp) =>
  tray
    .getByRole("listitem")
    .filter({ hasText: text })
    .filter({
      hasNot: tray.page().getByRole("button", { name: "Dismiss alert" }),
    });

export const SCRUB = "storage.btrfs_scrub_start";
export const POOL_MOUNT = "/mnt/e2e";
export const MiB = 1024 * 1024;

/** The test pool's UUID; creates it on vdb and vdc in the browser first, with 1 GiB to scrub. */
export async function ensurePool(page: Page): Promise<string> {
  const mounted = async () =>
    (await vmSsh(`findmnt -no UUID ${POOL_MOUNT} || true`)).trim();
  if (await mounted()) return mounted();
  await page.goto("/storage");
  await page.getByRole("button", { name: "Create pool" }).click();
  const dialog = page.getByRole("dialog", { name: "Create pool" });
  await dialog.getByLabel("Name", { exact: true }).fill("e2e");
  await dialog.getByRole("checkbox", { name: /^\/dev\/vdb · / }).check();
  await dialog.getByRole("checkbox", { name: /^\/dev\/vdc · / }).check();
  await dialog.getByRole("button", { name: "Create pool" }).click();
  await expect(dialog).toBeHidden({ timeout: 60_000 });
  await vmSsh(
    `head -c 1G /dev/urandom | sudo tee ${POOL_MOUNT}/fill.bin >/dev/null && sync`,
  );
  return mounted();
}

/**
 * Signs in and starts a scrub of the test pool in the browser, with each
 * disk's scrub reads capped (a test knob of the kernel) so it lasts long
 * enough. Returns the pool's UUID and the running job.
 */
export async function startScrub(
  page: Page,
  before: Set<string>,
  bytesPerSecond: number,
) {
  await signIn(page);
  const uuid = await ensurePool(page);
  await vmSsh(
    `sudo btrfs scrub cancel ${POOL_MOUNT} >/dev/null 2>&1; ` +
      `for f in /sys/fs/btrfs/${uuid}/devinfo/*/scrub_speed_max; do echo ${bytesPerSecond} | sudo tee $f >/dev/null; done`,
  );
  await page.goto(`/storage?pool=${uuid}`);
  await page.getByRole("button", { name: "Check for errors" }).click();
  const job = await waitForJob(SCRUB, before, "running");
  return { uuid, job };
}

// Asks the kernel (BTRFS_IOC_SCRUB_PROGRESS) whether any device of the
// filesystem at argv[1] is being scrubbed; btrfs-progs only sees its own scrubs.
const SCRUB_PROGRESS = `
import errno, fcntl, os, struct, sys
fd = os.open(sys.argv[1], os.O_RDONLY)
running = False
for devid in range(1, 64):
    args = bytearray(1024)
    struct.pack_into("<Q", args, 0, devid)
    try:
        fcntl.ioctl(fd, (3 << 30) | (1024 << 16) | (0x94 << 8) | 29, args)
        running = True
    except OSError as e:
        if e.errno not in (errno.ENOTCONN, errno.ENODEV):
            raise
print("running" if running else "idle")
`;

/** Whether the kernel runs a scrub of the test pool. */
export const scrubRunning = async () =>
  (await vmSsh(`sudo python3 -I - ${POOL_MOUNT}`, SCRUB_PROGRESS)).trim() ===
  "running";

/** The pool's exclusive operation as sysfs names it, such as "balance paused". */
export const exclusiveOperation = async (uuid: string) =>
  (await vmSsh(`cat /sys/fs/btrfs/${uuid}/exclusive_operation`)).trim();

const DATA_DISKS = ["vdb", "vdc", "vdd", "vde"];

/** Caps the I/O of the VM's data disks (0 lifts it); the cap outlasts a power cut. */
export async function throttle(bytesPerSecond: number, disks = DATA_DISKS) {
  for (const disk of disks) {
    await rig(["throttle", disk, String(bytesPerSecond)]);
  }
}

// Scheduled scripts created on the Schedules page, disabled, with a calendar
// of 2099, and started with "Run now".

export const SCHEDULED_SCRIPT = "scheduled.script";
const NEVER = "2099-01-01 00:00:00";

/** A schedule name no earlier run of a spec used. */
export const uniqueName = (what: string) =>
  `e2e ${what} ${Date.now().toString(36)}`;

export const unitOf = (id: string) => `linuxio-schedule-${id}.service`;

export const scheduleRow = (page: Page, name: string) =>
  page.getByRole("row").filter({ hasText: name });

/**
 * Opens the Schedules page's create dialog, fills it in to run script as the
 * test user, unsaved, and returns the dialog.
 */
export async function fillSchedule(page: Page, name: string, script: string) {
  await page.goto("/services/schedules");
  await page.getByRole("button", { name: "Create task" }).click();
  const dialog = page.getByRole("dialog", { name: "Create scheduled task" });
  await dialog.getByLabel("Name", { exact: true }).fill(name);
  await dialog.getByLabel("Bash script").fill(`${script}\n`);
  await dialog.getByRole("combobox", { name: "Run as" }).click();
  await page
    .getByRole("option", { name: process.env.LINUXIO_VM_USER, exact: true })
    .click();
  await dialog.getByRole("combobox", { name: "Repeat" }).click();
  await page.getByRole("option", { name: "Custom (advanced)" }).click();
  await dialog.getByLabel("Calendar expression").fill(NEVER);
  await dialog.getByLabel("Timeout seconds").fill("3600");
  return dialog;
}

/**
 * Creates a schedule that runs script as the test user, selects it on the
 * Schedules page and returns its ID.
 */
export async function createSchedule(page: Page, name: string, script: string) {
  const dialog = await fillSchedule(page, name, script);
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(dialog).toBeHidden({ timeout: 30_000 });
  await scheduleRow(page, name).getByText(name, { exact: true }).click();
  await expect(page).toHaveURL(/[?&]schedule=[0-9a-f-]{36}$/);
  return new URL(page.url()).searchParams.get("schedule")!;
}

export const runNow = (page: Page, name: string) =>
  scheduleRow(page, name).getByRole("button", { name: "Run now" }).click();

/** The runs of schedule id, oldest first. */
export const runsOf = async (id: string) =>
  (await scheduledRecords(SCHEDULED_SCRIPT)).filter(
    (r) => r.metadata?.identity?.[0] === id,
  );

/**
 * Polls until schedule id has count runs, the newest in state and with reason
 * (none unless given; any at all with anyReason), and returns that one.
 */
export async function waitForRun(
  id: string,
  state: string,
  {
    count = 1,
    reason,
    anyReason = false,
    timeout = 60_000,
  }: {
    count?: number;
    reason?: string;
    anyReason?: boolean;
    timeout?: number;
  } = {},
): Promise<ScheduledRecord> {
  let run: ScheduledRecord | undefined;
  await expect
    .poll(
      async () => {
        const runs = await runsOf(id);
        run = runs.at(-1);
        return {
          runs: runs.length,
          state: run?.state,
          reason: anyReason ? reason : run?.reason,
        };
      },
      { intervals: [1_000], message: `run ${count} ${state}`, timeout },
    )
    .toEqual({ runs: count, state, reason });
  return run!;
}

/**
 * Waits until unit has stopped, its stop hooks included: the end hook records
 * the run, saves its output, and raises and delivers its alert before the unit
 * is inactive.
 */
export async function waitForIdle(unit: string) {
  await expect
    .poll(
      async () =>
        (await vmSsh(`systemctl show -p ActiveState --value ${unit}`)).trim(),
      { intervals: [500], message: `${unit} stopped`, timeout: 60_000 },
    )
    .toMatch(/^(inactive|failed)$/);
}
