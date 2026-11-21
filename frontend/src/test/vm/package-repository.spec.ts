import path from "node:path";

import { expect, test } from "@playwright/test";

import {
  activeSince,
  hostSha256,
  INSTALL_FROM_REPO,
  installedVersion,
  jobIds,
  jobRecord,
  mainPid,
  MiB,
  nextVersion,
  PACKAGE_REPO,
  plainBuild,
  publish,
  SCRUB,
  scrubRunning,
  signIn,
  signOut,
  signOutIfSignedIn,
  startScrub,
  stopAptRuns,
  vmDistro,
  vmSsh,
  waitForJob,
} from "./vm";

// docs/TODO/packaging.md, Release gates: LinuxIO updated from its signed
// repository from a shell, as an administrator or an unattended upgrade
// does. The VM runs the build `make vm-install` put in; each test publishes
// this checkout's build as the next version in the VM's signed repository
// (linuxio-vm.sh repo-publish) and updates to it with apt or dnf:
//
// - with a job running, the package restarts linuxio.target and nothing
//   else: LinuxIO runs the new package with no `linuxio restart`, and the job
//   host, which is outside the target, runs the job on;
// - a stopped LinuxIO stays stopped;
// - an index (Packages, repomd.xml) or a package changed after signing is
//   refused, and LinuxIO stays as it was.
//
// The jobs-update.spec.ts update goes through the Updates page instead. This
// file leaves the newest published version installed and running, the VM's
// repository and its source file in place, the apt timers stopped, and the
// e2e pool on vdb and vdc.

const ui = expect.configure({ timeout: 15_000 });

test.afterEach(({ page }) => signOutIfSignedIn(page));

/** Runs command in the VM and returns its exit status and output, without failing. */
async function attempt(command: string) {
  const out = await vmSsh(`{ ${command} ; } 2>&1; echo "exit=$?"`);
  const status = Number(out.match(/exit=(\d+)\s*$/)?.[1] ?? "-1");
  return { status, out };
}

/**
 * The LinuxIO units that are active, but the job hosts, which are outside
 * linuxio.target and never stopped or restarted by the package.
 */
const activeUnits = async () =>
  (
    await vmSsh(
      "systemctl list-units --no-legend --plain --state=active 'linuxio*' | awk '{print $1}' | grep -v '^linuxio-jobs@' | sort",
    )
  ).trim();

const isActive = async (unit: string) =>
  (await vmSsh(`systemctl is-active ${unit} || true`)).trim();

/** What the web server's process runs: its path, " (deleted)" if replaced. */
const webserverExe = async () => {
  const pid = await mainPid("linuxio-webserver.service");
  expect(pid, "the web server runs").not.toBe("0");
  return {
    pid,
    exe: (await vmSsh(`sudo readlink /proc/${pid}/exe`)).trim(),
    sha: (await vmSsh(`sudo sha256sum /proc/${pid}/exe`)).split(" ")[0],
  };
};

/**
 * The repository files a client checks against the signed index: the index
 * itself (Packages, Packages.gz and their by-hash copies; repomd.xml), or the
 * package of version.
 */
const indexFiles =
  vmDistro === "fedora"
    ? `${PACKAGE_REPO}/stable/x86_64/repodata/repomd.xml`
    : `${PACKAGE_REPO}/dists/stable/main/binary-amd64`;
const packageFile = (version: string) =>
  vmDistro === "fedora"
    ? `${PACKAGE_REPO}/stable/x86_64/linuxio-${version}-1.x86_64.rpm`
    : `${PACKAGE_REPO}/pool/main/l/linuxio/linuxio_${version}_amd64.deb`;

/** Flips every bit of the byte in the middle of file. */
const flipMiddleByte = (file: string) =>
  vmSsh(
    `python3 -I -c 'import sys; p = sys.argv[1]; b = bytearray(open(p, "rb").read()); b[len(b) // 2] ^= 0xFF; open(p, "wb").write(b)' ${file}`,
  );

// A copy of what a test changes, put back whatever happens, so the files
// after this one on the VM find a repository apt and dnf accept.
const BACKUP = "/tmp/e2e-repository-backup";
const backUp = (target: string) =>
  vmSsh(`rm -rf ${BACKUP} && cp -a ${target} ${BACKUP}`);
const restore = (target: string) =>
  vmSsh(`rm -rf ${target} && cp -a ${BACKUP} ${target} && rm -rf ${BACKUP}`);

test.describe
  .serial("LinuxIO updated from its signed repository in a shell", () => {
  test.skip(
    plainBuild() === "",
    "needs LINUXIO_VM_BUILD_DIR, the build make test-vm-browser installs",
  );

  test("an update with a job running restarts LinuxIO and leaves the job running", async ({
    page,
  }) => {
    // The bounds: an apt run 600 s, the scrub's start with the pool on a
    // fresh VM 120 s, the publish 180 s, the update 300 s, the scrub's end
    // 300 s: some 1500 s.
    test.setTimeout(1_800_000);
    await stopAptRuns();
    const installed = await installedVersion();
    expect(installed, "make vm-install installed LinuxIO").not.toBe("");

    const before = await jobIds();
    // 1 MiB/s per disk: the scrub outlasts the publish and the update.
    const { uuid, job } = await startScrub(page, before, MiB);
    const speed = (bytesPerSecond: number) =>
      vmSsh(
        `for f in /sys/fs/btrfs/${uuid}/devinfo/*/scrub_speed_max; do echo ${bytesPerSecond} | sudo tee $f >/dev/null; done`,
      );
    try {
      const host = await mainPid("linuxio-jobs@0.service");
      expect(host, "the root job host runs").not.toBe("0");
      const webserver = await webserverExe();
      const targetSince = await activeSince("linuxio.target");

      const version = nextVersion(installed);
      await publish(plainBuild(), version);
      expect(
        await scrubRunning(),
        "the scrub ended before the update: lower its speed",
      ).toBe(true);
      console.log(
        await vmSsh(`set -o pipefail; ${INSTALL_FROM_REPO} 2>&1 | tail -n 5`),
      );

      // The package restarted linuxio.target once its files were in.
      expect(await installedVersion()).toBe(version);
      expect(await isActive("linuxio.target")).toBe("active");
      expect(
        await activeSince("linuxio.target"),
        "the package restarted linuxio.target",
      ).toBeGreaterThan(targetSince);
      // The job host was not restarted and runs the job on.
      expect(await mainPid("linuxio-jobs@0.service")).toBe(host);
      expect((await jobRecord(job.id))?.state).toBe("running");
      expect(await scrubRunning()).toBe(true);

      // The web server the sign-in reaches is a new process of the new
      // package's file.
      await signIn(page);
      await ui(page.getByRole("button", { name: "Account" })).toBeVisible();
      const after = await webserverExe();
      expect(after.pid).not.toBe(webserver.pid);
      expect(after.exe).toBe("/usr/libexec/linuxio/linuxio-webserver");
      expect(after.sha).toBe(
        await hostSha256(path.join(plainBuild(), "linuxio-webserver")),
      );
      await signOut(page);

      await speed(0);
      await waitForJob(SCRUB, before, "completed", { timeout: 300_000 });
    } finally {
      await speed(0);
    }
  });

  test("an update of a stopped LinuxIO leaves it stopped", async ({ page }) => {
    // The bounds: the publish 180 s, the update 300 s, the start 60 s.
    test.setTimeout(900_000);
    await vmSsh("sudo linuxio stop");
    expect(await isActive("linuxio.target")).toBe("inactive");
    const stopped = await activeUnits();
    try {
      const version = nextVersion(await installedVersion());
      await publish(plainBuild(), version);
      console.log(
        await vmSsh(`set -o pipefail; ${INSTALL_FROM_REPO} 2>&1 | tail -n 5`),
      );
      expect(await installedVersion()).toBe(version);
      expect(await isActive("linuxio.target")).toBe("inactive");
      expect(await activeUnits(), "what was active, and only that").toBe(
        stopped,
      );
    } finally {
      await vmSsh("sudo linuxio start");
    }
    expect(await isActive("linuxio.target")).toBe("active");
    await signIn(page);
    await ui(page.getByRole("button", { name: "Account" })).toBeVisible();
  });

  test("an index changed after signing is refused, and LinuxIO stays as it was", async () => {
    test.setTimeout(900_000);
    const installed = await installedVersion();
    const version = nextVersion(installed);
    await publish(plainBuild(), version);
    await backUp(indexFiles);
    try {
      if (vmDistro === "fedora") {
        // repomd.xml no longer matches repomd.xml.asc.
        await vmSsh(`sed -i 's|<revision>|<revision>1|' ${indexFiles}`);
        // dnf5 refuses the metadata and goes on without it: it says why and
        // exits 0, with nothing to upgrade (seen on Fedora 45). A failing
        // exit would need skip_if_unavailable=False in linuxio.repo, which
        // would stop every dnf transaction on the host while the
        // repository is unreachable.
        const refused = await attempt("sudo dnf upgrade -y --refresh linuxio");
        console.log(refused.out);
        expect(refused.out).toMatch(
          /repomd\.xml GPG signature verification error/,
        );
      } else {
        // An index whose sums are no longer the ones the signed Release
        // lists, under every name apt may fetch it by.
        await vmSsh(
          `cd ${indexFiles} && plain="$(sha256sum <Packages | cut -d' ' -f1)" && gz="$(sha256sum <Packages.gz | cut -d' ' -f1)" && ` +
            "sed -i '0,/^Description: /s//Description: e2e /' Packages && gzip -9n -c Packages >Packages.gz && " +
            'cp Packages "by-hash/SHA256/${plain}" && cp Packages.gz "by-hash/SHA256/${gz}"',
        );
        const refused = await attempt("sudo apt-get update");
        console.log(refused.out);
        expect(refused.status, refused.out).not.toBe(0);
        expect(refused.out).toMatch(/Hash Sum mismatch|unexpected size/i);
        // apt keeps the lists it had: nothing newer is offered.
        console.log(
          (
            await attempt(
              "sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y linuxio",
            )
          ).out,
        );
      }
      expect(await installedVersion()).toBe(installed);
    } finally {
      await restore(indexFiles);
    }
    expect(await isActive("linuxio.target")).toBe("active");
  });

  test("a package changed after signing is refused, and LinuxIO stays as it was", async ({
    page,
  }) => {
    test.setTimeout(900_000);
    const installed = await installedVersion();
    // The version the previous test published and never installed.
    const version = nextVersion(installed);
    const file = packageFile(version);
    expect(
      (await vmSsh(`test -f ${file} && echo published || true`)).trim(),
    ).toBe("published");
    await backUp(file);
    try {
      await flipMiddleByte(file);
      const refused = await attempt(
        vmDistro === "fedora"
          ? "sudo dnf upgrade -y --refresh linuxio"
          : "sudo apt-get update -q && sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y linuxio",
      );
      console.log(refused.out);
      expect(refused.status, refused.out).not.toBe(0);
      expect(refused.out).toMatch(
        vmDistro === "fedora"
          ? /checksum|digest|signature|does not match/i
          : /Hash Sum mismatch|unexpected size/i,
      );
      expect(await installedVersion()).toBe(installed);
      expect(await isActive("linuxio.target")).toBe("active");
    } finally {
      await restore(file);
    }

    // The same package as signed installs.
    console.log(
      await vmSsh(`set -o pipefail; ${INSTALL_FROM_REPO} 2>&1 | tail -n 5`),
    );
    expect(await installedVersion()).toBe(version);
    await signIn(page);
    await ui(page.getByRole("button", { name: "Account" })).toBeVisible();
  });
});
