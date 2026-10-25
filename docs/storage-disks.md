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

## Inventory

`storage.get_topology` is a read-only, retry-safe bridge Call returning
`StorageTopologyInventory`: the block-device tree from
`lsblk --json --bytes --tree` (loop devices omitted, repeated nodes merged
with all their parents) and the current mount table. The subprocess honours
request cancellation and has a ten-second timeout. Volume and mount
mutations invalidate it through the central operation invalidation map.

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
