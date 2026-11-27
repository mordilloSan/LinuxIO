# Btrfs management

The **Storage → Snapshots** tab (`/storage/snapshots`) holds snapshots with
rollback and automatic snapshots, and, under its Replication and Subvolumes
views (`?view=replication`, `?view=subvolumes`), replication to another
filesystem or host and the subvolumes themselves; `/storage/subvolumes`,
which alerts and saved runs link to, redirects there with its view and pool.
Each pool's page on the
**Storage → Pools** tab (`/storage/pools/<uuid>`, see [Pool page](#pool-page))
holds the rest: health, what protects its data, snapshots, devices and
profiles, space, quotas, compression and maintenance timers.
Everything on the host is read and driven through the kernel's btrfs ioctls,
`mount(2)`, `/proc/self/mountinfo` and the disks' superblocks: devices,
profiles, balance, scrub, space, health, pools and partition tables,
subvolume and quota listing, subvolume create, delete, snapshot and default,
read-only flags, compression properties, quotas and limits, and the send side
of replication. `btrfs-progs` is used only where the kernel has no interface:
`mkfs.btrfs`, `btrfs receive`, and the commands a replication job runs on its
target, which may be another host. There is no daemon. Everything is on
demand or runs from `linuxio-worker`, started by two systemd timers, a
boot-time service, a resync service and a rebuild service (see
[Missing disks](#missing-disks)) and the replication service each snapshot pass starts (see
[Replication](#replication)).

`make test-btrfs-integration` runs every one of these operations against the
real kernel on loop devices, loses and returns disks of a mirror to check
the missing-disk handling, cuts a resync by a power loss mid-scrub and
continues it from what the disks and the state kept, fails a disk's writes
during a resync through a device-mapper error target, exercises
[partition management](./storage-disks.md#partition-management), checks
that **Force** never takes a disk of an unmounted pool
(`TestBtrfsMemberIntegration` in `backend/bridge/handlers/storage`) nor
zeroes a disk mounted or held after the checks (`TestBtrfsAddWipeIntegration`),
that no writer touches a device claimed after its checks and that md and LVM2
signatures are read as libblkid reads them (`TestPartitionClaimIntegration`),
that a pool whose top level a read mounted can be destroyed, its record
with it (`TestBtrfsDestroyIntegration`), and, acting as other accounts, that the
kernel decides by a [shared folder](./shared-folders.md)'s ACL, keeps its
snapshots root's alone until its earlier versions open them, and that the
recycle bin's emptying keeps a folder new to the bin and leaves a subvolume
inside it alone (`TestBtrfsSharedFolderIntegration`); it needs sudo.

Parsing command output is avoided because `btrfs-progs` prints JSON for only a
few commands, and no Go btrfs library covers balance, scrub and devices. A new
storage feature therefore starts from the ioctl or syscall, and each kernel
struct is pinned by the request-number test in `ioctl_test.go`, because the
request number encodes the struct size. A bug here can lose data, and the unit
tests run against a fake kernel that has missed real ones. Work on degraded
mounts, resync, replace or split brain therefore starts from what the kernel,
`btrfs-progs`, mdadm and systemd do, gets an independent review, and adds a
case to `make test-btrfs-integration`. Only that target, or
`make vm-test-btrfs-integration` in the test VM, counts as kernel-level
verification.

## Capability and privileges

The `btrfs` capability checks for the `btrfs` executable (`btrfs-progs`) and
gates the Pools and Snapshots tabs and the pools on the Overview and the Disks page; see
[Capabilities](./capabilities.md#btrfs-tools). All are
**administrator-only**: they require a privileged session, reads run
`btrfs` as root and the configuration files are root `0600`. The Pools and
Snapshots tabs are listed only while a btrfs filesystem is mounted; pools are
created from the Overview or the Disks page.

## Pool page

The **Pools** tab (`/storage/pools`) lists the mounted pools as cards: the
pool's name, its health (the health banner's own verdict, `poolHealth`), its
disks and how it keeps data ("two copies on different disks"), what it stores
and about how much is free, and a chip per disk. Opening one goes to
`/storage/pools/<uuid>`, built as the VM page is: the list folds into that
pool's card beside a panel, Escape or the close button goes back, and a pool
that is not mounted says so. `/storage?pool=<uuid>`, which the alerts link to,
redirects there. A pool found on disk but not mounted is listed on the
Overview's *Worth a look* and stays on the Disks page with **Mount** and
**Destroy**.

The panel leads with the health banner, then four tabs (`?tab=`):

- **Overview**: *Protection* says what a disk failure costs, from the weakest
  profile the pool keeps (single, dup and raid0 lose the pool; raid1 and
  raid10 survive one disk, raid1c3 two, raid1c4 three), the last check's
  result or that it was never checked, the newest snapshot or that there is
  none, and whether an enabled replication job copies the pool elsewhere and
  how its last copy went. *Disks* shows each device with its allocation and an
  **Add a disk** tile that opens the Disks tab. *Maintenance* lists the scrub,
  balance, trim and defrag periods of the pool's mountpoint.
- **Snapshots**: the snapshot list with its schedule (see
  [Snapshots](#snapshots)).
- **Disks**: the devices card and the Space card.
- **Settings**: quotas, compression and maintenance.

**Rename…** and **Destroy…** are in the panel's menu; destroying names every
disk it erases and returns to the list.

Docker's btrfs storage-driver subvolumes are hidden from every list.

## Filesystem health

Each pool's details show its devices, allocation, profiles and per-device
error counters. A device the kernel knows but cannot find is listed as missing
with an empty path, so it can be removed or replaced; one that dropped out of
the mounted pool is listed as dropped out, with the path it had (see
[Missing disks](#missing-disks)). The health banner at the
top of the details leads with the worst condition and the one action that
fixes it: **Replace missing disk** on a degraded pool, a disk that dropped
out coming before the errors its failed writes count, and the error counters
of a missing disk (since 7.1 the kernel marks a pulled disk missing, with the
writes it failed counted) reading as degraded, not errors; **Reclaim space** when
metadata is nearly full. Health, scrub and space alerts link to those details;
snapshot and replication alerts link to the Snapshots tab's Snapshots and
Replication views.

Alerts are updated whenever the list is loaded and, independently of the
browser, by `linuxio-btrfs-health.timer`, which runs `linuxio-worker
health` two minutes after boot and every ten minutes. The units ship with the
package; its first install enables the timer when the host has a btrfs
filesystem, and the bridge enables it the first time an administrator lists
filesystems, covering pools created after install.

| Alert | Severity | Raised when | Resolved when |
|---|---|---|---|
| `btrfs/health-<uuid>` | error | a device reports errors or is missing; "running without n of m disks" when degraded, naming a disk that dropped out of the mounted pool and saying the pool takes it back only when mounted again | the counters are zero and no device is missing |
| `btrfs/scrub-<uuid>` | error | the last scrub found uncorrectable errors | a later scrub finishes clean |
| `btrfs/space-<uuid>` | warning | metadata is over 90% full with less than 1 GiB unallocated | space is reclaimed |
| `btrfs/offline-<uuid>` | error or warning | a pool is found on disk but not mounted (error when disks are missing or the boot check refused to mount it, and then says why) | the pool is mounted |
| `btrfs/bound-<uuid>` | warning | by the health run only: systemd still stops a mount of the pool with the disk it names, though its `/etc/fstab` line says `x-systemd.device-bound=false`: LinuxIO recorded the mount under its source and systemd keeps the dependency, or the record could not be written (see [Pools](#pools)) | no mount of the pool stops with a device, or the pool is no longer mounted; left as it was when systemd could not be asked |
| `btrfs/resync-<uuid>` | warning, error after three failed resyncs | a pool that ran with a disk missing has all its disks again and is being resynced | the resync finished |
| `btrfs/resynced-<uuid>` | info; a warning when its scrub found uncorrectable errors, which it counts in its title, or while a device of the pool still counts errors, for which it says why | a resync finished and the pool is whole again, while the notification settings' *Pool resync* is *Also when a pool is whole again* (the default); it names the returned disks whose error counters it reset or kept | dismissed, or the next resync starts |
| `btrfs/spare-<uuid>` | warning | the pool's [hot spare](#hot-spare) is absent, too small or no longer empty; one occurrence per state | the spare is ready again, it is removed, or the pool is gone |
| `btrfs/rebuild-<uuid>` | warning while an [automatic rebuild](#missing-disks) runs, or waits for its disk or for the pool; error when it failed, or when a missing member was not rebuilt onto its returned disk or the spare, with the reason, one occurrence per reason | the rebuild started, failed, or was refused | the rebuild was verified, the member was replaced or removed, or nothing waits on a person any more |
| `btrfs/rebuilt-<uuid>` | info | an automatic rebuild was verified; names the disk | dismissed, or the next rebuild is verified |

Incomplete health data neither raises nor resolves; alerts of pools that no
longer exist are dropped by the timer, the run alerts of the resync and
rebuild units (`jobs/run-btrfs-resync-<uuid>`, `jobs/run-btrfs-rebuild-<uuid>`)
among them. A condition that lasts is one
occurrence however many health checks see it, so it is delivered once, and
again only after it cleared and came back. Scrub start/cancel is on the banner;
see [Devices](#devices-and-profiles).

## Missing disks

A pool that loses a disk keeps running, and so does its data as long as its
profiles keep a copy. After a reboot systemd never mounts it: udev holds a
btrfs device back until every member is present (`64-btrfs.rules`), and
upstream leaves the "mount with n of m disks" decision to storage software.
LinuxIO makes that decision the way mdadm's last-resort timer does for md
arrays, and repairs the pool once the disk is back, which btrfs leaves to a
scrub.

**At boot**, `linuxio-btrfs-degraded.service` (once per boot, after
`local-fs.target`, before Docker, containerd, Samba, NFS and libvirt, given
at most three minutes) runs `linuxio-worker degraded`. Pools that
`/etc/fstab` names by `UUID=` without `noauto` or `_netdev` and that are
missing disks get 30 seconds for the disks to appear (an
`x-systemd.device-timeout` is not consulted); a pool that is then still incomplete is mounted with
`degraded` at each of its fstab mountpoints, parents first, with each
entry's options, unless the disks present may not hold its newest data:

- the present disks disagree on their superblock generation, which every
  commit stamps on each disk it writes, so one of them missed writes;
- they are older than the newest generation LinuxIO recorded for the pool;
- or a disk that took the writes of the pool's last degraded run is missing.

A pool mounted this way raises its "running without n of m disks" alert at
once, so it reaches the bell and any configured email or webhook right after
boot. Mounting such a set would write a second history over the first. That
pool stays unmounted and its offline alert says why; **Mount degraded** remains for
a person who knows the other disks are gone for good, and does not apply this
check. The kernel itself refuses a pool missing more disks than its profiles
survive. A mount that fails leaves the entries inside its mountpoint
unmounted.

**Which devices are disks of the pool.** A leg of an md array with its
metadata at the end (0.90 or 1.0), a path of a multipath disk and an image
of an LVM mirror show the pool's superblock as well, and the kernel keeps one
device for each disk of a pool, the one with the newest generation or, at
the same generation, the last one registered (`device_list_add` in
`fs/btrfs/volumes.c`): registering a leg would make the mount open the leg
instead of its array, or fail when md holds it. So the unmounted pools are
listed (`FindOfflinePools`, which the boot check, the health run, the
overview and the bridge's device checks share) without a device that is
part of another one: one something holds (md binds a leg it assembles and
device-mapper each device under a map, LVM, multipath or dm-crypt, and both
claim it), a partition of a disk something holds, which the kernel lets
nothing claim (`bd_may_claim` in `block/bdev.c`), such as a path's
partition under a multipath map, one udev probed as a member of another set
(an `ID_FS_TYPE` ending in `_member`: libblkid reports an md, LVM or
firmware RAID signature before any filesystem, and multipath's rule names a
path `mpath_member`), and, for a device udev has no entry for yet, one whose
own md or LVM signature shows (`ReadMemberSignature`'s read, through a plain
open that claims nothing, since an exclusive one could fail a mount or an
md assembly in that instant). That is what upstream registers: udev's
`64-btrfs.rules` and btrfs-progs' `device scan` take only what libblkid
calls btrfs, and the scan skips a multipath path
(`is_multipath_path_device`). A device udev probed as anything else stays a
disk, the safe side for the device checks. The pool's label and size come
from a disk, since a leg is read through its own page cache, which the
array's writes bypass. Missing disks are counted by device ID (devid), so
two devices on one disk count once. A pool that shows only on parts of
other devices has no disk present: the boot check and the health run see
it, and its alert says where it shows, but the overview does not list it,
since it has nothing to mount or wipe. The boot check leaves a pool
unmounted, and its offline alert says why, when it cannot tell which
devices are the pool's disks: two present devices carry one devid (two
paths of one disk before multipath maps them, or a disk and a copy of it;
LinuxIO does not pick one), udev marks one a path of a multipath disk
(`DM_MULTIPATH_DEVICE_PATH`), or one shows an md or LVM signature udev has
not probed yet. The bring-up of a complete pool applies the same rule to
every present device, not only the ones udev scanned. A pool missing a disk
is also left unmounted when that disk shows on a part of another device:
its array or map is there but has not started, as when mdadm's last-resort
timer has yet to start an array missing a leg, and a degraded mount would
open an episode for a disk that is present. **Mount degraded** on the
overview is a person's decision and does not apply this last rule. Right
before it registers anything, a mount of an unmounted pool, the boot
check's or **Mount** on the overview, reads every device's md and LVM
signature again, through an exclusive open that also refuses a device
something claims, and refuses the pool (a 409) when it cannot tell its
disks. It then has the kernel forget each part of another device that shows
the pool's superblock (`BTRFS_IOC_FORGET_DEV`, by its path), so one
registered before by other means, such as `btrfs device scan` naming a
leg, is not opened in place of its array or as a disk its array lacks, and
registers the disks. The boot check mounts a pool's further fstab lines
without registering again, since the first mount holds the disks. `make
test-btrfs-integration` mounts a pool whose disk is an md 1.0 mirror,
another disk gone, degraded through the array without registering a leg,
then, with the array stopped and a leg registered by hand, degraded through
the other disk alone (`TestBtrfsArrayMemberIntegration`).

udev also holds back a complete pool whose device replace a power loss cut,
because the kernel's ready check counts the replace target as an extra disk;
after the same 30 seconds the service mounts it with its fstab options and
without `degraded`, unless its disks disagree on their generation, and the
kernel continues the replace (a pool also missing a disk takes the degraded
path above).

udev can also leave a complete pool unready. Its rule asks the kernel
whether a disk's pool is complete (`BTRFS_IOC_DEVICES_READY`) and marks a
disk scanned before the others not ready (`SYSTEMD_READY=0`); the disk that
completes the pool then has udev evaluate those again (`udevadm trigger -p
ID_BTRFS_READY=0`, matched against udev's database). When two disks are
scanned at the same moment that trigger can miss the first, which stays not
ready. Every disk of the pool claims `/dev/disk/by-uuid/<uuid>` at the same
priority and the last one processed owns it (`udev-node.c` `link_update`),
systemd counts the device as found only when the disk the link points to is
ready (`device.c` `device_setup_devlink_unit_one`), and the kernel sends no
event when a pool becomes complete. When the link points at the disk left
behind, the fstab mount gives up after its 90 second device timeout, with
nothing said. In the test VM (systemd 259, kernel 7.0) that happened at
about one boot in ten, after power cuts and orderly reboots alike: the
first open of `/dev/btrfs-control` loads the btrfs module, and the udev
workers of both disks, which wait for that load, scan within microseconds of
each other. So the boot check first looks for unmounted pools that fstab
mounts at boot, with no disk missing by their superblocks and no replace
target, whose mount unit systemd generated from that fstab (its `SourcePath`,
not masked) and has not started, and whose `/dev/disk/by-uuid/<uuid>` device
it has not found. Ten seconds after it starts it acts on such a pool only
through the disks udev's rule itself scanned: those udev's database
(`/run/udev/data`) names btrfs of this pool with the `ID_BTRFS_READY` only
the rule sets, and that nothing holds. A leg of an md array with its metadata
at the end, a multipath path or an LVM mirror image shows the btrfs
superblock too, but udev probes it as what it belongs to, and registering it
would let btrfs open it under its array. Those disks must be one for each
devid of the pool, at least one of them left not ready, none with an md or
LVM signature when read (an exclusive open, which nothing holding the disk
allows), and the kernel must then call them complete
(`BTRFS_IOC_DEVICES_READY`, asked of them only, which counts the registered
disks against the superblock's `num_devices`). It then writes `change` to
`/sys/class/block/<disk>/uevent` of each disk udev left not ready, which is
what the rule's trigger does for them, and asks systemd to start the pool's
fstab mounts: that joins the boot's own mount job, or starts it again when
the job already gave up. systemd then mounts the pool with its own options
and ordering. The check waits up to 30 seconds for those mounts and logs
`btrfs pool mounted after udev evaluated its disks again`, then updates the
pool's alerts. A pool whose device systemd has found or is still mounting by
then is left to it without an alert; the offline alert is raised only for a
pool known not mounted, and a pool whose mounts cannot be listed gets none.
The health run two minutes after the boot raises it for any pool still
unmounted, so a pool the check left alone is not silent. A pool whose device
systemd found is left to systemd, whatever its mount then
does, and so is one missing a disk or holding a replace target, which takes
the paths above. As on any boot where systemd mounts a pool itself, the
degraded path's refusal of disks that missed writes does not run for a
complete pool. This runs before those paths, whose 30 seconds count from the
check's start, and once more after their wait, for a pool whose disks came
meanwhile. Such a pool is up 16 to 18 seconds after the boot in the test
VM (the check starts at about 6) instead of not at all, and the root job
host, which starts after the boot check, resumes its jobs on it.

The record lives in `/var/lib/linuxio/btrfs-pools.json`: every mount LinuxIO
makes and every health run notes each pool's generation, and while a pool runs
degraded, which disks take its writes and which miss them. A disk that dies while the pool is
mounted is only recorded by the next health run, so a crash within those ten
minutes, followed by only the dead disk coming back, is not caught. The
recorded generation can belong to a transaction a crash then loses, which
makes the check refuse a set that was in fact current: the safe side.
Only **Destroy pool** removes a record (a health run that listed the pool
before the destroy can write it once more). The record of a pool whose disks are
disconnected stays however long they are away, since it is what refuses the
stale one when a single disk comes back; the record of a pool wiped outside
LinuxIO stays too, and nothing reads it: a record is looked up only by the
UUID of a pool that is mounted or found on a disk.

**When the disk is back.** A disk that reappears while the pool is mounted is
listed by the kernel but never opened, so it is shown as missing until the
pool is mounted again (a reboot); so is one that dropped out of the mounted
pool (see below). Once every disk is in use, the health run
starts `linuxio-btrfs-resync@<uuid>.service` (`linuxio-worker resync`), unless
a scrub of the pool runs (a start while the unit runs joins its run):
a scrub checks every copy and rewrites those the returned disk missed from
the good ones, then a soft convert (`convert=<profile>,soft`) gives chunks
written while too few disks were present, which btrfs allocates as `single`,
the pool's most redundant profile again. Until then the `resync` alert is up.

The scrub runs as the episode's resync run, `resyncRun` in the pool record,
which its scrub record carries (see [Scrub progress](#scrub-progress)), so
the next resync continues a scrub cut short instead of starting over: after
a power loss, a crash or a kill, a shutdown, or **Stop check** on the
overview, it scrubs each device that had not finished from one byte below
the offset it saved, a device whose scrub aborted included, keeps the first
start and adds up the counters, and the *whole again* notice counts the
corrections of both parts. A scrub of the run that finished counts, though
the cut came before the resync noted it. The next resync is the next health
run's, two minutes after a boot or within ten minutes, once every disk is in
use; the resync stays a scheduled run and never goes through the job host.
Only the same episode continues. A degraded mount LinuxIO makes, a pool the
boot check finds already mounted degraded (a root the initramfs mounted with
`rootflags=degraded`), or a health run that sees the pool degraded starts a
new one, whose resync scrubs everything again, since writes made with a disk
missing may land below the saved offsets, and a scrub that finishes after
its episode ended does not count for the new one. The pool's generation
cannot tell an episode, since every commit moves it, with a disk missing or
not. A pool left unmounted while a disk is missing takes no writes and keeps
its episode, so its resync continues once the disk is back. The continuation
refuses a pool that lacks a disk. A disk under a device ID that the record
names with another UUID starts from 0. `jobs-phase3.spec.ts` cuts a resync
this way in the test VM: a disk drops out on the guest's PCI bus, the boot
check mounts the pool degraded, the writes made meanwhile fill holes in
every data chunk, and a power loss cuts the resync past half its scrub; the
next run reads less than nine tenths of a full scrub, still corrects stale
copies, and the returned disk alone then holds every file and scrubs clean.

**A disk that drops out of a mounted pool**, unplugged or off its bus, has
its block device deleted. Since 7.1 the kernel then marks the member missing
itself (`btrfs_remove_bdev` in `super.c`, also in 6.19 and 7.0 built with
`CONFIG_BTRFS_EXPERIMENTAL`, which Ubuntu's 7.0 is not), and continues
degraded, or shuts the filesystem down when the remaining disks cannot keep
it read-write; DEV_INFO names that disk `<missing disk>`, which LinuxIO
lists as a missing disk with no path. When it is the disk the mount names as
its source, mountinfo shows that source as `<missing disk>` too
(`btrfs_show_devname`), and keeps it after the disk comes back, which the
mounted pool refuses as a duplicate. LinuxIO tells a pool's mounts by their
source, so for such a mount it asks the mount itself (`ListMounts`): FS_INFO
through an open of the mountpoint that must reach that mount, by the mount
ID mountinfo gives it, so a mount over it or a parent folder renamed since
is not taken for it; a source that is not a path is never looked up as a
device, which would read a file of that name in the working directory.
Mounting the top level, which subvolumes, snapshots, settings and
replication need when no mount of it exists, and the remount of a rollback
then name the mount's source while that block device exists, else a member
DEV_INFO gives a path that exists (`deviceOf`), asked through one open of a
mount that must still be that pool's by its FS_INFO; an older kernel names a
source that dropped out by its old path, which no longer exists. Without
all this the pool read as unmounted while the disk was gone, and no rebuild
started once it was back. An older kernel keeps the member with
its dead block device until the pool is unmounted: DEV_INFO still names it
by its path (`btrfs_ioctl_dev_info`), sysfs calls it neither missing nor
unwriteable, chunks are still allocated on it, and every write to it fails
and is counted in its write and flush errors, so what is written meanwhile
has one good copy. Nor does the mounted pool take the disk back: a scan of
it under another device number is refused as a duplicate, and one under its
old path changes nothing (`device_list_add` in `volumes.c`). LinuxIO counts
such a member missing and dropped out (`markDroppedOut` in `dropout.go`).
The kernel links each block device it opened for the pool under
`/sys/fs/btrfs/<uuid>/devices`, named after it (`btrfs_sysfs_add_device` in
`sysfs.c`), and removes the link only when the member leaves the pool; once
the block device is deleted the link reaches nothing. A device that has its
name again, the same disk back or another, is reached by the link but is not
the one the pool holds: btrfs holds its members exclusively, so an exclusive
open fails with `EBUSY` on a member it holds and succeeds on any other
device. That open is read-only, so it makes udev probe nothing again, and
one refused opens nothing. A member is matched to the link of its own name,
else of the device its path resolves to; one that matches none, as
`/dev/root` for a root the kernel mounted without an initramfs, counts as
dropped out only when as many links of gone devices match no member, and its
health is unknown when they disagree. The next health run therefore opens
the episode while the pool runs on the others, with the disk's counters as
the pool was last seen whole, and the health alert and the pool's details
say the disk dropped out and that the pool takes it back only when it is
mounted again: unmounted and mounted from the Disks page, which
registers the pool's disks first, or a reboot; with the automatic rebuild on
(below), that LinuxIO rebuilds the pool onto the disk once it is back. Until then no resync starts,
and one started anyway stops without counting a failed attempt; the mount
that takes the disk back gives a complete pool with a stale member, and the
next health run starts the resync. A reboot without the disk takes the
degraded path above, mounting the pool on the disks that took the writes.
The disk, back under its old name or another, carries the superblock of the
mounted pool, which does not hold it: until the pool takes it back or its
device in the pool is replaced or removed, every operation that writes a
device refuses a disk whose superblock names that pool, and whatever is at
the old path, which udev may not have probed yet, as possibly the disk that
dropped out (`deviceInUse`); only the automatic rebuild takes it (below). A bridge of a session without sudo, whose
exclusive open is refused, still sees a link that reaches nothing. The
kernel removes a member's link before it lets the block device go, as the
member leaves the pool or the pool is unmounted (`btrfs_rm_device`,
`close_ctree`), so a device found gone counts only while its link is still
there. A scrub started meanwhile
scans the disks left, and its flush passes the gone link by. In the test VM
on 7.0.0-44, a `scsi_debug` disk deleted from its SCSI bus under a mounted
raid1 pool kept its path in DEV_INFO with sysfs `missing` 0 and `writeable`
1 and a link that reached nothing; it came back under a new name, since the
kernel keeps the old one while the pool holds the dead block device, and
its registration was refused (`EEXIST`). Mounted again, the resync's scrub
corrected 16412 errors of the copies it missed, after which the disk alone
held every file (`TestBtrfsDropoutIntegration`).

**Rebuild onto a disk that comes back.** When a member of a mounted pool is
missing and its own disk comes back, pulled and plugged in again, LinuxIO
erases that disk and replaces the member's devid onto it with
`BTRFS_IOC_DEV_REPLACE`, without unmounting, then resyncs the pool as after
any episode. It is on by default and switched per pool on its Disks card
(**Rebuild automatically onto a disk that returns**, `storage.btrfs_auto_rebuild`,
stored as `noAutoRebuild` in `btrfs-pools.json`); switched off, a disk that
comes back waits for the pool's next mount, as above. The mounted pool cannot
take the disk back itself: a scan of the returned disk is refused as a
duplicate while the member holds its dead block device, and after a degraded
mount the kernel names the member by the disk's path but never opens it
(`device_list_add`). A replace copies every extent onto the disk from the
other mirrors, so no stale copy is left, where a remount and a scrub repair
only what has a checksum. This is the one place LinuxIO erases a member of a
btrfs pool without a person. In the test VM on 7.0 (`TestBtrfsRebuildProofIntegration`)
a disk pulled off the PCI bus and plugged back came back under a new name,
was refused by the scan, and after `WipePoolSuperblocks` and a replace of
its devid the pool was whole: a read-only scrub clean, every file right, the
nodatacow file too, raid1 only, after an unmount and mount and after a reboot;
after a degraded mount without the disk the same worked once the chunks the
degraded mount wrote `single` were converted.

The health run decides (`checkRebuilds` in `rebuild.go`) and starts
`linuxio-btrfs-rebuild@<uuid>.service` (`linuxio-worker rebuild <uuid>`),
which decides again on what it reads then (`Rebuild`). The disk is rebuilt
onto only when all of these hold:

- it is a whole disk (never a partition, md, device-mapper or loop device)
  that sysfs shows nothing holding, without partitions;
- its own primary superblock carries the pool's fsid, the devid of a member
  that is missing now, that member's device UUID as `DEV_INFO` reports it,
  and a generation no newer than the pool's (`FS_INFO`); a replace target's
  superblock (devid 0) is never one;
- its WWN and serial, with its size, as udev reports them (the identity a
  hot spare is named by, `wholeDisks` in `spare.go`), are those LinuxIO
  recorded for that devid while the pool was whole; a disk udev has not
  reported yet waits for the next health run. Every health run that
  sees the pool whole outside an episode records each member's
  (`identities` in `btrfs-pools.json`), and a verified replace records the
  new disk's for its devid;
- that identity tells the disk apart: no other member was recorded with it,
  and no other connected disk has it, as behind USB bridges that report one
  serial number for every disk;
- no other device carries that member's superblock;
- the same disk, by that identity, in one attachment (its `diskseq`, which
  the kernel raises each time a disk is attached) and one boot, was seen by
  two health runs in a row (`returned`), about ten minutes with the timer;
- it was not rebuilt onto in the last 24 hours (`rebuilt`), and not refused
  or failed earlier in the episode (`refused`);
- it is no smaller than the member as the filesystem knows it.

A copy of the member's disk is identical to it in every superblock field,
and a disk replaced earlier whose superblock was never erased (it was dead)
claims the same devid and device UUID as its replacement; only the hardware
identity tells them apart, so a member without one recorded, or a disk
without a WWN or serial, is never rebuilt onto automatically. The two
sightings keep a disk that is still being seated, or flapping on a loose
cable, from being erased at once, and the day keeps a disk that keeps
dropping out from being fed back in a loop: on kernels before 7.1 a target
that dies during the replace forces a two-disk pool read-only (below).

The rebuild, onto the returned disk or the spare, also needs the pool
mounted read-write, every kind of its chunks in a profile that keeps a
second copy (`raid1`, `raid1c3`, `raid1c4`, `raid10`), no more members
missing than its weakest such profile loses (one for `raid1` and `raid10`,
two for `raid1c3`, three for `raid1c4`; `beyondTolerance`), no chunk that
keeps one copy (`single`, `dup`, `raid0`) with a stripe on the missing
member, and nothing else running on it that the kernel or LinuxIO would refuse beside a
replace: an exclusive operation (sysfs `exclusive_operation`: balance,
device add or removal, replace, resize), a replace running or suspended, a
balance, a scrub, or the pool's resync unit. The chunks are read from the
chunk tree (`oneCopyChunks`, a tree search of tree 3): a degraded mount
allocates `single` chunks on the disks present, which a rebuild of another
member leaves alone, but one left on the member by an earlier episode whose
convert gave up, or by a convert cut short, has its only copy on that disk,
which the erase would destroy. The count of missing members is what keeps
a second failure from becoming a loss: `raid1` on three disks keeps each
chunk on two of them, so with two members missing, kernel-missing and
dropped out alike, the chunks those two held together have no copy on the
disk that stayed. Erasing the one that came back would erase their only
copy, and a replace onto it, or onto the spare, cannot copy them, yet ends
well for the kernel, which is still within what `raid1` loses. So nothing
is rebuilt, and the alert and the page say to reconnect the other disks and
mount the pool again so btrfs takes them back; a rebuild on record whose
disk is already erased waits for the same. And a returned disk is not
erased while a
member that stayed counts read, corruption or generation errors: the
returned disk's old copy may then be the only good one of a block, and the
alert says to unmount the pool and mount it again, so btrfs takes the disk
back and the resync repairs both disks, then to reset the counters with
`btrfs device stats --reset <mountpoint>`, which LinuxIO has no action for.
While that disk is connected the hot spare is not used either: rebuilding
from a member that counts errors while a better copy sits on the returned
disk is what the rule is against. A count that changes is the same
refusal, one alert occurrence. For these two refusals the page and the
alert give that advice in place of replacing the missing disk, which would
give up the copy they keep (`advice` beside `reason` in the pool's
`rebuild`). These are judged at every run, and only when there is something to rebuild onto:
a pool with the switch off and no ready spare raises nothing. A busy pool,
or one whose profiles could not be read, waits for the next health run
without an alert; a pool that cannot be rebuilt, and a disk refused, raise
the `btrfs/rebuild-<uuid>` error with the reason, once however many health
runs see it (a reason never names what changes between runs, such as the
pool's generation).

Right before it writes, the runner holds the disk lock (`LockDisk`, so udev
probes nothing meanwhile) and checks the disk again through one exclusive
open for writing, which the kernel refuses while anything holds the disk:
the open must reach the disk the lock holds (their device numbers), and on
it partitions and holders by sysfs, its md and LVM2 signatures, its
superblock and generation, and the identity udev has for that device
number. The lock's descriptor, held until the replace has opened the disk,
keeps the disk's name from passing to another disk meanwhile, even if it
goes away: sd and virtio_blk give a name back only once the disk's last open
is closed. For the hot spare, the disk the lock holds must be the one the
spare names. A check that could not be made, as a sysfs read that failed or
a disk udev reports no identity for at that moment, fails the run and the
next health run tries again; only what a check found refuses the disk.
It then records the rebuild (`rebuild` in `btrfs-pools.json`: the devid, its
device UUID, the disk's identity and node, the time, the boot, and the start
time the kernel reports for the pool's last replace, `prior`), erases the
pool's superblocks through that same open (`WipePoolSuperblocks`), has the
kernel forget the disk, and starts the replace with
`cont_reading_from_srcdev_mode` AVOID, so the copy reads the other mirrors.
The forget is refused with `EBUSY` once the erase is done when the kernel had
marked the member missing, since the returned disk's scan gave that member the
disk's device number; the runner accepts that refusal there and only there.
The disk is refused for everyone else meanwhile (`deviceInUse`, as above);
the runner alone takes it, through these checks, not through that rule.

The kernel's word that the replace finished is not trusted. In the test VM
a returned disk pulled again during the copy was never noticed by the
kernel: `num_write_errors` is read in `scrub.c` but raised nowhere (6.8, 7.0,
7.1), so the copy ran to the end, the commit that installed the dead target
could write neither its superblock nor the dead source's, aborted, and forced
the two-disk pool read-only, and the ioctl still returned success with the
replace finished and `DEV_INFO` naming the dead target. Since 7.1 the kernel
marks the pulled target missing (`btrfs_remove_bdev`), so the commit that
installs it skips it rather than failing (`write_all_supers` writes no
superblock to a device that is not writeable), and the replace finishes "from
`<missing disk>` (devid 2) to `<missing disk>`" with the pool writable and
the member missing (test VM, Fedora's 7.3.4), takes a source disk that is
still present out of the pool and erases its superblocks, and corrupts
kernel memory from then on (see [Known limits](#known-limits)). LinuxIO
therefore cancels a replace whose new disk goes away, the rebuild's and one
from the pool details alike (`replaceDevice`): every two seconds while the
kernel copies it looks at the kernel's link to the new disk under the
pool's devices, and when that reaches nothing it sends the cancel, on which
the kernel destroys the target and keeps the source, and the replace fails
saying the new disk went away. On 7.0 the pool still goes read-only, since
the dead target, which that kernel does not mark missing, fails the commit
of the cancel; LinuxIO's next mount cancels the replace the kernel then
suspends, as before. On 7.1 and later the pool stays writable with the
member as it was, but the kernel has taken the target off its count of
writable disks, where a target was never counted (`btrfs_remove_bdev`), and
the count stays one short until the kernel forgets the pool: the pool is
given no new chunk, and a read-write mount of it is refused (`EACCES`). So
where the kernel had marked the target missing, the failure also says to
restart the server. A disk that goes within the last two seconds of the
copy is not caught. So the runner, and a
replace started from the pool details (`ReplaceResult`, `ResumeReplace`),
check that the kernel reports the replace finished, that the devid is in use
at the new disk's path and not dropped out, that the pool is not read-only
(`statfs`), and that the pool holds the new disk (an exclusive open of it
fails with `EBUSY`; one that works, or a disk gone, fails the check, and any
other error, such as a refused permission, is a check that could not be made
and is made again by the next run); the runner also checks the disk in the
slot is the one it wrote, by its identity. A stop of the runner does not cut
this check or the write that follows. Only then is the record cleared, the disk noted as
rebuilt onto, a bigger disk grown into, and the `btrfs/rebuilt-<uuid>` notice
raised; the runner then starts a health run, which starts the resync, whose
scrub and soft convert end the episode as after any other (no counters are
reset in an episode with a replace, see [Error counters](#missing-disks)).

A rebuild that fails after the erase keeps its record, as failed, raises the
`btrfs/rebuild-<uuid>` error and is not tried again onto that disk without a
person; while the record is set no other rebuild starts on the pool, save
onto its hot spare while the member is missing or its slot still holds the
disk the failed rebuild wrote, and no resync runs or calls the pool whole. A
replace of that devid from the pool details, once verified, or the device
leaving the pool, clears it; a runner that fails after that writes nothing
back. Whether a replace the kernel reports is the rebuild's own is told by
its start time against `prior`, never by the clock: a replace started after
the record has another, whatever the clock did, and the kernel's last
replace from before it has the same, and a finished replace whose slot holds
the recorded disk is checked as after a replace (`verifyReplaced`) before
its record is cleared, whatever its start time says. The record matters at the next mount: after the failure
above, the surviving disk's superblock already carries the replace with its
cursor past every extent, and mounting the pool with the failed target
present makes the kernel "continue" that replace, which ends at once and
leaves on the target everything it never copied (seen in the test VM: 32768
stale checksummed sectors a scrub then found; stale copies of nodatacow data
cannot be found). So a mount LinuxIO makes (**Mount** and **Mount degraded**
on the overview, `MountOfflinePool`, and the boot check, `MountDegradedPools`)
of a pool with a record first erases the disk with the record's identity that
carries the pool's superblock as the replace target (devid 0), mounts
degraded when that leaves a member missing, and cancels the replace the
kernel then suspends ("cannot continue dev_replace, tgtdev is missing") or
continues. It decides that before it writes anything
(`planUnverifiedErase`), and the boot check judges the pool, by the same
plan, before anything is erased. The replace target holds no copy the pool
needs, and is erased unless it is newer than every device that stays (a
replace target gets the pool's superblock at every commit,
`btrfs_init_dev_replace_tgtdev` and `write_all_supers`, so its generation is
the last commit that reached it, as a member's is); a stale disk among those
that stay is the ordinary rules' matter. A disk with the record's identity
that carries the member's devid, which a finish nobody checked left there,
or the disk a failed rebuild left untrusted that a hot spare replaces
(`untrusted` in the record), may hold the only copy of what another member
held: it is erased only when every other member of the pool is present and
none is older than it, and is otherwise left as it is and the pool judged as
without a record, never refused because of it. What is left is judged as
without a record: the boot check's refusal of a pool that missed writes,
or the person's choice of a degraded mount. LinuxIO refuses the mount only
where it cannot tell, each time with a way out the reason names: two
connected disks with the record's identity (disconnect the ones that are
not the pool's), a device carrying devid 0, or carrying the member's devid
where it would be erased, whose identity udev does not report, as some
enclosures hide a disk's serial (wait until udev reports it, or disconnect
it), and a replace target that is the only device present (reconnect the
pool's other disks). The overview's mount then fails with the reason and the
way out (a 409), and the boot check leaves the pool unmounted with both in
the offline alert, which the health run raises the same way and which then
does not advise the degraded mount that would be refused alike. The erase
itself goes through the disk lock and an exclusive open that must reach the
disk the lock holds and find on it the identity, the devid and the
generation the decision was made on, and refuses it when it has
partitions, a holder or an md or LVM2 signature. A rebuild that was cut
rather than failed, by a stop of its unit or a power loss, is then started
again onto the same disk, found by its identity, only as the cut left it:
the pool's superblock with its magic zeroed and its fsid kept (LinuxIO's
erase and the kernel's `btrfs_scratch_superblock` both zero only the
magic), a replace target's superblock of the pool, or the member's own from
before the erase, with no filesystem udev reports on it other than this
pool's. A disk put to another use since, or not the only one with that
identity, fails the rebuild instead, and one onto a hot spare the person
has removed since fails too. A disk that is not connected is waited for,
without the unit being started for it, while the page and the
`btrfs/rebuild-<uuid>` warning say the rebuild waits for it, not that it
copies (`waiting` with the reason); 20 minutes after the record, two
health runs, it fails, saying that the disk coming back does not resume it,
which lets a ready hot spare take the member. A rebuild started again obeys
the pool's checks: it waits while another operation runs or the pool
cannot take a replace. One whose disk still carries the member's own
superblock, never erased, is set aside rather than failed when the checks
above refuse that disk or the switch is off: the record goes, nothing was
erased, the pool takes the disk back at a mount, and LinuxIO's next mount
does not erase it. With the switch off, a rebuild onto a returned disk
already erased fails, saying to replace the device. A rebuild started
again writes its record only over its own: one a person's replace cleared
meanwhile stays cleared. A rebuild cut by a stop or a crash in the boot it
started in is followed to its end and checked by the next run; what runs
on after a stop of the unit (the check and the record's writes) is
bounded to a minute, below the 90 seconds systemd gives the unit to stop.
When the pool was mounted with the target before LinuxIO could act (by
systemd, with `degraded` in fstab, or by hand), a replace the kernel finished
then is not trusted: the pool is not called whole, the error says what
happened and to unmount the pool and mount it again from the overview, which
erases the disk and mounts degraded, then replace the device; a ready hot
spare replaces that member at once, reading the other mirror.

`make vm-test-btrfs-integration` runs this path on the test VM's kernel
(`TestBtrfsAutoRebuildIntegration`; it skips on a host, since it pulls the
VM's virtio disks off the PCI bus): a rebuild onto the returned disk with the
member dropped out, the same with the disk the mount names as its source,
and with the member missing after a degraded mount, a
clone of the member on another disk refused by its identity and left as it
was, two disks claiming the member refused, a replace onto the hot spare, and
the returned disk pulled again mid-rebuild, after which the rebuild cancels
its replace and fails with its record kept, LinuxIO's degraded mount erases
the disk, and a replace from the pool details makes the pool whole;
each ends whole by the same checks as the proof.

A disk whose writes fail while it stays attached, as when its cable comes
loose, or under a device-mapper or md device that stays when the disk under
it goes, is not marked missing: the kernel does that at mount, or when the
block device it holds goes away (above). Only the disk's write and flush
error counters rise, while the
other disks take every write, some below its saved offset. The scrub record
therefore keeps each device's write and flush error counters as they were
when its scan began. A continued resync scans a device whose counters
changed since from 0, done or not; a finished scrub of the run does not
count once a device's counters changed, a device it scanned left the pool,
or a disk was added that it did not scan, onto which a convert may write
second copies; and the resync checks the counters again when its scrub ends and once
more right before it declares the pool whole, after the convert, which
writes the second copies onto the returned disk, and in an attempt that
skipped the scrub because it already counted. A change fails the attempt,
naming the device and saying its writes failed, and the scrub no longer
counts, so the next resync scrubs again; so does a scrub record that is no
longer the resync's. That also catches a repair whose write failed, which
the kernel counts as corrected (`scrub_stripe_report_errors` never consults
the write errors). The check refuses what it cannot read: error counters it
could not read fail the attempt, and a disk gone missing starts a new
episode.

A replace keeps the device's ID and UUID, since the kernel gives the target
the source's (`btrfs_dev_replace_finishing`), so the continuation carries on
from that device's offset: the replace wrote every copy that has a checksum
to the target from one it checked as a scrub does. A sector with no good
copy is not written there, and data without checksums (`nodatacow`) is
copied as read, so a stale copy of it may stay, as a scrub would leave it
too. A balance, a removal or a convert writes the chunks it moves anew, from
reads checked against their checksums, so it leaves no stale copy below an
offset, except of data without checksums, copied as read, and on a pool
with the experimental remap tree (kernel 7.0 and later, built with
`CONFIG_BTRFS_EXPERIMENTAL`), which relocates by a raw copy with no
checksum check.

**Error counters.** The scrub counts each stale copy it finds on the
returned disk as an error of that disk, repaired or not: a data copy whose
checksum fails as a corruption error, a tree block of an older generation as
a generation error (`scrub_stripe_report_errors` in 7.0; 6.8's scrub counts
none), and a read that hit a stale data copy before counted a corruption
error there too (`btrfs_data_csum_ok`). Nothing but a reset lowers a
counter, so they would keep the health alert up beside the *whole again*
notice.

After a resync that ended whole, its own scrub record finished with no
uncorrectable error and no replace running, suspended or finished during the
episode, LinuxIO resets the counters of a disk the episode recorded missing
if and only if the kernel uses it under the same device ID and UUID, its
read, write and flush counters are all zero, and its counters were all zero
the last time LinuxIO saw the pool whole, or, with no such record, when it
went missing. It resets with `BTRFS_IOC_GET_DEV_STATS` and
`BTRFS_DEV_STATS_RESET`, the call `btrfs device stats -z` makes, which
zeroes all five counters of a device (`btrfs_get_dev_stats`), so what it
clears is the corruption and generation errors of the stale copies. In every
other case the disk keeps all its counters, the health alert stays, and a
warning in the journal and the notice say why. A disk that counts any failed
read, write or flush is never reset by LinuxIO, whatever the cause, so a
disk that dropped out of a mounted pool, which counted the writes and
flushes that failed as it went (`btrfs_log_dev_io_error`), keeps them. Once
someone has looked at a disk, `btrfs device stats -z` clears its counters;
LinuxIO offers no reset of its own.

Every health run that sees a pool whole outside an episode saves each
device's counters (`counters` in `btrfs-pools.json`), and so does the end of
an episode. The episode records each disk missing as it opens (`missing`) by
device ID and UUID, since a disk added after a removal can take a freed ID
(`find_next_devid`), with the counters saved for it; a disk that goes
missing later in the same episode is recorded with what it reads then. A
replace puts a new disk under the missing disk's ID and UUID
(`btrfs_dev_replace_finishing`), which holds no stale copy: LinuxIO's own
replace drops that disk from the episode, and the resync resets nothing in a
pool whose last replace, as `BTRFS_IOC_DEV_REPLACE` reports it, finished
after the episode began or still runs. A disk that stayed present is never
reset, and neither is any disk in an attempt that failed, was stopped or
found uncorrectable errors, nor in an episode that recorded no missing disk.

The reset opens the pool once, checks that the mount holds it, and issues
every ioctl on that descriptor; a disk counts as in use as the inventory
reads it (`devinfo/<id>/missing` and `writeable`). The kernel writes a reset
to the device tree at its next commit (`btrfs_run_dev_stats` in
`commit_cowonly_roots`), which the reset does not start, and 7.0 does not
commit at unmount without a running transaction (`btrfs_commit_super`; 6.8
starts one): with the commit below disabled by hand, the root test on
7.0.0-44 read the counters back at the next mount with no write in between,
an observation no step of the suite repeats. The resync therefore commits on
every attempt that ends whole with a recorded disk in use, whether or not
that attempt reset anything: it sets the label to itself, since
`BTRFS_IOC_SET_FSLABEL` starts a transaction and commits it whatever the
label (`btrfs_ioctl_set_fslabel`), under `btrfs-label.lock`, which a rename
through LinuxIO takes too. The reset and the commit run outside the pool
record's lock and before the resync ends the episode, so a crash at any
point leaves an episode the next resync ends the same way, on counters
already zero and with the commit made again. They go on past a stop of the
resync, with two minutes at most for the mount lookup and the label lock;
the ioctls themselves block as long as the kernel does, and setting the
label of a frozen filesystem waits for it to thaw. Since a disk is reset
only with its write and flush counters at zero, the scrub's check of them
(`failedWritesSince`) reads the same before and after, and a retry does not
scan it again. Each reset is one journal line with the pool, the device and
every counter as the kernel returned it, a warning when a read, write or
flush failed in the instant between the check and the reset. The notice
says what the scrub corrected by its own record, names the counters reset,
and while any device of the pool still counts errors is a warning that says
why each does. The resync refreshes the pool's alerts as it ends, so the
health alert clears at once when no device counts errors. A health run that
cannot read the pool records leaves the resync alerts as they are.

A failed resync is retried by the next health run, without scrubbing again
once the scrub finished. **Stop check** on a resync counts as a failed
attempt too. When a power loss cuts the resync's convert, the
kernel continues it at the next read-write mount and refuses a second
balance meanwhile, so the next resync follows that balance to the end when
the arguments the kernel reports are those of the convert it recorded
starting (in `btrfs-pools.json`, cleared with the episode), instead of
starting one. It compares with that record rather than with what is left to
convert, since the kernel relocates chunks from the highest address down,
whatever their kind, and a convert of data and metadata cut midway can leave
one kind done; without a record, as after a new episode cleared it while
the kernel's balance survived into the next mount, it compares with what is
left, and a convert cut after one kind finished then fails the attempts
until the kernel's balance ends. It takes
over no other balance: a running one that is not its convert,
an admin's or a job's, fails the attempt as already running, and a paused
one fails it too, saying the admin resumes or cancels it, since an admin
paused it or mounted with `skip_balance`, the recovery option for a balance
that fails at mount. A resync stopped while it follows its convert, at a
shutdown or by a stop of its unit, leaves the balance running for the kernel
to pause at unmount and continue at the next mount; one stopped while it
runs its own convert cancels it, and the next resync converts again. Before
it declares the pool whole it reads the allocation again: chunks left to
convert by the rule that started the convert, system chunks with metadata,
fail the attempt. After three failures the alert turns into an error with the
last cause, and mounting the pool again (or a reboot) re-arms it. Since these
alerts report it, the run of a resync that fails still completes once they
were brought up to date, with the summary *Resync failed*: the unit is not
marked failed and its end hook raises no run alert of its own
([Alerts](./alerts.md#who-writes-with-no-session)); a resync that could not
find its pool or read its state, or whose worker is killed, fails its run
and gets that run alert.
Uncorrectable scrub errors go to the scrub alert, which stays up at error
severity until a later scrub finds none, and the *whole again* notice of
that resync is a warning that says how many errors could not be corrected;
that resync resets no counters. Files with copy-on-write disabled
(`nodatacow`, common for VM images) have no checksums, so a scrub cannot
tell their stale copies; replace a disk that returns to a pool holding such
files instead. A disk that keeps failing writes while still attached is not
counted as missing: the health alert reports its error counters, and a scrub
once it is fixed repairs what it missed.

### Known limits

- A complete pool udev left unready is mounted only once the boot check has
  udev evaluate its disks again, ten seconds after the check starts: until
  then a service not ordered after `linuxio-btrfs-degraded.service`, or a
  script that reads the pool as soon as the host answers, finds it
  unmounted. When a pool never mounts, the check and what is ordered after
  it (Docker, the root job host) wait up to about 80 seconds: 10 to settle
  and 30 for the mounts, twice when a pool missing a disk was waited for.
- The boot check acts only on pools fstab names by `UUID=`, at a mountpoint
  that is not a symlink (systemd names the unit after the path the link
  resolves to); one mounted by `LABEL=`, a device path or through a link
  stays unmounted. An `x-systemd.automount` line it brings up is mounted at
  once rather than on first access. A line without `nofail` is never
  helped: its mount is required by `local-fs.target`, which fails first and
  sends the boot into emergency mode before the check runs. A pool whose
  disks appear after the check's first look is looked at again only when a
  pool missing a disk was waited for (`MountDegradedPools`). A mountpoint
  whose unit name is too long for systemd, which then names the unit by a
  hash of it (`unit_name_from_path`, since systemd 248), is never brought
  up: the check asks for the unhashed name, which systemd refuses.
- While the check, or a mount of an unmounted pool, reads a disk's md and
  LVM signature through an exclusive open, a mount of the pool by an fstab
  line that itself carries `degraded` could start in that instant and
  proceed without that disk.
- A path of a multipath disk that udev has not processed yet, or that
  multipath's rule does not mark, is a disk to LinuxIO, as it is to
  btrfs-progs; two such paths are refused as two devices on one disk. Until
  multipath maps the disk, a pool on it or on its partitions is mounted
  neither by the boot check nor from the overview.
- A leg of an array that has not started is erased by Format, Delete or
  Initialize on a confirmation that names its array (a multipath path, one
  that names `mpath_member`), as any member of a stopped array is; the
  confirmation does not name the pool whose superblock the leg shows.
- **Destroy pool** on an unmounted pool wipes the disks present; a disk under
  an array that has not started keeps its superblock.
- Writes made with a disk missing that LinuxIO does not record are not seen:
  a degraded mount it did not make (by hand, or on another system) written
  to and made whole again before a health run sees it, the moment between a
  mount and the record of it, and a mount whose record write failed, which
  is only logged. A continued resync then skips what its scrub had covered
  before the cut.
- The kernel writes a disk's error counters at a transaction commit
  (`btrfs_run_dev_stats` in `commit_cowonly_roots`; every 30 seconds by
  default, or as the `commit=` mount option sets), so a power cut within one
  commit interval of a failed write can lose the rise, and the continued
  resync then trusts that disk's offset.
- The saved counters are a health run's, every ten minutes: corruption or
  generation errors a disk counted between the last run that saw the pool
  whole and its dropout are reset with the stale copies'; the journal line
  keeps them.
- A corruption or generation error is counted alike whatever caused it, so
  every one the returned disk counted during the episode is reset with the
  stale copies' counts: bit rot or writes the disk lost, found by the scrub
  or by an ordinary read, also while it was back in use and another disk was
  still missing. The journal line and the notice keep the counts.
- A pool that lost a disk before any health run saw it whole has no saved
  counters, and uses the counters each disk read once it was missing.
- The reset does not cover a disk that dropped out of a mounted pool, which
  keeps the writes that failed.
- A dropout opens the episode at the next health run (ten minutes at most).
  When the pool is unmounted or the machine shut down before that run, and
  a later mount takes the disk back, no episode opens: a pool that is not
  mounted is only checked for what keeps it from mounting, and a mount of a
  complete pool opens none. The copies the disk missed stay stale until a
  scrub.
- The rebuild onto a disk that comes back starts at the second health run in
  a row that sees it: up to twenty minutes after it is back with the timer,
  sooner when health runs are started by hand, since it counts runs, not
  time.
- A disk without a WWN or serial, a member on a partition, md,
  device-mapper or loop device, and a member LinuxIO never saw whole, such
  as one missing since before the first health run of a release that
  records identities, are never rebuilt onto automatically; the alert says
  why.
- The rebuild was run in the test VM on Ubuntu's 7.0 and Fedora's 7.3.4;
  on 6.8 it rests on its source.
- Since 7.1 a replace whose target is pulled mid-copy finishes onto the
  missing disk, and the kernel then corrupts its own memory: the finish
  puts that disk on the pool's allocation list without counting it writable
  (`btrfs_dev_replace_finishing`), and each recalculation of the pool's free
  space fills an array sized for the writable disks with every disk on that
  list (`alloc_virtual_chunk` in `volumes.c`, new in 7.1), 32 bytes past its
  end. An unmount leaves the disk on the list (the `WARNING` in
  `btrfs_close_one_device`), and a scan of the returned disk under the same
  device number frees it while linked, so the next mount writes to freed
  memory. Shown in the test VM on Fedora's 7.3.4 booted with `slub_debug`,
  the second with `btrfs` and `mount` alone, the first during LinuxIO's own
  degraded mount after a rebuild whose disk dropped out again; without
  `slub_debug` two of five runs of the root tests ended with the guest
  crashed. An upstream bug, as is the count of writable disks the cancel
  leaves one short (above): after either, only a restart of the server
  puts the kernel's view of the pool right. LinuxIO cancels the replace
  when it sees the disk go, which keeps the kernel from finishing it; when
  the copy ends
  within two seconds of the disk going, or a replace the kernel continued
  at a mount is only watched (`ResumeReplace`), the kernel finishes first,
  LinuxIO's check refuses the result and its error says to restart the
  server, until which the kernel may crash or corrupt data.
- A pool unmounted by hand while its rebuild copies fails that rebuild, and
  when the pool is then mounted by systemd at boot, where the kernel takes
  the copy up and ends it, the record still says failed: LinuxIO did not
  watch that end, so it keeps the alert, starts no resync, and leaves the
  disk for a person to replace, or for the hot spare.
- A rebuild cut by a power loss or a stop starts again from the beginning,
  since LinuxIO erases its target before it mounts the pool rather than let
  the kernel continue a replace it cannot check.
- A returned disk that was mounted alone elsewhere and written stays at a
  generation below the pool's and passes the checks: nothing on the disk
  tells it from the member as it dropped out, and its writes are lost, as
  they are when the pool takes the disk back at a mount.
- A disk put to a raw use after the erase that leaves the erased superblock
  at 64 KiB and writes no signature udev reports is taken by a rebuild
  started again onto it.
- A member's error counters from before the episode, which a repair at the
  time left raised, hold the rebuild onto its returned disk back until they
  are reset with `btrfs device stats --reset <mountpoint>`, as errors
  counted during the episode do; LinuxIO has no action for it.
- A replace target newer than every other disk present is not erased: the
  pool is judged as without a record, so the boot check refuses it as a
  pool whose disks missed writes, and a degraded mount from the overview
  lets the kernel continue that replace.
- A disk an unchecked finish left in the member's slot, or the untrusted
  disk a spare replaces, is not erased while another member is missing or
  older: the pool is mounted with it, its stale copies included, until a
  person replaces it.
- While an unverified rebuild is on record, a device of the pool carrying the
  replace target's devid or the member's that udev has not reported keeps
  LinuxIO's mount of the pool from going ahead.
- The record tells a rebuild watched to its end from one the kernel
  continued at a mount by its boot: a pool unmounted and mounted again by
  hand in the boot a rebuild started in, after its runner died, has the
  continued replace checked as if watched.
- A disk refused or failed is not tried again automatically until the
  episode ends, when the pool is whole again; a cause that went away
  meanwhile does not retry it. A person replaces the member from the pool
  details, onto that disk too.
- The pool listing tells a dropped disk by an exclusive open, which fails at
  once on a disk the pool holds. When a pool is being unmounted or a disk
  removed at that instant the open can succeed and claim the disk for a
  moment, and a Destroy or wipe running then is refused as busy; a retry
  finishes it.
- On a kernel that marks the member missing itself (7.1), a returned disk
  the kernel has not listed again is refused as a disk with a filesystem,
  which **Overwrite existing data** erases, not as the pool's disk. Not run
  on such a kernel.
- A dropout is seen in the block device the pool holds: a disk under a
  device-mapper or md device (dm-crypt, for one) that drops out leaves that
  device, whose I/O fails, and counts as a disk whose writes fail. A new
  disk under a dropped disk's name that something else holds exclusively
  reads as the pool's; the kernel keeps that name while the pool holds the
  dead block device, as the test VM showed for SCSI. A healthy member that
  no device link matches (`/dev/root` on a root mounted without an
  initramfs) reads as dropped out when the target of a running replace
  goes away, since the kernel links the target and DEV_INFO does not list
  it: a wrong alert and an extra scrub, nothing erased.
- The kernel keeps only the last replace: a replace that finished during the
  episode followed by one that was cancelled hides the first. An episode
  start taken from a clock running ahead at boot can be later than a
  replace's stop time. A suspended replace that cannot resume keeps every
  reset from happening, the safe side.
- The commit reads the label and sets it again: a rename by hand (`btrfs
  filesystem label`) in the moment between the two is put back; a rename
  through LinuxIO waits for the lock.
- A scrub started from the pool details between two resync attempts replaces
  the resync's saved progress, and the resync then starts over; once the
  resync's scrub had finished, it also costs one failed attempt, since the
  final check finds a record that is not the resync's.
- A continued resync reads the dev extent where each device stopped again
  (1 GiB at most) and counts its errors twice, and a power loss costs up to a
  minute of its progress.
- Kernels before 6.4 scrub with older code, and no test VM runs one: that a
  repair has completed before a continuation skips it rests there on a
  reading of 6.1's source (`scrub_enumerate_chunks` waits for every bio and
  worker of a dev extent before the next). No VM run cuts a resync while the
  saved offset is the end of a dev extent whose repairs are still queued,
  which is how mainline before 6.11 reports progress: that case rests on
  the source and the unit tests.
- No test rig shows that the flush covers a disk's lost write cache: the
  frozen copy of the root test keeps every completed write, and so does the
  test VM's power cut, since the host keeps what the virtual disk wrote; the
  order of flush and save is unit-tested only.
- systemd unmounts a pool with the disk its mount names (see
  [Pools](#pools)) on a systemd older than 255, whatever its line says, and
  on a newer one when its `/etc/fstab` line names it by `LABEL=`, a device
  path or a quoted `UUID="..."`, has the type `auto`, binds it on purpose or
  gives `x-systemd.device-bound` a value systemd cannot read; when its
  mountpoint in fstab is a symlink (the mount is then at another path), or
  so long that systemd names the unit by a hash; and for a folder of a
  subvolume bind-mounted on the line's mountpoint. Otherwise it does so only
  until the health run has seen the mount: two minutes after boot, and ten
  minutes at most after a mount, or after a replace or a removal changed
  which disk the kernel names as the mount's source. Each such change leaves
  one more line in `/run/mount/utab` until the next boot, and so does a
  mount LinuxIO unmounts.

## Top-level access

Subvolume and snapshot operations need the filesystem's top level
(`subvolid=5`). On first use LinuxIO mounts it at `/run/linuxio/btrfs/<uuid>`,
an ordinary host mount, skipped when a user `subvolid=5` mount exists, and
leaves it mounted until reboot, or until **Destroy pool** unmounts it.

A path from the top level is acted on as root, and a folder on the way may
be writable by another local user, who could make it a symbolic link: a
subvolume typed as `shared/inbox/2026` would be created wherever `inbox`
points, and a delete could hit a subvolume of the same name elsewhere. So
create, delete, snapshot, read-only, compression, size limit, the folders
made for snapshots, the renames and the free-name check of a rollback, and
the snapshots a replication sends all reach their path from
the top-level mount with `openat2` under `RESOLVE_BENEATH` and
`RESOLVE_NO_SYMLINKS`, then work through that one handle (`openBelow` in
`backend/common/btrfs/below.go`). A path that is or goes through a link is
refused (`ErrLinkOnPath`); there is no reason for one on a subvolume path,
and the real path works. Those flags do not stop at a mountpoint, and a
user's own top-level mount can have another filesystem mounted on a folder
below it, so the folder reached must also report the same fsid as the
top-level mount (`onFilesystemOf`, `ErrNotOnFilesystem` otherwise):
`RESOLVE_NO_XDEV` would refuse a subvolume of the pool mounted in place as
well. The routes that act on one subvolume or snapshot answer 403 for a
link and 400 for another filesystem; a rollback or a replication that meets
either fails as a task, since it may be part-way through. A size limit goes by the
subvolume's id on the top level, with no path at all, and compression is
set only on a subvolume root, so a plain folder put at the name meanwhile
takes neither. A rename never lands on a name that is taken
(`RENAME_NOREPLACE`). The way to the top-level mount itself is root's and
resolves as usual. `openat2` needs Linux 5.6.

## Subvolumes

The Subvolumes view lists each subvolume with:

- its name, default and read-only markers, and where its files are on the
  host: under the mount of the subvolume or of the nearest one above it
  (LinuxIO's own `/run/linuxio/btrfs` mounts and bind mounts do not count);
- its size, with a bar against its size limit when one is set (sizes need
  quotas, turned on in the pool settings);
- what uses it: SMB and NFS shares and Docker folders, each credited to the
  deepest subvolume holding its path, since a nested subvolume is snapshotted
  on its own;
- its snapshot count and automatic schedule.

Each row can take a snapshot, set a size limit and compression, toggle
read-only, become the default subvolume or be deleted. Nested subvolumes
appear as their own rows.

**Create subvolume** asks for a folder on the host and a name, and shows the
host path it will create. The folder starts at the pool's top-level mount, or
else at the mount of its shallowest mounted subvolume, and the folder picker
browses the whole host. The bridge opens the folder as the file browser does,
refusing a link another local user could have planted
([Privilege Pattern](privilege_pattern.md#root-acting-in-the-session-users-folders)),
checks through that handle that it is on this pool (`BTRFS_IOC_FS_INFO`),
finds its path from the top level (`BTRFS_IOC_INO_LOOKUP` and the
subvolume list), and creates the subvolume through the same handle, so the
path checks below apply to where it really lands. A place inside Docker's
layer store is refused, since the list leaves those out. The folder must
exist, and a name already taken there is a conflict. The kernel hands back
no handle to the new subvolume, so it is reopened by name to get mode 0755,
and the mode is set only on a subvolume root that the bridge's user owns and
that was born since the create began (`isNewSubvolume`): whoever can write
the folder could otherwise put another folder at that name first. The dialog
refuses a name with a space, a comma or a slash before sending, and a folder
whose path below the pool's mount has a space or a comma. When the picked
folder is, or lies below, a refused link, it puts the same folder reached
through what the link points to in the field, to check and create again.

The new subvolume is root's, mode 0755, as from the typed path and from
`btrfs subvolume create` in a root shell; it is not handed to the folder's
owner as the file browser hands over a new folder. Its path is checked as
the folder stood when it was opened: a folder on the way renamed in the
moment before the create gets the empty subvolume where it now is, and the
route then says it was created but could not be read back. Nothing is
undone when a step after the create fails, since deleting by name could
delete what was moved to that name meanwhile: the error says the subvolume
was created, a second try answers 409, and the table shows it at its next
refetch. A
place no lasting mount shows, such as an unmounted subvolume, is reached
instead by typing a path from the top level, the dialog's other mode, which
it opens in when the pool has no such mount. Subvolume paths may not
contain `,` or whitespace, which would break `subvol=` mount options and
`/etc/fstab`. Deleting a leftover
`.linuxio-rollback-*` subvolume (see [Rollback](#rollback)) is done here.

### For other features

Two functions serve features outside the storage page, which reach their
folder themselves through folders they checked:

- **`CreateSubvolumeAt(dir, name)`** creates the subvolume `name` in the open
  folder `dir` and sets it 0755 as **Create subvolume** does, through the
  handle and on what `isNewSubvolume` accepts, without looking the pool up
  or reading the subvolume back. `name` is one name, never a path. The
  memory page makes its swap file's folder with it
  ([Memory overflow](memory.md)).
- **`SwapfileLayout(dir)`** says whether the filesystem holding the open
  folder can hold a swap file: the kernel takes one only on a filesystem of
  one device whose data block groups are all single
  (`btrfs_swap_activate`). It reads the device count with
  `BTRFS_IOC_FS_INFO` and the data profiles with `BTRFS_IOC_SPACE_INFO`,
  through the handle. Neither ioctl checks a capability, so it answers a
  session without sudo, which the memory status needs.

## Snapshots

Layout under the top level:

```text
<top>/linuxio-snapshots/<source>/<YYYYMMDDTHHMMSSZ>-<kind>
```

`<source>` is the subvolume path with `/` escaped as `%2F` (and `%` as `%25`).
`<kind>` is `manual`, `timeline`, `pre-update` or `pre-rollback`. Snapshots
are read-only. Nested subvolumes are not included in a snapshot.

A snapshot holds the files as they were, permissions included, so whoever
could read a file then could read it there for as long as the snapshot is
kept. `linuxio-snapshots` and each `linuxio-snapshots/<source>` are
therefore root's alone: they are made `0700` whatever the umask of what
makes them (the timer's service runs with systemd's `022`, under which they
were `0755`), and one found more open is narrowed, every source's folder
at once, at the next snapshot of any source (`ensureSnapshotDir`) and at
each health pass of `linuxio-worker` (`btrfs.NarrowSnapshotDirs`), so the
folder of a source that takes no snapshot any more does not stay open. The
one exception is a shared folder that offers earlier versions, whose
`linuxio-snapshots/<source>` carries an ACL that lets read who may read the
folder ([Shared Folders](./shared-folders.md#network)); for it
`linuxio-snapshots` itself is `0711`, passed through and listed by nobody
but root. Narrowing leaves a folder with an ACL as it is, and takes
`linuxio-snapshots` down to `0711` and no further, so one that was `0755`
ends `0711` whether or not a shared folder uses it: the names of the
sources can be probed there, their snapshots cannot be reached. It touches
only what LinuxIO made: root's folders, on the subvolume of a
`linuxio-snapshots` that is root's own. A subvolume or a mount put there,
a folder of another owner, and everything in a `linuxio-snapshots` that is
a user's (the top level of a disk they mounted) are left alone. A source's
folder that cannot be narrowed does not fail the snapshot of another
source; the health pass names it in its output.

The snapshot list, on the pool page's Snapshots tab and in the Subvolumes
tab's Snapshots view, groups them under the local day they were taken (Today,
Yesterday, then the date), newest first. Chips narrow it to one kind present:
*Scheduled* (`timeline`), *Before updates*, *By hand* (`manual`, never pruned)
and *Before rollbacks*. A row gives the time, with its age today and
yesterday, and names its subvolume only when the pool has snapshots of more
than one; selecting it opens a panel with its date, its subvolume, why it is
kept, its size when quotas give one, and **Browse files** (the file browser
at `/run/linuxio/btrfs/<uuid>/linuxio-snapshots/...`), **Roll back…** and
**Delete…**. **Restore files** is a copy in the file browser; there is no
separate restore operation.

**Why it is kept** (`KeptBy` in `retention.go`, set by the list route with
`ExplainKept`) mirrors the snapshot pass: taken by hand and never pruned; no
rule covers its subvolume; its rule is off, which leaves its scheduled
snapshots (a rule off with **Snapshot before updates** still prunes those);
one of the newest three before updates or rollbacks; the newest of an hour,
day, week or month the rule keeps, the smallest named; the parent the next
incremental replication needs; or removed at the next pass.
`TestKeptByAgreesWithPrune` holds it to `Prune`, which it does not change.
When the rules or replication jobs cannot be read the reason is left out.

## Rollback

Rollback replaces a source subvolume with a snapshot of it, as a background
task:

1. Take a read-only `pre-rollback` snapshot of the source.
2. Create a read-write copy of the chosen snapshot as the sibling
   `<source>.linuxio-restore-<ts>`.
3. Rename the source to `<source>.linuxio-rollback-<ts>`.
4. Rename the restore copy to the source path. On failure the old subvolume is
   renamed back.
5. Set the default subvolume to the new one when the source was the default.
6. Delete the old subvolume when the source is not mounted. Otherwise keep it
   (reported as "leftover") and remount each mount of the source explicitly
   with its original options (`mount -t btrfs -o <opts>,subvol=/<source>
   UUID=...`), then verify each mount. Bind mounts of a path inside the source
   are not remounted; the result lists them and the rollback takes effect there
   after reboot.

The rollback refuses to start when `<source>.linuxio-restore-<ts>` or
`<source>.linuxio-rollback-<ts>` already exists.

Sources `/`, `/usr`, `/var` and `/etc` cannot be remounted live; the task
finishes and the UI asks for a **reboot** instead.

Caveats:

- The renamed old subvolume keeps its ID, so a mount pinned by `subvolid=`
  would follow it; when `subvolid` and `subvol` disagree the mount fails at
  boot. The rollback therefore refuses a source whose `/etc/fstab` entry, or
  for `/` the kernel `rootflags=`, contains `subvolid=`. Replace it with
  `subvol=/<source>` first.
- A leftover `.linuxio-rollback-*` subvolume is never cleaned up
  automatically. Delete it from the Subvolumes card when done.

## Automatic snapshots

Rules live in `/etc/linuxio/btrfs-snapshots.json`. A rule names a filesystem
and source subvolume, a period for timeline snapshots, how many to keep per
hour, day, ISO week and month bucket, and **Snapshot before updates**. An
enabled rule must keep at least one snapshot; 0 keeps none of that bucket.

One hourly timer drives all rules:
`linuxio-btrfs-snapshot.timer` (`OnCalendar=hourly`, `Persistent=true`)
starts `linuxio-btrfs-snapshot.service`, which runs
`linuxio-worker snapshot`. The service is a package unit; the bridge writes
the timer to `/etc/systemd/system` when a rule or job is saved, and enables
it while at least one rule or replication job is enabled and disables it
otherwise. **Run now** runs the
snapshot and prune pass from the bridge; it does not replicate (each
replication job has its own **Run now**). A timer pass in which a rule
failed still completes, since each failure raised its own alert, and its
summary in the history counts it (*2 taken, 1 failed*); it exits with 1 for
a failure without an alert (rules or jobs that cannot be read, a replication
that could not be started), and only that gets the run alert of the unit's
end hook ([Alerts](./alerts.md#who-writes-with-no-session)).

- A rule is due when its newest `timeline` snapshot is older than its period
  (5-minute tolerance for the hourly tick).
- Retention keeps the newest N per hour/day/ISO-week/month bucket. `manual`
  snapshots are never pruned. `pre-update` and `pre-rollback` snapshots keep
  the newest 3.
- A failed snapshot or an unmounted filesystem raises an alert
  `btrfs/snapshot-<uuid8>-<path>`; a later good run resolves it.
- When a replication job is enabled, the pass ends by starting
  `linuxio-btrfs-replicate.service` without waiting for it
  ([Replication](#replication)), so a send that takes hours never holds up
  the next hour's snapshots.

## Pre-update snapshots

Rules with **Snapshot before updates** are snapshotted, whether or not the
rule is enabled; a disabled rule prunes only its `pre-update` snapshots and
never its timeline:

- by LinuxIO's own package update task and offline apply, before they start;
- by `ExecStartPre=-/usr/libexec/linuxio/linuxio-worker snapshot pre-update` in the
  `linuxio-alert.conf` drop-in LinuxIO writes for the distribution's
  auto-update unit when its notification policy is not `never` (see
  [Alerts](./alerts.md#automatic-updates-notification-policy)). The `-` means
  a snapshot failure never blocks the upgrade. Existing installs get the line
  the next time the auto-update policy is saved.

Manual `apt` or `dnf` runs are not hooked.

## Pools

A pool is one btrfs filesystem over one or more whole disks. **Create pool**
on the Overview or the Disks page formats free disks with `mkfs.btrfs`, with the data
and metadata profiles chosen from `single`, `dup`, `raid0`, `raid1`,
`raid1c3`, `raid1c4` and `raid10` (`raid5` and `raid6` are refused),
registers the disks with the kernel, mounts the pool and records it in
`/etc/fstab` as
`UUID=<uuid> <mountpoint> btrfs defaults,noatime,nofail,x-systemd.device-bound=false 0 0`,
without the last option on a systemd older than 255.
systemd stops a mount that comes from `/etc/fstab` when the device the mount
names as its source goes away (`StopPropagatedFrom=` on that device's unit,
`mount_add_device_dependencies` in systemd's `mount.c`, since 249), and a
pool names one of its disks: pulling that disk of a raid1 pool unmounted the
whole pool in the test VM (systemd 259), where pulling the other did not, and
what was writing to the pool went on writing to the folder under the
mountpoint. `x-systemd.device-bound=false` tells systemd to leave the mount,
so that btrfs carries on with the disks it has and
[Missing disks](#missing-disks) applies. The boolean is read since systemd
255. An older one takes the option's mere presence as bound and ties the
mount to its device with `BindsTo=`, harder than without the option, so
there LinuxIO neither writes the option nor does any of what follows
(`btrfs.UnboundMountsHonoured`, from the manager's `Version`; a manager that
does not say counts as older).

The line alone does not do it. Once a filesystem is mounted, systemd reads
its options from the mount table as libmount gives it
(`get_mount_parameters`), where an `x-` option exists only in libmount's own
record, `/run/mount/utab`. mount(8) writes that record under the device
`/dev/disk/by-uuid/<uuid>` named when it mounted, the kernel names any member
as the mount's source, and libmount joins a record to a mount only when
source, mountpoint and root are the same. In the test VM (util-linux 2.41)
the record said `/dev/vdc` and the mount `/dev/vdb` on six boots out of
eight; the mount then kept `StopPropagatedFrom=dev-vdb.device` with the
option on its line, and pulling `vdb` unmounted the pool. A mount LinuxIO
makes with `mount(2)` has no record at all.

So the health run makes the option count (`unbindPoolMounts` in
`fstab_bound.go`), for every mounted pool. The line of `/` is never touched
(systemd ties no mount of the root to a device), and the filesystem `/` is
on is left alone altogether when it is on one disk, where its other mounts
go with the system; on more than one, they are kept like any pool's. A data
pool on one disk gets the option too: when that disk goes, the pool stays
mounted and what writes to it gets I/O errors, where systemd would unmount it
and the writes would land in the folder under the mountpoint, on the system
disk. A pool can also gain a second disk later, and its line is then right
already.

1. Every btrfs line of `/etc/fstab` that names the pool by `UUID=` and says
   nothing about `x-systemd.device-bound` has
   `,x-systemd.device-bound=false` appended to its options, under the lock
   every LinuxIO edit of the file takes (`/run/linuxio-fstab.lock`, in a
   folder root alone writes) and with the rest of the file left byte for
   byte; systemd is then reloaded and one Info line names the mountpoints.
   Lines are read as mount(8) reads them, fields apart by blanks and tabs. A
   line that carries the option already is left as written: one that says
   true, or the bare option, is an administrator's to keep, here and in
   step 2. When the lock cannot be taken or the file cannot be replaced, the
   lines stay as they are, a warning is logged, and step 2 still runs.
2. For each mount of the pool whose line says false (`false`, `no`, `0`,
   `off`, as systemd's `parse_boolean` reads them), it asks systemd whether
   the mount still stops with a device (the mount unit's
   `StopPropagatedFrom`, over D-Bus). If it does, and libmount's record does
   not hold the mount under its source and subvolume with the option yet,
   it has mount(8) write that record, with the options mount(8) itself
   records for a mount it makes: the line's `x-` options and `_netdev`,
   without the `x-mount.` ones, which are mount(8)'s own to act on:
   `mount --fake --internal-only --no-canonicalize -t btrfs -o subvolid=<id>,<options> -- <source> <mountpoint>`.
   `--fake` makes no `mount(2)` call and writes only the record;
   `--no-canonicalize` keeps the source as the kernel names it, where
   mount(8) would write `/dev/mapper/<name>` for `/dev/dm-0`; and the
   subvolume gives the record its root (without it the record of a subvolume
   mount says `ROOT=/` and joins nothing). libmount gives the mount its
   newest record whole, so one that joined at boot without the option is
   replaced by this one, `_netdev` and all. systemd reads the record at
   once: in the test VM the dependency was gone within three seconds, with
   no remount, and pulling the source disk then left the pool and a
   subvolume mount of it mounted. One Info line says what was recorded. A
   mount is recorded once: where systemd keeps the dependency, the same
   record again would change nothing. That libmount joins the record, for
   the top level and a subvolume, is checked by the root test
   `TestBtrfsMountRecordIntegration`; that systemd then drops the dependency
   is seen in the test VM only.
3. A mount that stops with a device all the same, whose record could not be
   written, or that names no device path to record it under, raises
   `btrfs/bound-<uuid>`, which goes when no mount of the pool stops with a
   device, or with the mount.

Which disks it may take is decided as for every device operation (see
[Which disks a pool may take](#which-disks-a-pool-may-take)). The mount point
must be an unused, empty path
outside the system directories. The dialog estimates the usable data space
for the chosen profile and disks, allocated the way btrfs does, so mixed disk
sizes are counted right; metadata overhead is not included. Metadata (the
filesystem tree and checksums) defaults to `dup` on one disk, `raid1` on two
and `raid1c3` from three, where `mkfs.btrfs` would pick `raid1`. A metadata
profile less redundant than the data profile is never offered, ranked
`single` = `raid0` < `dup` < `raid1` = `raid10` < `raid1c3` < `raid1c4`:
weaker metadata lets a disk failure the data survives take the whole pool.

Disks are used whole unless **Partition the disks** is on. Then each disk
gets a GPT, written natively, with one Linux partition from 1 MiB that stops
1 GiB before the end, and the pool is built from the partitions. That slack
lets a replacement disk of the same nominal size that is a few sectors
smaller still take a member's place. The table is written by the same code as
[partition management](./storage-disks.md#partition-management) on the Disks
page, where these partitions can be deleted once the pool is gone, or the
disk initialized to be used whole again.

A create that fails or is canceled before the pool is recorded in
`/etc/fstab` is rolled back while the disks are still locked. The pool is
unmounted if the create mounted it. Every superblock copy (64 KiB, 64 MiB and
256 GiB into each member) that carries the new pool's UUID has its magic
zeroed, including the temporary magic a failed `mkfs.btrfs` leaves; copies of
any other filesystem are left alone. Then the partitions the create added are
deleted, newest first. A partition whose superblocks could not be wiped is
kept, and nothing is wiped while the pool cannot be unmounted; the error
reports what was left. A trim cannot be undone: when a member is refused
right before its trim or before `mkfs.btrfs` (see
[Safety](./storage-disks.md#safety)), the members trimmed before it stay
discarded, and the create fails naming the refused device. Once the pool is in fstab it is kept, whatever fails
after. The rollback removes only what the create made: what was on a disk
before, such as a filesystem overwritten with **Force** or the old contents
of a disk given a new GPT, is not restored.

A shared folder is a subvolume under the pool's top-level mount with a size
limit (which turns quotas on), the default automatic snapshot rule and an
access table, made in one operation from **Storage → Shared folders → Add
Share → Create shared folder** ([Shared Folders](./shared-folders.md)). Only
pools with a permanent top-level mount are offered.

`nofail` keeps a pool with a missing disk from blocking boot. Such a pool is
not mounted, since btrfs refuses that without `degraded`, so the overview
lists it from the disks' superblocks as an unmounted pool with its missing
disk count. The backend computes that count (`missing`) by devid and leaves
out an interrupted device replace's target, which the kernel scans as devid
0 but is no member, and every device that is part of another one, such as a
leg of an md array (see [Missing disks](#missing-disks)). **Mount** mounts a
complete pool and **Mount degraded** one with missing disks, so its data
stays reachable until the disk is replaced; both use the pool's fstab
options, so `subvol=` and `compress=` apply, and both refuse, with a 409, a
pool whose devices LinuxIO cannot tell are its disks. A pool
without an fstab entry is mounted at `/mnt/<label>` and recorded. Pools in
fstab are also mounted degraded at boot; see [Missing disks](#missing-disks).

**Destroy pool** unmounts every mount of the pool, removes its fstab entries
and zeroes the btrfs superblock magic on each disk; everything on the pool is
lost. Each disk is opened exclusively after the unmount and before fstab
changes: a disk something still claims, such as the pool still mounted in
another mount namespace, which the unmount here does not reach, refuses the
destroy with a 409 that says its mounts here are unmounted and nothing was
erased. The wipe opens each disk
exclusively again and refuses one claimed since, but by then fstab is edited
and the disks before it are wiped. A pool holding `/`, `/boot` or another protected mount is refused
(a 409);
LinuxIO's own top-level mount under `/run/linuxio/btrfs/<uuid>` (see
[Top-level access](#top-level-access)), left by any subvolume, snapshot or
settings read, is not one: it is unmounted with the rest. An
unmounted pool is destroyed the same way from its superblocks, with the disks
that are present when some are missing, which is how a disk of an old pool is
freed for a new one. A destroy that wiped every disk it found also drops the
pool's hot spare and its record in `btrfs-pools.json` (`ForgetPool`).

### Which disks a pool may take

Create pool, Add device and Replace device refuse a device that is:

1. mounted, an active swap, or held (LVM, md, dm-crypt, multipath);
2. a member of a btrfs pool, mounted or not, by the kernel's list of mounted
   pools and the superblocks of unmounted ones; a device that shows a pool's
   superblock as part of another one, such as a leg of an md array, is no
   member (see [Missing disks](#missing-disks)) and is refused by rule 1
   while held and by rule 5 as the member it is otherwise
   (`TestALegLeftOutOfAPoolIsStillRefused` runs every write on one);
3. named in `/etc/fstab`;
4. partitioned, or the parent of another device;
5. a member of a btrfs pool, an md array, an LVM volume group or any other
   set by its own signature (`btrfs`, `linux_raid_member`, `LVM2_member`, and
   every other signature that ends in `_member`, such as `zfs_member` or a
   firmware RAID's `isw_raid_member`), whether the pool is mounted, the array
   running or the volume group active or not. The md and LVM2 signatures are
   also read from the device itself just before the write, so one udev has
   not probed yet is refused too (see
   [Safety](./storage-disks.md#safety));
6. a pool's [hot spare](#hot-spare), except to Replace device in that pool.

Rules 1 to 3 and 6 are the check every device operation shares with the
[Disks page](./storage-disks.md#safety) and md RAID. While a disk of a
mounted pool is dropped out, rule 2 also refuses a disk whose own superblock
names that pool and whatever is at the dropped disk's old path: either may
be that disk, which the pool takes back when mounted again, or the automatic
rebuild erases and replaces the member onto (see
[Missing disks](#missing-disks)); no request takes it. Any other filesystem
signature needs **Force**, which erases it: the bridge zeroes the device's
first and last 2 MiB before the add, and `mkfs.btrfs -f` does the same for a
create. Replace has no **Force**: its target must be blank.

The checks read an inventory, so the bridge opens the device with `O_EXCL`
to zero it; the kernel refuses that (`EBUSY`, a 409) while the device is
mounted, an active swap, held by LVM, md or dm-crypt, or, for a whole disk,
while one of its partitions is. The claim ends when the file is closed,
before `BTRFS_IOC_ADD_DEV`, which claims the device exclusively itself
(`btrfs_init_new_device` in `fs/btrfs/volumes.c` opens it with
`BLK_OPEN_WRITE` and the filesystem as holder; `bd_may_claim` in
`block/bdev.c` grants a claim only while the device has no other holder, and
`bdev_release` gives up a file's claim when it is closed). The other writers
open the same way: the trim before a format or a pool create
(`discardDevice`), the signature read before a format, a delete, a pool
create, a device add or replace and an md add or replace
(`ReadMemberSignature`), the busy check before a mount (`CheckNotBusy`),
the edges of a new partition, and the wipes of **Destroy pool**
(`WipeDevice`) and of the undo of a failed create (`WipePoolSuperblocks`).
A btrfs filesystem that is mounted holds its devices, so those wipes refuse
it, while one that is only registered does not (`btrfs_scan_one_device`
opens the device without a holder). A whole disk that gets a new GPT, on
**Initialize disk** or for a pool's partition, is zeroed at its edges and
written through one exclusive open, so nothing can claim it between the
two. `mkfs` repeats its own exclusive open. The target of a replace is
written only by the kernel, which claims it exclusively the same way
(`btrfs_init_dev_replace_tgtdev` in `fs/btrfs/dev-replace.c`).

**Force** never covers rule 5. The one erase of a btrfs pool member without
a person is the automatic rebuild's of a member's own returned disk, after
its identity and stability checks ([Missing disks](#missing-disks)). LinuxIO does not try to tell a disk with
leftover btrfs data from a member of an unmounted pool: both are freed with
**Destroy** on the unmounted pool the overview lists. A member of a stopped
md array or an inactive volume group is freed with `mdadm` or `vgreduce` and
`pvremove`, or erased with **Initialize disk**, or **Format** or **Delete**
on a partition, on the Disks page, whose confirmations name it. The 409 says what the disk belongs to and how to free
it, such as "Member of btrfs pool old; destroy the pool, or remove the device
from it, first". The dialogs list such a disk disabled with that reason,
whether **Force** is on or not; the reason comes from the `member` field of
the block-device inventory (see
[Inventory](./storage-disks.md#inventory)). A btrfs signature on a disk
whose superblock the bridge cannot read lists no pool to destroy; such a disk
is freed with **Initialize disk**, or **Format** on a partition, on the Disks
page.

This is stricter than upstream on purpose:

- `mkfs.btrfs -f` and `btrfs device add -f` skip only the existing-signature
  probe (`check_overwrite` in `test_status_for_mkfs`, `mkfs/common.c`;
  `cmd_device_add` passes `-f` to `test_dev_for_mkfs`, `cmds/device.c`). The
  mount check that remains (`check_mounted_where`, `common/open-utils.c`)
  scans the other members of the device's filesystem and reports it mounted
  only when one of them is, so a member of an unmounted pool is overwritten.
  The exclusive open refuses only a device something holds, which a stopped
  md array or an inactive volume group does not.
- `mdadm --create` warns about an md superblock or a filesystem it finds
  (`check_raid`, `check_ext2` in `Create.c`) and asks to continue, which
  `--run` skips. `mdadm --add` writes its superblock over whatever the device
  carries; only a device from the same array is considered for a re-add
  (`Manage.c`).
- `pvcreate` refuses a physical volume of a volume group without `-ff`, and
  then asks unless `-y` is given (`_pvcreate_check_used`,
  `tools/toollib.c`); it skips md components when it scans devices. Any other
  signature it wipes after asking, or with `-f` without asking (`_blkid_wipe`,
  `lib/device/dev-type.c`).

Apart from `mkfs.btrfs`, pools use no command: disks are registered with the
`BTRFS_IOC_SCAN_DEV` ioctl, mounted with `mount(2)`, partitioned by writing
the GPT and asking the kernel to re-read it, and discovered by reading the
primary superblock at 64 KiB.

## Devices and profiles

| Action | Behaviour |
|---|---|
| Add device | Free devices only (see [Which disks a pool may take](#which-disks-a-pool-may-take)). **Force** erases an old filesystem, never a disk of another pool, md array or volume group. |
| Remove device | Refused below the minimum device count of the current profiles, and refused when the current profiles cannot be determined or have no minimum LinuxIO knows (`raid5`, `raid6`). |
| Replace device | Replaces one member with a free, blank device, or with the pool's own [hot spare](#hot-spare). A missing member, one that dropped out included, is copied from the other mirrors (`cont_reading_from_srcdev_mode` AVOID), and so is one whose disk an automatic rebuild left unverified (a rebuild of that device on record), which may hold stale copies no checksum finds; a present one is read first, as `btrfs replace start` does. The result is checked as the automatic rebuild's is: finished, the new disk in use and held by the pool, the pool writable ([Missing disks](#missing-disks)); a replace the kernel reports done that fails this check fails the job. A bigger replacement disk is grown into automatically. |
| Hot spare | Sets, changes the delay of, or removes the pool's hot spare (see [Hot spare](#hot-spare)). |
| Grow | Resizes a device to its whole disk when the filesystem uses less of it, for example after a replace done by hand. |
| Rename | Sets the filesystem label from the pool page's menu. |
| Balance | **Balance** on the pool's Disks card asks what to balance. *Lightly used areas* (the default) balances data chunks at most 10, 25, 50 (the default) or 75% full, which turns slack into unallocated space. *Everything* balances every chunk with no filter, which spreads data and metadata over all disks, as after adding one. *Change profile* converts: see the next row. On a pool missing a disk only *Change profile* is offered, and `Balance` refuses any balance that is not a convert: the kernel writes every chunk it moves as `single` on the disks present (`btrfs_reduce_alloc_profile`), which the resync would then have to convert back. |
| Change profile | Profiles `single`, `dup`, `raid0`, `raid1`, `raid1c3`, `raid1c4`, `raid10`. `raid5` and `raid6` are refused. Converting metadata converts system chunks with it. Metadata weaker than the resulting data profile is converted too, to the same default as Create pool, unless nothing stronger fits on the present devices. The outcome is verified against the resulting allocation. |
| Reclaim space | The health banner's action when metadata is nearly full: a balance of data chunks at most 50% full. |
| Scrub | Start and cancel. The last result is recorded in `/var/lib/linuxio/btrfs-scrub.json`, with a running scrub's progress (see [Scrub progress](#scrub-progress)); scrubs run by btrfsmaintenance are read from `/var/lib/btrfs/scrub.status.<uuid>`. A scrub is refused while the kernel already scrubs the pool, for another scrub or a device replace, while LinuxIO's own scrub of it still runs, and when a device could not be read. |

Remove, replace, balance and scrub are blocking kernel calls run inside the
job, so progress comes from the kernel. A removal has no kernel progress: its
percentage is the share of the device's allocation already moved off it, and
a resumed removal starts again at 0 for what is left. A scrub started
elsewhere is still shown as running. A balance the Disks card is not
following, one started from the command line, by btrfsmaintenance's timer or
by Reclaim space, is read from the kernel (`BALANCE_PROGRESS`, the pool's
`balance` field) and shown on the card with its percentage and a **Cancel**,
or as paused, which holds the pool until it ends; the card offers no other
balance, addition, removal or replace meanwhile, and names `btrfs balance
resume` and `btrfs balance cancel` for a paused one, which a `skip_balance`
mount leaves paused. A bridge that is
not root cannot read it (the ioctl needs `CAP_SYS_ADMIN`) and shows none. The ioctls run on a locked thread with async signals
blocked: since kernel 6.19 any signal aborts a scrub or a replace (and a replace
aborted that way loses its target). A freeze still interrupts them: a scrub
continues by itself (see [Scrub progress](#scrub-progress)), a replace fails.
A refusal the kernel answers with a positive code, such as 8
("exclusive operation in progress"), fails the job.

### Hot spare

A pool can have one **hot spare**: an empty whole disk the administrator
names for it on the pool's Disks card (**Set hot spare**). When a member of
the pool has been missing for the spare's delay, LinuxIO replaces it onto
the spare, the way an md array uses its spare. A disk is the spare of one
pool only; there are no global spares. The designation is LinuxIO's own
state, `/var/lib/linuxio/btrfs-spares.json` by pool UUID (`spare.go` in
`backend/common/btrfs`): nothing is written to the disk until a replace uses
it, so a spare carries no signature another system or tool could misread.

**What may be a spare.** Only a pool whose every profile keeps a second copy
(`raid1`, `raid1c3`, `raid1c4`, `raid10`) is offered one: a `single`,
`raid0` or `dup` pool cannot run with a member missing, so there is nothing
for a spare to stand in for, and the card says so; a pool of one disk shows
no hot spare row at all. The disk must be:

1. a whole disk, never a partition, device-mapper, md, loop or RAM device,
   that rules 1 to 6 of [Which disks a pool may take](#which-disks-a-pool-may-take)
   accept without **Force**, as a replace target must be: unused, with no
   partitions or holders, and no filesystem or member signature, by udev and
   by an exclusive read of the disk's own md, LVM2 and primary btrfs
   signatures. A partition table with no partitions, as **Initialize disk**
   leaves, counts as empty;
2. recognisable after a reboot: udev reports a WWN or a serial
   (`ID_WWN_WITH_EXTENSION` or `ID_WWN`, and `ID_SERIAL`) that, with its
   size, names no other connected disk. The designation records these and
   the size, never the kernel name, which another disk can take after a
   reboot;
3. no smaller than any member as the filesystem knows it (each device's
   `total_bytes`, a missing member's included): the kernel refuses a replace
   onto a smaller target (`btrfs_init_dev_replace_tgtdev` in
   `fs/btrfs/dev-replace.c`). A larger spare is grown into after the
   replace, as after a manual one.

A refusal is a 400 when the disk or the request cannot serve (not a whole
disk, no identity, an identity another disk shares, too small, a pool that
keeps one copy, a delay outside 0 to 10080 minutes), and a 409 when the disk
is in use, not empty, or the spare of another pool that is there.

**Reservation.** While a disk is a pool's spare, the block-device inventory
marks it, and any partition of it, with `hotSpareOf` (the pool's UUID and
label), and `deviceInUse` refuses it with a 409 naming the pool, such as
"Hot spare of pool tank; remove the hot spare in the pool's details first".
So every operation that writes a device refuses it: Create pool (whole disks
and free space), Add device, Replace device in another pool, Initialize
disk, Create partition, Format, Delete, Mount, md add and replace, and
setting it as another pool's spare. The disk pickers list it disabled as
"Hot spare of tank". The one exception is Replace device in the spare's own
pool: the administrator may pick the spare (never a partition of it) as the
new disk. It stays the spare, and reserved, while the replace runs, and the
designation is dropped once the replace has installed it, on the first run
or on a resume after a reboot, which finds the new disk by its slot; a
replace that fails or is canceled leaves it the spare, which the kernel has
then blanked again. The reservation holds while the pool is there: mounted,
named by the superblock of a connected disk, or reported by udev on a
member. A pool destroyed outside LinuxIO reserves nothing, and naming the
disk as another pool's spare drops that stale designation, with an Info
line in the log. **Destroy pool** drops its pool's. When a designation's
identity names several connected disks, the spare is `absent` and every one
of them is reserved.

**State.** Each pool of `storage.btrfs_list` carries its spare (`spare`) as
it is when read: the disk's node now, its identity and size, the delay, and
a state, with the reason when it is not ready. A read of the pool, by the
page, a write's checks or the health run, looks at sysfs and udev's database
only and never opens the spare, so it neither wakes a disk that spun down
nor claims it under a replace. The disk's own md, LVM2 and btrfs signatures
and any claim on it are read through an exclusive open when the spare is
set and right before it is used (`btrfs.SpareOf`).

| State | When |
|---|---|
| `ready` | its identity names exactly one connected disk of its size, no smaller than any member, unclaimed and empty |
| `absent` | its identity names no connected disk of its size, or more than one; no other disk stands in |
| `too-small` | a member is larger than the spare, as after a replace onto a bigger disk grew it |
| `in-use` | something other than LinuxIO partitioned it, wrote a signature udev reports, or holds it; at use time also a claim or a signature only the disk shows |
| `unknown` | it could not be checked: with the pool's devices not all read, or, at use time, without the privilege to open the disk |

`absent`, `too-small` and `in-use` raise the `btrfs/spare-<uuid>` warning
(see [Filesystem health](#filesystem-health)); `unknown` leaves it as it
was.

**Delay.** "Use the spare after a disk has been missing for N minutes", per
pool: 10 by default, from 0, which is the next health run (the health timer
runs every ten minutes), to a week. It is set with the spare and changed
with **Change delay**.

**Routes.** `storage.btrfs_spare_set` (`uuid`, `device`, `delayMinutes`),
`storage.btrfs_spare_delay` (`uuid`, `delayMinutes`) and
`storage.btrfs_spare_clear` (`uuid`), each a privileged Mutation with no
result; the page refreshes the pool list and the inventory. Beside them,
`storage.btrfs_auto_rebuild` (`uuid`, `enabled`) switches the rebuild onto a
disk that comes back ([Missing disks](#missing-disks)) for a mounted pool, a
404 for one that is not mounted; each pool of `storage.btrfs_list` carries it as
`autoRebuild` and its rebuild as `rebuild` (`state` `waiting`, `running`,
`failed` or `refused`, with the devid, the disk and the reason).

**Automatic replace.** The health run (`checkRebuilds`) starts
`linuxio-btrfs-rebuild@<uuid>.service` for a mounted pool with a missing
member once the member has been missing for the spare's delay, counted from
when the episode recorded it missing (`at` in `btrfs-pools.json`), and the
spare reads `ready`, is no smaller than the member, and was not refused
earlier in the episode. A disk of the member that came back wins: while it
waits for its second health run, the spare is not used, and it is used
when that disk is refused. The runner (`Rebuild`) reads the spare again
right before it writes, through `SpareOf`, whose exclusive open finds a
claim or a signature udev did not report, holds its disk lock, whose disk
must be the one the spare names, records the rebuild, and replaces the member onto it reading the other mirrors, as for
a disk that came back but with nothing to erase. The designation is
dropped (`ClearSpare`) only once the new member passes the check after a
replace ([Missing disks](#missing-disks)); a replace that fails leaves the
disk the spare, which the kernel has blanked again, and the spare is not
tried again automatically in that episode; a person may still pick it for a
replace from the pool details. The spare also replaces at once a member
whose disk a failed rebuild left untrusted, while that disk is still the one
in the slot; its record keeps that disk, which LinuxIO's mount erases too
until the spare is verified. The same rules as for a disk that came back
apply: a pool that is read-only, keeps one copy, holds the only copy of
chunks on the member, or runs another operation waits or refuses, and the
`btrfs/rebuild-<uuid>` alerts say so. The error counters of the members that
stayed hold the spare back only while the member's own disk is connected,
whose old copy may be the better one; with that disk away they do not,
since nothing is erased. A rebuild onto the spare started again after a cut
fails once the person has removed the spare. The Disks card says in one sentence what LinuxIO does by itself
when a disk goes missing, with the switch of the rebuild onto a disk that
comes back.

#### Known limits

- Nothing is written to the spare, so the reservation is LinuxIO's alone:
  `mkfs`, `mdadm` or `pvcreate` run by hand can still take the disk, which
  then reads `in-use` and raises the warning.
- A disk udev reports no WWN or serial for, as many virtual disks and every
  loop device, cannot be a spare; nor can a multipath disk, whose paths
  share one identity.
- The exclusive read that finds the spare empty is closed before the
  kernel's own claim, as for any replace target (see
  [Safety](./storage-disks.md#safety)).
- Between uses the state comes from udev: a signature udev has not reported,
  or a claim without a holder, shows only when the spare is set or about to
  be used, which then refuses it.
- A designation is kept only while its pool is there, so the spare of a
  pool whose every disk is disconnected, such as an enclosure switched off,
  may be given to another pool, which drops the first pool's designation.
- An unmounted pool keeps its spare reserved, but its spare is not shown or
  checked, and its warning stays as it was, until the pool is mounted.
- The delay counts from the health run that recorded the member missing,
  and the spare is used at the first health run after it: up to ten minutes
  later than the delay with the timer.
- A spare left with the pool's replace-target superblock, by a replace
  canceled while the spare was missing, fails a rebuild started again onto
  it: the spare is taken only empty, as LinuxIO's erase and the kernel's
  cancel leave it.
- A rebuild started again onto the spare checks it as `SpareOf` does: a
  filesystem udev reports fails the rebuild, but a superblock udev has not
  reported, other than an md, LVM2 or btrfs one, is not seen, and the disk
  is taken (`TestRebuildSpareRestartOntoAForeignDisk`).
- A state file that cannot be read fails every operation that writes a
  device, Replace device included, with an error naming the file, until it
  is repaired or removed; the Disks page then lists the disks without the
  reservation.

### Cancel

Cancel asks the kernel to stop, and the job ends `canceled` when its handler
has returned. A cancelled job is never resumed. The pool's Disks card offers
**Cancel** while a balance, a removal or a replace of that pool runs, and
re-attaches each after a reload; the tray cancels any of them too. The card
cancels through the job (`tasks.cancel`), so the record gets
`cancel_requested` and a shutdown right after does not resume it; only a
balance with no running job, such as one started elsewhere, falls back to
`storage.btrfs_balance_cancel`. A removal or a replace is canceled only
after a confirmation that names the device and says what the cancel leaves,
as below.

- **Balance**: `BALANCE_CTL cancel`. The kernel deletes the balance item, so
  the work done stays but the rest is dropped.
- **Scrub**: `SCRUB_CANCEL`, refused while a device replace runs, because the
  kernel would cancel the replace with it.
- **Replace**: `DEV_REPLACE cancel`. The target is destroyed and the source
  kept. It is sent again while the kernel answers, in the ioctl's result, that
  no replace runs yet. A cancel that reaches a replace followed after a reboot in its last
  step is too late: the new disk is installed and the job ends completed. A
  replace the kernel says copies from another device is not this job's and is
  left running.
- **Remove**: `RM_DEV_V2` with the name `cancel`, which needs kernel 5.14; an
  older kernel refuses it and the removal runs to the end. It is sent again
  while the kernel answers that no removal runs yet. A cancelled removal leaves
  the device a member, and the data already moved off it stays moved, so
  removing it again continues where it stopped. A cancel that lands just as a
  removal finishes can make the kernel cancel the next balance or removal at
  its start (`chunk relocation canceled on start` in dmesg); start it again.

### Shutdown and resume

When the job host stops (a shutdown or `systemctl stop`), each of these jobs
is suspended and recorded as interrupted with a checkpoint. After a shutdown or
a power loss the root host resumes it once at the next boot. A job a crash
interrupted, or a host stopped by hand, is not resumed at a later reboot; start
it again. There is no manual resume.

| Job | On stop | After the boot |
|---|---|---|
| Balance | Paused; the kernel keeps the item and continues it when the pool is mounted read-write | Follows the kernel's balance; resumes it if still paused; counts it finished if it is gone after a pause; runs it again (`soft` when converting) if it is gone and was never paused |
| Scrub | Stopped, each device's position stored in the checkpoint; the scrub record keeps its progress too | Continues from the progress the job saved in the scrub record, after a power loss too (see [Scrub progress](#scrub-progress)); without the job's own record from each device's stored position, or from the start when none was read. The counters add up. Errors in the dev extent where each device stopped count twice |
| Replace | Cancelled with the job's context; the source is kept | Follows the replace the kernel continued at mount, and refuses one the kernel says copies from another device. A finished replace counts only if it began after the job did and the new disk passes the check after a replace; otherwise start it again. A suspended one needs the pool remounted |
| Remove | Cancelled as above; moved data stays moved | Issues the removal again, with its percentage counting what is left; refuses a degraded pool |

A stop pauses a balance every time, so a paused balance holds the pool's
exclusive operation after `systemctl stop linuxio-jobs@0` by hand: remove,
replace and resize are refused until the pool is mounted again or the balance
is resumed (`btrfs balance resume`); a host started again in the same boot
does not resume it. A SIGKILL of the host cancels a balance in the kernel, so
a crash loses it.

A replace the power loss cut is continued by the kernel when the pool is
mounted, and the job follows it by device id, never by device path. See
[Missing disks](#missing-disks) for why such a pool is mounted at boot.

### Scrub progress

The kernel keeps no scrub state: each `BTRFS_IOC_SCRUB` call counts only
itself, and returns, whatever its outcome, how far it got on its device
(`last_physical`). btrfs-progs saves that per device every few seconds in
`/var/lib/btrfs/scrub.status.<uuid>`, and `btrfs scrub resume` restarts each
device that did not finish from it, adding the saved counters. LinuxIO keeps
the same in its own record, `/var/lib/linuxio/btrfs-scrub.json`, and does not
write btrfs-progs' file, so `btrfs scrub status` does not show LinuxIO's
scrubs: the pool details do.

- **What is saved.** Per pool: the scrub's start, outcome and counters, the
  job ID of the scrub that writes it or the run of a pool resync's scrub
  (empty for any other scrub outside a job), and the process that writes it
  (pid, start time and boot).
  Per device: its UUID, the highest `last_physical` the kernel reported in
  this scrub, whether it finished or aborted, and its write and flush error
  counters when its scan began (see [Missing disks](#missing-disks)). The pool's counters hold
  every kernel call that returned; the counters of a call still running stay
  with its device until it returns, so the total is the pool's counters plus
  the devices'. A scrub that ended keeps each device's UUID, offset and
  whether it finished or aborted, but no counters per device.
- **When.** At the start, with what a resumed scrub carries; whenever a
  device's kernel call returns; every minute while a device's position moved,
  read with `BTRFS_IOC_SCRUB_PROGRESS`; and at the end, with what each call
  returned. A power loss costs at most a minute of progress. Every write
  reads and rewrites the whole file under `btrfs-scrub.json.lock`, by the job
  host and `linuxio-worker resync` alike, so two pools scrubbing at once
  keep both entries; the file is replaced atomically and fsynced.
- **Flush.** The kernel's repair writes carry no flush (`scrub_write_sectors`),
  and a scrub commits no transaction unless a superblock was bad, so a disk
  could still hold them in its cache when an offset above them is saved.
  Before each write that moves an offset or marks a device done, and before
  the outcome, the scrub sends each disk of the pool a cache flush: `fsync`
  of the block device, opened read-only so udev sees no change, a no-op on a
  disk without a write cache. The disks are those the kernel lists in
  `/sys/fs/btrfs/<uuid>/devices`, by their kernel names, since the path a
  member reports can name no device node (`/dev/root` for a root mounted
  without an initramfs). A list shorter than the pool's present devices
  fails the flush, and so does a member with no node in `/dev`, as in a
  container, where every scrub then ends aborted. The flush runs in the
  scrub's loop and cannot be interrupted: a disk whose flush hangs holds the
  scrub, as it holds the kernel's own commits. The kernel has completed
  every repair outside the dev extent holding a reported offset, because it
  waits for every repair at the end of each dev extent before the next
  (`flush_scrub_stripes` since 6.4, `scrub_enumerate_chunks` before). Within
  an extent the offset can run ahead of repairs still queued: it moves to
  the extent's end, or on RAID5/6 past a data stripe it has only queued, and
  only since 6.11 also past a stripe after that stripe's repair. The one
  byte back scans the extent holding the offset again. A failed flush skips that write, so the record keeps the
  offsets last flushed; at the end it fails the scrub, whose record is then
  the last one a flush preceded, marked aborted, so what was read since is
  read and counted again rather than twice. btrfs-progs flushes nothing
  before it writes its status file.
- **Refusals.** A scrub, fresh or resumed, is refused before it writes
  anything, so it cannot overwrite a running scrub's record:
  - when the inventory could not read every device: it leaves such a device
    out, so the scrub would skip it, a resume would drop its saved state, and
    the outcome would read finished;
  - as a conflict, while the kernel scrubs any device of the pool, a missing
    one included, as btrfs-progs checks, or runs a device replace, during
    which it refuses every scrub;
  - as a conflict, while the scrub on record is still `running` and the
    process that wrote it lives: between kernel calls, or while a call waits
    for a thaw, the kernel reports no scrub. A record whose final save failed
    therefore keeps refusing scrubs of that pool until that process exits,
    for a job until the job host restarts.

  A scrub that starts between those checks and its first call is refused by
  the kernel on every device (`EINPROGRESS`): it fails as a conflict too and
  puts back the record it replaced, unless the other scrub saved its own
  meanwhile. One the kernel refused on some devices and ran on others also
  fails as a conflict, and keeps its record, aborted, since it checked part of
  the pool.
- **Resume.** After a reboot the scrub job resumes from the record when the
  record's job ID is its own and the scrub is still `running` (a power loss),
  `canceled` (Suspend at a shutdown) or `aborted` (a device aborted, then
  Suspend canceled the rest). Each device that had not finished
  starts one byte below its saved offset; one that finished or aborted is
  skipped, and an abort keeps the outcome aborted; a device the record does
  not name, or names with another UUID because another disk took its device
  ID, starts at 0 (a replace target takes the source's UUID with its ID, and
  holds only copies the replace checked); a missing device is skipped. The resumed scrub keeps the
  first start and adds the counters so far to its own, so an uncorrectable
  error found before the cut stays in the outcome and its `btrfs/scrub` alert
  stays up. Its record is written before its first kernel call, so a second
  cut early in the resume loses nothing. Without such a record (another job's,
  a scrub's outside a job, or none) the resume takes the Suspend checkpoint's
  positions, and without positions scrubs everything again. It carries on the
  canceled or aborted scrub on record, keeping its start, counters and abort,
  only when the record names no job ID and no devices: such a record does not
  say whose it is, and dropping it could hide an error or an abort its first
  part found. A pool resync continues its scrub the same way from a record of
  its run, through its next run rather than at boot (see
  [Missing disks](#missing-disks)), with two differences:
  a device whose scrub aborted is scanned again from its offset, since the
  resync must check every copy, and a record of its run that finished counts
  as its scrub with no kernel call. A record that
  lags the checkpoint, because its last save failed, is still preferred: its
  offsets and counters are from the same moment, so the part after them is
  read and counted again rather than skipped uncounted.
- **One byte back.** The kernel skips a dev extent only when it ends at or
  before the start and scrubs any other one whole, so a start inside an
  extent re-reads that extent. One byte below the saved offset also re-reads
  the extent the offset ends: the kernel can report the end of an extent while
  up to 8 MiB of its stripes are still queued, and on RAID5/6 it moves past a
  parity stripe before it sees that the stripe's check was interrupted. The
  cost is one dev extent (1 GiB at most) read again per device, and its
  errors counted twice.
- **Live percentage.** While a scrub runs, the pool details and the job's
  progress add the record's counters to the kernel's live ones, so a resumed
  scrub, or one whose call was issued again, continues from where it was
  instead of near 0%. The pool details do so only while the process that
  wrote a running record lives, so a record a crash left is never added to a
  scrub btrfs-progs runs; while it lives the scrub reads as running even when
  the kernel reports nothing, between calls or waiting for a thaw. A running
  record without a live process, one a crash left or one whose writer could
  not be identified, gives only the scrub's start.
- **A freeze.** Since kernel 6.19 a suspend to RAM or hibernation, a
  filesystem freeze (`fsfreeze`) or a frozen cgroup ends a scrub call with
  `EINTR` (`should_cancel_scrub`). While the job is not canceled the call is
  issued again, from one byte below the `last_physical` it returned and never
  below where it started, its counters added to the device's; the new call
  waits in the kernel for the thaw (`mnt_want_write`). The kernel re-reads
  the dev extent holding the start, so after three interrupted calls in a row
  that did not get past their start the device ends aborted: without a limit,
  freezes that keep coming, or a kernel that keeps interrupting, would have
  the scrub issue calls forever. An `EINTR` once the job is canceled, or once
  any of its calls was canceled (Suspend, the scrub cancel route), counts as
  canceled, even in a scrub that already reads aborted. While the new call
  waits for a thaw, `SCRUB_CANCEL` finds no scrub to cancel, so a canceled
  scrub sends it again every two seconds until its calls return, and the
  scrub that starts at the thaw is canceled at once. The kernel cancels per
  filesystem, so this also cancels a scrub another tool starts on the pool in
  that time. A filesystem left frozen through a shutdown holds the job host
  until systemd's stop timeout, because the call waiting for the thaw cannot
  be interrupted, not even by SIGKILL. When the scrub cancel route or
  Suspend, which cancel the scrub from outside it, finds
  nothing to cancel because every call waits for a thaw, it marks the
  record of the scrub running in a live process instead (`cancelRequested`);
  the scrub reads the mark every tick and at every save, so the cancel is
  taken, reaches every call once it starts, and stops an interrupted call
  from being issued again. When the kernel did cancel calls, the scrub
  cancels the rest by itself and no record is marked, so a scrub starting
  next cannot take the request. A scrub starting or resuming never takes over
  a mark, its own record being told apart by boot, process and start, so a
  cancel that had not taken effect before a power loss is forgotten and the
  job resumes. A cancel accepted just as the last call returned leaves the
  scrub finished, and a resync's run marked canceled. A record whose final
  save failed accepts cancels that change nothing until the job host
  restarts. A device replace is not issued again: its target is lost, and
  the job fails.
  Before 6.19 the kernel does not stop either for a freeze: `fsfreeze` waits
  until it ends, and a suspend can fail to freeze the job host.

## Replication

Jobs live in `/etc/linuxio/btrfs-replication.json`: a source subvolume, a
target, and **Keep on target**. Jobs run after each hourly snapshot pass and
from the job's own **Run now**.

The hourly pass does not replicate itself. When a job is enabled it ends by
asking systemd, over D-Bus (`StartUnit`, mode `replace`), to start
`linuxio-btrfs-replicate.service`, a oneshot that runs
`linuxio-worker replicate`, and exits without waiting for it: the
call returns once the start job is queued, like `systemctl --no-block`. The
replication therefore runs in a unit of its own, and a first send that takes
hours no longer keeps the snapshot service busy and its hourly runs skipped.
There is no second timer. While a replication still runs, the next pass's
start finds the unit's start job running and is merged into it (systemd's
`job_install()`: a start job is a superset of a start job, so it is a "late
merge" into the running job, which is not run again). The hour's replication
is thus skipped, not queued, and the running one carries on.

- The unit, like the snapshot service, is a package unit under
  `/usr/lib/systemd/system`, with its run-record drop-in beside it: it is
  there whether or not a job is enabled, and an update replaces it with the
  programs it names. Saving a rule or a job writes only the timer, which
  holds a schedule and no command line. Disabling every rule and job stops
  and disables the timer.
- The replication run's exit status follows the snapshot pass's: 0 also
  when jobs failed, since each raised its job's alert and the run's summary
  counts them, 1 for a failure without one (jobs that cannot be read, an
  outcome that could not be recorded), which gets the run alert of the unit's
  end hook. A run in which another run, such as one started from the page,
  held every enabled job did no work: it completes with a summary that says
  it was skipped, so its end hook changes no alert, and it raises no notice. Its summary line
  counts the jobs that
  replicated (sent or already up to date), those that sent, and the errors;
  a job whose outcome could not be recorded still counts by what it did.

- **Local** target: a btrfs subvolume path on a **different** filesystem.
- **SSH** target: host, user, port and an absolute path on the remote.
- The snapshot is streamed with the kernel's send ioctl into
  `btrfs receive`, locally or over SSH; receive has no kernel interface.
- A missing target path is created with `btrfs subvolume create`.
- The target is dedicated to the job and not shared with any other job or
  host; two jobs, enabled or not, may not name the same one. Only direct
  children of the target subvolume count as its copies.
- Incremental sends use the newest common snapshot, matched by received UUID.
  Only fully received copies count. If none is common, a full send runs.
- A failed receive removes the partial copy and raises an alert
  `btrfs/replication-<job id>`; a later good run resolves it.
- **Keep on target** prunes older copies; 0 keeps everything.

SSH setup:

1. Open the job dialog. LinuxIO's key is
   `/etc/linuxio/btrfs-replication/id_ed25519` (generated on first use); its
   public half is shown in the UI.
2. Add the public key to the target user's `authorized_keys`. That user must
   be able to run `btrfs receive`, `btrfs subvolume list|show|create|delete`
   and `test`, normally root.
3. **Check host key**, compare the fingerprints, then **Trust**. Trust writes
   exactly the fingerprints shown to
   `/etc/linuxio/btrfs-replication/known_hosts`; nothing else populates it.

Connections use `StrictHostKeyChecking=yes`, `BatchMode=yes`,
`ConnectTimeout=15` and `ServerAliveInterval=30`; an unknown or changed host
key fails the job.

## Space

The pool page's **Space** card, the pool's card on the Pools tab and its card
on the Disks page give two figures, each byte counted once however many copies the profiles
keep (`StoredBytes` and `FreeBytes` in `inventory.go`):

- **stored**: the used bytes of every data, metadata and system row, as the
  kernel's `SPACE_INFO` reports them;
- **about N free**: the kernel's own room for more data, the figure `df`
  shows (statfs's `f_bavail`, `poolFreeBytes` in `inventory.go`). btrfs
  counts it disk by disk for the profile new data gets
  (`btrfs_calc_avail_data_space`), so a raid1 pool's free space is about half
  its raw free space, a mirror of a 1 TB and a 4 TB disk has about 1 TB to
  give, not 2.5, and a missing disk adds nothing. `btrfs filesystem usage`
  divides the unallocated space of all disks together and overstates the
  uneven mirror, which is why LinuxIO does not use its estimate.

The bar is stored against stored plus free. **Details: how the disk is laid
out** opens the rows behind them: each kind and profile with what is used of
what is set aside, the space not set aside yet, and the UUID. A disk's row
on the Disks card says how much of it is **allocated** to the pool. When the
space cannot be read, the Space card says so and shows no figures, and the
pool's card on the Disks page says **Space unknown**.

### Known limits

- A pool running without a disk writes new data as `single` on the disks
  present (`btrfs_reduce_alloc_profile`), and the free figure counts that
  room, as `df` does: it is room without a second copy.
- The kernel's figure drops toward 0 when metadata is nearly full, before the
  data chunks are; the `btrfs/space` alert says why.
- The Snapshots view's Today and Yesterday are worked out when the list
  loads, so a page left open past midnight keeps them until the list is read
  again.

## Quotas and compression

Both are in the **Settings** card of the pool details, with the
btrfsmaintenance timers.

- **Quotas** toggle enables or disables qgroups with the quota ioctl. With
  quotas on, subvolume and snapshot sizes are read from the quota tree and
  limits apply (the qgroup limit ioctl). btrfs has no per-user quotas; a
  size limit applies to a subvolume, such as a shared folder.
- **Compression (filesystem-wide)**: btrfs applies the compress option to every
  mount of a filesystem, so there is one setting per filesystem, read from its
  first mount. It is applied with
  a `mount(2)` remount with `compress[-force]=<alg>[:<level>]` on that mount and
  persisted into every existing `/etc/fstab` entry of the filesystem (matched
  by mountpoint or `UUID=` source); only the options field is rewritten, and
  comments and dump/pass fields are preserved. **Off** persists as removal of
  the option. The card says **Kept after a restart** when every such entry
  carries it, and **Until the next restart** otherwise.
- **Compression per subvolume** sets the `btrfs.compression` extended
  attribute, which is that property.

## Files written on the host

| Path | Owner | Purpose |
|---|---|---|
| `/run/linuxio/btrfs/<uuid>` | root, tmpfs | Mount of the filesystem's top level; gone at reboot |
| `<top>/linuxio-snapshots/` | root, `0700` when made (`0711` once a shared folder offers earlier versions, and where an earlier version made it `0755`) | Snapshots, each source's folder `0700`, or with an ACL while a shared folder offers earlier versions |
| `/etc/linuxio/btrfs-snapshots.json` | `root:root` `0600` | Snapshot rules |
| `/etc/linuxio/btrfs-replication.json` | `root:root` `0600` | Replication jobs |
| `/etc/linuxio/btrfs-replication/id_ed25519` (`.pub`) | root, key `0600` | SSH key for replication |
| `/etc/linuxio/btrfs-replication/known_hosts` | root | Trusted target host keys |
| `/usr/lib/systemd/system/linuxio-btrfs-snapshot.service` | root, package | Runs `linuxio-worker snapshot` |
| `/etc/systemd/system/linuxio-btrfs-snapshot.timer` | root | Hourly trigger; written when a rule or job is saved, deleted when the package is removed |
| `/usr/lib/systemd/system/linuxio-btrfs-replicate.service` | root, package | Runs `linuxio-worker replicate`; started by the snapshot pass while a replication job is enabled |
| `/usr/lib/systemd/system/linuxio-btrfs-health.service` | root, package | Runs `linuxio-worker health` |
| `/usr/lib/systemd/system/linuxio-btrfs-health.timer` | root, package | Every ten minutes |
| `/usr/lib/systemd/system/linuxio-btrfs-degraded.service` | root, package | Runs `linuxio-worker degraded` at boot |
| `/usr/lib/systemd/system/linuxio-btrfs-resync@.service` | root, package | Runs `linuxio-worker resync <uuid>` |
| `/var/lib/linuxio/btrfs-scrub.json` | root | Last scrub LinuxIO ran per filesystem, and a running one's progress; written under `btrfs-scrub.json.lock` |
| `/var/lib/linuxio/btrfs-pools.json` | root | Per pool: newest generation seen, whether and on which disks it ran degraded, when the episode began, the disks it ran without with their error counters as the pool was last seen whole or as they went missing, each device's error counters as a health run or the end of an episode last saw the pool whole, and the run of the episode's resync scrub (`resyncRun`) |
| `/var/lib/linuxio/btrfs-label.lock` | root | Lock taken around every label change, the resync's commit of a counter reset included |
| `/var/lib/linuxio/btrfs-spares.json` | root, `0644` | Per pool: its [hot spare](#hot-spare)'s WWN, serial and size, and the delay; written under `btrfs-spares.json.lock`. Readable by everyone, so an unprivileged session's Disks page shows the reservation too |
| `/etc/systemd/system/<upgrade-service>.d/linuxio-alert.conf` | root | Auto-update drop-in, including the pre-update snapshot line |
| `/etc/fstab` | root | Pool entries LinuxIO creates; compression options of existing entries; `x-systemd.device-bound=false` on the `UUID=` lines of mounted pools (the health run, see [Pools](#pools)) |
| `/run/mount/utab` | root | libmount's record of mounts' userspace options, written through `mount --fake` by the health run for a pool mount systemd would stop with its disk; gone at the next boot |
| `/var/lib/linuxio/alerts.json` | `root:root` `0600` | Alerts `btrfs/health-*`, `btrfs/scrub-*`, `btrfs/space-*`, `btrfs/offline-*`, `btrfs/bound-*`, `btrfs/resync-*`, `btrfs/resynced-*`, `btrfs/spare-*`, `btrfs/snapshot-*`, `btrfs/replication-*` |

## Not implemented

- `raid5` and `raid6` profiles, refused on purpose.
- Encryption under a pool. It needs `cryptsetup` (LUKS), and LinuxIO adds no
  binaries for btrfs.
- Booting into snapshots from GRUB.
- Hooking `apt` or `dnf` run by hand.
- Docker's btrfs storage-driver subvolumes, hidden from lists.
- Inclusion of nested subvolumes in snapshots.
- Automatic cleanup of `.linuxio-rollback-*` leftovers.
