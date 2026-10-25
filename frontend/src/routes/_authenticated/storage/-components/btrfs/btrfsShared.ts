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

// Mirrors btrfs.AllowedProfiles and btrfs.MinDevices; raid5/raid6 are never
// offered.
export const PROFILE_MIN_DEVICES: Record<string, number> = {
  single: 1,
  dup: 1,
  raid0: 2,
  raid1: 2,
  raid1c3: 3,
  raid1c4: 4,
  raid10: 4,
};

// Ranked by disks lost without losing a copy, then by copies: dup survives
// bad sectors but not a dead disk, so it sits between single and raid1.
const PROFILE_PROTECTION: Record<string, number> = {
  single: 0,
  raid0: 0,
  dup: 1,
  raid1: 2,
  raid10: 2,
  raid1c3: 3,
  raid1c4: 4,
};

/**
 * The profiles that protect metadata at least as well as data is protected:
 * weaker metadata lets a failure the data survives take the whole filesystem.
 */
export const metadataProfiles = (profiles: string[], data = "") =>
  profiles.filter(
    (profile) => PROFILE_PROTECTION[profile] >= (PROFILE_PROTECTION[data] ?? 0),
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

// Copies kept of each block and the fewest devices a chunk spans, per profile.
const PROFILE_LAYOUT: Record<string, { copies: number; stripe: number }> = {
  single: { copies: 1, stripe: 1 },
  dup: { copies: 2, stripe: 1 },
  raid0: { copies: 1, stripe: 2 },
  raid1: { copies: 2, stripe: 2 },
  raid1c3: { copies: 3, stripe: 3 },
  raid1c4: { copies: 4, stripe: 4 },
  raid10: { copies: 2, stripe: 4 },
};

/**
 * Data a profile can hold on disks of the given sizes, filled the way btrfs
 * allocates: each step takes the disks with the most free space, so mixed
 * sizes are counted right. Metadata overhead is ignored.
 */
export const usableBytes = (profile: string, sizes: number[]): number => {
  const layout = PROFILE_LAYOUT[profile];
  const total = sizes.reduce((sum, size) => sum + size, 0);
  if (!layout || sizes.length < layout.stripe) return 0;
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
    if (ready < layout.stripe || width < layout.stripe) return usable;
    for (let i = 0; i < width; i++) free[i] -= step;
    usable += (width / layout.copies) * step;
  }
};
