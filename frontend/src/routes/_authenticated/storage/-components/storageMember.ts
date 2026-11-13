import type {
  DiskMemberConfirmed,
  StorageBlockDevice,
  StorageMember,
} from "@/api";

/**
 * What a member signature says a device belongs to, such as "btrfs pool
 * tank", worded as the bridge words it in its refusals.
 */
export const describeMember = ({ kind, name, uuid }: StorageMember) => {
  const id = name || uuid;
  if (kind === "btrfs") return id ? `btrfs pool ${id}` : "a btrfs pool";
  if (kind === "md") return id ? `md array ${id}` : "an md array";
  if (kind === "other")
    return name ? `another storage set (${name})` : "another storage set";
  if (name) return `LVM volume group ${name}`;
  return uuid
    ? `an LVM volume group (physical volume ${uuid})`
    : "an LVM volume group";
};

/**
 * The member signatures among `devices` that an Initialize, Format or Delete
 * erases, as the request confirms them: the bridge refuses unless they are
 * exactly the members it finds, so a device that changed since the dialog
 * opened is never erased on the old confirmation.
 */
export const memberConfirmations = (
  devices: StorageBlockDevice[],
): DiskMemberConfirmed[] =>
  devices.flatMap(({ path, member }) =>
    member
      ? [{ device: path, kind: member.kind, uuid: member.uuid ?? "" }]
      : [],
  );

/**
 * Why a device cannot join a btrfs pool, or null: in use, in /etc/fstab, or
 * part of another pool, array or volume group, which force never erases.
 * The bridge refuses the same devices; this only explains the disabled ones.
 */
export const poolDeviceBlocker = (device: StorageBlockDevice) => {
  if (device.busy) return device.busy;
  if (device.inFstab) return "In /etc/fstab";
  return device.member ? `Member of ${describeMember(device.member)}` : null;
};

export interface PoolDeviceChoice {
  device: StorageBlockDevice;
  /** Why it is disabled, or null. */
  reason: string | null;
}

/**
 * The whole devices a pool create or device add lists: not mounted, not one
 * of `taken` (the pool's own or a mounted pool's), and without partitions or
 * holders. Blocked devices are listed with their reason; a device with an old
 * filesystem only once force is on, since force is what erases it.
 */
export const poolDeviceChoices = (
  devices: StorageBlockDevice[],
  taken: string[],
  force: boolean,
): PoolDeviceChoice[] => {
  const partitioned = new Set(devices.flatMap((entry) => entry.parents));
  return devices
    .filter(
      (entry) =>
        entry.mountpoints.length === 0 &&
        !taken.includes(entry.path) &&
        !partitioned.has(entry.path),
    )
    .map((device) => ({ device, reason: poolDeviceBlocker(device) }))
    .filter(
      ({ device, reason }) => reason !== null || device.fsType === "" || force,
    );
};

/** The plain-words label of the force switch. */
export const FORCE_LABEL =
  "Force: erase an old filesystem or encrypted volume on the disk (never a disk of another pool, RAID array or volume group)";
