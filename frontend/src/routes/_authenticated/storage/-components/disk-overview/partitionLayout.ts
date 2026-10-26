import type {
  BtrfsFilesystem,
  BtrfsOfflinePool,
  StorageBlockDevice,
} from "@/api";

export type LayoutRow =
  | {
      kind: "partition";
      device: StorageBlockDevice;
      reason: string | null;
      /** Name of the btrfs pool it belongs to, whatever `reason` says. */
      pool: string | null;
    }
  | { kind: "free"; startBytes: number; sizeBytes: number };

const rowStart = (row: LayoutRow) =>
  row.kind === "free" ? row.startBytes : (row.device.startBytes ?? 0);

const poolOf = (
  device: StorageBlockDevice,
  btrfs: BtrfsFilesystem[],
  offline: BtrfsOfflinePool[],
) => {
  const pool =
    btrfs.find((fs) => fs.devices.some((d) => d.path === device.path)) ??
    offline.find((fs) => fs.devices.includes(device.path));
  return pool ? pool.label || pool.uuid : null;
};

/**
 * Why a disk or partition cannot be formatted, deleted or reinitialized, or
 * null. The bridge re-checks all of this; the reasons only explain disabled
 * actions.
 */
const deviceReason = (device: StorageBlockDevice, pool: string | null) => {
  if (device.busy) return device.busy;
  if (device.inFstab) return "In /etc/fstab";
  return pool ? `Member of btrfs pool ${pool}` : null;
};

/** The disk from start to end: its partitions and free regions, by start. */
export const diskLayout = (
  disk: StorageBlockDevice,
  devices: StorageBlockDevice[],
  btrfs: BtrfsFilesystem[],
  offline: BtrfsOfflinePool[],
): LayoutRow[] => {
  const rows: LayoutRow[] = [
    ...devices
      .filter((d) => d.type === "part" && d.parents.includes(disk.path))
      .map((device) => {
        const pool = poolOf(device, btrfs, offline);
        return {
          kind: "partition" as const,
          device,
          reason: deviceReason(device, pool),
          pool,
        };
      }),
    ...(disk.freeRegions ?? []).map((region) => ({
      kind: "free" as const,
      ...region,
    })),
  ];
  return rows.sort((a, b) => rowStart(a) - rowStart(b));
};

/** Why the disk cannot be initialized, or null. */
export const initializeBlocker = (
  disk: StorageBlockDevice,
  rows: LayoutRow[],
  btrfs: BtrfsFilesystem[],
  offline: BtrfsOfflinePool[],
): string | null => {
  if (disk.readOnly) return "Read-only disk";
  const own = deviceReason(disk, poolOf(disk, btrfs, offline));
  if (own) return own;
  for (const row of rows) {
    if (row.kind === "partition" && row.reason) return row.reason;
  }
  return null;
};
