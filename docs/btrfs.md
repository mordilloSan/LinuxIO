# Btrfs management

The **Storage → Btrfs** tab (`/storage/btrfs`) manages btrfs filesystems:
health, subvolumes, snapshots with rollback, automatic snapshots, devices and
profiles, replication to another filesystem or host, quotas and compression.
It drives `btrfs-progs` and standard system tools; there is no daemon and no
new binary beyond the existing `linuxio` CLI. Everything is on demand or runs
from one systemd timer.

## Capability and privileges

The `btrfs` capability checks for the `btrfs` executable (`btrfs-progs`) and
gates the tab; see [Capabilities](./capabilities.md#btrfs-tools). The tab is
**administrator-only**: the route requires a privileged session, reads run
`btrfs` as root and the configuration files are root `0600`.

Docker's btrfs storage-driver subvolumes are hidden from every list.

## Filesystem health

Each filesystem card shows its devices, allocation, profiles and per-device
error counters. A missing device or a non-zero counter raises an error alert
`btrfs/health-<uuid>` whenever the list is loaded, and resolves it when the
condition clears. Incomplete health data neither raises nor resolves. A
device the mounted `btrfs filesystem show` omits (it only prints `*** Some
devices missing`) is listed from `btrfs device stats` as a missing device with
an empty path, so it can be removed or replaced.
Scrub start/cancel is on the same card; see [Devices](#devices-and-profiles).

## Top-level access

Subvolume and snapshot operations need the filesystem's top level
(`subvolid=5`). On first use LinuxIO mounts it at `/run/linuxio/btrfs/<uuid>`,
an ordinary host mount, skipped when a user `subvolid=5` mount exists, and
leaves it mounted until reboot.

## Subvolumes

The Subvolumes card lists subvolumes with ID, path, read-only state and
default marker, and can create, delete, set read-only and set the default
subvolume. Nested subvolumes appear as their own rows. Subvolume paths may not
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
`linuxio btrfs-snapshot run`. The timer is enabled while at least one rule or
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
- by `ExecStartPre=-/usr/local/bin/linuxio btrfs-snapshot pre-update` in the
  `linuxio-alert.conf` drop-in LinuxIO writes for the distribution's
  auto-update unit when its notification policy is not `never` (see
  [Alerts](./alerts.md#automatic-updates-notification-policy)). The `-` means
  a snapshot failure never blocks the upgrade. Existing installs get the line
  the next time the auto-update policy is saved.

Manual `apt` or `dnf` runs are not hooked.

## Devices and profiles

| Action | Behaviour |
|---|---|
| Add device | Free devices only. A device with partitions or holders is refused even with **Force**; Force wipes a foreign filesystem signature. |
| Remove device | Refused below the minimum device count of the current profiles, and refused when the current profiles cannot be determined. |
| Replace device | Replaces one member with a free device. |
| Balance / convert | Profiles `single`, `dup`, `raid0`, `raid1`, `raid1c3`, `raid1c4`, `raid10`. `raid5` and `raid6` are refused. Runs in the background; the outcome is verified against the resulting allocation. |
| Scrub | Start and cancel. |

Cancelling the task interrupts a device remove. Balance, replace and scrub are
kernel operations that continue if the browser task is cancelled; reopen the
page to re-attach to a running balance.

## Replication

Jobs live in `/etc/linuxio/btrfs-replication.json`: a source subvolume, a
target, and **Keep on target**. Jobs run from the hourly timer after
snapshots and from the job's own **Run now**.

- **Local** target: a btrfs subvolume path on a **different** filesystem.
- **SSH** target: host, user, port and an absolute path on the remote.
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

- **Quotas** toggle runs `btrfs quota enable|disable`. With quotas on,
  subvolume and snapshot sizes are shown and limits apply
  (`btrfs qgroup limit`).
- **Compression (filesystem-wide)**: btrfs applies the compress option to every
  mount of a filesystem, so there is one setting per filesystem, read from its
  first mount. It is applied with
  `mount -o remount,compress[-force]=<alg>[:<level>]` on that mount and
  persisted into every existing `/etc/fstab` entry of the filesystem (matched
  by mountpoint or `UUID=` source); only the options field is rewritten, and
  comments and dump/pass fields are preserved. **Off** persists as removal of
  the option. "Persisted in fstab" means every such entry carries it.
- **Compression per subvolume** uses `btrfs property set ... compression`.

## Files written on the host

| Path | Owner | Purpose |
|---|---|---|
| `/run/linuxio/btrfs/<uuid>` | root, tmpfs | Mount of the filesystem's top level; gone at reboot |
| `<top>/linuxio-snapshots/` | root | Snapshots |
| `/etc/linuxio/btrfs-snapshots.json` | `root:root` `0600` | Snapshot rules |
| `/etc/linuxio/btrfs-replication.json` | `root:root` `0600` | Replication jobs |
| `/etc/linuxio/btrfs-replication/id_ed25519` (`.pub`) | root, key `0600` | SSH key for replication |
| `/etc/linuxio/btrfs-replication/known_hosts` | root | Trusted target host keys |
| `/etc/systemd/system/linuxio-btrfs-snapshot.service` | root | Runs `linuxio btrfs-snapshot run` |
| `/etc/systemd/system/linuxio-btrfs-snapshot.timer` | root | Hourly trigger |
| `/etc/systemd/system/<upgrade-service>.d/linuxio-alert.conf` | root | Auto-update drop-in, including the pre-update snapshot line |
| `/etc/fstab` | root | Compression options, only for existing entries |
| `/var/lib/linuxio/alerts.json` | `root:root` `0600` | Alerts `btrfs/health-*`, `btrfs/snapshot-*`, `btrfs/replication-*` |

## Not implemented

- `raid5` and `raid6` profiles, refused on purpose.
- Booting into snapshots from GRUB.
- Hooking `apt` or `dnf` run by hand.
- Docker's btrfs storage-driver subvolumes, hidden from lists.
- Inclusion of nested subvolumes in snapshots.
- Automatic cleanup of `.linuxio-rollback-*` leftovers.
