export const BTRFS_TOAST_META = {
  label: "Open Btrfs",
  to: "/storage/btrfs",
} as const;

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
