# Storage disks

Storage → Disks (`/storage`) lists the physical drives with their SMART
details and, under **Storage space**, how they are used: disks → pool →
volumes for btrfs pools, disks → array or device → volumes for RAID, LVM
and dm-crypt, disk → volumes for plain partitions, and server → volumes for
network shares. Drives with nothing on them are listed under **Disks without
volumes**. A drive used in more than one way, such as an EFI partition beside
a RAID member, appears once in each place.

## Partitions

A plain disk's volumes name their partition. Partitions that nothing mounts
or builds on sit under their disk as **Not mounted**, with their filesystem
or **No filesystem** and their size. A partition is in use when it is
mounted (swap included), when it is the parent of another device (a RAID
array, an LVM physical volume, a dm-crypt mapping), or when it is a member of
a btrfs pool, mounted or not; only one member of a multi-device btrfs pool
reports the mount point, so membership comes from the pool listing. Without
pool access (unprivileged, or no btrfs tools) the other members of a mounted
multi-device pool show as not mounted. Partitions on a disk missing from the
drive list are not shown.

## Partition management

An expanded drive's **Partitions** tab shows the table type, or **No
partition table**, and lists the disk from start to end: partitions with
their size, filesystem and label and what uses them, and the free regions
between them. Its actions are for administrators.

| Row | Action | Enabled when |
|---|---|---|
| Free region (GPT disk) | **Create partition**: size, the whole region by default; optionally a filesystem and label, and with them **Mount after formatting** (mount point, **Mount at boot**) | always |
| Partition | **Format**: filesystem and label; **Overwrite existing data** when it already has one | the partition is unused |
| Partition with a filesystem, not mounted | **Mount**: mount point, **Mount at boot** | not busy, and not a btrfs pool member (pools mount from the pool view) |
| Partition (GPT disk) | **Delete**: the confirmation names the partition, its size and its filesystem | the partition is unused |
| Disk | **Initialize disk**: an empty GPT replaces any table; the confirmation lists every partition that will be lost | the disk and all its partitions are unused and the disk is writable |

A disabled action's tooltip gives the reason, such as "Mounted at /srv",
"Used by /dev/md127", "In /etc/fstab" or "Member of btrfs pool tank". The
filesystems are ext4, xfs and btrfs on one device; multi-disk pools come from
the pool flow. A type whose `mkfs` is missing is listed as unavailable with a
link to install it (see [Filesystem tools](./capabilities.md#filesystem-tools)).
Labels are limited to 16 bytes for ext4, 12 for xfs and 255 for btrfs.
xfs needs a partition of at least 300 MiB and btrfs at least 109 MiB, the
smallest `mkfs.xfs` and `mkfs.btrfs` accept; the dialogs disable a type the
size is too small for, and the bridge refuses it before writing anything.
MBR partitions can be
formatted and mounted, but neither deleted nor added; **Initialize disk**
replaces the MBR with a GPT.

**Unmount** of a local filesystem offers **Also remove from /etc/fstab**, so a
partition mounted at boot can be released and deleted. Create and Format run
as tasks with steps (Partitioning, Trimming, Formatting, Mounting), because
discarding the whole partition can take minutes on a large SSD. The bridge
runs that trim itself, as 1 GiB `BLKDISCARD` calls on an exclusive open, so
the Trimming step shows its percent done, then runs `mkfs` with its
no-discard flag (`-E nodiscard`, `-K`). Pool creation trims its members the
same way. A device without discard support skips the step. Delete, Mount and
Initialize are calls.

### Safety

The bridge re-checks every action against a fresh inventory; the page's
disabled states are only hints. A device is in use when:

1. it, or for a disk one of its partitions, is mounted or an active swap;
2. it has holders (LVM, md, dm-crypt, multipath);
3. it is a member of a btrfs filesystem, mounted or not;
4. `/etc/fstab` names it by path, `/dev/disk/by-*` link, `UUID=`,
   `PARTUUID=`, `LABEL=` or `PARTLABEL=`.

Format, Delete and Initialize refuse on all four, with the reason as a 409.
Mount refuses on the first three, and on the fourth only with **Mount at
boot**, so a partition already in fstab can still be mounted for now. Create
needs a GPT disk that is not itself in use; its other partitions may be.
Create, Format, Delete and Initialize refuse a read-only disk.

The kernel backs these checks, so a stale inventory cannot cause a
destructive write:

- **Delete** asks the kernel to drop the partition (`BLKPG_DEL_PARTITION`)
  before touching the table. The kernel drops by number, so Delete first
  checks that the kernel's partition of that number has the GPT entry's
  start and size, and refuses with a 409 when the kernel has not re-read an
  edit made elsewhere. The kernel refuses with `EBUSY` while anything has
  the partition open; a udev probe holds it open briefly, so `EBUSY` is
  retried for about a second. Only then are the backup and primary GPT
  entries cleared, and if that write fails the partition is given back to the
  kernel.
- **Format** and **Mount** open the partition with `O_EXCL`, which the kernel
  refuses while it is mounted or held. `mkfs` repeats that check:
  `mkfs.ext4` never gets the doubled `-F` that skips it.
- **Initialize** opens the disk with `O_EXCL`, which the kernel refuses while
  the disk or one of its partitions is mounted or held, then drops every
  partition from the kernel, which also catches a partition that is merely
  open. If one is busy, the ones already dropped are given back and nothing
  is written. Only then are the disk's edges zeroed, an empty GPT written and
  the table re-read (`BLKRRPART`). If that write fails, the dropped
  partitions are given back too.

Create and Delete edit one entry of the existing GPT in place. If a copy does
not validate, they refuse with the reason, such as "no valid GPT header at
LBA 1" or a bad checksum; if the two copies differ, with "partition table
copies differ; repair it first". They write the backup copy before the
primary. If the kernel refuses a new partition (`BLKPG_ADD_PARTITION`),
Create clears its entry again, so the space stays free. Initialize never
reads the old table: it writes a new one over the zeroed disk edges.
Partitions start and end on 1 MiB boundaries and are at
least 1 MiB; pool partitions keep their own minimum and slack (see
[Pools](./btrfs.md#pools)). A new partition's first and last 2 MiB are
zeroed, so a filesystem that used to live there does not show up again.
Delete does not wipe; the next create does, as in UDisks2.

Formatting picks the filesystem UUID and passes it to `mkfs`, so mounting and
fstab never wait for udev. **Mount at boot** writes
`UUID=<uuid> <path> <type> defaults,nofail 0 <pass>`, with pass 2 for ext4
and 0 for xfs and btrfs. Mount points follow the pool rules: an absolute,
clean path outside the system directories, not mounted, not already in
fstab when **Mount at boot** is on, and empty if it exists; a missing one is
created. Create, Format, Delete and Initialize hold the lock systemd-udevd
honours on the whole disk
([block device locking](https://systemd.io/BLOCK_DEVICE_LOCKING/)), so until
they finish udev neither probes the disk nor re-reads its table, which would
briefly remove every unused partition. `make test-btrfs-integration` checks
these refusals against the real kernel on a loop device; it needs sudo.

## Inventory

`storage.get_topology` is a read-only, retry-safe bridge Call returning
`StorageTopologyInventory`: the block-device list (loop devices, RAM disks
and hidden multipath paths omitted; empty drives are listed, as lsblk
does; each device lists all its parents) and the current mount table. The bridge builds the list natively,
with no subprocess:

- `/sys/class/block` for devices, sizes, partition numbers and start offsets,
  parents (`slaves/`), holders and the transport.
- `/run/udev/data/b<major>:<minor>` for filesystem type, UUID and label,
  partition table type, PARTUUID, PARTLABEL, model and serial.
- `/proc/self/mountinfo` for mount points, matched by `major:minor` or, for
  filesystems with an anonymous device such as btrfs, by source.
- `/proc/swaps` and `/etc/fstab` for the in-use rule and the `inFstab` flag.

Each device also reports `busy` (mounted, active swap, or used by a holder)
and, for GPT disks, `freeRegions`.

systemd does not promise the udev database format: udev(7) says only that
libudev provides access to it. A device udev has not probed yet (a brief window
after it appears) shows no filesystem until the next refresh. `lsblk` has the
same gap unless it runs as root.

`TestNativeInventoryMatchesLsblk` compares the inventory field by field with
`lsblk` on the machine running the test, and skips where `lsblk` or the udev
database is missing. `storage.get_drive_info` comes from the same reader,
filtered to disks, and reports the size in bytes. Volume and mount mutations
invalidate the inventory through the central operation invalidation map.

## Live activity

`monitoring.get_live` supplies each drive's read and write rate, shown on the
drive's row in Storage space. Lines leaving a drive carry layered grain
streams, reads flowing towards the volumes and writes back, through the
shared `AppTopologyEdge`. Grain speed is decorative, not a throughput scale.
Lines into partitions nothing uses stay still, and a pool or array's lines to
its volumes carry no flow, since the rates belong to whole drives. Rates and
grains disappear when a monitoring poll fails; a sampler that keeps
answering with an old sample is not detected. **Pause animation** freezes
the grains in place, and reduced motion removes the animation while keeping
the numbers.
