import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { expect, type Locator, type Page } from "@playwright/test";

// Helpers for the specs in this directory. They reach the VM only through
// packaging/vm/linuxio-vm.sh, which acts on the linuxio-e2e domains alone.
const here = path.dirname(fileURLToPath(import.meta.url));
const rigPath = path.resolve(here, "../../../../packaging/vm/linuxio-vm.sh");
const execFileAsync = promisify(execFile);

// make test-vm-browser and test-vm-browser-rerun list the VMs of the run in
// LINUXIO_VM_NUMBERS, _URLS and _PASSWORDS, in one order. Playwright runs one
// worker for each VM, and a worker keeps to the VM at its parallel index: the
// config gives it that VM's
// address, and LINUXIO_VM_INDEX sends every rig command it starts, here and
// in the specs, to that VM.
const slot = Number(process.env.TEST_PARALLEL_INDEX ?? 0);
const ofThisWorker = (list: string) =>
  (process.env[list] ?? "").trim().split(/\s+/)[slot] ?? "";
const vmNumber = ofThisWorker("LINUXIO_VM_NUMBERS");
if (vmNumber === "") {
  // A worker past the list (--workers) would send its reboots to another's VM.
  throw new Error(
    `worker ${slot} has no VM in LINUXIO_VM_NUMBERS: run the specs with make test-vm-browser or make test-vm-browser-rerun, without --workers`,
  );
}
process.env.LINUXIO_VM_INDEX = vmNumber;
const vmPassword = ofThisWorker("LINUXIO_VM_PASSWORDS");

/**
 * The distribution of the run's VMs: DISTRO on the Make target, which
 * mk/vm.mk exports as LINUXIO_VM_DISTRO, the variable the rig itself reads to
 * pick the domain. Empty means ubuntu, as in the rig.
 */
export const vmDistro = process.env.LINUXIO_VM_DISTRO || "ubuntu";

export const fixture = (name: string) => path.join(here, "fixtures", name);

/** Runs a rig subcommand (ssh, reboot, power-cut, off-for, throttle) and returns its stdout. */
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
  // Fedora has no unattended runs: its seed switches dnf-makecache.timer off.
  if (vmDistro !== "ubuntu") return;
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
 * reason it logged.
 */
export async function loggedJobs(
  message: string,
): Promise<{ id: string; reason: string }[]> {
  const out = await vmSsh(
    `sudo journalctl -b -u linuxio-jobs@0.service -o json | jq -c --arg m '${message}' ` +
      `'select(.MESSAGE == $m) | {id: .LINUXIO_JOB_ID, reason: (.LINUXIO_REASON // "")}'`,
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
  const password = login?.password ?? vmPassword;
  expect(username, "LINUXIO_VM_USER").not.toBe("");
  expect(password, "LINUXIO_VM_PASSWORDS").not.toBe("");

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
 * as a privileged route from a session without sudo). A mutation runs as a
 * job and needs the job's ID, a UUID, in `id`; its answer is the job's result.
 */
export function bridgeCall(
  page: Page,
  route: string,
  request: unknown,
  id?: string,
): Promise<BridgeResult> {
  return page.evaluate(
    async ({ route, request, id }) => {
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
              JSON.stringify({ route, request, id }),
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
    { route, request, id },
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
/** The scrub job's label, which names it in its alerts. */
export const SCRUB_LABEL = "Scrubbing btrfs filesystem";
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
 * enough. prepare, if given, runs on the pool first. Returns the pool's UUID
 * and the running job.
 */
export async function startScrub(
  page: Page,
  before: Set<string>,
  bytesPerSecond: number,
  prepare?: (uuid: string) => Promise<unknown>,
) {
  await signIn(page);
  const uuid = await ensurePool(page);
  await prepare?.(uuid);
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

// Prints how many bytes the kernel's running scrub of the filesystem at
// argv[1] has read on all devices (BTRFS_IOC_SCRUB_PROGRESS).
const SCRUBBED_BYTES = `
import errno, fcntl, os, struct, sys
fd = os.open(sys.argv[1], os.O_RDONLY)
total = 0
for devid in range(1, 64):
    args = bytearray(1024)
    struct.pack_into("<Q", args, 0, devid)
    try:
        fcntl.ioctl(fd, (3 << 30) | (1024 << 16) | (0x94 << 8) | 29, args)
    except OSError as e:
        if e.errno not in (errno.ENOTCONN, errno.ENODEV):
            raise
        continue
    data, tree = struct.unpack_from("<QQ", args, 48)
    total += data + tree
print(total)
`;

/** The bytes the kernel's running scrub of the test pool has read, 0 if none runs. */
export const scrubbedBytes = async () =>
  Number(await vmSsh(`sudo python3 -I - ${POOL_MOUNT}`, SCRUBBED_BYTES));

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
 * The schedule's row, scrolled into the table's rendered window. The table
 * renders only the rows near its viewport, in the order of the definition
 * files (by ID, so a new schedule lands anywhere), and the specs leave their
 * schedules behind: scheduleRow alone finds the row only while few exist.
 */
async function revealScheduleRow(page: Page, name: string) {
  const row = scheduleRow(page, name);
  const scroller = page
    .getByRole("table", { name: "Scheduled tasks" })
    .locator(".app-dt__scroll");
  await expect(scroller).toBeVisible();
  await expect
    .poll(
      async () => {
        await scroller.evaluate((el) => {
          el.scrollTop = 0;
        });
        for (;;) {
          if ((await row.count()) > 0) return true;
          const moved = await scroller.evaluate((el) => {
            const top = el.scrollTop;
            el.scrollTop = top + el.clientHeight / 2;
            return el.scrollTop !== top;
          });
          if (!moved) return (await row.count()) > 0;
          await page.waitForTimeout(50);
        }
      },
      { intervals: [500], message: `schedule row ${name}`, timeout: 20_000 },
    )
    .toBe(true);
  return row;
}

/**
 * Clicks what target picks in the schedule's row, finding the row again when
 * the table has moved it out of its window since (opening a schedule shows
 * its details under the table, which shrinks it). The click is dispatched to
 * the element: the table's rows change height as they are measured, so a
 * click that scrolls to its target first can miss it.
 */
export const clickInScheduleRow = (
  page: Page,
  name: string,
  target: (row: Locator) => Locator,
) =>
  expect(async () => {
    const row = await revealScheduleRow(page, name);
    await target(row).dispatchEvent("click", undefined, { timeout: 3_000 });
  }).toPass({ intervals: [500], timeout: 30_000 });

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

export interface ScheduleChoices {
  /** A calendar expression other than the one of 2099. */
  calendar?: string;
  /** Tick "Run after a missed activation" (`Persistent=`). */
  persistent?: boolean;
  /** The script's timeout in seconds (default 3600). */
  timeoutSeconds?: number;
}

/**
 * Creates a schedule that runs script as the test user, selects it on the
 * Schedules page and returns its ID.
 */
export async function createSchedule(
  page: Page,
  name: string,
  script: string,
  choices: ScheduleChoices = {},
) {
  const dialog = await fillSchedule(page, name, script);
  if (choices.calendar) {
    await dialog.getByLabel("Calendar expression").fill(choices.calendar);
  }
  if (choices.persistent) {
    await dialog
      .getByRole("checkbox", { name: "Run after a missed activation" })
      .check();
  }
  if (choices.timeoutSeconds) {
    await dialog
      .getByLabel("Timeout seconds")
      .fill(String(choices.timeoutSeconds));
  }
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(dialog).toBeHidden({ timeout: 30_000 });
  await clickInScheduleRow(page, name, (row) =>
    row.getByText(name, { exact: true }),
  );
  await expect(page).toHaveURL(/[?&]schedule=[0-9a-f-]{36}$/);
  return new URL(page.url()).searchParams.get("schedule")!;
}

/** Enables schedule id, named name, and waits until its timer is active. */
export async function enableSchedule(page: Page, name: string, id: string) {
  await clickInScheduleRow(page, name, (row) =>
    row.getByRole("button", { name: "Enable", exact: true }),
  );
  await expect
    .poll(
      async () =>
        (
          await vmSsh(
            `systemctl is-active linuxio-schedule-${id}.timer || true`,
          )
        ).trim(),
      { intervals: [500], message: `timer of ${name}`, timeout: 30_000 },
    )
    .toBe("active");
}

export const runNow = (page: Page, name: string) =>
  clickInScheduleRow(page, name, (row) =>
    row.getByRole("button", { name: "Run now" }),
  );

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

// LinuxIO from the VM's signed package repository (linuxio-vm.sh repo-publish;
// docs/development.md, "Test VM"): a build is published there as a version,
// and apt, dnf or the Updates page install it from there, checking every
// signature as they would against the published repository.

/** The VM's repository, PACKAGE_REPO in the rig: apt/ on Ubuntu, rpm/ on Fedora. */
export const PACKAGE_REPO = `/srv/linuxio-repo/${vmDistro === "fedora" ? "rpm" : "apt"}`;

/** The build make made, which `make vm-install` installs (mk/vm.mk). */
export const plainBuild = () => process.env.LINUXIO_VM_BUILD_DIR ?? "";
/** This tree with a version of its own (make vm-update-build). */
export const updateBuild = () => process.env.LINUXIO_VM_UPDATE_BUILD ?? "";

/**
 * The version install gives the package: PACKAGE_VERSION on the Make target,
 * or this checkout's (packaging/scripts/package-version.sh).
 */
export async function checkoutVersion(): Promise<string> {
  const given = process.env.LINUXIO_VM_PACKAGE_VERSION ?? "";
  if (given !== "") return given;
  const script = path.resolve(
    here,
    "../../../../packaging/scripts/package-version.sh",
  );
  return (await execFileAsync(script, [], { encoding: "utf8" })).stdout.trim();
}

/** The version of the LinuxIO package installed in the VM, "" when none is. */
export const installedVersion = async () =>
  (
    await vmSsh(
      vmDistro === "fedora"
        ? "! rpm -q linuxio >/dev/null 2>&1 || rpm -q --qf '%{VERSION}' linuxio"
        : "dpkg-query -W -f '${Version}' linuxio 2>/dev/null || true",
    )
  ).trim();

/**
 * A version above v in dpkg's and rpm's order: v with +e2e1, or the next
 * number when v already ends in +e2eN.
 */
export function nextVersion(v: string) {
  const ours = v.match(/^(.*\+e2e)(\d+)$/);
  return ours ? `${ours[1]}${Number(ours[2]) + 1}` : `${v}+e2e1`;
}

/** Version v as PackageKit names it: the RPM's carries its release, -1. */
export const packageKitVersion = (v: string) =>
  vmDistro === "fedora" ? `${v}-1` : v;

/**
 * Publishes the build in buildDir as LinuxIO version into the VM's
 * repository; note, when given, goes in the package's SELinux module as a
 * comment.
 */
export const publish = (buildDir: string, version: string, note?: string) =>
  rig(["repo-publish", buildDir, version, ...(note ? [note] : [])]);

/**
 * The shell command that installs LinuxIO from the repository, or updates it
 * to the newest version there, as an administrator does: the package
 * manager's refresh first, then the install.
 */
export const INSTALL_FROM_REPO =
  vmDistro === "fedora"
    ? "sudo dnf install -y --refresh linuxio && sudo dnf upgrade -y linuxio"
    : "sudo apt-get update -q && sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -q -o DPkg::Lock::Timeout=600 linuxio";

/** sha256sum of a file on this host. */
export async function hostSha256(file: string) {
  return createHash("sha256")
    .update(await readFile(file))
    .digest("hex");
}

/** The MainPID of unit, "0" when it does not run. */
export const mainPid = async (unit: string) =>
  (await vmSsh(`systemctl show -P MainPID ${unit}`)).trim();

/** When unit last became active, in microseconds since boot; 0 if never. */
export const activeSince = async (unit: string) =>
  Number(
    (
      await vmSsh(`systemctl show -P ActiveEnterTimestampMonotonic ${unit}`)
    ).trim(),
  );
