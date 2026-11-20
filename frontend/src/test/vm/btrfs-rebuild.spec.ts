import { expect, test, type Page } from "@playwright/test";

import {
  ensurePool,
  MiB,
  noSessions,
  POOL_MOUNT,
  rig,
  scheduledRecords,
  signIn,
  signOut,
  signOutIfSignedIn,
  vmSsh,
  waitForIdle,
} from "./vm";

// docs/btrfs.md (Missing disks, "Rebuild onto a disk that comes back"): a
// disk of the mounted test pool drops off the guest's PCI bus and comes back,
// while the pool holds its dead block device. The health
// checks, started by hand, see it back twice, start the rebuild unit as the
// installer put it, which erases the disk and replaces the member onto it,
// and the resync then makes the pool whole. The pool page shows each step.
// The same holds after a reboot, for the other disk. The rig's data disks
// carry serials (packaging/vm/linuxio-vm.sh), the identity the rebuild
// checks.

const HEALTH_TIMER = "linuxio-btrfs-health.timer";
const HEALTH_SERVICE = "linuxio-btrfs-health.service";
const REBUILD = "scheduled.btrfs_rebuild";
const RESYNC = "scheduled.btrfs_resync";
/** This check's files on the test pool. */
const DIR = `${POOL_MOUNT}/rebuild-e2e`;

test.afterEach(({ page }) => signOutIfSignedIn(page));

/** The test pool's member disks by name, such as vdb. */
const poolDisks = async () =>
  (
    await vmSsh(
      `sudo btrfs filesystem show ${POOL_MOUNT} | awk '/ path /{print $NF}'`,
    )
  )
    .split("\n")
    .filter(Boolean)
    .map((d) => d.replace("/dev/", ""))
    .sort();

/** The disk whose serial is serial, by name; "" when none is connected. */
const diskWithSerial = async (serial: string) =>
  (
    await vmSsh(
      `lsblk -dnro NAME,SERIAL | awk -v s=${serial} '$2 == s {print $1}'`,
    )
  ).trim();

/**
 * The disk systemd follows for the pool's /dev/disk/by-uuid device, which
 * the fstab mount depends on. The disk this check pulls is another one.
 */
async function boundDisk(uuid: string) {
  const unit = (
    await vmSsh(`systemd-escape -p --suffix=device /dev/disk/by-uuid/${uuid}`)
  ).trim();
  const sysfs = (await vmSsh(`systemctl show -P SysFSPath '${unit}'`)).trim();
  return sysfs.split("/").pop() ?? "";
}

/** The device the kernel shows as the mount's source, such as vdb. */
const sourceDisk = async () =>
  (await vmSsh(`findmnt -no SOURCE ${POOL_MOUNT}`)).trim().replace("/dev/", "");

/**
 * A member to pull: neither the disk the mount follows nor the one it shows
 * as its source, nor one with a serial in rebuilt, which LinuxIO does not
 * rebuild onto twice in a day. Before a health run has seen an fstab mount,
 * pulling its source unmounts the pool: systemd stops the mount when the
 * device it names as its source goes (StopPropagatedFrom=), and
 * x-systemd.device-bound=false on the line does not reach it when mount(8)
 * recorded the option under another member than the one the kernel names.
 */
async function memberToPull(uuid: string, rebuilt: string[]) {
  const avoid = [await boundDisk(uuid), await sourceDisk()];
  for (const disk of await poolDisks()) {
    const serial = (await vmSsh(`lsblk -dno SERIAL /dev/${disk}`)).trim();
    if (!avoid.includes(disk) && !rebuilt.includes(serial)) return disk;
  }
  throw new Error(
    `no member to pull besides ${avoid.join(", ")} and ${rebuilt.join(", ")}`,
  );
}

/** What the pool's mount unit stops with: "" once systemd reads the line's option. */
const mountStopsWith = async () =>
  (
    await vmSsh(
      `systemctl show -P StopPropagatedFrom $(systemd-escape -p --suffix=mount ${POOL_MOUNT})`,
    )
  ).trim();

/** Whether the pool is mounted, by the kernel's own entry for it. */
const poolMounted = async (uuid: string) =>
  (
    await vmSsh(
      `test -d /sys/fs/btrfs/${uuid} && mountpoint -q ${POOL_MOUNT} && echo yes || true`,
    )
  ).trim() === "yes";

/** The btrfs device ID disk carries in its superblock. */
const devidOf = async (disk: string) =>
  Number(
    await vmSsh(
      `sudo btrfs inspect-internal dump-super /dev/${disk} | awk '$1 == "dev_item.devid" {print $2}'`,
    ),
  );

/** LinuxIO's state of the pool (docs/btrfs.md, Missing disks). */
interface PoolState {
  degraded: boolean;
  identities?: { id: number; disk?: { serial?: string } }[];
  rebuild?: unknown;
  rebuilt?: { disk: { serial?: string } }[];
}

const poolState = async (uuid: string) =>
  JSON.parse(
    await vmSsh(
      `sudo jq -c --arg u ${uuid} '.[$u] // null' /var/lib/linuxio/btrfs-pools.json`,
    ),
  ) as PoolState | null;

/** The stored alert id (source/key), if it is raised. */
async function storedAlert(id: string) {
  const out = await vmSsh(
    `sudo jq -c --arg id '${id}' '[.alerts[] | select(.id == $id)][0]' /var/lib/linuxio/alerts.json`,
  );
  return JSON.parse(out) as {
    severity: string;
    title: string;
    message: string;
  } | null;
}

/** Takes disk off the guest's PCI bus, as a disk that drops out. */
async function pullDisk(disk: string) {
  const fn = (
    await vmSsh(`basename "$(readlink -f /sys/block/${disk}/device/..)"`)
  ).trim();
  expect(fn, `the PCI function of ${disk}`).toMatch(
    /^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$/,
  );
  await vmSsh(
    `echo 1 | sudo tee /sys/bus/pci/devices/${fn}/remove >/dev/null && sudo udevadm settle`,
  );
  expect(await vmSsh(`test -e /dev/${disk} && echo there || true`)).toBe("");
}

/** Rescans the PCI bus and returns the name the disk with serial came back as. */
async function plugBack(serial: string) {
  await vmSsh(
    "echo 1 | sudo tee /sys/bus/pci/rescan >/dev/null && sudo udevadm settle",
  );
  let name = "";
  await expect
    .poll(async () => (name = await diskWithSerial(serial)), {
      intervals: [1_000],
      message: `the disk ${serial} back on the bus`,
      timeout: 30_000,
    })
    .not.toBe("");
  return name;
}

/** Runs one health check now and waits for it to end. */
const healthRun = () => vmSsh(`sudo systemctl start ${HEALTH_SERVICE}`);

/** Stops the health timer until the next boot, so only this check runs them. */
async function holdHealthChecks() {
  await vmSsh(`sudo systemctl stop ${HEALTH_TIMER}`);
  await waitForIdle(HEALTH_SERVICE);
}

/** The SHA-256 of each file of the check. */
const hashFiles = () =>
  vmSsh(`sudo sh -c 'cd ${DIR} && sha256sum -- *.bin' 2>&1 || true`);

/** Shows the pool page until it shows the health heading title. */
async function pageShows(page: Page, uuid: string, title: string) {
  await expect(async () => {
    await page.goto(`/storage?pool=${uuid}`);
    await expect(page.getByRole("heading", { name: title })).toBeVisible({
      timeout: 3_000,
    });
  }).toPass({ intervals: [2_000], timeout: 120_000 });
}

/** The runs of route on the pool's unit since earlier, oldest first. */
async function runsSince(route: string, unit: string, earlier: Set<string>) {
  return (await scheduledRecords(route))
    .filter((r) => r.executor.unit === unit && !earlier.has(r.id))
    .map((r) => ({
      state: r.state,
      message: (r.result as { message?: string } | undefined)?.message,
    }));
}

/**
 * Waits until systemd mounted the test pool at boot: the boot check has udev
 * evaluate a complete pool's disks again when it left them unready.
 */
async function mountedAtBoot(uuid: string) {
  await expect
    .poll(
      async () =>
        (await vmSsh(`findmnt -no UUID ${POOL_MOUNT} || true`)).trim(),
      {
        intervals: [2_000],
        message: "systemd mounting the pool at boot",
        timeout: 150_000,
      },
    )
    .toBe(uuid);
}

/**
 * One dropout and return of disk while the pool stays mounted, rebuilt by
 * the unit the health check starts and resynced after it. With page, the
 * pool page is followed through each step.
 */
async function dropAndRebuild(uuid: string, disk: string, page?: Page) {
  const serial = (await vmSsh(`lsblk -dno SERIAL /dev/${disk}`)).trim();
  const devid = await devidOf(disk);
  const rebuildUnit = `linuxio-btrfs-rebuild@${uuid}.service`;
  const resyncUnit = `linuxio-btrfs-resync@${uuid}.service`;
  const earlierRebuilds = new Set(
    (await scheduledRecords(REBUILD)).map((r) => r.id),
  );
  const earlierResyncs = new Set(
    (await scheduledRecords(RESYNC)).map((r) => r.id),
  );

  await healthRun();
  expect(
    (await poolState(uuid))?.identities?.find((m) => m.id === devid)?.disk
      ?.serial,
    "the identity the health check recorded while the pool was whole",
  ).toBe(serial);

  await pullDisk(disk);
  expect(await poolMounted(uuid), "the pool still mounted").toBe(true);
  await vmSsh(
    `sudo bash -c 'for i in $(seq -w 1 4); do head -c 16M /dev/urandom >${DIR}/out-${disk}-$i.bin; done; sync'`,
  );
  await healthRun();
  if (page) await pageShows(page, uuid, "Disk dropped out");

  const back = await plugBack(serial);
  // Under a new name while the pool holds the dead block device, as a
  // rule; the rebuild does not depend on it.
  console.log(`${disk} (${serial}, devid ${devid}) came back as ${back}`);
  await healthRun();
  if (page) await pageShows(page, uuid, "Disk back");
  expect(await runsSince(REBUILD, rebuildUnit, earlierRebuilds)).toEqual([]);

  // Paced, so the page sees the copy, then the resync's scrub, run.
  await vmSsh(
    `for f in /sys/fs/btrfs/${uuid}/devinfo/*/scrub_speed_max; do echo ${16 * MiB} | sudo tee $f >/dev/null; done`,
  );
  await healthRun();
  if (page) {
    await pageShows(page, uuid, "Rebuilding");
    await expect(
      page.getByText(`onto /dev/${back}, the disk that came back`),
    ).toBeVisible();
  }
  await expect
    .poll(() => runsSince(REBUILD, rebuildUnit, earlierRebuilds), {
      intervals: [2_000],
      message: "the rebuild run",
      timeout: 300_000,
    })
    .toEqual([{ state: "completed", message: "Rebuilt" }]);
  if (page) {
    await expect(async () => {
      await page.goto(`/storage?pool=${uuid}`);
      await expect(page.getByText(/^Checking for errors · /)).toBeVisible({
        timeout: 3_000,
      });
    }).toPass({ intervals: [1_000], timeout: 120_000 });
  }
  await vmSsh(
    `for f in /sys/fs/btrfs/${uuid}/devinfo/*/scrub_speed_max; do echo 0 | sudo tee $f >/dev/null; done`,
  );
  await expect
    .poll(() => runsSince(RESYNC, resyncUnit, earlierResyncs), {
      intervals: [2_000],
      message: "the resync run the rebuild's health check started",
      timeout: 300_000,
    })
    .toEqual([{ state: "completed", message: "Resynced" }]);
  if (page) await pageShows(page, uuid, "Healthy");

  expect((await poolDisks()).includes(back), `${back} is a member`).toBe(true);
  const state = await poolState(uuid);
  expect(state?.degraded).toBe(false);
  expect(state?.rebuild).toBeUndefined();
  expect(state?.rebuilt?.some((r) => r.disk.serial === serial)).toBe(true);
  expect(await vmSsh(`sudo btrfs filesystem df ${POOL_MOUNT}`)).not.toMatch(
    /^(Data|Metadata|System), single:/m,
  );
  expect(await storedAlert(`btrfs/rebuild-${uuid}`)).toBeNull();
  expect(await storedAlert(`btrfs/rebuilt-${uuid}`)).toMatchObject({
    severity: "info",
    message: expect.stringContaining(`/dev/${back}`),
  });
  expect(await storedAlert(`btrfs/health-${uuid}`)).toBeNull();
  // The unit ran as the installer put it.
  expect(
    (await vmSsh(`systemctl show -P FragmentPath ${rebuildUnit}`)).trim(),
  ).toBe("/etc/systemd/system/linuxio-btrfs-rebuild@.service");
  return back;
}

test("a disk pulled from a mounted pool and put back is rebuilt and resynced, also after a reboot", async ({
  page,
}) => {
  test.setTimeout(1_800_000);
  await signIn(page);
  const uuid = await ensurePool(page);
  await page.goto(`/storage?pool=${uuid}`);
  // A session's listing enables and starts the health timer.
  await expect
    .poll(
      async () =>
        (await vmSsh(`systemctl is-enabled ${HEALTH_TIMER} || true`)).trim(),
      { message: HEALTH_TIMER, timeout: 30_000 },
    )
    .toBe("enabled");
  await expect(
    page.getByRole("checkbox", {
      name: "Rebuild automatically onto a disk that returns",
    }),
  ).toBeChecked();
  for (const disk of ["vdb", "vdc", "vdd", "vde"]) {
    expect(
      (await vmSsh(`lsblk -dno SERIAL /dev/${disk}`)).trim(),
      `${disk}'s serial: a VM defined before the rig gave its disks one gets them at its next make vm-reset`,
    ).toBe(`linuxio-${disk}`);
  }
  try {
    await holdHealthChecks();
    await vmSsh(
      `sudo bash -c 'rm -rf ${DIR} && mkdir ${DIR} && for i in $(seq -w 1 8); do head -c 32M /dev/urandom >${DIR}/before-$i.bin; done; sync'`,
    );
    // Two more disks, so that after the reboot a member is left that is
    // neither the disk the mount is bound to, its source, nor the one
    // rebuilt onto.
    await vmSsh(`sudo btrfs device add /dev/vdd /dev/vde ${POOL_MOUNT}`);
    console.log(
      `the mount is bound to ${await boundDisk(uuid)}, its source is ${await sourceDisk()}`,
    );
    const first = await memberToPull(uuid, []);
    const firstSerial = (await vmSsh(`lsblk -dno SERIAL /dev/${first}`)).trim();
    await dropAndRebuild(uuid, first, page);
    const hashes = await hashFiles();
    expect(hashes).toMatch(/^([0-9a-f]{64} {2}\S+\.bin\n)+$/);
    await signOut(page);
    await noSessions();

    // The rebuilt pool after a reboot, and the installed unit rebuilding the
    // other disk in that boot.
    await rig(["reboot"]);
    await mountedAtBoot(uuid);
    await holdHealthChecks();
    expect(await hashFiles(), "every file after the reboot").toBe(hashes);
    const scrub = await vmSsh(
      `sudo btrfs scrub start -B -r ${POOL_MOUNT} 2>&1; echo "exit $?"`,
    );
    console.log(scrub);
    expect(scrub).toMatch(/^exit 0$/m);
    expect(scrub).toMatch(/^Error summary:\s+no errors found$/m);
    console.log(
      `after the reboot the mount is bound to ${await boundDisk(uuid)}, its source is ${await sourceDisk()}: ${await vmSsh(
        `systemctl show -p BindsTo -p StopPropagatedFrom -p SourcePath $(systemd-escape -p --suffix=mount ${POOL_MOUNT})`,
      )}`,
    );
    // The health run has systemd apply the line's option to the mount made at
    // boot (docs/btrfs.md, "Pools"): the mount then stops with no device, and
    // the pull of its source disk, which unmounted the pool before, leaves it
    // mounted for the rebuild. The source is pulled unless it is the disk
    // rebuilt before the reboot, which is not rebuilt onto twice in a day.
    await healthRun();
    await expect.poll(mountStopsWith, { timeout: 30_000 }).toBe("");
    const source = await sourceDisk();
    const sourceSerial = (
      await vmSsh(`lsblk -dno SERIAL /dev/${source}`)
    ).trim();
    const second =
      sourceSerial === firstSerial
        ? await memberToPull(uuid, [firstSerial])
        : source;
    console.log(
      `after the health run the mount stops with nothing; pulling ${second}${second === source ? ", its source" : " (the source was rebuilt before the reboot)"}`,
    );
    await dropAndRebuild(uuid, second);
    const after = await hashFiles();
    for (const line of hashes.trim().split("\n")) {
      expect(after, "every file after the second rebuild").toContain(line);
    }
  } catch (error) {
    console.log(
      await vmSsh(
        `sudo journalctl -b -o short-monotonic --no-pager -u ${HEALTH_SERVICE} -u 'linuxio-btrfs-rebuild@*' -u 'linuxio-btrfs-resync@*' | tail -n 80`,
      ).catch((e: unknown) => String(e)),
    );
    throw error;
  } finally {
    // Every disk back, the pool mounted, its throttles lifted and the health
    // checks running again; a cleanup that fails is logged.
    await vmSsh(
      "sudo bash -s",
      `echo 1 >/sys/bus/pci/rescan
udevadm settle
for f in /sys/fs/btrfs/${uuid}/devinfo/*/scrub_speed_max; do echo 0 >"$f"; done
rm -rf ${DIR}
for serial in linuxio-vdd linuxio-vde; do
  added=$(lsblk -dnro NAME,SERIAL | awk -v s=$serial '$2 == s {print $1}')
  if [ -n "$added" ] && btrfs filesystem show ${POOL_MOUNT} | grep -q "/dev/$added$"; then
    btrfs device remove "/dev/$added" ${POOL_MOUNT}
  fi
done
systemctl start ${HEALTH_TIMER}
`,
    ).catch((e: unknown) => console.log(`cleanup failed: ${String(e)}`));
  }
});
