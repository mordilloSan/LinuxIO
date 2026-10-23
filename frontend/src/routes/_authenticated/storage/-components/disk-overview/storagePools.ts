import type { BtrfsFilesystem, FilesystemInfo, NFSMount } from "@/api";

import type { DriveInfo } from "./types";
import { findBackingDrive } from "./utils";

interface PoolDisk {
  key: string;
  missing: boolean;
  name: string;
  sizeBytes: number;
}

interface PoolSummary {
  allocatedBytes: number;
  disks: PoolDisk[];
  freeBytes: number;
  name: string;
  /** Data profile as shown to people, e.g. "RAID 10"; empty when unknown. */
  profile: string;
  uuid: string;
  volumes: string[];
}

/** "raid10" -> "RAID 10", "raid1c3" -> "RAID 1C3", "dup" -> "DUP". */
export const profileLabel = (profile: string) => {
  const raid = /^raid(\d+)(c\d+)?$/.exec(profile);
  if (raid) return `RAID ${raid[1]}${raid[2]?.toUpperCase() ?? ""}`;
  if (profile === "dup") return "DUP";
  return profile.charAt(0).toUpperCase() + profile.slice(1);
};

/**
 * A btrfs filesystem as a pool: its disks, data profile, space and mounts.
 * `index` numbers unlabelled pools; mount points belong to the volumes.
 */
export const describePool = (
  fs: BtrfsFilesystem,
  index: number,
): PoolSummary => {
  // A mixed allocation reports its kind as "data+metadata".
  const data = fs.allocation.find((a) => a.kind.split("+").includes("data"));
  const mountpoints = [...new Set(fs.mountpoints.map((m) => m.mountpoint))];
  return {
    uuid: fs.uuid,
    name: fs.label || `Storage pool ${index + 1}`,
    profile: data ? profileLabel(data.profile) : "",
    allocatedBytes: Math.max(fs.totalBytes - fs.unallocatedBytes, 0),
    freeBytes: Math.max(fs.totalBytes - fs.usedBytes, 0),
    disks: fs.devices.map((device) => ({
      key: device.path || String(device.id),
      name: device.path ? device.path.replace(/^\/dev\//, "") : "Missing disk",
      sizeBytes: device.sizeBytes,
      missing: device.missing,
    })),
    volumes: mountpoints,
  };
};

/** "btrfs" -> "Btrfs", "vfat" -> "FAT", "nfs4" -> "NFS"; others as reported. */
export const fsTypeLabel = (fstype: string) => {
  const known: Record<string, string> = {
    btrfs: "Btrfs",
    xfs: "XFS",
    vfat: "FAT",
    exfat: "exFAT",
    ntfs: "NTFS",
    zfs: "ZFS",
  };
  if (fstype.startsWith("nfs")) return "NFS";
  if (fstype.startsWith("cifs") || fstype === "smb3") return "SMB";
  return known[fstype] ?? fstype;
};

interface PoolStack {
  fs: BtrfsFilesystem;
  pool: PoolSummary;
  volumes: FilesystemInfo[];
}

interface DriveStack {
  drive: DriveInfo;
  volumes: FilesystemInfo[];
}

/** Volumes with no physical drive behind them: network shares, mapper devices. */
interface SourceStack {
  key: string;
  label: string;
  network: boolean;
  volumes: FilesystemInfo[];
}

export interface StorageLayout {
  driveStacks: DriveStack[];
  idleDrives: DriveInfo[];
  poolStacks: PoolStack[];
  sourceStacks: SourceStack[];
}

/**
 * Places every mounted filesystem exactly once: under its btrfs pool, else
 * under the drive its partition lives on, else under its source (an NFS
 * server or a mapper device). Drives with neither are listed as idle.
 */
export const buildStorageLayout = (
  btrfs: BtrfsFilesystem[],
  filesystems: FilesystemInfo[],
  drives: DriveInfo[],
  nfsMounts: NFSMount[],
): StorageLayout => {
  const claimed = new Set<string>();
  const poolStacks = btrfs.map((fs, index) => {
    const pool = describePool(fs, index);
    const volumes = filesystems.filter((v) =>
      pool.volumes.includes(v.mountpoint),
    );
    for (const v of volumes) claimed.add(v.mountpoint);
    return { fs, pool, volumes };
  });

  const byDrive = new Map<string, DriveStack>();
  const bySource = new Map<string, SourceStack>();
  for (const volume of filesystems) {
    if (claimed.has(volume.mountpoint)) continue;
    const drive = findBackingDrive(volume.device, drives);
    if (drive) {
      const stack = byDrive.get(drive.name) ?? { drive, volumes: [] };
      stack.volumes.push(volume);
      byDrive.set(drive.name, stack);
      continue;
    }
    const nfs = nfsMounts.find((m) => m.mountpoint === volume.mountpoint);
    const network = nfs !== undefined || volume.fstype.startsWith("nfs");
    const label = nfs ? `${nfs.server}:${nfs.exportPath}` : volume.device;
    const stack = bySource.get(label) ?? {
      key: label,
      label,
      network,
      volumes: [],
    };
    stack.volumes.push(volume);
    bySource.set(label, stack);
  }

  const pooled = new Set(
    poolStacks.flatMap(({ pool }) =>
      pool.disks.flatMap(
        (disk) => findBackingDrive(disk.name, drives)?.name ?? [],
      ),
    ),
  );
  return {
    poolStacks,
    driveStacks: drives.flatMap((drive) => byDrive.get(drive.name) ?? []),
    sourceStacks: [...bySource.values()],
    idleDrives: drives.filter(
      (drive) => !pooled.has(drive.name) && !byDrive.has(drive.name),
    ),
  };
};
