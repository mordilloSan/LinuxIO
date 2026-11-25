import type { BtrfsDevice, BtrfsFilesystem } from "@/api";

export const GiB = 1024 ** 3;

const noErrors = { write: 0, read: 0, flush: 0, corruption: 0, generation: 0 };

export const poolDevice = (id: number, path: string): BtrfsDevice => ({
  id,
  path,
  sizeBytes: 100 * GiB,
  usedBytes: 51 * GiB,
  missing: false,
  physicalBytes: 100 * GiB,
  growable: false,
  errors: noErrors,
});

/** A healthy two-disk raid1 pool, "tank" at /mnt/tank. */
export const poolFixture = (
  overrides: Partial<BtrfsFilesystem> = {},
): BtrfsFilesystem => ({
  uuid: "uuid-tank",
  label: "tank",
  mountpoints: [
    {
      mountpoint: "/mnt/tank",
      device: "/dev/sda",
      uuid: "uuid-tank",
      subvol: "/",
      subvolId: 5,
      options: [],
    },
  ],
  totalBytes: 200 * GiB,
  unallocatedBytes: 98 * GiB,
  usedBytes: 82 * GiB,
  storedBytes: 41 * GiB,
  freeBytes: 59 * GiB,
  allocation: [
    {
      kind: "data",
      profile: "raid1",
      totalBytes: 50 * GiB,
      usedBytes: 40 * GiB,
    },
    { kind: "metadata", profile: "raid1", totalBytes: GiB, usedBytes: GiB },
  ],
  devices: [poolDevice(1, "/dev/sda"), poolDevice(2, "/dev/sdb")],
  scrub: {
    status: "none",
    errorCount: 0,
    uncorrectableErrors: 0,
    correctedErrors: 0,
    bytesScrubbed: 0,
    percent: 0,
  },
  degraded: false,
  lowMetadataSpace: false,
  autoRebuild: true,
  ...overrides,
});

/** An unlabelled one-disk pool. */
export const singlePool = poolFixture({
  uuid: "uuid-root",
  label: "",
  allocation: [
    {
      kind: "data",
      profile: "single",
      totalBytes: 50 * GiB,
      usedBytes: 40 * GiB,
    },
  ],
  devices: [poolDevice(1, "/dev/nvme0n1p2")],
});
