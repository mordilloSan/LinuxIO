import { expect, test } from "@playwright/test";

import {
  createSchedule,
  enableSchedule,
  noSessions,
  rig,
  runNow,
  runsOf,
  scheduledOutput,
  signIn,
  signOut,
  signOutIfSignedIn,
  uniqueName,
  unitOf,
  vmSsh,
  waitForIdle,
  waitForRun,
} from "./vm";

// docs/scheduled-scripts.md, Testing: what a scheduled script does when its
// timeout passes, when its time passed while the host was off, and when
// LinuxIO's own processes restart while it runs. Like jobs-phase4.spec.ts,
// each script is a schedule created on the Schedules page.

test.afterEach(({ page }) => signOutIfSignedIn(page));

const unitProperty = async (unit: string, property: string) =>
  (await vmSsh(`systemctl show -p ${property} --value ${unit}`)).trim();

/**
 * The alert raised for scheduled run id, if one was. A host that never raised
 * an alert has no alerts file yet.
 */
async function alertOf(id: string) {
  const out = await vmSsh(
    `sudo sh -c 'cat /var/lib/linuxio/alerts.json 2>/dev/null || echo "{}"' | jq -c '[(.alerts // [])[] | select(.id == "jobs/${id}")][0]'`,
  );
  return JSON.parse(out) as { title: string; message: string } | null;
}

/** The lines of run id's saved output without their journal prefix. */
const outputText = async (id: string) =>
  (await scheduledOutput(id))
    .toString("utf8")
    .split("\n")
    .filter(Boolean)
    .map((line) => line.replace(/^\S+ \S+ linuxio-schedule\[\d+\]: /, ""));

test("a script that passes its timeout is killed and recorded as failed", async ({
  page,
}) => {
  test.setTimeout(180_000);
  await signIn(page);
  const name = uniqueName("timeout");
  // The odd sleep time names the process, to see that systemd ended it too.
  const id = await createSchedule(
    page,
    name,
    "echo started; sleep 31337; echo unreachable",
    { timeoutSeconds: 8 },
  );
  await runNow(page, name);
  const run = await waitForRun(id, "running");
  const started = Date.now();
  const ended = await waitForRun(id, "failed", { timeout: 60_000 });
  expect(Date.now() - started, "seconds the run lasted").toBeLessThan(40_000);
  expect(ended).toMatchObject({
    id: run.id,
    error: { message: "timed out" },
  });
  await waitForIdle(unitOf(id));
  expect(await unitProperty(unitOf(id), "Result")).toBe("timeout");
  expect(
    await vmSsh("pgrep -f '[s]leep 31337' || echo none"),
    "the script's child process",
  ).toBe("none\n");
  expect(await outputText(run.id)).toEqual(["started"]);
  await expect
    .poll(() => alertOf(run.id), { timeout: 15_000 })
    .toMatchObject({ title: `${name} failed`, message: "timed out" });
});

test("a missed activation runs at the next boot only when the schedule asks", async ({
  page,
}) => {
  test.setTimeout(900_000);
  await signIn(page);
  // Both run once a day, three minutes from now by the VM's clock. The first
  // asks to catch up a missed activation, the second does not.
  const dueAt = (await vmSsh("date -u -d '+3 min' +%Y-%m-%dT%H:%M:00Z")).trim();
  const due = Date.parse(dueAt);
  const calendar = `*-*-* ${dueAt.slice(11, 19)}`;
  const caught = uniqueName("catch up");
  const skipped = uniqueName("no catch up");
  const caughtId = await createSchedule(page, caught, "echo caught up", {
    calendar,
    persistent: true,
  });
  const skippedId = await createSchedule(page, skipped, "echo skipped", {
    calendar,
  });
  await enableSchedule(page, caught, caughtId);
  await enableSchedule(page, skipped, skippedId);
  expect(await runsOf(caughtId)).toHaveLength(0);
  await signOut(page);
  await page.close();

  // Off for four minutes: the time passes while the VM is down, and it boots
  // after it.
  await rig(["off-for", "240"]);
  const booted = Number(
    (
      await vmSsh("echo $(( $(date -u +%s) - $(cut -d. -f1 /proc/uptime) ))")
    ).trim(),
  );
  expect(booted * 1000, "the VM booted after the time").toBeGreaterThan(due);

  const run = await waitForRun(caughtId, "completed", { timeout: 120_000 });
  const started = Date.parse(run.created_at);
  expect(
    started,
    "the catch-up ran after the boot, not at the time it was due",
  ).toBeGreaterThan(due + 60_000);
  expect(started - booted * 1000, "seconds after the boot").toBeLessThan(
    120_000,
  );
  expect(await outputText(run.id)).toEqual(["caught up"]);
  expect(
    await unitProperty(`linuxio-schedule-${caughtId}.timer`, "LastTriggerUSec"),
  ).not.toBe("");

  // The other timer was due as well and ran nothing; it waits for tomorrow.
  expect(await runsOf(skippedId), "the schedule that did not ask").toHaveLength(
    0,
  );
  expect(
    await unitProperty(unitOf(skippedId), "ActiveEnterTimestamp"),
    "the unit of the schedule that did not ask",
  ).toBe("");
});

test("a run keeps going and is recorded when the job host and the bridges restart", async ({
  page,
}) => {
  test.setTimeout(300_000);
  await signIn(page);
  const name = uniqueName("restarts");
  const id = await createSchedule(
    page,
    name,
    "echo before; sleep 90; echo after",
  );
  await runNow(page, name);
  const run = await waitForRun(id, "running");
  const invocation = await unitProperty(unitOf(id), "InvocationID");

  // The root job host: the one that lists the run and marks runs of a host
  // that stopped. A run of this boot that systemd still runs stays running.
  await vmSsh("sudo systemctl restart linuxio-jobs@0.service");
  expect(
    (await runsOf(id)).at(-1),
    "after the job host restarted",
  ).toMatchObject({ id: run.id, state: "running" });
  // The control plane (webserver, auth socket), then the session's bridge,
  // which holds the page's live output stream. (Not `restart --full`: it
  // can leave the webserver socket failed on a restart race.)
  await vmSsh("sudo linuxio restart");
  expect((await runsOf(id)).at(-1), "after linuxio restart").toMatchObject({
    id: run.id,
    state: "running",
  });
  await vmSsh("sudo systemctl stop 'linuxio-auth@*.service'");
  await noSessions();
  expect(
    (await runsOf(id)).at(-1),
    "after the session bridge stopped",
  ).toMatchObject({ id: run.id, state: "running" });
  expect(await unitProperty(unitOf(id), "InvocationID")).toBe(invocation);

  const ended = await waitForRun(id, "completed", { timeout: 150_000 });
  expect(ended.id).toBe(run.id);
  await waitForIdle(unitOf(id));
  expect(await outputText(run.id)).toEqual(["before", "after"]);
  expect(await alertOf(run.id)).toBeNull();
  await page.close();
});
