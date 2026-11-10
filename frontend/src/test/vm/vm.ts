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

/** The root job host's records, read from its store in the VM. */
export async function jobRecords(route?: string): Promise<JobRecord[]> {
  const out = await vmSsh(
    "sudo sh -c 'cat /var/lib/linuxio/jobs/0/*.json 2>/dev/null' | jq -cs .",
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

/** The record of a run a systemd timer starts (docs/TODO/jobs.md 2.14). */
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

export async function signIn(page: Page) {
  const username = process.env.LINUXIO_VM_USER ?? "";
  const password = process.env.LINUXIO_VM_PASSWORD ?? "";
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
