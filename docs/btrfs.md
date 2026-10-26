# Btrfs management

The **Storage → Subvolumes** tab (`/storage/subvolumes`) manages subvolumes,
and, under its Snapshots and Replication views (`?view=snapshots`,
`?view=replication`), snapshots with rollback, automatic snapshots and
replication to another filesystem or host. Each pool's details on the **Storage → Disks** overview
(`/storage?pool=<uuid>`) hold the rest: health, devices and profiles, space,
quotas, compression and maintenance timers.
Everything on the host is read and driven through the kernel's btrfs ioctls,
`mount(2)`, `/proc/self/mountinfo` and the disks' superblocks: devices,
profiles, balance, scrub, space, health, pools and partition tables,
subvolume and quota listing, subvolume create, delete, snapshot and default,
read-only flags, compression properties, quotas and limits, and the send side
of replication. `btrfs-progs` is used only where the kernel has no interface:
`mkfs.btrfs`, `btrfs receive`, and the commands a replication job runs on its
target, which may be another host. There is no daemon. Everything is on
demand or runs from `linuxio-btrfs-timer`, started by two systemd timers, a
boot-time service and a resync service (see [Missing disks](#missing-disks)).

`make test-btrfs-integration` runs every one of these operations against the
real kernel on loop devices, loses and returns disks of a mirror to check
the missing-disk handling, and exercises
[partition management](./storage-disks.md#partition-management); it needs
sudo.

## Capability and privileges

The `btrfs` capability checks for the `btrfs` executable (`btrfs-progs`) and
gates the Subvolumes tab and the pools on the Disks overview; see
[Capabilities](./capabilities.md#btrfs-tools). Both are
**administrator-only**: they require a privileged session, reads run
`btrfs` as root and the configuration files are root `0600`. The Subvolumes
tab is listed only while a btrfs filesystem is mounted; pools are created
from the Disks overview.

Docker's btrfs storage-driver subvolumes are hidden from every list.

## Filesystem health

Each pool's details show its devices, allocation, profiles and per-device
error counters. A device the kernel knows but cannot find is listed as missing
with an empty path, so it can be removed or replaced. The health banner at the
top of the details leads with the worst condition and the one action that
fixes it: **Replace missing disk** on a degraded pool, **Reclaim space** when
metadata is nearly full. Health, scrub and space alerts link to those details;
snapshot and replication alerts link to the Subvolumes tab's Snapshots and
Replication views.

Alerts are updated whenever the list is loaded and, independently of the
browser, by `linuxio-btrfs-health.timer`, which runs `linuxio-btrfs-timer
health run` two minutes after boot and every ten minutes. The units ship with the
package; the installer enables the timer when the host has a btrfs
filesystem, and the bridge enables it the first time an administrator lists
filesystems, covering pools created after install.

| Alert | Severity | Raised when | Resolved when |
|---|---|---|---|
| `btrfs/health-<uuid>` | error | a device reports errors or is missing; "running without n of m disks" when degraded | the counters are zero and no device is missing |
| `btrfs/scrub-<uuid>` | error | the last scrub found uncorrectable errors | a later scrub finishes clean |
| `btrfs/space-<uuid>` | warning | metadata is over 90% full with less than 1 GiB unallocated | space is reclaimed |
| `btrfs/offline-<uuid>` | error or warning | a pool is found on disk but not mounted (error when disks are missing; says why the boot check left it unmounted) | the pool is mounted |
| `btrfs/resync-<uuid>` | warning, error after three failed resyncs | a pool that ran with a disk missing has all its disks again and is being resynced | the resync finished |
| `btrfs/resynced-<uuid>` | info | a resync finished and the pool is whole again | dismissed, or the next resync starts |

Incomplete health data neither raises nor resolves; alerts of pools that no
longer exist are dropped by the timer. Scrub start/cancel is on the banner;
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
at most three minutes) runs `linuxio-btrfs-timer degraded run`. Pools that
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

The record lives in `/var/lib/linuxio/btrfs-pools.json`: every mount LinuxIO
makes and every health run notes each pool's generation, and while a pool runs
degraded, which disks take its writes. A disk that dies while the pool is
mounted is only recorded by the next health run, so a crash within those ten
minutes, followed by only the dead disk coming back, is not caught. The
recorded generation can belong to a transaction a crash then loses, which
makes the check refuse a set that was in fact current: the safe side.

**When the disk is back.** A disk that reappears while the pool is mounted is
listed by the kernel but never opened, so it is shown as missing until the
pool is mounted again (a reboot). Once every disk is in use, the health run
starts `linuxio-btrfs-resync@<uuid>.service` (`linuxio-btrfs-timer resync`):
a scrub checks every copy and rewrites those the returned disk missed from
the good ones, then a soft convert (`convert=<profile>,soft`) gives chunks
written while too few disks were present, which btrfs allocates as `single`,
the pool's most redundant profile again. Until then the `resync` alert is up.
Only a scrub that finished counts: one canceled from the overview, or cut
short by a crash (its record then reads as aborted), is run again. A failed
resync is retried by the next health run, without scrubbing again once the
scrub finished; after three failures the alert turns into an error with the
last cause, and mounting the pool again (or a reboot) re-arms it.
Uncorrectable scrub errors go to the scrub alert. Files with copy-on-write
disabled (`nodatacow`, common for VM images) have no checksums, so a scrub
cannot tell their stale copies; replace a disk that returns to a pool holding
such files instead. A disk that keeps failing writes while still attached is
not counted as missing: the health alert reports its error counters, and a
scrub once it is fixed repairs what it missed.

## Top-level access

Subvolume and snapshot operations need the filesystem's top level
(`subvolid=5`). On first use LinuxIO mounts it at `/run/linuxio/btrfs/<uuid>`,
an ordinary host mount, skipped when a user `subvolid=5` mount exists, and
leaves it mounted until reboot.

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
read-only, become the default subvolume or be deleted; the card creates new
ones. Nested subvolumes appear as their own rows. Subvolume paths may not
contain `,` or whitespace, which would break `subvol=` mount options and
`/etc/fstab`. Deleting a leftover
`.linuxio-rollback-*` subvolume (see [Rollback](#rollback)) is done here.

## Snapshots

Layout under the top level:

```text
<top>/linuxio-snapshots/<source>/<YYYYMMDDTHHMMSSZ>-<kind>
```

`<source>` is the subvolume path with `/` escaped as `%2F` (and `%` as `%25`).
`<kind>` is `manual`, `timeline`, `pre-update` or `pre-rollback`. Snapshots
are read-only. Nested subvolumes are not included in a snapshot.

**Browse** opens the file browser at
`/run/linuxio/btrfs/<uuid>/linuxio-snapshots/...`. **Restore files** is a copy
in the file browser; there is no separate restore operation.

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
`linuxio-btrfs-timer snapshot run`. The timer is enabled while at least one rule or
replication job is enabled and disabled otherwise. **Run now** runs the
snapshot and prune pass from the bridge; it does not replicate (each
replication job has its own **Run now**).

- A rule is due when its newest `timeline` snapshot is older than its period
  (5-minute tolerance for the hourly tick).
- Retention keeps the newest N per hour/day/ISO-week/month bucket. `manual`
  snapshots are never pruned. `pre-update` and `pre-rollback` snapshots keep
  the newest 3.
- A failed snapshot or an unmounted filesystem raises an alert
  `btrfs/snapshot-<uuid8>-<path>`; a later good run resolves it.
- Replication jobs run from the same invocation, after snapshots.

## Pre-update snapshots

Rules with **Snapshot before updates** are snapshotted, whether or not the
rule is enabled; a disabled rule prunes only its `pre-update` snapshots and
never its timeline:

- by LinuxIO's own package update task and offline apply, before they start;
- by `ExecStartPre=-/usr/local/bin/linuxio-btrfs-timer snapshot pre-update` in the
  `linuxio-alert.conf` drop-in LinuxIO writes for the distribution's
  auto-update unit when its notification policy is not `never` (see
  [Alerts](./alerts.md#automatic-updates-notification-policy)). The `-` means
  a snapshot failure never blocks the upgrade. Existing installs get the line
  the next time the auto-update policy is saved.

Manual `apt` or `dnf` runs are not hooked.

Units and drop-ins written by earlier releases still call the hidden
`linuxio btrfs-snapshot` and `linuxio btrfs-health` verbs; the CLI execs
`linuxio-btrfs-timer` with the equivalent arguments.

## Pools

A pool is one btrfs filesystem over one or more whole disks. **Create pool**
on the Storage overview formats free disks with `mkfs.btrfs`, with the data
and metadata profiles chosen from `single`, `dup`, `raid0`, `raid1`,
`raid1c3`, `raid1c4` and `raid10` (`raid5` and `raid6` are refused),
registers the disks with the kernel, mounts the pool and records it in
`/etc/fstab` as `UUID=<uuid> <mountpoint> btrfs defaults,noatime,nofail 0 0`.
A disk with partitions, holders or a mount is refused; a foreign filesystem
signature needs **Force**. The mount point must be an unused, empty path
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

A shared folder can be created on a pool from **Shares → Add Folder Share →
New folder on a pool**: it creates a subvolume under the pool's top-level
mount, optionally limits its size (turning quotas on) and adds the default
automatic snapshot rule, then shares it over SMB and/or NFS. Only pools with
a permanent top-level mount are offered.

`nofail` keeps a pool with a missing disk from blocking boot. Such a pool is
not mounted, since btrfs refuses that without `degraded`, so the overview
lists it from the disks' superblocks as an unmounted pool with its missing
disk count. **Mount** mounts a complete pool and **Mount degraded** one with
missing disks, so its data stays reachable until the disk is replaced; both
use the pool's fstab options, so `subvol=` and `compress=` apply. A pool
without an fstab entry is mounted at `/mnt/<label>` and recorded. Pools in
fstab are also mounted degraded at boot; see [Missing disks](#missing-disks).

**Destroy pool** unmounts every mount of the pool, removes its fstab entries
and zeroes the btrfs superblock magic on each disk; everything on the pool is
lost. A pool holding `/`, `/boot` or another protected mount is refused.

Apart from `mkfs.btrfs`, pools use no command: disks are registered with the
`BTRFS_IOC_SCAN_DEV` ioctl, mounted with `mount(2)`, partitioned by writing
the GPT and asking the kernel to re-read it, and discovered by reading the
primary superblock at 64 KiB.

## Devices and profiles

| Action | Behaviour |
|---|---|
| Add device | Free devices only. A device with partitions or holders is refused even with **Force**; Force wipes a foreign filesystem signature. |
| Remove device | Refused below the minimum device count of the current profiles, and refused when the current profiles cannot be determined. |
| Replace device | Replaces one member with a free device. A bigger replacement disk is grown into automatically. |
| Grow | Resizes a device to its whole disk when the filesystem uses less of it, for example after a replace done by hand. |
| Rename | Sets the filesystem label from the pool details. |
| Balance / convert | Profiles `single`, `dup`, `raid0`, `raid1`, `raid1c3`, `raid1c4`, `raid10`. `raid5` and `raid6` are refused. Converting metadata converts system chunks with it. Metadata weaker than the resulting data profile is converted too, to the same default as Create pool, unless nothing stronger fits on the present devices. The outcome is verified against the resulting allocation. |
| Reclaim space | A balance of data chunks at most 50% full, which turns slack into unallocated space. |
| Scrub | Start and cancel. The last result is recorded in `/var/lib/linuxio/btrfs-scrub.json`; scrubs run by btrfsmaintenance are read from `/var/lib/btrfs/scrub.status.<uuid>`. |

Remove, replace, balance and scrub are blocking kernel calls run inside the
task, so progress comes from the kernel and cancelling the task cancels the
operation. A balance or scrub started elsewhere is still shown as running.

## Replication

Jobs live in `/etc/linuxio/btrfs-replication.json`: a source subvolume, a
target, and **Keep on target**. Jobs run from the hourly timer after
snapshots and from the job's own **Run now**.

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
  the option. "Persisted in fstab" means every such entry carries it.
- **Compression per subvolume** sets the `btrfs.compression` extended
  attribute, which is that property.

## Files written on the host

| Path | Owner | Purpose |
|---|---|---|
| `/run/linuxio/btrfs/<uuid>` | root, tmpfs | Mount of the filesystem's top level; gone at reboot |
| `<top>/linuxio-snapshots/` | root | Snapshots |
| `/etc/linuxio/btrfs-snapshots.json` | `root:root` `0600` | Snapshot rules |
| `/etc/linuxio/btrfs-replication.json` | `root:root` `0600` | Replication jobs |
| `/etc/linuxio/btrfs-replication/id_ed25519` (`.pub`) | root, key `0600` | SSH key for replication |
| `/etc/linuxio/btrfs-replication/known_hosts` | root | Trusted target host keys |
| `/etc/systemd/system/linuxio-btrfs-snapshot.service` | root | Runs `linuxio-btrfs-timer snapshot run` |
| `/etc/systemd/system/linuxio-btrfs-snapshot.timer` | root | Hourly trigger |
| `/etc/systemd/system/linuxio-btrfs-health.service` | root | Runs `linuxio-btrfs-timer health run` |
| `/etc/systemd/system/linuxio-btrfs-health.timer` | root | Every ten minutes |
| `/etc/systemd/system/linuxio-btrfs-degraded.service` | root | Runs `linuxio-btrfs-timer degraded run` at boot |
| `/etc/systemd/system/linuxio-btrfs-resync@.service` | root | Runs `linuxio-btrfs-timer resync <uuid>` |
| `/var/lib/linuxio/btrfs-scrub.json` | root | Last scrub LinuxIO ran per filesystem |
| `/var/lib/linuxio/btrfs-pools.json` | root | Per pool: newest generation seen, and whether and on which disks it ran degraded |
| `/etc/systemd/system/<upgrade-service>.d/linuxio-alert.conf` | root | Auto-update drop-in, including the pre-update snapshot line |
| `/etc/fstab` | root | Pool entries LinuxIO creates; compression options of existing entries |
| `/var/lib/linuxio/alerts.json` | `root:root` `0600` | Alerts `btrfs/health-*`, `btrfs/scrub-*`, `btrfs/space-*`, `btrfs/offline-*`, `btrfs/resync-*`, `btrfs/resynced-*`, `btrfs/snapshot-*`, `btrfs/replication-*` |

## Not implemented

- `raid5` and `raid6` profiles, refused on purpose.
- Encryption under a pool. It needs `cryptsetup` (LUKS), and LinuxIO adds no
  binaries for btrfs.
- Booting into snapshots from GRUB.
- Hooking `apt` or `dnf` run by hand.
- Docker's btrfs storage-driver subvolumes, hidden from lists.
- Inclusion of nested subvolumes in snapshots.
- Automatic cleanup of `.linuxio-rollback-*` leftovers.
