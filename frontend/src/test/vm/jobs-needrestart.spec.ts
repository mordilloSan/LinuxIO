import path from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";

import {
  jobIds,
  jobRecord,
  MiB,
  noSessions,
  rig,
  SCRUB,
  scrubRunning,
  signOut,
  signOutIfSignedIn,
  startScrub,
  stopAptRuns,
  vmSsh,
  waitForJob,
} from "./vm";

// docs/TODO/jobs.md 3.9, finding 2: on Ubuntu, apt runs needrestart after each
// dpkg run, and needrestart restarts every service still running a binary
// that was replaced. An update replaces the binaries and leaves the job hosts
// running (docs/TODO/jobs.md 2.12), so the installer ships an override,
// packaging/etc/needrestart/conf.d/linuxio.conf, that exempts them. This check
// installs the build `make vm-install` installed a second time, under a
// running scrub, and runs `needrestart -u NeedRestart::UI::stdio -l` itself as
// root, in list mode (-r l, which restarts nothing) and in automatic mode
// (-r a). apt's hook runs `apt-pinvoke -m u` instead, which this check does
// not run; in needrestart's source -m u selects automatic mode, which decides
// the restarts by the same override. -l checks libraries only, so that a
// pending kernel asks for nothing.
//
// It leaves the apt timers stopped until the next boot, the e2e pool on vdb
// and vdc that ensurePool creates on a fresh VM (with its 1 GiB fill.bin), the
// scrub's completed record in the root job host's store, that host on a
// replaced binary, /usr/local/bin/linuxio-monitoring rewritten (a copy renamed
// over it) and linuxio-monitoring.service restarted; `make vm-reset` undoes
// all of it.

const OVERRIDE = "/etc/needrestart/conf.d/linuxio.conf";
const HOST = "linuxio-jobs@0.service";
/** A daemon the override does not exempt: the installer restarts it. */
const DAEMON = "linuxio-monitoring.service";
const DAEMON_BINARY = "/usr/local/bin/linuxio-monitoring";
/** The build directory `make vm-install` builds (the Makefile's default BIN_DIR). */
const BUILD = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../../build",
);
/** The headings of needrestart's report (NeedRestart::UI::stdio). */
const TO_RESTART = {
  l: "Services to be restarted:",
  a: "Restarting services...",
};
const DEFERRED = "Service restarts being deferred:";

test.afterEach(({ page }) => signOutIfSignedIn(page));

const mainPid = async (unit: string) =>
  (await vmSsh(`systemctl show -P MainPID ${unit}`)).trim();

/** The binary process pid runs; a replaced one reads `<path> (deleted)`. */
const exe = async (pid: string) =>
  (await vmSsh(`sudo readlink /proc/${pid}/exe`)).trim();

/** The number of entries needrestart's configuration puts in override_rc. */
const overrides = async () =>
  Number(
    await vmSsh(
      `sudo perl -e 'our %nrconf; our $LOGPREF = ""; do "/etc/needrestart/needrestart.conf"; die $@ if $@; print scalar keys %{$nrconf{override_rc}}'`,
    ),
  );

/**
 * Runs needrestart as root in mode and returns the services its report lists
 * to restart (in list mode) or as restarted (in automatic mode), and those it
 * defers. It fails if needrestart exits with an error, as it does when a
 * configuration file does not parse.
 */
async function needrestart(mode: "l" | "a") {
  const out = await vmSsh(
    `sudo needrestart -u NeedRestart::UI::stdio -r ${mode} -l 2>&1`,
  );
  // Without a terminal, debconf says how it falls back to its
  // non-interactive frontend.
  const report = out
    .split("\n")
    .filter((line) => !line.startsWith("debconf: "));
  console.log(`needrestart -r ${mode} -l:\n${report.join("\n").trim()}`);
  const listed = new Map<string, string[]>();
  let heading = "";
  for (const line of report) {
    if (/^\S/.test(line)) {
      heading = line.trim();
      continue;
    }
    const units = [...line.matchAll(/\S+\.service\b/g)].map((m) => m[0]);
    listed.set(heading, [...(listed.get(heading) ?? []), ...units]);
  }
  return {
    restart: listed.get(TO_RESTART[mode]) ?? [],
    deferred: listed.get(DEFERRED) ?? [],
  };
}

test("needrestart leaves a job host that runs a replaced binary alone", async ({
  page,
}) => {
  // The bounds of the waits below: an apt run 600 s, the scrub's start (with
  // the pool on a fresh VM) about 120 s, the sessions 30 s, the install about
  // 60 s, the scrub's end 300 s: some 1110 s.
  test.setTimeout(1_200_000);
  await stopAptRuns();
  expect(
    await vmSsh(`test -f ${OVERRIDE} && echo installed || echo missing`),
    "make vm-install installed the override",
  ).toBe("installed\n");

  const before = await jobIds();
  // 1 MiB/s per disk: the scrub outlasts the install and needrestart by
  // minutes. The cap is lifted once they are done.
  const { uuid, job } = await startScrub(page, before, MiB);
  const speed = (bytesPerSecond: number) =>
    vmSsh(
      `for f in /sys/fs/btrfs/${uuid}/devinfo/*/scrub_speed_max; do echo ${bytesPerSecond} | sudo tee $f >/dev/null; done`,
    );
  try {
    const host = await mainPid(HOST);
    expect(host, "the root job host runs").not.toBe("0");
    // A session's bridge would run a replaced binary too.
    await signOut(page);
    await noSessions();

    expect(
      await scrubRunning(),
      "the scrub ended before the install: lower its speed",
    ).toBe(true);
    const installed = await rig(["install", BUILD]);
    expect(installed).toContain(`Installed ${OVERRIDE}`);
    expect(await mainPid(HOST), "the install left the job host running").toBe(
      host,
    );
    expect(await exe(host)).toBe("/usr/local/bin/linuxio-bridge (deleted)");
    expect((await jobRecord(job.id))?.state).toBe("running");

    // The install restarted the daemons. Replace one daemon's binary as the
    // installer does (a new file renamed over it) and leave it running.
    await vmSsh(
      `sudo cp -p ${DAEMON_BINARY} ${DAEMON_BINARY}.e2e && sudo mv -f ${DAEMON_BINARY}.e2e ${DAEMON_BINARY}`,
    );
    const daemon = await mainPid(DAEMON);
    expect(daemon, "the daemon runs").not.toBe("0");
    expect(await exe(daemon)).toBe(`${DAEMON_BINARY} (deleted)`);

    // The control: with the override moved away, needrestart would restart
    // the job host. The override adds its six entries to the distribution's.
    const withOverride = await overrides();
    await vmSsh(`sudo mv ${OVERRIDE} ${OVERRIDE}.off`);
    try {
      expect(await overrides(), "override_rc without the override").toBe(
        withOverride - 6,
      );
      const control = await needrestart("l");
      expect(control.restart).toEqual(expect.arrayContaining([HOST, DAEMON]));
      expect(control.deferred).not.toContain(HOST);
    } finally {
      await vmSsh(
        `sudo sh -c 'test ! -e ${OVERRIDE}.off || mv ${OVERRIDE}.off ${OVERRIDE}'`,
      );
    }

    // With the override, the job host is deferred and the daemon is not.
    const listed = await needrestart("l");
    expect(listed.restart).toContain(DAEMON);
    expect(listed.restart).not.toContain(HOST);
    expect(listed.deferred).toContain(HOST);

    // Automatic mode restarts the daemon and leaves the host and its scrub.
    const restarted = await needrestart("a");
    expect(restarted.restart).toContain(DAEMON);
    expect(restarted.restart).not.toContain(HOST);
    expect(restarted.deferred).toContain(HOST);
    const newDaemon = await mainPid(DAEMON);
    expect(newDaemon, "needrestart restarted the daemon").not.toBe(daemon);
    expect(newDaemon).not.toBe("0");
    expect(await mainPid(HOST), "needrestart left the job host").toBe(host);
    expect((await jobRecord(job.id))?.state).toBe("running");
    expect(await scrubRunning(), "the scrub runs on").toBe(true);

    await speed(0);
    await waitForJob(SCRUB, before, "completed", { timeout: 300_000 });
    expect(await mainPid(HOST), "the same host ran the scrub to its end").toBe(
      host,
    );
  } finally {
    await speed(0);
  }
});
