import type { BtrfsSnapshotRule } from "@/api";

export const BTRFS_TOAST_META = {
  label: "Open subvolumes",
  to: "/storage/subvolumes",
} as const;

// Devices, health and settings live with the pool on the Disks tab.
export const BTRFS_POOL_TOAST_META = {
  label: "Open storage",
  to: "/storage",
} as const;

// LinuxIO's own top-level mounts vanish at reboot, so nothing outside LinuxIO
// should point into them.
export const TRANSIENT_TOP = "/run/linuxio/btrfs/";

// Subvolumes that distros and container runtimes create for themselves. They
// are hidden by default because snapshotting or deleting them helps nobody.
const SYSTEM_SUBVOLUME_PATHS = [
  "var/lib/portables",
  "var/lib/machines",
  "var/lib/docker",
  "var/lib/containers",
  "docker/btrfs/subvolumes",
  ".snapshots",
];

export const isSystemSubvolume = (path: string) =>
  SYSTEM_SUBVOLUME_PATHS.some(
    (system) =>
      path === system ||
      path.startsWith(`${system}/`) ||
      path.endsWith(`/${system}`) ||
      path.includes(`/${system}/`),
  );

/**
 * The profiles LinuxIO creates and converts to (raid5 and raid6 never), as
 * the allowed entries of `profiles` in backend/common/btrfs/devices.go have
 * them; btrfsShared.test.ts reads that table and fails when they differ.
 * minDevices is the fewest disks, and the fewest a chunk spans; copies are
 * kept of each block; rank orders by disk failures survived, then copies:
 * dup survives bad sectors but not a dead disk, so it sits between single
 * and raid1.
 */
export const BTRFS_PROFILES: Record<
  string,
  { minDevices: number; copies: number; rank: number }
> = {
  single: { minDevices: 1, copies: 1, rank: 0 },
  dup: { minDevices: 1, copies: 2, rank: 1 },
  raid0: { minDevices: 2, copies: 1, rank: 0 },
  raid1: { minDevices: 2, copies: 2, rank: 2 },
  raid1c3: { minDevices: 3, copies: 3, rank: 3 },
  raid1c4: { minDevices: 4, copies: 4, rank: 4 },
  raid10: { minDevices: 4, copies: 2, rank: 2 },
};

/** The profiles that fit on `devices` disks. */
export const profilesFor = (devices: number) =>
  Object.keys(BTRFS_PROFILES).filter(
    (profile) => BTRFS_PROFILES[profile].minDevices <= devices,
  );

/**
 * The profiles that protect metadata at least as well as data is protected:
 * weaker metadata lets a failure the data survives take the whole filesystem.
 */
export const metadataProfiles = (profiles: string[], data = "") =>
  profiles.filter(
    (profile) =>
      // A profile LinuxIO does not offer, such as raid5, is never one.
      (BTRFS_PROFILES[profile]?.rank ?? -1) >=
      (BTRFS_PROFILES[data]?.rank ?? 0),
  );

export const METADATA_HINT =
  "Filesystem structure and checksums. Kept at least as redundant as data.";

/** dup on one disk, raid1 on two, raid1c3 from three; else the first allowed. */
export const recommendedMetadata = (options: string[], disks: number) => {
  const wanted = disks >= 3 ? "raid1c3" : disks === 2 ? "raid1" : "dup";
  return options.includes(wanted) ? wanted : options.at(0);
};

// Hourly snapshots kept for a day, then daily, weekly and monthly ones: the
// rule the switch turns on, and the dialog's starting point. 03:00 Sunday
// only matters once the cadence is changed to daily or weekly.
export const defaultRule = (uuid: string, path: string): BtrfsSnapshotRule => ({
  uuid,
  path,
  enabled: false,
  every: "hourly",
  hour: 3,
  weekday: 0,
  keep: { hourly: 24, daily: 7, weekly: 4, monthly: 6 },
  beforeUpdates: false,
});

/**
 * Data a profile can hold on disks of the given sizes, filled the way btrfs
 * allocates: each step takes the disks with the most free space, so mixed
 * sizes are counted right. Metadata overhead is ignored.
 */
export const usableBytes = (profile: string, sizes: number[]): number => {
  const layout = BTRFS_PROFILES[profile];
  const total = sizes.reduce((sum, size) => sum + size, 0);
  if (!layout || sizes.length < layout.minDevices) return 0;
  if (profile === "single") return total;
  if (profile === "dup") return total / 2;
  const free = [...sizes];
  const step = total / 2000;
  let usable = 0;
  for (;;) {
    free.sort((a, b) => b - a);
    const ready = free.filter((f) => f >= step).length;
    // raid0 and raid10 stripe over every disk with space, raid10 in pairs;
    // the mirrored profiles write each copy to a different disk.
    const width =
      profile === "raid0"
        ? ready
        : profile === "raid10"
          ? ready - (ready % 2)
          : layout.copies;
    if (ready < layout.minDevices || width < layout.minDevices) return usable;
    for (let i = 0; i < width; i++) free[i] -= step;
    usable += (width / layout.copies) * step;
  }
};
