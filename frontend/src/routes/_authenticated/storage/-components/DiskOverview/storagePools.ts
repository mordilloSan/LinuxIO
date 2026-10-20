import type { BtrfsFilesystem } from "@/api";

export interface PoolDisk {
  key: string;
  missing: boolean;
  name: string;
  sizeBytes: number;
}

export interface PoolSummary {
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

/** A btrfs filesystem as a pool: its disks, data profile, space and mounts. */
export const describePool = (fs: BtrfsFilesystem): PoolSummary => {
  // A mixed allocation reports its kind as "data+metadata".
  const data = fs.allocation.find((a) => a.kind.split("+").includes("data"));
  const mountpoints = [...new Set(fs.mountpoints.map((m) => m.mountpoint))];
  return {
    uuid: fs.uuid,
    name: fs.label || mountpoints[0] || fs.uuid.slice(0, 8),
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
