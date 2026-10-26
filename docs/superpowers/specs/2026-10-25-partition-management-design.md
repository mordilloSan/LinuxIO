# Partition management and a native block-device inventory

Status: draft for review (2026-10-25)

## Goal

From Storage → Disks, an administrator can create, format, mount and delete
partitions, and initialize a disk with an empty GPT, without a terminal. Each
step works on its own on an existing partition; the create dialog can also
chain format and mount in one go. The block-device inventory behind the
Disks page stops running `lsblk` and reads the kernel (sysfs) and the udev
database directly, so the feature adds no host tool beyond the `mkfs`
programs, for which no kernel API exists.

### Decisions taken

| Question | Decision |
|---|---|
| What does "create" produce | Raw partition; format and mount are separate actions, also offered as options in the create dialog |
| Partition tables | GPT only. MBR disks list their partitions read-only; **Initialize disk** replaces any table with an empty GPT |
| Formats | ext4, xfs, btrfs (single device; multi-disk pools stay in the btrfs flow) |
| Mechanism | LinuxIO writes the GPT itself and tells the kernel through ioctls, extending `backend/common/btrfs/partition.go`; formatting runs `mkfs.*` |
| Inventory | Replace `lsblk` with sysfs + `/run/udev/data` + `/proc/self/mountinfo` + `/proc/swaps`, in this change |

### Out of scope

MBR editing; vfat, exFAT and swap formats; LUKS; resizing or moving
partitions; formatting a disk without a partition table; partition names and
type GUIDs other than Linux filesystem data; LVM listing (still `pvs`/`vgs`/
`lvs`); `/etc/crypttab` references (an unopened LUKS partition shows its
`crypto_LUKS` signature in the delete confirmation but is not refused).

## User experience

The expanded drive in Storage → Disks gets a **Partitions** tab beside
Overview, SMART Attributes, Drive Information and Self-Tests. It lists the
disk from start to end: partitions (number, size, filesystem and label,
mount point, or what uses it) and free regions. The table type is shown; a
disk without a table says so.

| Row | Action | Enabled when |
|---|---|---|
| Free region (GPT disk) | **Create partition**: size (default the whole region, MiB/GiB/TiB), optional format (type + label), optional mount (path + **Mount at boot**) offered only with a format | always |
| Partition | **Format**: type + label; **Overwrite existing filesystem** required when it has a signature | partition unused |
| Partition with a filesystem, not mounted | **Mount**: path + **Mount at boot** | no holders, not a btrfs pool member (pools mount from the pool view) |
| Partition (GPT disk) | **Delete**: confirmation naming the partition, its size and its signature | partition unused |
| Disk | **Initialize disk**: confirmation listing every partition that will be lost | disk and all its partitions unused, disk writable |

Disabled actions carry the reason as a tooltip ("Mounted at /srv",
"Member of btrfs pool tank", "In /etc/fstab", "Holds md127"). Format types
whose `mkfs` program is missing are listed as unavailable with the
capability install action. Label length is checked in the dialog: ext4 16
bytes, xfs 12 characters, btrfs 255 bytes.

**Unmount** on a local filesystem gains **Also remove from /etc/fstab**, so a
partition mounted at boot can be released and deleted. Today unmount leaves
the fstab entry and the filesystem returns at the next boot.

Create and Format run as tasks with steps (Partitioning → Formatting:
"SSDs are trimmed first, which can take a few minutes" → Mounting),
because `mkfs` discards the whole device by default — the reason pool
creation became a task. Delete, Mount and Initialize are calls.

## Safety rules

The bridge re-checks everything against a fresh inventory; the UI's
disabled states are hints only.

A device is **in use** when any of these hold. Delete, format and initialize
refuse on all four rules, with the matching reason (`ErrConflict`). Mount
refuses on rules 1–3; rule 4 applies to Mount only when **Mount at boot** is
on, so a partition already in fstab can be mounted again just for now.

1. it, or for a disk any of its partitions, is mounted (`/proc/self/mountinfo`) or an active swap (`/proc/swaps`);
2. it has holders (`/sys/class/block/<dev>/holders/`: LVM, md, dm-crypt, multipath);
3. it is a member of a btrfs filesystem, mounted or not (`btrfs.ListFilesystems` and the offline pool scan, as the Disks page does today);
4. `/etc/fstab` refers to it by path, `/dev/disk/by-*` symlink, `UUID=`, `PARTUUID=`, `LABEL=` or `PARTLABEL=`.

Kernel-enforced backstops, so a stale inventory cannot cause a destructive
write:

- **Delete** asks the kernel to drop the partition (`BLKPG_DEL_PARTITION`)
  *before* touching the table. The kernel returns `EBUSY` while anything has
  the partition open (`bdev_del_partition` checks `bd_openers`). A udev or
  blkid probe can hold a brief read-only open, so `EBUSY` is retried for
  about one second before the delete is refused. Only then are the backup and
  primary GPT entries cleared. If writing the table fails, the partition is
  re-added to the kernel with `BLKPG_ADD_PARTITION`.
- **Format** and **Mount** open the partition with `O_EXCL`, which fails
  with `EBUSY` while it is mounted or claimed by a holder. `mkfs` runs
  without its double-force override (`-F -F`), so it repeats that check.
- **Initialize** first opens the whole disk with `O_EXCL` (`CheckNotBusy`).
  That already fails with `EBUSY` while the disk or any of its partitions is
  claimed (mounted or held): claiming a partition marks the whole disk as
  claimed too (`bd_finish_claiming` sets its holder to the `bd_may_claim`
  sentinel), so `bd_may_claim` refuses the disk. A partition that is merely
  open is not claimed, so Initialize then drops every partition from the
  kernel with `BLKPG_DEL_PARTITION`, which refuses one that has openers. If
  any is busy, the ones already dropped are re-added and nothing is written.
  Only then are the disk's edges wiped, an empty GPT written and the table
  re-read (`BLKRRPART`, which also refuses while any partition is open).

GPT rules, carried over from `AddPartition`:

- Both GPT copies must validate (header and entry CRCs, self and alternate
  LBAs, usable range) and agree, otherwise the operation is refused with
  "repair it first".
- The backup copy is written before the primary.
- Starts and ends align to 1 MiB, the libfdisk and sgdisk default grain.
- New partitions must be at least 1 MiB; libfdisk enforces no hard minimum.
  Pool creation keeps its 256 MiB minimum and its 1 GiB slack, since those
  exist for replacement disks.
- The requested range must lie inside one free region, and the slot must be
  empty.

**Wipe on create.** A new partition can land where an old filesystem lived, and
that filesystem would show up again. UDisks2 handles this with a libblkid
signature wipe of every new partition. Here, the first and last 2 MiB of the
new partition are zeroed with the existing `wipeDeviceEdges`, which mkfs-time
pool creation already uses. Those 2 MiB edges cover the primary signatures of
ext4, xfs, btrfs, md (0.90/1.0/1.2), LVM and ZFS labels. **Delete** does not
wipe, which matches UDisks2: the next create wipes.

Formatting generates the filesystem UUID and passes it in (`mkfs.ext4 -U`,
`mkfs.xfs -m uuid=`, `mkfs.btrfs -U`), so mounting and the fstab line never
wait for udev to re-probe. **Mount at boot** writes
`UUID=<uuid> <path> <type> defaults,nofail <pass>`, where the pass is 2 for
ext4 (fstab(5) for non-root filesystems) and 0 for xfs and btrfs (fsck.xfs is
a no-op, and btrfs documents 0). Mount points follow the pool rules
(`checkPoolMountpoint`): an absolute, clean path outside system directories,
not in fstab, not mounted, and empty if it exists. The mount point is created
when it is missing. Mounting uses `mount(2)`.

A read-only disk (`/sys/class/block/<dev>/ro`) refuses every mutation.

## Native inventory (replacing `lsblk`)

One reader in `backend/bridge/handlers/storage/block_devices.go` builds
`[]StorageBlockDevice` from:

| Field | Source |
|---|---|
| devices, kernel name, `major:minor` | `/sys/class/block/*` (`dev`) |
| size, read-only, rotational, logical sector | `size` ×512, `ro`, `queue/rotational`, `queue/logical_block_size` |
| partition number, start | `partition`, `start` ×512 |
| parents | a partition's parent directory (its disk), plus the `slaves/` entries |
| type | `part` if `partition` exists; `dm-*` by `dm/uuid` prefix (`LVM-` → `lvm`, `CRYPT-` → `crypt`, `mpath-` → `mpath`, else `dm`); `md*` → `md/level`; `loop*` → `loop` (skipped, as today); `sr*` → `rom`; else `disk` |
| name, path | kernel name and `/dev/<kname>`; device-mapper uses `dm/name` and `/dev/mapper/<name>` |
| fstype, uuid, label | udev `ID_FS_TYPE`, `ID_FS_UUID_ENC`, `ID_FS_LABEL_ENC` (`\xNN` decoded) |
| table type, PARTUUID, PARTLABEL | udev `ID_PART_TABLE_TYPE` (falling back to `ID_PART_ENTRY_SCHEME`), `ID_PART_ENTRY_UUID`, `ID_PART_ENTRY_NAME` |
| model | udev `ID_MODEL_ENC`, then `ID_MODEL`, then sysfs `device/model` |
| serial | udev `SCSI_IDENT_SERIAL`, `ID_SCSI_SERIAL`, `ID_SERIAL_SHORT`, `ID_SERIAL`, then sysfs `device/serial` |
| vendor | sysfs `device/vendor` |
| transport | lsblk's `get_transport` order: spi, fc/fcoe, sas, sbp, iscsi, usb (SCSI device link contains `usb`), `scsi_host` `proc_name` (`ahci`/`sata*` → sata, contains `ata` → ata), then names (`nvme*`, `vd*` → virtio, `mmcblk*` → mmc) |
| mount points | `/proc/self/mountinfo` matched by `major:minor` |
| swap | `/proc/swaps`, used for the in-use rule only (the inventory keeps showing path mount points only) |

The table above reproduces the lsblk behaviour the Disks page depends on.
lsblk reads the same udev properties through libudev
(`lsblk-cmd/properties.c`) and the same sysfs files.

`/run/udev/data/b<major>:<minor>` is read directly as `E:KEY=value` lines,
the way Prometheus node_exporter (`collector/diskstats_linux.go`) and
jaypipes/ghw do. systemd does not promise this format: udev(7) says only that
libudev provides access to it. A device that udev has not probed yet (a brief
window after it appears) shows no filesystem until the next refresh. lsblk
has the same gap unless it runs as root.

`storage.get_drive_info` is derived from the same reader, filtered to disks.
`ApiDisk.size` (an lsblk human string such as `"476.9G"`) becomes
`sizeBytes`. The frontend's `parseSizeToBytes` and the dashboard's and
DiskOverview's parsing are deleted.

`StorageBlockDevice` gains `ptType` (disks), `partNumber`, `startBytes`,
`uuid`, `label`, `partUUID`, `partLabel`, and `freeRegions`
(`[{startBytes, sizeBytes}]` for GPT disks: every aligned gap of at least
1 MiB). `freeBytes` stays for the pool dialog, still meaning the largest
region of at least 256 MiB with slack, which is what `AddPartition` uses.

## Backend layout

- `backend/common/btrfs/partition.go`:
  - `CreatePartition(ctx, device, startBytes, sizeBytes)` returns the new partition path.
  - `DeletePartition(ctx, device, number)`.
  - `InitializeDisk(ctx, device)`.
  - `FreeRegions(...)` generalizes `LargestFreeRegion`.
  - A `delKernelPartition` ioctl beside `addKernelPartition`.
  - `AddPartition` becomes `CreatePartition` over the largest region.

  The package name is a known wart (an ext4 partition is created by package
  `btrfs`). It stays because the sector, size and wipe helpers and the root
  loop-device harness live there; a move to its own package is a separate
  change.
- `backend/bridge/handlers/storage/partitions.go`:
  - Privileged handlers.
  - The in-use check (`checkDeviceUnused`), shared with the existing
    `checkBtrfsDeviceFree` and `checkMDDeviceFree` where their rules match.
  - Building the `mkfs` arguments.
  - Mount and the fstab line.

  `mkfs` runs through the existing context-aware command path with arguments
  passed separately; on failure its stderr is returned and its stdout is never
  parsed.
- API:

  | Endpoint | Kind | Request |
  |---|---|---|
  | `storage.partition_create` | task | `device, startBytes, sizeBytes, format?, mount?` |
  | `storage.partition_format` | task | `device, fsType, label, overwrite` |
  | `storage.partition_delete` | call | `device` |
  | `storage.partition_mount` | call | `device, mountpoint, persist` |
  | `storage.disk_initialize` | call | `device` |

  `storage.unmount_filesystem` takes `MountpointRemoveFstabRequest`, which
  NFS and CIFS unmount already use. Every new route is `Privileged()`. Tasks
  carry `{device, "partition"}` identity and are system singletons per device.
  Each success logs one Info line ("created partition", "formatted partition",
  "deleted partition", "mounted filesystem", "initialized disk") with the
  device as an attribute.
- Capabilities: `e2fsprogs` (`mkfs.ext4`), `xfsprogs` (`mkfs.xfs`) and
  `btrfs_progs` (`mkfs.btrfs`). Each is installable, with the Debian and RHEL
  package names, and is mirrored in `frontend/src/api/capabilities.ts`.
- Frontend:
  - A `PartitionsTab` plus create, format, mount and delete/initialize
    dialogs under `storage/-components/disk-overview/`.
  - The operation invalidation map: every new route invalidates
    `storage.get_topology`, `storage.get_drive_info`, the btrfs listings and
    `monitoring.get_live` filesystems.
  - Task recovery for create and format, as the SMART tests do.

## Testing

- **Go unit tests**:
  - `CreatePartition`, `DeletePartition` and `InitializeDisk` run on disk-image
    files with the kernel ioctls stubbed (`sectorSize`, `rereadPartitions`,
    `addKernelPartition`, plus the new delete stub). The checks: both copies
    parse and agree; a partition table produced by an existing tool, used as a
    golden fixture, keeps every untouched byte; refusals cover mismatched
    copies, a range outside a free region, a full slot table, a busy kernel
    (retry then refuse, re-add when the write fails) and a busy partition
    during initialize (earlier ones re-added).
  - `FreeRegions` boundaries.
  - The inventory reader against a fake `/sys/class/block` and
    `/run/udev/data` tree covering each device type and transport branch,
    `\xNN` decoding, a missing udev file and mount/swap matching.
  - The in-use rules, one table case per reason, including every fstab spec
    form.
  - `mkfs` arguments and the fstab line per filesystem.
- **Parity test** (unprivileged, part of the normal suite): on the machine
  running the tests, the native inventory equals `lsblk --json` for every
  field LinuxIO uses. It is skipped when `lsblk` or `/run/udev/data` is
  missing; `lsblk` remains a test-only tool. This is the evidence that
  replacing lsblk changed nothing for the existing Disks page, pool dialog and
  RAID checks.
- **Root loop-device test** (`make test-btrfs-integration`, run by the user):
  1. Initialize a disk.
  2. Create two partitions, then check the kernel's view (`/sys` start and
     size) and both GPT copies.
  3. Format one as ext4 and mount it.
  4. Confirm that deleting it is refused with `EBUSY` and the table is
     unchanged.
  5. Unmount it, then delete it.
  6. Create a partition at the same offset and confirm the old ext4 signature
     is gone.
  7. Confirm initialize is refused while the other partition is mounted.
  8. Hold that partition open without mounting it, so nothing claims it:
     Initialize drops p1, gets `EBUSY` on p2 and re-adds p1, and the kernel's
     view and both GPT copies are unchanged.
  9. Initialize to an empty table.
- **Frontend**:
  - The Partitions tab rows and disabled reasons.
  - The dialogs' requests, label limits, the overwrite gate, and capability
    gating.
  - The unmount fstab option.
  - The `sizeBytes` change in DiskOverview and the dashboard.
- **Make**: `make generate`, then `make test-quiet`. The browser suite
  (`make test-frontend-browser-quiet`) covers the Disks page fixture.

## Documentation

- `docs/storage-disks.md` gains a Partitions management section and an
  Inventory rewrite: no lsblk, its sources, and the udev caveat.
- `docs/btrfs.md` notes that pool partitioning shares this code.
- `docs/capabilities.md` lists the three new capabilities.

## Sources

- Kernel `bdev_del_partition` (`block/partitions/core.c`, mainline and
  v5.15) returns `EBUSY` on `bd_openers`. `BLKRRPART`
  (`blkdev_reread_part` / `disk_scan_partitions`) refuses while any
  partition is open.
- open(2): `O_EXCL` without `O_CREAT` on a block device fails with `EBUSY`
  when it is in use. `btrfs_scan_one_device` does not keep devices of an
  unmounted filesystem open.
- UDisks2 `handle_create_partition` (`udiskslinuxpartitiontable.c`) wipes
  every new partition with `bd_fs_wipe`. `handle_delete`
  (`udiskslinuxpartition.c`) does not wipe.
- util-linux lsblk `lsblk-cmd/properties.c` and `lsblk.c`
  (`get_transport`); libfdisk `topology_get_grain` (1 MiB grain) and
  `gpt_add_partition` (no minimum size).
- node_exporter `collector/diskstats_linux.go` and ghw
  `pkg/block/block_linux.go` read `/run/udev/data` directly. systemd
  `60-block.rules` (`OPTIONS+="watch"`) and `60-persistent-storage.rules`
  (`IMPORT{builtin}="blkid"`) refresh `ID_FS_*` after a write.
- mke2fs(8), mkfs.xfs(8), mkfs.btrfs(8) for `-U` / `-m uuid=`, label limits,
  force flags and default discard. fstab(5), fsck.xfs(8) and `fsck.btrfs`
  docs for the pass field.
