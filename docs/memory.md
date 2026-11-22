# Memory overflow

The **Memory overflow** dialog, opened from the Swap row of the dashboard's
Memory card or by its URL, `/?dialog=memory-overflow`, says how the host
copes when RAM runs out and changes it: swap on disk, zswap in front of disk
swap, or zram in place of the swap files. It lists the host's swap files and
creates, resizes and removes them, sets how eagerly the kernel moves data out
of RAM (`vm.swappiness`) and brings data parked on disk back into memory. The
backend is `backend/bridge/handlers/memory`, the contract is
`backend/bridge/apischema/memory.go`, and the dialog is
`MemoryOverflowDialog.tsx` with its view logic in `memoryStatusView.ts`, both
beside the card in `routes/_authenticated/-dashboard`.

It is a dialog on the dashboard and not a Settings tab because Settings holds
LinuxIO's own settings; a host setting is edited where the host shows it, as
the hostname and the clock are on the System Overview card.

Why it exists: a swap file on btrfs blocks snapshots of the subvolume that
holds it while it swaps, and Ubuntu puts `/swap.img` in the root subvolume, so
on a btrfs root the system drive cannot be snapshotted while the host swaps to
disk. A swap file that is too small, too large or in the wrong place would
otherwise need a terminal. The dashboard shows swap use and nothing more, and
nothing else in LinuxIO writes these settings.

## Who sees it

The dialog is **administrator-only**: every action in it is privileged, so
the Swap row of the Memory card is an edit button with a pencil for a session
with sudo and plain text for any other, and the URL opens the dialog only for
a session with sudo. The dialog is not gated on a
capability: the status works without a zram tool or TuneD, and the zram row
links to Capabilities when the package is missing.

The status read, `memory.get_swap_status`, is not privileged. It reads
world-readable files (`/proc`, `/sys`, `/etc/fstab`, the zram tools'
configuration files, `/var/lib/dpkg/status`), looks at the swap files and
their folders (`lstat`, `statfs` and two btrfs ioctls that need no
privilege), asks TuneD over D-Bus whether it runs, and returns no secret: of
the zram configuration it returns paths, device names and the size settings
of zram-generator's other files, of the swap files their paths, sizes and
filesystems, nothing else. A session without sudo reads the same status as
root, except that a folder or a file it may not look into is not judged (see
[Which swap LinuxIO acts on](#which-swap-linuxio-acts-on)). The dialog is
withheld because it has nothing a session without sudo could do, not because
the read is sensitive; see
[Privilege Pattern](./privilege_pattern.md#in-the-frontend).

The seven changes are privileged and declare `Privileged()`:
`memory.set_swap_mode`, `memory.drain_swap`, `memory.create_swap_file`,
`memory.resize_swap_file`, `memory.remove_swap_file` and
`memory.move_swap_file` are mutations that run as jobs with progress
(`SwapProgress`), `memory.set_swappiness` a plain mutation. All of them
return the status read after the change.

## The three modes

| Name in the dialog | Mode | What the host does |
|---|---|---|
| Compressed memory with the disk as backup | `zswap` | The kernel compresses rarely used pages into a pool in RAM in front of disk swap; when the pool is full the oldest pages go to disk. Disk swap stays on and zswap is enabled. |
| Compressed memory only | `zram` | A zram device (half of RAM, at most 8 GiB, compressed with zstd) takes the place of the swap files: every swap file LinuxIO acts on is removed, with its `/etc/fstab` line, so nothing is written to disk and nothing comes back after a restart. A swap partition, an LVM volume and a swap file LinuxIO does not act on stay on beside zram and take what it cannot hold. Without them there is no backup: when zram fills up the kernel's OOM killer picks a victim. |
| Disk only | `disk` | Disk swap is the only swap, zswap is off and zram is stopped. A host with no disk swap gets LinuxIO's own swap file first. |

Two more values describe a host and cannot be chosen. `none` is a host with
no active swap device. `mixed` is zram active beside a swap file LinuxIO can
remove, which is what a switch to zram leaves when it could not remove that
file (see [Room](#room-for-what-comes-back)); applying zram finishes it.

### How the current mode is derived

From the devices in `/proc/swaps`, the swap file rows and zswap's `enabled`
parameter (`deriveMode`), first match wins:

1. a zram device and a swap file LinuxIO can remove both active: `mixed`;
2. a zram device: `zram`, whatever other swap stays beside it and whatever
   zswap says;
3. no device: `none`;
4. disk swap only, a file or a partition, with zswap enabled: `zswap`; with
   zswap off: `disk`.

So a host that runs zram beside a swap partition reads `zram`, and can
choose any mode.

### Recommendation and presets

The backend picks the recommended mode, first match wins (`recommendMode`):

| Host | Recommended |
|---|---|
| No active disk swap | `zram` |
| A swap file on an SD card or eMMC (`mmcblk*` under `/proc/self/mountinfo`) | `zram`, to spare the card |
| A swap file on a removable drive (`removable` is `1` for the disk under `/sys/class/block`) | `zram`, to spare the drive |
| An active swap file on btrfs outside LinuxIO's folder (`blocks_snapshots`) | `zram`, because the file blocks snapshots |
| A kernel without zswap | `disk` |
| Anything else | `zswap` |

zram takes the place of nothing but the swap files it removes, so a swap
partition or a swap file LinuxIO does not act on is no reason against it.

`disk_hint` is `mmc` or `removable` for the second and third rows. The
removable flag is what a USB stick or a card in a USB reader reports; for a
swap file on a partition it is read from that partition's disk. A USB disk
enclosure reports itself as fixed and is not flagged, which is right for a
USB SSD, and a swap file on a device-mapper volume (LVM, LUKS) is not flagged
whatever is under it. A swap file under a FUSE mount (type `fuse` or
`fuse.*`) is not flagged either: any user can make one through the setuid
`fusermount` and name any `/dev/...` as its source. A `fuseblk` mount is,
since the kernel opens the block device its source names.

Choosing a mode applies its preset to the vm sysctls. A sysctl a preset does
not list stays at the kernel's default.

| Mode | swappiness | page-cluster | watermark_boost_factor | watermark_scale_factor |
|---|---|---|---|---|
| zram | 180 | 0 | 0 | 125 |
| zswap | 60 | default | default | default |
| disk | 60 | default | default | default |

The zram row follows the Pop!_OS and Arch values; the zram size is half of RAM
capped at 8 GiB, as Fedora's default is. The status lists the presets
(`presets`) so the dialog's "Recommended for <mode>" caption can follow the
selected row, and `recommended_swappiness` is the preset of the current mode,
or of the recommended one when the host is `none` or `mixed`.

## The status

`memory.get_swap_status` reads, with no subprocess:

| Source | Gives |
|---|---|
| `/proc/swaps` | active devices: path (with `\040` and the other octal escapes undone), kind (`file`, `partition` or `zram`), size, used, priority |
| `/proc/meminfo` | `MemTotal`, `MemAvailable`, `SwapTotal`, `SwapFree` |
| `/proc/sys/vm/{swappiness,page-cluster,watermark_boost_factor,watermark_scale_factor}` | the four sysctls |
| `/sys/module/zswap/parameters/{enabled,compressor,max_pool_percent}` | zswap; no directory means `zswap.available` is false |
| `/sys/block/zram<N>/{disksize,mm_stat}` | each zram device's size, stored data, compressed size and memory used |
| `/etc/fstab` | the swap files it names: each line of type `swap` whose first field, with octal escapes undone, is an absolute path outside `/dev`, with the options of the first line that names it (`fstabSwaps` in `fstab.go`) |
| `/proc/self/mountinfo` | the filesystem and disk of each swap file and of LinuxIO's swap folder (`fs_type`, `disk_hint`) |
| `/sys/class/block/<device>` | whether that disk is removable (`disk_hint`), and the `major:minor` of the device a swap file's filesystem is on, for the hibernation check |
| `/sys/power/resume`, `/sys/power/resume_offset` | the device and the offset the host resumes from (see [The hibernation file](#the-hibernation-file)) |
| each swap file, by `lstat`, and the first page of one that is off | its length, owner and kind, and whether it holds a swap signature |
| the folder of each swap file and LinuxIO's swap folder (or the nearest folder above it that exists): `statfs`, and on btrfs `BTRFS_IOC_FS_INFO`, `BTRFS_IOC_SPACE_INFO` and the folder's inode | the filesystem's kind by its magic, free and total space, read-only, whether btrfs can hold a swap file (`btrfs.SwapfileLayout`), whether the folder is a subvolume |
| TuneD, through `power.GetStatus` | `tuned_active` and `tuned_profile` |
| the `zram` capability | `zram_available` |
| systemd over D-Bus, only with both zram tools installed | `zram_provider` (see [The zram provider](#the-zram-provider)) |
| the provider's configuration file, `/etc/default/zramswap` or `/etc/systemd/zram-generator.conf`, and its package's checksum of it in `/var/lib/dpkg/status` | `foreign_zram_config`, when it is not LinuxIO's (see [A zram configuration file that is not LinuxIO's](#a-zram-configuration-file-that-is-not-linuxios)) |
| zram-generator's other configuration files, only with zram-generator installed | `zram_generator_devices` (see [Other zram-generator configuration](#other-zram-generator-configuration)) |

From these it derives the mode, the recommendation, `zram_planned_size`,
`zram_unavailable_reason` (`package_missing`, or empty), `can_drain` with
`drain_blocked_reason`, `swap_files` and `create` (see
[Swap files](#swap-files)).

**What fails the route.** A required file that cannot be read or parsed
(`/proc/swaps`, `/proc/meminfo`, `/etc/fstab` when it is there), an optional
kernel file that exists and is malformed (a non-numeric sysctl, an `mm_stat`
with fewer than three fields, a `resume_offset` that is not a number), and any
read error other than "does not exist". A format change in the kernel should
surface rather than read as zero. **What degrades.** A missing optional file
reads as zero or absent (no zswap directory, no `mm_stat`, no `/etc/fstab`, no
mountinfo, no `/sys/power/resume`); a line of fstab or mountinfo that does not
parse is skipped; a zram device that leaves while it is read (its directory
gone, or a read that fails with `ENODEV`), as when zram's unit resets it, is
skipped; a folder whose filesystem cannot be read is not judged (Debug log); a
failed TuneD read counts as TuneD not running (Debug log only); with both zram
tools installed, a unit state that cannot be read makes zram-tools the
provider (Debug log only); a host with neither zram tool is
`zram_available: false` with an empty `zram_provider`; the provider's
configuration file that cannot be read counts as not LinuxIO's, and a
zram-generator file or folder that cannot be read is left out (Debug log
only).

## Swap files

### Which swap LinuxIO acts on

`swap_files` has a row (`SwapFileRow`, built in `swaprows.go`) for every swap
file: each active one, in the order of `/proc/swaps`, then each one
`/etc/fstab` names that is not active, in fstab order. A partition, an LVM or
LUKS volume and a zram device are not swap files: they are in `devices` only,
and the dialog lists them with their kind, size and use and nothing to press.
LinuxIO never turns one of them off.

Resize, Remove, Move, a drain and a switch to zram act only on a row that
passes one strict check. A row that fails it carries the first reason that
applies in `locked_reason`, and nothing acts on it:

| `locked_reason` | When | What the dialog says |
|---|---|---|
| `not_in_fstab` | The file swaps and no fstab line names it by its exact path: it was started by hand, by a `.swap` unit file, by a tool such as dphys-swapfile, or through a line that names it by another path | Something other than /etc/fstab turns it on, so LinuxIO cannot keep it off |
| `not_root_only` | The path is not clean, a folder from `/` down is a link, is not root's or may be written by a group or everyone, or what is at the path is not a regular file root owns | Another user could put a file of their own at this name |
| `hibernation` | The host resumes at a file offset from the device the file's filesystem is on | Changing this file would break hibernation |

- **The path is compared as a string**, fstab's first field with the
  `/proc/swaps` path, both with octal escapes undone. No tag, no link, no
  second name. `/proc/swaps` names a file turned on through a linked folder
  by its real path (read in the test VM), so it matches only a line that
  names the real path; a line that names it through the link is a row of its
  own, which fails `not_root_only`.
- **Only root can change the path** (`rootOnlySwapFile`, on the walk
  `rootOnlyPath` does). `/swap.img`, `/swapfile` and
  `/var/lib/linuxio/swap/swapfile` pass. A missing file passes, so a line
  that names a file that is not there is a row with Remove. A folder or a
  file the reader may not look into is not judged: a session without sudo
  reads the status too, and every change runs as root and checks again on
  handles (see [The building blocks](#the-building-blocks)).
- **A row that fails the root-only rule is not read through its path.** No
  `lstat` of the file and no filesystem read: it shows what `/proc/swaps`,
  `/etc/fstab` and mountinfo say, and a size of 0 when it is not active.
- **The temporary file of a resize** (`<path>.linuxio-resize`, see
  [Resize](#resize)) has no fstab line of its own: when it swaps beside a
  file a line names, that line counts, and the row has `temporary` set.

Nothing is "managed" or chosen: two swap files that pass are two rows with
the same buttons.

A row also carries `active`, `in_fstab`, `size` (the swap size of an active
file, the file's length otherwise), `used`, `fs_type`, `disk_hint`,
`free_space` (of its filesystem), `max_size` (the largest size a resize may
ask for), `blocks_snapshots` (a file on btrfs outside LinuxIO's folder, see
[Move](#move)), `can_move` and `resize_blocked_reason`, which for a row that
passes the strict check says why Resize is not offered:

| `resize_blocked_reason` | When | The row reads |
|---|---|---|
| `missing` | fstab names the file and nothing is at its path | Missing |
| `no_swap_header` | The file is off and holds no swap signature | Not a swap file |
| `off` | The file is in fstab and does not swap (a header the reader may not read counts as there) | Off |
| `unsupported_filesystem` | Not ext4, xfs or btrfs (see [The filesystems](#the-filesystems)) | |
| `read_only` | The filesystem is read-only | |
| `btrfs_layout` | btrfs with more than one device or data that is not single | |

LinuxIO never turns on a swap file fstab names that is off, as an older
LinuxIO's hold-off left it (see [Upgrading](#upgrading-from-an-older-linuxio))
or an admin's `swapoff` does. It can only be removed.

### The hibernation file

`/sys/power/resume` names the device the host resumes from as `major:minor`,
`0:0` when unset, and `/sys/power/resume_offset` the page offset of the swap
header on it, `0` for a partition (read on Ubuntu 26.04 and Fedora 45 in the
test VM). While the offset is not zero, every swap file on that device is
locked: the offset points at the file's first block, which a resize or a new
file would move. LinuxIO does not work out which file the offset belongs to.

The device of a file's filesystem is read from the mount's source, through
`/sys/class/block/<name>/dev` (`mountDevice`). The file's own `st_dev` is no
help on btrfs, where it is an anonymous `0:N` of the subvolume, and neither is
mountinfo's `major:minor`. A filesystem whose device cannot be told, as one
whose source is not under `/dev`, counts as the resume device.

### The building blocks

Every swap file change is built from five steps:

| Block | What it does |
|---|---|
| **Make a swap file** | Through its folder's handle: create the name with `O_CREAT\|O_EXCL\|O_NOFOLLOW`, mode `0600`; on btrfs set NOCOW on the empty file; `fallocate` to the size; `fsync`; `mkswap -- <path>`. A failure removes the file again, by its device and inode (`makeSwapFile`, `swapfile_make.go`) |
| **Turn on** | `swapon [-p <n>] [--discard[=<policy>]] -- <path>`, then wait up to a minute for the path in `/proc/swaps` (`swapOn` in `swapon.go`, `turnOn`) |
| **Turn off** | The room check on a freshly read status, then `swapoff -- <path>` as a process of its own (`turnOff`, see [Turning a swap file off](#turning-a-swap-file-off)) |
| **Edit `/etc/fstab`** | Add LinuxIO's line, or remove every swap line whose first field, unescaped, is the path, under the fstab lock and written atomically; then a systemd reload, so the unit its generator makes follows (`addFstabSwap`, `removeFstabSwap` in `fstab_edit.go`) |
| **Delete** | `unlinkat` through the folder's handle, only while a fresh `/proc/swaps` does not list the path and the name still holds the file that was checked, by device and inode (`deleteSwapFile`) |

**Folders are reached through handles.** Each block reaches the file's folder
from `/` one name at a time with `O_DIRECTORY|O_NOFOLLOW` and checks on each
handle that it is a folder root owns that no group or everyone may write
(`openRootOnlyFolder` in `swapfile_dir.go`), which is `rootOnlyPath` decided
on handles rather than names: no folder on the way can be swapped for a link
between the check and the use, and since only root can change any of them, no
name below can either. A refusal is `ErrConflict`. `swapon`, `mkswap` and
`swapoff` look the path up themselves, so they get it only after that walk,
and for `swapon` after the file was found to be a regular file root owns
(`statSwapFileAt`).

**Why LinuxIO runs `swapon` itself.** A swap file is turned on by util-linux's
`swapon`, run by LinuxIO, and not by starting the unit systemd generates for
its fstab line: no unit name is derived and nothing waits for systemd to see
a device leave or come back. systemd follows `/proc/swaps` either way; its unit
for the line reads active after LinuxIO's `swapon` and inactive after its
`swapoff` (read in the test VM, systemd 259 on Ubuntu 26.04 and 262 on
Fedora 45). What happens at boot is still systemd's, from the line.

`swapon` gets what the file's line asks for: `pri=<n>` becomes `-p <n>`, and
`discard`, `discard=once` or `discard=pages` becomes `--discard[=<policy>]`,
which set what systemd's `swapon -o <options>` sets at boot (the kernel log
showed the same flags both ways in the VM). Every other option is ignored. A
`pri=` that is not a priority from -1 to 32767, a second one, or a discard
policy `swapon` does not have is not passed on: the line's options are checked
before a resize or a drain turns the file off, and such a line refuses the
operation with `ErrConflict`, since the file could not be turned on again as
its line says. A `swapon` that fails with the path listed all the same counts
as done.

### The filesystems

LinuxIO makes a swap file, its own and the temporary file of a resize, and
resizes one, only on ext4, xfs and btrfs, which all take a fallocated swap
file (read in the VM on each). The kind is the folder's `statfs` magic
(`EXT4_SUPER_MAGIC`, `XFS_SUPER_MAGIC`, `BTRFS_SUPER_MAGIC`), and it must agree
with the type mountinfo gives the mount, since ext2, ext3 and ext4 share one
magic, and mountinfo names a path that could have changed since; the
filesystem must be writable; and btrfs must be able to hold a swap file.
Otherwise the reason is `unsupported_filesystem`, `read_only` or
`btrfs_layout`.

**btrfs.** The kernel takes a swap file on btrfs only on a filesystem of one
device whose data block groups are all single, and only a file that is NOCOW,
not compressed and without holes (`btrfs_swap_activate`). An active swap file
keeps the subvolume that holds it from being snapshotted ("cannot snapshot
subvolume with active swapfile"), and its block groups are not scrubbed or
balanced, as the kernel log says at each `swapon`. `btrfs.SwapfileLayout`
(`backend/common/btrfs/swapfile.go`) reads the device count and the data
profiles through the folder's handle with two ioctls that need no privilege,
so the status answers a session without sudo
([Btrfs](./btrfs.md#for-other-features)).

NOCOW is set on the new, empty file (`setNoCow`): `FS_IOC_GETFLAGS`, and when
the file has inherited `FS_COMPR_FL` (from a folder with `chattr +c`) or has
`FS_NOCOMP_FL`, a first `FS_IOC_SETFLAGS` that clears both, since btrfs refuses
a change of NOCOW beside them with `EINVAL`; then a second that sets
`FS_NOCOW_FL` and `FS_NOCOMP_FL`, and a read back, since btrfs sets NOCOW only
on an empty file and otherwise sets nothing without an error. The kernel reads
and writes a 4-byte int whatever size the request number encodes. A resize
reads the flag again before it changes a file and refuses one that lost it.
The request numbers and flag values are pinned by a test and were read in the
VM's kernel headers.

**`mkswap`** writes the header. It is in util-linux everywhere, and where
SELinux is on it labels the file `swapfile_t` (read on Fedora 45), which
LinuxIO's policy keeps across a relabel; the label is not what lets the file
swap ([SELinux](./process-systemd-architecture.md#selinux)). It runs after
every change of length: a grown file without a new header swaps at its old
size, and a shrunk one is refused by the kernel (read in the VM).

### Create

`memory.create_swap_file` makes LinuxIO's swap file,
`/var/lib/linuxio/swap/swapfile`, at the size the request gives. That is the
only path LinuxIO makes a swap file at, one file at a time: `create` reads
`exists` while it is a row. The admin does not choose the folder; a swap file
on another disk is made there by hand once, with its fstab line, and LinuxIO
then resizes and removes it like any other that passes the strict check.

- **The folder.** `/var/lib/linuxio` is LinuxIO's state folder,
  `0755 root root` (`packaging/systemd/linuxio-tmpfiles.conf`). `swap` in it
  is made `0755` when it is missing, so a session without sudo can see the
  file for the status; the file is `0600 root root`.
- **On btrfs the folder is a subvolume of its own**, made through the handle
  of `/var/lib/linuxio` (`btrfs.CreateSubvolumeAt`). A snapshot leaves nested
  subvolumes out (read in the VM), so the swap file neither blocks a snapshot
  of the subvolume that holds `/var/lib` nor lands in one. An empty plain
  folder at the name is removed and replaced by a subvolume (the kernel
  refuses to remove a folder that is not empty); a plain folder with anything
  in it, or anything at the name that is not a folder only root can change,
  is `path_taken`. On Fedora `/var` is a subvolume of its own, and the swap
  subvolume is made in it.
- **The SELinux label of the new subvolume.** A new btrfs subvolume has no
  label of its own (`ls -Z` shows `?`), and what is made in it reads
  `unlabeled_t` (read on Fedora 45). So the subvolume LinuxIO makes is given
  the context of `/var/lib/linuxio`, read and written through the two handles
  (`labelLikeParent`). A folder without a context, or a kernel or
  filesystem without SELinux, leaves it as it is; a label that cannot be set
  is a Warn log line, and the file swaps all the same.
- **Size.** In whole MiB, at least 256 MiB, or `ErrInvalidArgs` before the
  change waits for the lock. The dialog starts at `create.default_size`, half
  of RAM, at least 1 GiB and at most 8 GiB as zram's planned size, and never
  above `create.max_size`; it is a starting value, not a limit. The limit is
  the disk: the filesystem's free space (`f_bavail` times the block size) must
  hold the file and still leave `create.reserve`, 1 GiB or 5% of the
  filesystem when that is more. `create.max_size` is the largest size that
  leaves it, and a larger request is `ErrInvalidArgs`. On btrfs `statfs` is an
  estimate: a `fallocate` that meets `ENOSPC` removes the file and fails with
  `ErrConflict`.
- **`create.blocked_reason`**, checked in this order: `exists`;
  `folder_not_root_only`, a folder on the path is a link or others can change
  it; `path_taken`; `unsupported_filesystem`; `read_only`; `btrfs_layout`;
  `not_enough_space`, the filesystem has no room for 256 MiB beside the
  reserve. The status judges the nearest folder at or above
  `/var/lib/linuxio/swap` that exists; the change judges again on handles,
  and once more on the swap folder itself, since another filesystem mounted
  on it is where the file would go. `create` also carries the path, the
  filesystem, the free space and the reserve, so the dialog can say why.

The steps (`createSwapFile` in `operations.go`), one progress step,
`create_swap_file`:

1. Refuse with `ErrConflict` when a fresh status's `create.blocked_reason` is
   set.
2. Delete a swap file a Create that was cut short left at the path: a regular
   file root owns, with a swap signature, that no fstab line names and that
   does not swap. Anything else there is `path_taken`.
3. Make the folder, then the swap file.
4. Add the line `/var/lib/linuxio/swap/swapfile none swap sw,nofail 0 0` and
   reload systemd.
5. Turn it on.

A failure at step 4 or 5 removes the line and then the file, in that order, so
a line never names a missing file; a line that cannot be removed keeps the
file. Once the file swaps it stays, since added swap is never undone. Once
the file is made, the rest is not cancelled with the request. The Info line
is `swap file created`, with `path` and `size`.

**`nofail`** makes `swap.target` want the file's unit instead of requiring it:
the fstab generator requires the unit of a swap line without it. A file that
is missing at boot, as after a rollback of the subvolume that holds
`/var/lib`, then does not keep `swap.target`, and zram with it, from starting;
its unit fails and `systemctl is-system-running` reads `degraded` (both read
on Ubuntu and Fedora in the VM). LinuxIO adds only this one line, leaves a
line that is already there, and refuses a path fstab would need escaped.

### Resize

`memory.resize_swap_file` changes the size of a swap file in place: same
folder, same name, same fstab line, so it never writes `/etc/fstab`. The
kernel refuses to change the size of a swap file that is on (`ETXTBSY`), and
to rename or delete one (`EPERM`), on ext4, xfs and btrfs (read in the VM). So
a resize turns the file off, changes it and turns it on again, and the
question is only what the host swaps on meanwhile.

It is offered for a row that passes the strict check, swaps and is not a
temporary file (`resize_blocked_reason` empty). The request's `path` selects
that row on a fresh status; `size` is whole MiB, at least 256 MiB, at most the
row's `max_size` and not the file's present length, or `ErrInvalidArgs`. A
file that is off, or turns out to be off when the resize reads it, is
refused: it can only be removed.

The steps (`resizeRun.do`):

1. **`make_temporary_swap`, when needed.** When the file is the only active
   swap device, or its data does not pass the room check without help,
   LinuxIO makes `<path>.linuxio-resize` beside it, in the same folder, with
   no fstab line, and turns it on. Its size is the data parked on the file
   plus a quarter, at least 256 MiB and at most the file's length, in whole
   MiB (`resizeTemporarySize`).
2. **`resize_swap_file`.** Turn the file off, after the room check on a fresh
   status, which now counts the temporary file's free space. Then, through the
   file's handle, opened with `O_NOFOLLOW` and refused unless it is a regular
   file root owns, the file that was checked and off: on btrfs read NOCOW;
   `fchmod 0600`; then, to grow, `fallocate` and `fsync` and then `mkswap` on
   the path, and to shrink, `mkswap` with the new size first and then
   `ftruncate` and `fsync`. `mkswap` is run once more when the first run
   fails. Turn the file on with the options of its fstab line.

   The order keeps the header from ever saying more than the file holds: a
   crash in the middle of a grow leaves the old header on a longer file,
   which swaps at its old size, and in the middle of a shrink the smaller
   header on a file not yet cut, which swaps at its new size. A header longer
   than its file is what the kernel refuses (read in the VM).
3. **`remove_temporary_swap`.** Turn the temporary file off, after its own room
   check, and delete it.

**`max_size`** (`resizeMaxSize`): the filesystem must hold the temporary file,
when one is needed, and the growth, and still leave the reserve of Create. A
shrink that needs no temporary file needs no room, so it is allowed on a disk
fuller than the reserve allows; a resize that needs a temporary file the disk
has no room for has a `max_size` of 0.

What a failure leaves:

| Fails | Left |
|---|---|
| Making or turning on the temporary file | Nothing changed; the temporary file is deleted |
| Turning the file off: no room (`ErrConflict`), a `swapoff` that fails or is cancelled | The file swaps at its old size; the temporary file is turned off and deleted |
| Growing (`ENOSPC` is `ErrConflict`), a file that lost NOCOW or is no longer the file that was checked | The file is cut back to its old length with its old header and turned on again at its old size; then step 3 |
| `mkswap` twice, the cut or the sync of a shrink, or cutting a failed grow back | The file is turned on again when its header lets it; when it cannot be, the error says that it is off and that the host swaps on the temporary file, which stays on. The row then reads Not a swap file or Off, with Remove |
| Turning the file on | The file is off; the temporary file stays on, since added swap is never undone, and the error says the host swaps on it |
| Step 3: no room, or cancelled | The file has its new size; the temporary file stays on, and the error says so |

Once the file is off, bringing it back is not cancelled with the request;
taking the temporary file away is a `swapoff` like any other, and a cancel
leaves it on. The Info lines are `temporary swap file added`,
`swap file resized` (`path`, `size`, `previous_size`) and
`temporary swap file removed`.

**A leftover temporary file.** One that does not swap, as a crash or a reboot
leaves it (it has no fstab line), is not listed. It is deleted at the start of
the next Resize or Remove of its file (`leftover temporary swap file deleted`)
when it is a regular file root owns that holds a swap signature; anything
else at that name refuses the Resize or Remove with `ErrConflict` and is
left. One that swaps is a row with `temporary` set and Remove only, which
turns it off and deletes it; a Resize of its file uses it instead of making
another and removes it at the end; a Remove of its file is refused while it
swaps; a switch to zram removes it before its file.

### Remove

`memory.remove_swap_file` (`removeRow`), one progress step,
`remove_swap_file`, for a row that passes the strict check:

1. Turn the file off, after the room check on a fresh status; no room is
   `ErrConflict`. A file that is off skips this.
2. Remove its fstab lines, every swap line that names the path, and reload
   systemd. A temporary file has none.
3. Delete the file. A missing file skips this. `/etc/fstab` is read again
   first, and a file whose line is back, as another program that rewrites
   the file without LinuxIO's lock can put it back, is kept.

- **The fstab line goes before the file.** A crash between the two leaves a
  file that is off and that nothing turns on, which wastes disk and nothing
  else. The other order would leave a line that names a missing file, whose
  unit fails at every boot.
- **Removing the last swap is allowed.** The dialog says that the host then
  has no swap and that Linux closes programs when memory runs out.
- Before step 1, an off temporary file beside the row is deleted as
  [Resize](#resize) says, and one that swaps refuses the Remove with
  `ErrConflict`: without the row it stands in for it would read as
  `not_in_fstab`, and could not be removed.
- Once the file is off, the rest is not cancelled with the request. The
  file's folder stays, LinuxIO's subvolume included. The Info line is
  `swap file removed`, with `path`, `size` and `was_active`.

### Move

A swap file on btrfs blocks snapshots of the subvolume that holds it while it
swaps, and that cannot be fixed in place: a snapshot taken while the file is
off shares its blocks, and `swapon` then refuses the file ("swapfile must not
be copy-on-write") until the snapshot is deleted (read in the VM). So
`memory.move_swap_file` makes LinuxIO's swap file, in its subvolume, at the
size of the old one, and then removes the old one: swap is added before any is
removed. It has two progress steps, `create_swap_file` (Create, at the old
file's length rounded up to whole MiB, at least 256 MiB) and
`remove_swap_file` (Remove, on a status read again; a file that has gone
meanwhile is left). A removal that fails leaves both swapping, and the error
says so.

`blocks_snapshots` is set for a swap file on btrfs outside LinuxIO's folder.
Move is offered (`can_move`) for an active row with it that passes the strict
check and is not temporary, on the filesystem LinuxIO's folder is on (the same
mount source and type), while Create is offered and has room for a file of
that size beside the old one. For a file on another filesystem the dialog
shows the line without the action, since a move would take the file off the
disk the admin chose. Nothing moves unless the admin presses Move.

### Turning a swap file off

A resize, a removal, a switch to zram and a drain take a swap file away by
running `swapoff` on its `/proc/swaps` path themselves (`swapOffDevice`,
`swapoff.go`). systemd runs the same `swapoff` for a stop, but kills it after
the unit's timeout, 90 seconds by default, and a full swap file on a slow disk
takes longer than that to read back. Run by LinuxIO it has as long as the disk
needs. Nothing bounds it but the request.

- **It is a process of its own**, util-linux's `swapoff`, and not a
  `swapoff(2)` call from the job host. While the pages come back the kernel
  makes the caller the first process it kills when memory runs out
  (`set_current_oom_origin` in the kernel's `mm/swapfile.c`), and that has to
  be a helper and not the job host, which runs every job. The room check is
  made once, before the call, so memory other programs take during a long
  `swapoff` can still run out; the helper is then killed and the change
  fails.
- **A `swapoff` that is killed, or cancelled with the request, leaves the
  file swapping.** `swapoff(2)` gives up when a signal is pending and the
  kernel puts the device back, with the pages already read in memory
  (`try_to_unuse` and the reinsert in `mm/swapfile.c`). A cancelled request
  kills the helper. The kernel looks for the signal between one process's
  memory and the next, not inside one, so the cancel takes effect when the
  process being read back is done: at once for many small processes, after
  all of its parked data for one large one.
- **systemd follows.** It watches `/proc/swaps`, and marks the unit of a
  device that left inactive (`swap_process_proc_swaps` in systemd's
  `src/core/swap.c`).
- **A file that left counts as off**, whatever `swapoff` returned.
- **The path must be one only root can redirect** (`rootOnlyPath`).
  `swapoff(2)` looks the path up again, so in a folder another user can write,
  a link put at the path, or at the name a deleted file is listed under
  (`<path> (deleted)`), would send the call to another active swap device,
  which would leave without the room check made for this one. Every folder on
  the path must be a folder, not a link, that root owns and no group or
  everyone may write; otherwise `swapoff` is not run, the file keeps swapping
  and the change fails with `ErrConflict`. The strict check applies the same
  test first, so this holds only when a folder changes between the status and
  the call.
- The path must be absolute, as every `/proc/swaps` path is: `swapoff` reads
  `LABEL=` and `UUID=` arguments as tags.

The job host runs as root with no capability or system call filter
(`packaging/systemd/linuxio-jobs@.service`), and its `OOMPolicy=continue`
keeps a killed helper from taking the other jobs with it.

## Changing the mode

`memory.set_swap_mode` takes `mode`, `disk`, `zswap` or `zram`; any other
value is `ErrInvalidArgs`. `confirm_replace` names the zram configuration
file that is not LinuxIO's the user agreed the change may replace (see
[A zram configuration file that is not LinuxIO's](#a-zram-configuration-file-that-is-not-linuxios)).
`confirm_remove` is, for zram, the paths of the swap files the user agreed the
switch removes. `swap_file_size` is, for disk or zswap on a host with no disk
swap, the size of the swap file the switch makes: 0 for
`create.default_size`, otherwise whole MiB and at least 256 MiB, or
`ErrInvalidArgs` before the change waits for the lock. It runs as a task and
reports each step as it starts (`SwapProgress`: step code, 1-based index of
total, and the swap file a step acts on). The dialog shows the step's message
while it runs. Its result (`SwapModeResult`) is the status after the change
plus `left`: what the change left as it was and why (see
[What a change leaves as it was](#what-a-change-leaves-as-it-was)).

The status after a change, here and for every other change route, is read up
to five times, 250 ms apart (`statusAfterChange`). The route read it fine
before the change, so a read that fails now most likely met files under
`/proc` and `/sys` while the change still moved them. A read that still fails
fails the request with an error that says the change was made; nothing is
undone, and the Info line for the change is still logged.

It refuses before anything changes, with `ErrConflict`, a mode the host cannot
take: zram without a zram tool; zswap on a kernel without it; zswap or disk
with no active swap file or partition while a swap file is in fstab and off
(LinuxIO does not turn it on: remove it, and LinuxIO makes its own), or while
LinuxIO cannot make its swap file (`create.blocked_reason`); with
zram-generator, zswap or disk while another of its files defines a device (see
[Other zram-generator configuration](#other-zram-generator-configuration)); and
zram when `confirm_remove` does not name exactly the swap files the switch
removes, in any order, so a file is never removed that the confirm did not
show (`confirmedRemove` in `plan.go`). A `swap_file_size` above
`create.max_size` is `ErrInvalidArgs`. It also refuses a run that would
replace the provider's configuration file while that file is not LinuxIO's
and the request does not name it in `confirm_replace`. The plan makes these
checks under the mutation lock, on a status read for the request; that one
reading of `/etc/fstab` gives the rows the plan acts on and the options a
file is turned on with (`readView`). Each step that removes swap reads
`/proc/swaps`, memory and the rows again just before it (see
[Room](#room-for-what-comes-back)).

### Order of steps

The planner (`planSwapMode`) always **adds swap before it removes any**, and
checks each step before the next, so a failure leaves the host no less safe
than it was.

To `zram`:

1. `write_config`: write the zram provider's configuration, after keeping a
   copy of one that is not LinuxIO's, and the sysctl drop-in.
2. `start_zram`: set zram to start at boot and make it swap at the planned
   size, waiting for a zram device in `/proc/swaps` (up to a minute). What
   "at boot" and "its unit" mean depends on the
   [zram provider](#the-zram-provider). On a status read in the step:
   - no zram swaps: the unit is started. `zramswap.service` that reads active
     with nothing swapping, as after a `swapoff` by hand, is restarted, since
     a start would do nothing;
   - zram swaps at the planned size (in whole MiB, as the configuration
     gives it): nothing is done, since a restart empties it;
   - zram swaps at another size, as the device zram-tools starts when its
     package is installed outside Capabilities (an install from Capabilities
     starts none, see [The zram capability](#the-zram-capability)):
     the unit is stopped, waited for and started again,
     so the configuration just written applies. This takes zram away for a
     moment, so it is done only when that zram is the provider's one device,
     a swap file or partition is active, and zram's data fits (see
     [Room for what comes back](#room-for-what-comes-back)). Otherwise zram
     keeps its size until its unit next restarts, and the result says so.
     Once zram is stopped, bringing it back is not cancelled with the
     request, and a stop that fails or is not waited out queues a start
     behind it, and one systemd reports over with zram still swapping does
     not, as in `stop_zram`. A device whose size cannot be read is left
     alone. A start that fails leaves the host on its disk swap, and the run
     fails.

   Before any start of zram, with either tool, a `zram0` that is set up but
   is not in `/proc/swaps`, read just then, is reset through
   `/sys/block/zram0/reset` (`resetStaleZram`). Both tools can leave it that
   way: `zramswap stop` when it cannot unload the zram module, which the test
   VM shows it often cannot right after its own `swapoff` ("Module zram is in
   use"), and zram-generator when the reset its setup service runs as its
   swap unit stops meets the device still held right after the `swapoff` and
   fails with "Device or resource busy", which the test VM showed on the third
   stop in a row, leaving the setup service `failed`. With zram-tools the
   kernel then refuses a new size, and `zramswap start` swaps on the device
   at the size it has. With zram-generator the setup cannot set the device up
   at all ("Failed to configure compression algorithm"), and zram stays off.
   Only a device that holds swap, by the signature `mkswap` writes, is reset:
   one that holds anything else was set up by someone for another use, a
   start would format it, and the step refuses with `ErrConflict` instead. A
   device that swaps is never reset, and the kernel refuses the reset while
   something holds the device open, so it is tried again for five seconds;
   both are read again before each attempt, since another tool can start
   swapping on the device or format it meanwhile.
   With zram-tools a reset that still fails is logged and the start goes on;
   with zram-generator, which could not start, it fails the step, naming the
   device. A start queued behind a stop that may still take effect (see
   above) is preceded by the same reset with zram-generator once zram no
   longer swaps. A `failed` setup service needs nothing more: a start of the
   swap unit starts it again. With zram-tools, a hold that a job host killed
   during an install from Capabilities left in `/run`
   (`/run/systemd/system/zramswap.service.d/90-linuxio-memory.conf`, see
   [The zram capability](#the-zram-capability)) would make systemd skip the
   start, so right before it the step removes that file, and its folder
   when empty, and reloads, when the file holds exactly LinuxIO's hold
   (`clearLeftoverZramToolsHold`, Info line
   `leftover zram-tools install hold removed`); any other file there is left
   alone and the start goes on as it would. After the start the size is
   read again, and a device that came up at another size than planned is
   reported (`zram_size`, `other_size`).
3. `apply_sysctls`: write the preset live.
4. `disable_zswap`: turn zswap off and keep it off at boot (see below).
5. `remove_swap_file`, once for each swap file that passes the strict check,
   a temporary resize file before the file it stands in for
   (`zramRemovals`). On a fresh status: a file that has gone is skipped; one
   that no longer passes the check fails the run with `ErrConflict`, and the
   run rolls back; an active one is removed only while a zram device swaps and
   its data passes the room check, and otherwise stays, with its fstab line,
   and the result says why (`zram_not_swapping`, `not_enough_memory`). It is
   removed as [Remove](#remove) does: turned off, its lines taken out of
   `/etc/fstab`, deleted. A file that is off, or missing, only loses its line
   and the file.

To `zswap` or `disk`:

1. `create_swap_file`: when no swap file or partition is active and no swap
   file is in fstab, LinuxIO's swap file is made and turned on as
   [Create](#create) does, at `swap_file_size`. Otherwise the step is not
   planned: a swap file in fstab that is off is never turned on.
2. `stop_zram`: when a zram provider is installed, stop zram's unit, wait for
   zram to leave `/proc/swaps` and turn zram off at boot. zram that is not
   running is only turned off at boot. zram the provider does not run, because
   another tool set it up or systemd reports the provider's unit `inactive` or
   `failed`, is left swapping and nothing is turned off. Without a provider
   the step is not planned and zram is left alone. The stop is a systemd job
   that can still take zram away after the step gave up on it, with zram
   still set to start at boot, so a stop that fails or is not waited out (an
   error from the stop, a cancel, the wait's bound, a stop systemd never
   carried out, a read that fails) queues a start of the same unit behind
   it, on a context the cancel does not reach (`stopZramUnit`). A stop
   systemd reports over with zram still swapping (the unit `failed`,
   `active` again after `deactivating`, or `inactive`) queues nothing:
   nothing can take zram away later, and a start would run the provider's
   start against a device that is set up and swapping, so zram stays. Either
   way the step then fails and the run rolls back.
3. `apply_sysctls`: write the preset to the drop-in and live.
4. `enable_zswap` (zswap) or `disable_zswap` (disk).

A swap partition, an LVM volume and a swap file that fails the strict check
appear in no step.

Details that follow from the code:

- zswap's compressor is requested as `zstd`. Once zswap has started since
  boot, the kernel refuses a compressor it does not have with `ENOENT` (the
  test VM shows it on kernels 7.0 and 7.3), and one it cannot build a pool
  with with `EINVAL`; the mode then keeps the compressor the kernel has.
  Before zswap first starts after a boot the kernel takes any name, and falls
  back to its default when zswap starts. Either way the compressor is read
  back after enabling and that value is persisted.
- zswap is turned off only when it reads enabled. On a kernel where zswap
  failed to start, a write of `N` fails for nothing and would take the whole
  switch down.
- `zram` and `disk` write the tmpfiles entry as `enabled` `N`, whatever zswap
  reads at the time. A kernel built with zswap on, or booted with
  `zswap.enabled=1`, would otherwise return to zswap at the next boot, in
  front of zram too, and zswap reading off now says nothing about that. A file
  that already says `N` is not written again. On a kernel without zswap there
  is nothing to turn off and the file is removed.
- Leaving zram, the keys of the three sysctls only zram sets leave the
  drop-in, and each goes back live to the kernel's default (3, 15000, 10)
  when its value is LinuxIO's and the live value is still zram's. The value
  is LinuxIO's when the drop-in held the key before the run (it is read
  before the run rewrites it), the run wrote it there, or a zram device
  swapped as the run started: on a host that runs zram, zram's numbers are
  zram's, also where a settle wrote them live without the drop-in. A live
  value that equals zram's on a host that neither held the key nor ran zram
  is an admin's and stays.
- Every kernel parameter written is read back and must equal what was written.
- A vm sysctl the kernel does not have (`watermark_boost_factor` came in
  Linux 5.0) is not written live, as a preset or as a kernel default; the
  drop-in keeps its key.

### Room for what comes back

Taking swap away pushes its pages into RAM. Each removal checks on a **freshly
read status** taken just before it, not the status from the start of the
request:

- Turning a swap file off, for a resize and its temporary file, a removal,
  `remove_swap_file` and a drain: the file's used bytes plus a 512 MiB margin
  must be below `MemAvailable` plus the free space on the other active disk
  swap devices, since the kernel can push pages out to those while it reads
  the file's back (`roomToTurnOff`).
- Stopping zram, or restarting it at the planned size: the data in the zram
  devices (the larger of the device's swap use and its stored original size,
  which also counts pages filled with one value), with the same margin,
  below `MemAvailable` plus the free space on disk swap devices
  (`zramFits`).
- The drain's status check: the data on every file a drain turns off, below
  `MemAvailable` plus the free space of the disk swap it leaves alone.

The check errs on the safe side: pages zswap holds for a swap file count as
used on it, as `/proc/swaps` counts them, and free space in zram is not
counted as room. It is host-wide, so the memory limit of a cgroup whose pages
are on the file is not part of it (see [the backlog](./TODO/backlog.md)).

`remove_swap_file` also needs a zram device swapping in that same fresh read.
Zram was started earlier in the run, but if it has gone since, removing the
file would leave no swap at all, so the file stays and the run still
succeeds, with the host in disk mode.

### What a change leaves as it was

A step that cannot safely take a device away ends without an error and leaves
it. The request then succeeds, and its result lists each thing left in `left`,
as `what`, `reason` and, for a swap file, `path`. The dialog shows the reasons
in a warning in place of the success message, and keeps the chosen row so
Apply can be pressed again.

| `what` | `reason` | When |
|---|---|---|
| `swap_file` | `not_enough_memory` | The file's data does not fit in memory and free swap. Applying again later can finish. |
| `swap_file` | `zram_not_swapping` | No zram device swaps when the file would be removed. |
| `zram` | `not_enough_memory` | zram's data does not fit in memory and free disk swap. |
| `zram` | `not_managed` | The provider's unit does not run that zram (another tool set it up, the unit is `inactive` or `failed`, with `zram-generator` a device other than `zram0` swaps), or no provider is installed. |
| `zram_size` | `not_enough_memory`, `not_managed` | zram runs at another size than planned and cannot be restarted for that reason. |
| `zram_size` | `no_other_swap` | The same, and no swap file or partition is active: a restart would leave the host without swap meanwhile, and with none at all if the start failed. |
| `zram_size` | `other_size` | zram came up at another size than planned: a drop-in overrides the size, or the device could not be reset. Every later change would restart it again. |

A swap file left active beside zram makes the mode `mixed`, with its entry. A
switch away from zram that ends `mixed` always has a reason: `not_managed` for
zram is added after the run when no step left zram. `zram_size` does not make
the mode `mixed`. The success log line says what stayed, read from the status
after the change and not from what the steps left, so a device no step
reaches counts too: `swap_file_left_active`, a swap file LinuxIO can remove
that swaps after a switch to zram; `zram_left_active`, zram that swaps after a
switch to disk or zswap; and `zram_size_kept`, a `zram_size` entry in `left`.
Applying again helps only for `not_enough_memory`.

### Sysctls follow the mode the host ends in

The run writes the target mode's sysctls, and a rollback puts back the ones
from before. Neither is right when the host ends in another mode, which
happens because swap is never undone: zram that went away before a swap file
would be removed leaves disk swap only, and a switch away from zram that fails
after zram stopped does too. So after the last step, and after a rollback, the
status is read again, and when the host is in `disk`, `zswap` or `zram` and
the live sysctls are another mode's, that mode's preset is written live
(`settleSysctls`). This keeps swappiness 180 and zram's readahead off a host
that swaps to disk only, and disk's values off a host that ended on zram
only. A host left `mixed` keeps the sysctls it has.

Only the live values are settled. The drop-in stays as the run or the
rollback left it, because it belongs with what the next boot starts: the
zram unit's enablement and its configuration, as the run set them for the
target or the rollback left them. After a successful run a failure to write
the live values is logged and does not fail the request; after a rollback it
is joined into the error.

### Rollback

When a step fails the run undoes what it changed, last change first, on a
context that outlives a cancelled request, within 30 seconds, and joins every
failed undo into the error it returns:

- if the run set zram to start at boot, that is undone, so the next boot does
  not start zram with the configuration the rollback put back: `zramswap.service`
  is disabled again, and with `zram-generator` the restored configuration is
  followed by a systemd reload, so no generated unit outlives its file. zram's
  unit is also stopped when no zram device swaps, and zram that swaps stays.
  Once the run took a swap file's line out of `/etc/fstab`, this is not done:
  the file is not made again, so the zram configuration the run wrote and
  zram's start at boot stay, or the next boot would have no swap
  (`keepZramAtBoot`);
- each kernel parameter the run wrote goes back to the value it had before;
- each configuration file the run wrote or removed goes back to its previous
  content and mode, or is removed if it did not exist. The copy kept of a
  zram configuration file that was not LinuxIO's stays: it holds what the
  file held, which the rollback put back.

The previous content of a file, and the previous value of a parameter, is
recorded **before** it is written: a write can land and still report an error
(the directory sync after a rename, the close of a kernel file), and putting
the old content back is harmless when the write did not land.

Rollback **never undoes added swap**. Zram that is swapping and a swap file
`create_swap_file` made stay, and swap that a passed room check removed is
not brought back, a removed swap file included; only a zram stop the step
gave up on before systemd reported it over has a start queued behind it, by
the step and not the rollback (see `stop_zram`). A failed run therefore ends
with swap: what it started with, plus what it added, less what a room check
let it take away while other swap was active. The sysctls then follow the
mode the host ended in (see above).

### One change at a time

The seven change routes share a mutex (`mutationMu` in `executor.go`); a
second request while one runs fails with `ErrConflict` ("another memory change
is running"), and the dialog holds Apply, the drain button, the swap file
buttons and the slider while any change is pending. The lock covers the whole
host: the routes are privileged mutations, a mutation runs only in a job
host, and every admin login shares the root job host
([Jobs](./jobs.md#terms)), so every memory change, from any session, runs in
that one process.

An install of zram-tools from Capabilities, a privileged job in the same
process, takes the lock too, from just before its package step until the
step after it ends (see [The zram capability](#the-zram-capability)), so a
memory change during that install fails with the same `ErrConflict`, and
an install that starts while a change runs leaves the package's service as
the package sets it up and says so in its warning.

## Swappiness

`memory.set_swappiness` takes 0 to 200 (`ErrInvalidArgs` outside it). It writes
`/proc/sys/vm/swappiness`, checks it reads back, then merges `vm.swappiness`
into the drop-in, keeping other keys. A failed write restores the previous
live value, and a failed persist also restores the drop-in.

**TuneD.** TuneD sets swappiness itself on every profile switch and at boot,
so while it runs `set_swappiness` refuses with `ErrConflict` naming the
profile, the dialog shows the slider read-only with "Managed by the TuneD
profile <name>", and `set_swap_mode` leaves swappiness alone, live and in the
drop-in, while still applying the preset's other keys. A mode change under
TuneD therefore runs with TuneD's swappiness.

TuneD tells any user whether it runs (`is_running`) and its profile
(`active_profile`); only a profile switch asks for authentication. So the
bridge of a session without sudo, which reads the status as its user, sees
TuneD as root does (TuneD 2.28 on Fedora 45 and 2.25 on Ubuntu 26.04, in the
test VM). The changes do not rest on that read: all of them are privileged and
run as root, which reads TuneD again.

## Draining

"Bring back to memory" (`memory.drain_swap`) empties data parked on disk swap
by turning swap files off, which makes the kernel read their pages into RAM
(`swapoff`), and on again. zram is never drained: its data is already in
memory. A swap partition, an LVM volume, a swap file that fails the strict
check and the temporary file of a resize are never drained.

The files drained are the active swap files that pass the strict check and
hold data, in the order of the status (`drainTargets`), one at a time, for
each: the check of its fstab options (see
[The building blocks](#the-building-blocks)), the room check on a fresh status
and `swapoff` (see [Turning a swap file off](#turning-a-swap-file-off)), then
`swapon` with the options of its fstab line and a wait of up to a minute for
it in `/proc/swaps`. A file that has left `/proc/swaps` by its turn is
skipped. A failure at a later file says that the ones drained before it stay
drained. The success log line names the files drained (`devices`), and a drain
that drained none logs no success line. The dialog reports the file in
progress.

The route refuses a drain with `ErrConflict`, and the status predicts it with
`can_drain` and `drain_blocked_reason`, one of these, checked in this order:

| Code | Meaning |
|---|---|
| `nothing_parked` | no disk swap device has data on it |
| `other_swap` | the data is only on swap LinuxIO does not turn off: a partition, an LVM volume, a swap file that fails the strict check, or a temporary resize file |
| `not_enough_memory` | the data on the files a drain turns off, with the margin, does not fit in memory and the free space of the disk swap it leaves alone |

**A `swapoff` that fails or is cancelled leaves the file swapping**, so
nothing is turned on and the error names the file. Once a file is off,
bringing it back is not cancelled with the request, and the drain reports the
file off when it is not back within a minute.

## Waiting on systemd

zram is stopped through its provider's unit, since the unit also resets the
device, or tries to (see `start_zram`). Before a mode change stops zram, it
reads the unit's state. A unit that reads `inactive` or `failed` would not
take zram away, so no stop is issued and zram stays; this covers zram set up
by another tool. A state that cannot be read does not hold the stop back.
Swap files involve no unit (see
[The building blocks](#the-building-blocks)).

Waits are bounded: a minute for a swap file or a zram device to appear in
`/proc/swaps`, ten minutes for zram to leave, polled every 250 ms. systemd's
own stop timeout for zram's unit (90 seconds by default) is not raised:
emptying zram moves pages inside memory.

While waiting for zram to leave, the unit's state is watched too, and the
wait ends early with an error, zram still in `/proc/swaps`, when:

- the unit is `failed`, or `active` again after having been seen
  `deactivating`: systemd stopped waiting, as it does when it kills a stop
  command that outlived the timeout;
- the unit is `inactive`: the stop finished without taking the device away,
  as when `zramswap.service` stops and a zram device another tool set up still
  swaps;
- the unit stays `active` for 15 seconds without ever being seen
  `deactivating`: systemd did not carry out the stop.

The test VM shows the first: a `zramswap.service` whose stop outlives its
`TimeoutStopSec` reads `failed` once systemd has killed the stop, with zram
still swapping, and the wait ends then.

A device that has left always counts as stopped, so the device is read once
more before giving up, and a state that cannot be read is waited for. The
first two end the stop, so zram stays and nothing is queued; after the
third, or a wait that is cancelled or runs to its bound, the stop may still
take effect and a start of the unit is queued behind it (see `stop_zram`
above). Either way the error goes through rollback.

## Files written on the host

The four kinds of configuration files LinuxIO manages start with
`# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.`
and are mode `0644`. The copy of a zram file that was not LinuxIO's keeps
that file's content and mode, header or not. Every write is atomic
(`atomicfile.Write`, which refuses to write through a symlink).

| Path | Written by | Content |
|---|---|---|
| `/etc/sysctl.d/90-linuxio-memory.conf` | every mode, and `set_swappiness` | `vm.<name> = <value>` lines. Keys the admin added stay. Leaving zram drops `vm.page-cluster`, `vm.watermark_boost_factor` and `vm.watermark_scale_factor`. Under TuneD `vm.swappiness` is neither written nor dropped. |
| `/etc/tmpfiles.d/linuxio-zswap.conf` | every mode, on a kernel with zswap | For `zswap`, `w` lines setting zswap's `compressor` and `enabled` (`Y`) at boot. For `zram` and `disk`, one `w` line setting `enabled` to `N`, so a kernel that starts with zswap on does not return to it. |
| `/etc/default/zramswap` | `zram`, with zram-tools | zram-tools' configuration: `ALGO=zstd`, `SIZE=<MiB>`, `PRIORITY=100`. It is a package conffile, so the next package upgrade asks about the change. |
| `/etc/systemd/zram-generator.conf` | `zram`, and leaving zram, with zram-generator | For `zram`: one `[zram0]` section with `zram-size = <MiB>`, `compression-algorithm = zstd` and `swap-priority = 100`. Leaving zram: comments only, so the generator makes no device. Either way it replaces what was there, and takes the place of the file a package puts in `/usr/lib/systemd`. |
| `<path>.before-linuxio`, beside either zram file | a run that replaces that file while it is not LinuxIO's | The file as it was, with its mode. Not undone by a rollback, and replaced by the next such copy. |

The swap files and their lines:

| Path | Written by | Content |
|---|---|---|
| `/var/lib/linuxio/swap` | Create, Move, a switch to disk or zswap on a host without disk swap | A folder `0755 root root`; on btrfs a subvolume with the SELinux context of `/var/lib/linuxio`. Never removed. |
| `/var/lib/linuxio/swap/swapfile` | the same | LinuxIO's swap file, `0600 root root`, NOCOW on btrfs. |
| `<path>.linuxio-resize`, beside a swap file | Resize | A temporary swap file, `0600 root root`, with no fstab line, deleted when the resize ends. |
| `/etc/fstab` | Create adds `/var/lib/linuxio/swap/swapfile none swap sw,nofail 0 0`; Remove, Move and a switch to zram take out the swap lines of the file they remove | Every other byte stays as it was; written under the fstab lock with `atomicfile.Write`, mode `0644`, then a systemd reload. |

The swap file and its fstab line stay when the package is removed or purged,
as the rest of `/var/lib/linuxio` does, so the host keeps its swap
([Packages](./packaging.md#removal) has the commands that take them away).

One more file lives only while zram-tools installs from Capabilities, and
starts with its own header, `# Managed by LinuxIO (Settings, Capabilities);
removed when the zram-tools install ends.`:
`/run/systemd/system/zramswap.service.d/90-linuxio-memory.conf`, `[Unit]`
with `ConditionPathExists=!/`, which keeps the package's start of
`zramswap.service` from happening. It is removed, with its folder and a
reload, when the install's step after the package ends; one a killed job
host left is removed by the next switch to zram with zram-tools
(`start_zram`), and a boot clears it (see
[The zram capability](#the-zram-capability)).

### A zram configuration file that is not LinuxIO's

The two zram files are not LinuxIO's alone: `/etc/default/zramswap` is a
zram-tools conffile an admin may have edited, and an admin may have written
`/etc/systemd/zram-generator.conf`, with devices and zram mounts
(`fs-type`, `mount-point`) of their own. So a file there is replaced without
asking only when (`asksToReplace` in `persist.go`):

- it is absent;
- it is LinuxIO's: its first line is the header above;
- it holds nothing but comments and blank lines. A comment starts with `#`,
  and in zram-generator's INI file also with `;`; `/etc/default/zramswap` is
  a shell file, where `;` starts none. A byte order mark at the start and
  CRLF line ends are read past, for the header too;
- or it is the file as its package shipped it, which nobody edited: on a
  dpkg host the owning package's stanza in `/var/lib/dpkg/status` records
  each conffile's MD5 in its `Conffiles` field (path, checksum, flags), and
  the file's own MD5 equals it. The status file is read as a stream up to
  the stanza that decides, and no `dpkg` command is run. Only an installed
  package's stanza counts (its `Status` ends in `installed`): after a rename
  the old package can stay in `config-files` state, listing the file with
  its old checksum. An entry flagged `obsolete`, which the package no longer
  ships, a host without dpkg (rpm hosts) and a file no installed package
  owns have no record, and the rules above decide. This is what zram-tools' own `/etc/default/zramswap`
  is on a host where the admin only installed the package: it has settings,
  and the first switch to zram replaces it without asking and keeps no
  copy.

Any other is not LinuxIO's. The status reports the provider's file in
`foreign_zram_config`: its `path` and the `copy_path` its copy is kept at,
`<path>.before-linuxio`, which does not end in `.conf`, so neither tool reads
it. A file the session user cannot read counts as not LinuxIO's there, since
it may hold anything. A run that would replace the file needs the request to
name that path in `confirm_replace`, and is refused otherwise, with
`ErrConflict` naming it, under the mutation lock on a fresh status and before
anything changes (`confirmedReplace` in `plan.go`). That is a switch to zram
with either tool, which writes the configuration, and with zram-generator a
switch to disk or zswap, which writes the file with no device; zram-tools
only disables its service then. The dialog's confirm step names the file and
the copy, and sends the path. A `confirm_replace` that names anything but one
of the two zram files is `ErrInvalidArgs`, before the change waits for the
lock.

The run then copies the file it is about to replace, as root and as the file
is at that moment, to `<path>.before-linuxio` with its mode, through
`atomicfile.Write`, which refuses to write through a symlink, replacing an
older copy, and only then writes the file (`keepZramConfigCopy` in
`mode.go`). A folder, a link or anything else at the copy's name that is not
a regular file stops the run there, with the file as it was. A failed run
puts the file back, as it does every file it wrote, and the copy stays; the
run's error says where the copy is. The
Info line of the change, `swap mode changed`, names the file it replaced
and the copy in `zram_config_replaced` and `zram_config_copy`, only when it
replaced one.

Besides these, with zram-tools `zramswap.service` is enabled or disabled.

Never touched: the kernel command line (zswap persists through tmpfiles, not
`zswap.enabled=`), swap partitions and LVM volumes, a swap file that fails the
strict check, the fstab lines of anything but a swap file a change removes,
and any systemd unit of a swap file. zram writeback is not exposed.

## The zram provider

Two tools set zram up, and the status names the one in use in
`zram_provider`. The mode change does the same steps with either; what differs
is the file and the unit.

| | `zram-tools` | `zram-generator` |
|---|---|---|
| Where | Debian and Ubuntu | Fedora, RHEL 9 and later, Arch; `systemd-zram-generator` on Debian and Ubuntu |
| Found by | the `zramswap` command (`hostcmd.LookPath`, which also looks in `/usr/sbin`) | the file `/usr/lib/systemd/system-generators/zram-generator` |
| Configuration | `/etc/default/zramswap` | `/etc/systemd/zram-generator.conf` |
| Start at boot | enable `zramswap.service` | the configuration holds a device; a systemd reload makes `dev-zram0.swap`, wanted by `swap.target` |
| Start now | reset a `zram0` left set up, then start `zramswap.service` | reset a `zram0` left set up, then start `dev-zram0.swap`, which requires `systemd-zram-setup@zram0.service` |
| Stop now | stop `zramswap.service`, which may leave `zram0` set up | stop `dev-zram0.swap`; the setup service is bound to it and resets the device, which can fail while the device is still held |
| Off at boot | disable `zramswap.service` | write a configuration with no device, then reload |
| "The provider runs it" | `zramswap.service` is not `inactive` or `failed` | `systemd-zram-setup@zram0.service` is `active`, and no zram device but `zram0` swaps |

With one installed, that one is the provider. With both, it is the one whose
unit runs zram now (`zramProvider`): `zram-generator` when systemd reads
`systemd-zram-setup@zram0.service` `active` and `zramswap.service` `inactive`
or `failed` (the "provider runs it" row above), `zram-tools` otherwise, also
when neither runs, when `zramswap.service` reads any other state (`unknown`,
`activating`, `reloading`) or a state cannot be read, so a host that had
zram-tools keeps working as before. The status asks
systemd over D-Bus for this only when both are installed; the `zram`
capability needs only one installed and asks nothing.

The generator column follows upstream: the generator's README applies a new
configuration with `systemctl daemon-reload` and `systemctl start /dev/zram0`,
and Fedora turns its default zram off with an empty
`/etc/systemd/zram-generator.conf`.

With `zram-generator`:

- LinuxIO configures `zram0` alone. Fedora's default
  (`zram-generator-defaults`) is also `zram0`, so a Fedora host starts in
  `zram` mode and choosing it again writes LinuxIO's size and algorithm for the
  next boot.
- The off file is not written, and systemd not reloaded, when the file
  already says so. When a later step fails the rollback puts the previous file
  back and reloads; zram, stopped by then, stays stopped until the next boot.
- `stop_zram` takes zram away only with proof that the generator made it: the
  setup service reads `active`, where a state that cannot be read counts as no
  proof, because systemd has a `dev-zram0.swap` unit for a `zram0` anyone
  swapped on.
- An admin's own `/etc/systemd/zram-generator.conf` is replaced only once
  the user confirmed it, and a copy is kept (see
  [A zram configuration file that is not LinuxIO's](#a-zram-configuration-file-that-is-not-linuxios)).
- Its other configuration files are read, never changed (below).

### Other zram-generator configuration

zram-generator reads more than LinuxIO's file. As upstream's source does it
(`locate_fragments` in `src/config.rs` of
[systemd/zram-generator](https://github.com/systemd/zram-generator), and the
`scan` of the `liboverdrop` crate it calls, read 2026-11-18):

- one main `zram-generator.conf`, the first that exists of `/run/systemd`,
  `/etc/systemd`, `/usr/local/lib/systemd` and `/usr/lib/systemd`. LinuxIO's
  file in `/etc` therefore takes the place of the two below it, as it does of
  Fedora's default in `/usr/lib`, and one in `/run` takes the place of
  LinuxIO's;
- then the drop-ins, `*.conf` in `zram-generator.conf.d` under the same four
  folders, in order of file name whatever folder they are in. Of two with the
  same name the one in the later folder (`/usr/lib`, `/usr/local/lib`,
  `/etc`, `/run`) is read; a link to `/dev/null` hides the earlier folders'
  file of its name; a name that starts with a dot and an entry that is not a
  regular file, a link to another file included, are skipped.

Each `[zramN]` section defines a device, merged across the files in that
order. It is swap unless it sets a `mount-point` or an `fs-type` other than
`swap`, which make it a zram mount. Its `zram-size` defaults to
`min(ram / 2, 4096)` and may be an expression, and the older `zram-fraction`
and `max-zram-size` still set a size when `zram-size` is not set; a device
whose size comes to 0 is discarded, and `systemd.zram=0` on the kernel
command line keeps the generator from making any. The files are parsed with
the `rust-ini` crate (0.15 to 0.21, default features), whose comments start
a line with `#` or `;`: a comment after a section header, or anywhere but at
the start of a line, makes it reject the whole file, and the generator then
makes no device at all.

With zram-generator installed, whichever tool is the provider, the status
lists in `zram_generator_devices` each swap `[zramN]` section of the main file
in `/run` and of every drop-in that is read (`readZramGeneratorDevices` in
`generator_config.go`): the file's `path`, the `device`, and the size settings
as written, `zram_size`, `zram_fraction` and `max_zram_size`, each empty when
the file sets none. A header followed by a comment still counts, although
`rust-ini` rejects that file: it errs towards refusing a switch away from
zram. The main files in `/etc`, `/usr/local/lib` and `/usr/lib` are not
listed. Nothing else of those files leaves the bridge. A file larger than
1 MiB counts as one that cannot be read; in the status such a file is
skipped with a Debug log that carries the error. A drop-in folder that cannot
be listed, or is not a folder, is skipped everywhere, as upstream skips it,
with a Debug log.

The test VM checks this reading against the generator itself (zram-generator
1.2.1 on Fedora 45): with drop-ins in all four folders, one hidden by a link
to `/dev/null`, two of the same name and a zram mount, a reload makes a
`dev-zramN.swap` unit for exactly the devices the status lists, beside those
of the main file, and a `systemd-zram-setup@zramN.service` for each of them
and for the mount.

With zram-generator as the provider, a switch to disk or zswap is refused with
`ErrConflict` while any device is listed, naming the first file: the device
would come back at every boot whatever LinuxIO's file says, and the mode would
then read `zram`. The refusal says how to turn the file off: a package's
drop-in under `/usr/lib` or `/usr/local/lib` is hidden by a link to
`/dev/null` of its name in `/etc/systemd/zram-generator.conf.d`, and any other
file is the admin's, or made at runtime, to change. The change reads those
files as root, under the lock: a drop-in or `/run` main file it cannot read
may bring zram back as well, so it refuses the switch too, naming the file
and the error (`generatorRefusal` in `plan.go`). A switch to zram is allowed, and what
those files set wins over LinuxIO's file, so the dialog lists them with the
size settings each writes.

## The zram capability

`zram` is available when either provider is installed
(`CheckZramAvailability`). Install goes through PackageKit with the Debian
package `zram-tools` or the RHEL package `zram-generator`; neither has a
service step, since `start_zram` does that. zram-generator's package starts
nothing. zram-tools' package enables `zramswap.service` and starts it as it
installs, so where zram-tools is the package installed and was not
installed yet, the capability's package hook, `ZramToolsInstallHook`
(`install.go`), keeps the start from happening: under the mutation lock,
held across the package step, a runtime drop-in with
`ConditionPathExists=!/` makes systemd skip the package's start, and after
the package step, whatever its outcome, the hook disables the service the
package enabled, unless it started at boot before the install, and removes
the drop-in. An install that runs to its end starts no zram, so nothing is
stopped, and the dialog reads the host as it was before the install. A
cancel during the package step waits for PackageKit's transaction to end
before the step after it ([Capabilities](./capabilities.md#install-flow));
a transaction still running at the install's 30-minute bound is the
exception ([Capabilities](./capabilities.md#known-limits)). The hook
changes nothing, and says why in the install's warning, while another
memory change runs or a file LinuxIO did not write is at the drop-in's
path. See [Capabilities](./capabilities.md#zram) for the steps, why the
hold is a condition and not a mask, and what the test VM showed.

## The dialog

- A banner, worst condition first, with at most one action: no swap at all
  ("If memory runs out, Linux will close programs."), with Turn on compressed
  memory, or a link to Capabilities while no zram tool is installed; `mixed`
  ("Compressed memory and a swap file are both active. Choose one below and
  apply to finish."); data parked on disk, with Bring back to memory, disabled
  with the reason when `can_drain` is false (for `other_swap` it names the
  devices that hold the data); an active swap file on btrfs that blocks
  snapshots, with Switch to compressed memory when zram can be chosen and
  every such file is one a switch removes; or "Memory is fine. Nothing is
  parked."
- Under the banner, while `zram_generator_devices` lists any, a note
  (`generatorFilesNote`) with each file, its device and the size settings it
  writes (`zram-size`, `zram-fraction`, `max-zram-size`), or that it sets no
  size, since LinuxIO's size then applies to `zram0` after a switch to zram.
  With zram-generator as the provider the note says that what these files set
  wins over LinuxIO's settings; with zram-tools as the provider it says only
  that zram-generator is installed too and sets up compressed memory from
  them.
- "When memory runs out": a single-choice list of three rows, in the order
  zswap, zram, disk, each with a plain name, its technical name once, a
  description, a caution where one applies, and chips for the recommended and
  the current row. A row that cannot be chosen shows why; the zram row shows a
  link to Capabilities instead of a choice while no zram tool is installed,
  and names what stays on beside compressed memory (a partition, a swap file
  LinuxIO does not act on). The zswap and disk rows need disk swap: an active
  swap file or partition, or else LinuxIO's swap file, which the row says it
  creates first, at a size the admin chooses. They cannot be chosen while a
  swap file in fstab is off ("LinuxIO does not turn it on: remove it under
  Swap files to choose this") or LinuxIO cannot create its swap file (the
  reason); with zram-generator as the provider they also say why the plan
  refuses them while another of its files defines a swap device, naming the
  first, and how to turn it off, as the refusal does. Apply is enabled for a
  different row, or for the current one while the host is `mixed`.
- **Swap files**, under the rows (`swapFileViews`, `otherSwapViews`,
  `createView`): one row per swap file with its path and its size and use
  ("1 GiB, 200 MiB used"), or the state it is in ("Missing", "Not a swap
  file, 1 GiB", "Off, 1 GiB"); under it what the file is and why an action
  is missing: a temporary file a resize left on, a line that names a missing
  file, a file fstab names that LinuxIO does not turn on, the
  `locked_reason` ("LinuxIO does not resize or remove it: …"), or the
  `resize_blocked_reason` of a filesystem; for an active file that blocks
  snapshots, a warning line ("<path> blocks snapshots of the subvolume it is
  in."), with "LinuxIO can make it again in a subvolume of its own, at the
  same size." when Move is offered; and the buttons Resize, Remove and Move
  that apply. Then each other swap device, "Partition" or "Compressed
  memory", with its size and use and no buttons; "No swap is on, and no
  swap file is in /etc/fstab." when there is nothing. Last, **Create a swap
  file** while LinuxIO can make its file, or "LinuxIO cannot create a swap
  file: …" with the reason, or nothing while its file is a row.
- An action that needs agreement or a size asks first, as a second step of
  the same dialog and not a dialog on top of it, which lists all of it:
  - **A change of mode** (`confirmView`). To zram, when it removes swap files
    or replaces a configuration file: "Compressed memory is turned on.", each
    file the switch removes with its size ("<path>, 1 GiB, is removed.", or
    for a missing file that its line is taken out of `/etc/fstab`), that
    data parked in a swap file is brought back to memory before the file is
    turned off and deleted, and what stays on beside compressed memory; the
    button is Turn on compressed memory, and it sends the files in
    `confirm_remove`. To disk or zswap on a host without disk swap: that
    LinuxIO creates its swap file, adds it to `/etc/fstab` and turns it on,
    with the size field; it sends `swap_file_size`. Either way, a provider's
    configuration file that is not LinuxIO's and would be replaced (to zram
    with either tool, to disk or zswap with zram-generator), with where its
    copy is kept; it sends `confirm_replace`. Any other change of mode
    applies at once. The step shows what it showed when Apply was pressed,
    while a refresh or the change itself moves the host.
  - **Create**: the line about the file, `/etc/fstab` and turning it on,
    and the size field.
  - **Resize**: the file's present size, "To change its size, LinuxIO turns
    the file off and on again. Data parked in it is brought back to memory
    first.", and the size field, starting at the present size; Resize is
    held while the field holds that size.
  - **Remove**: what is done to the file (turned off, taken out of
    `/etc/fstab` and deleted; for a temporary file turned off and deleted;
    for a missing one its line taken out), that parked data is brought back
    first, and, when it is the host's only active swap, "The host then has
    no swap. If memory runs out, Linux will close programs."
  - **Move**: that LinuxIO creates its swap file at the old one's size in a
    subvolume of its own, then turns the old one off, takes it out of
    `/etc/fstab` and deletes it.

  The size field takes whole MiB, says "From 256 to N MiB. N MiB is the
  largest size the disk allows.", and says what is wrong with anything else.
  A file step is built from the status as it refreshes, so its limits and
  warnings follow the host; when the file changed meanwhile, or Create is no
  longer offered, the step says so and cannot start. A step without a size
  opens with focus on Back, and Back returns focus to the control that
  opened the step.
- Advanced, collapsed: a slider for swappiness from 0 to 200, with the caption
  "Recommended for <mode>: N". A drag is saved when the pointer is released
  or focus leaves the slider. Arrow keys are saved half a second after the
  last key, so a run of them is one write and one toast; leaving the slider or
  closing the dialog with Escape saves a waiting key press at once. A step
  made while a save runs is sent when that save succeeds, a step waiting when
  a mode change or a drain starts is dropped, and after a failed save the
  slider goes back to the host's value.
- The status is read only while the dialog is open, refreshes every 15
  seconds, and each of the change routes invalidates it.
- **Every change but swappiness is a job** ([Jobs](./jobs.md)). A second step
  closes as its job starts: the dialog goes back to its first step, with
  focus on Close, and holds its controls while the job runs, since the host
  refuses a second change; the running step's message shows beside Close.
  Close and Escape work: the task tray shows the job's progress and can
  cancel it. Closing drops an unapplied choice and keeps the one a mode
  change is applying, so the dialog shows that change when it is opened
  again, on its first step. A step of the slider made while its save runs is
  still sent.
- Whatever follows the job reports its result, once
  (`frontend/src/tasks/memoryFeedback.ts` holds the texts):
  - The dialog that started it follows it for as long as the tab is not
    reloaded, also after the dashboard is left, and toasts the result; a
    swap file job's toast names the file ("Resized /swap.img"). A dialog
    opened meanwhile sees that change in the mutation cache, holds its
    controls and does not follow the job a second time.
  - After a reload, or on another tab, a dialog that opens finds the running
    job and takes it up (`useActiveTaskRecovery`, then the mutation's
    `watch`): it holds its controls and toasts the result. A mode change's
    job metadata carries the requested mode as `action`, and a swap file
    job's the operation as `action` and the file as `path`, for that.
  - When no dialog follows the job, the task tray reports it, from the
    entries for the six routes in `terminalTaskFeedback.ts`. A dialog marks
    the job as reported when it starts to follow it, so the tray stays quiet.
  - A result toast fired on another page than the dashboard has an "Open
    Memory overflow" button that opens the dialog by its URL
    (`MEMORY_TOAST_META`).
- The dialog's URL is the dashboard's `?dialog=memory-overflow`, validated by
  `validateDashboardSearch` (`frontend/src/api/dashboard-search.ts`); an unknown
  value is dropped. The pencil opens the dialog by setting it, and Close and
  Escape take it out; both replace the history entry, so Back leaves the
  dashboard rather than opening or closing the dialog. A reload, a bookmark or
  a link opens it, for a session with sudo only; the choices made in it are
  not in the URL. The `zram` capability's install toast links to it ("Open
  Memory overflow") from any page but the dashboard
  ([Capabilities](./capabilities.md#zram)).
- The dialog's code loads the first time it is opened, and the dialog then
  stays mounted so that it can animate closed.

## Privilege and safety

Root creates, resizes and deletes files and writes `/etc/fstab` here, on
behalf of a session with sudo, in the root job host. What keeps that narrow:

- **Request input.** A size, a whole number of MiB checked against the
  status's limits, and a path that only selects a row of a freshly read
  status with exactly that path (`selectRow`): no row is `ErrNotFound`, a row
  with a `locked_reason` is `ErrConflict`, and the path is used for nothing
  else (the job's metadata keeps it as given, for the tray's text). `mkswap`,
  `swapon` and `swapoff` get the row's path, absolute and clean, after `--`,
  as arguments of their own (`hostcmd.LookPath`, a cancel ends them).
- **Another local user.** Every folder on a path LinuxIO acts on is one only
  root can change, checked on handles walked from `/`, so no name on it can
  be replaced between the check and the action, and the file itself must be
  a regular file root owns. A swap file in a folder another user can write,
  or behind a link, is listed and never touched.
- **Files are reached through handles.** A file is made with
  `O_CREAT|O_EXCL|O_NOFOLLOW` through its folder's handle; a resize opens it
  with `O_NOFOLLOW` and works on the handle, after checking its device and
  inode; a delete checks device and inode right before `unlinkat` through
  the folder's handle. The btrfs subvolume is made through the handle of
  `/var/lib/linuxio`.
- **`/etc/fstab`** is written by root elsewhere already (the storage pages,
  `btrfs/fstab_bound.go`). Memory takes the same lock,
  `/run/linuxio-fstab.lock` (`btrfs.FstabLockPath`), adds only its own line,
  removes only lines whose type is `swap` and whose first field, unescaped,
  is the row's path, leaves every other byte as it was, and writes
  atomically.
- **Deleting** happens only when the file is off and a fresh `/proc/swaps`
  does not list the path; the kernel refuses to unlink an active swap file
  anyway (`EPERM`). A swap file that is off holds nothing, so a delete loses
  no data.
- **Disk filling** is bounded by the reserve, for Create, the growth of a
  resize and the temporary file.
- **SELinux.** `mkswap` labels the file `swapfile_t`, and an `optional` file
  context in `packaging/selinux/linuxio.cil` keeps that label across a
  relabel; it gives no domain a permission
  ([SELinux](./process-systemd-architecture.md#selinux)).
- **No new socket, binary or word.** The four swap file routes are
  `Privileged()` mutations in the job host, like the others.

## Upgrading from an older LinuxIO

An older LinuxIO switched to compressed memory by turning the swap file off
and keeping it off at boot, with a drop-in for its unit
(`/etc/systemd/system/<unit>.d/90-linuxio-memory.conf`, a
`ConditionPathExists=!/`) or, in older releases still, by masking the unit.
There is no code that undoes either. On such a host:

- The file's row reads **Off**, with "/etc/fstab names it, and LinuxIO does
  not turn it on." and Remove. It stays off at every boot through the old
  drop-in or mask, and the host reads `zram`.
- Disk only and zswap cannot be chosen while it is there: LinuxIO does not
  turn it on, and does not make its own beside it. **Remove** deletes the
  file and its fstab line; after that a switch to disk or zswap makes
  LinuxIO's own swap file.
- Remove leaves the old drop-in, or the mask, which then hold off a unit no
  line makes any more. They are cleaned up by hand, for `/swap.img`, whose
  unit is `swap.img.swap`:

  ```sh
  sudo rm -r /etc/systemd/system/swap.img.swap.d
  sudo systemctl unmask swap.img.swap
  sudo systemctl daemon-reload
  ```

  `systemd-escape --path --suffix=swap <path>` names the unit of another
  path.

A host whose swap files were on (disk or zswap) needs nothing: each file is
a row with Resize and Remove.

## Tests

- Go unit tests in `backend/bridge/handlers/memory` run against fixture roots
  (a temporary tree standing for `/`, `/proc`, `/sys` and `/etc`) and a fake
  `hostSystem` that stands for systemd, files, kernel parameters and the swap
  commands. They cover the parsers, mode derivation and recommendation, the
  planner for every transition and refusal, each step, rollback (including a
  cancelled request, a write that lands and then errors, a stop that never
  completes, and zram at boot kept once a swap file's line is gone), the room
  checks, the fail-fast and slow-success waits, the reason reported for each
  thing left, zram restarted at the planned size or kept at its own, the live
  sysctls after a run that ended in another mode, the status read again after
  each route's change, the provider chosen by the unit that runs zram, and
  the success lines. For swap files they cover the strict check (a plain
  path, a link on the path, a folder that is `0775` or not root's, a file
  that is a link, a tag or `/dev` source, an escaped path, two lines for one
  path, the hibernation file only on its device); the rows of a file that is
  off, missing or without a header; the filesystem reasons, the sizes, the
  reserve and `max_size`; the temporary file's size and when it is made;
  Create (each refusal, `ENOSPC` and a failed `mkswap` removing the file, the
  fstab line's text, a leftover at the path, the btrfs subvolume and NOCOW in
  two calls, the folder that is there, the SELinux label copied or not);
  Resize (with and without a temporary file, each failure of the table, a
  leftover temporary file off and on, fstab unchanged); Remove (the order,
  no delete while `/proc/swaps` lists the path, a changed inode, a file that
  is off, a temporary file); Move; the fstab edits (only the matching swap
  lines change, every other byte kept, the lock, a path that would need
  escaping); `swapon`'s flags from `pri=` and `discard`; the handle walk;
  the drain of swap files only and `other_swap`; and a switch to zram with
  one, two and no removable files, a `confirm_remove` that does not match,
  a switch to disk that creates the file, and a partition named in no step.
  `zram_config_test.go` covers a zram configuration file that is not
  LinuxIO's, `generator_test.go` zram-generator's other files,
  `swapoff_test.go` and `swapon_test.go` run scripts in place of the
  commands (their arguments, what they printed in the error, a cancel that
  ends them), and `install_test.go` covers the zram-tools install hook. In
  `backend/common/btrfs`, `swapfile_test.go` pins `SwapfileLayout` and
  `CreateSubvolumeAt` against the fake kernel. Run with
  `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/memory`.
- Frontend: `memoryStatusView.test.ts` and `MemoryOverflowDialog.test.tsx`
  (the real generated client with the calls mocked) cover the rows and their
  reasons, the swap file rows and their notes, the size field's limits, the
  confirm and file steps, the btrfs line and the jobs; `Memory.test.tsx`
  covers the pencil on the dashboard card and the dialog's URL, all under
  jsdom. `src/test/browser/memory-overflow.spec.ts` runs in Chromium against
  the Memory card in the browser fixture (`fixture/routes/MemoryOverflowPage.tsx`,
  which has no bridge, so nothing is applied): the pencil and the URL, Escape,
  a reload with the URL, the rows as one choice with the arrow keys, the
  slider and its caption, the confirm step's focus and what it names, a
  Resize's size field, and the Remove step of the host's last swap. Run it
  with
  `make test-frontend-browser-quiet PLAYWRIGHT_ARGS=src/test/browser/memory-overflow.spec.ts`.
- The root tests change `/etc`, swap and zram on the machine they run on, and
  put it back only through their own cleanup, so they are for the disposable
  test VM only. `make vm-test-memory-integration` runs them
  ([Test VM](./development.md#test-vm) has what the suite sets up and in
  which order; `suite_memory` in `packaging/vm/linuxio-vm.sh`). They drive
  the routes' own functions (`CreateSwapFile`, `ResizeSwapFile`,
  `RemoveSwapFile`, `MoveSwapFile`, `SetSwapMode`, `DrainSwap`,
  `GetSwapStatus`), so the real status, strict check and room check run, all
  with the job host's umask 077. The rig fails a test the suite names that
  does not run or that skips. A test that needs a swap file or a provider
  the VM lacks skips saying what was missing; each waits on a condition with
  a bound, never a bare sleep. The tests that change swap files take a
  baseline first and put it back: a swap file they removed is made again
  with its fstab lines as they were, one they resized gets its size back,
  and LinuxIO's file is removed when they made it.
  - `TestSwapFileBlocksIntegration` (`swapfile_blocks_integration_test.go`)
    takes a swap file through every block under the routes: made, its fstab
    line, on, off, grown, shrunk, its line removed and deleted, in LinuxIO's
    folder (a subvolume on btrfs, labelled like `/var/lib/linuxio`), beside
    the files in `/` with `pri=7,discard=once`, and on btrfs in a folder
    with `chattr +c`. It also stops a shrink between its two steps: with
    the smaller header written (`mkswap -- <path> <KiB>`) and the file still
    at its old length, the file swaps at the new size, and again after the
    cut.
  - `TestMemoryIntegration` (`integration_test.go`) starts from `/swap.img`
    in disk mode. A switch to zram with no `confirm_remove`, with one naming
    a file the switch does not remove, and with one missing a file, is each
    refused naming the files, and `/etc/fstab`, `/proc/swaps`, the files and
    LinuxIO's managed files are byte for byte as they were. The switch to
    zram then checks zswap off, swappiness 180 (unless TuneD runs), the
    three zram sysctls, zram at the planned size, `zramswap.service`
    enabled with `ALGO=zstd` or the `[zram0]` configuration with `zstd` and
    its setup service active, and each removed file gone from
    `/proc/swaps`, `/etc/fstab` and the disk with its unit not active. The
    switch back to disk makes LinuxIO's file at the default size, which
    swaps with its generated unit active, with no zram swapping and the
    provider off at boot. Then, as before this change: zram at 64 MiB
    beside the swap file ends at the planned size; a stop with `zram0`
    held open leaves the device set up and the next switch resets it; an
    admin's configuration, mode 0640, is refused without `confirm_replace`,
    a folder at the copy's name stops a confirmed switch at `write_config`
    with the swap files still swapping, and a confirmed switch keeps the
    copy with mode 0640; with zram-tools a stop that systemd gives up on
    (`TimeoutStopSec=5`) fails a switch to disk within a minute with
    `errStopAbandoned`, the unit `failed`, zram swapping and LinuxIO's file
    the switch made first still on; with zram-generator a drop-in device
    is listed and refuses a switch to disk, and the generator's units after
    a reload match the devices the status lists; last the zswap round trip
    with its tmpfiles entry, and a compressor the kernel does not have
    refused with `ENOENT` or `EINVAL`. The test starts and stops zram
    faster than systemd's start limit allows (five starts in ten seconds,
    and zramswap's stop fails whenever it cannot unload the module), so it
    clears the units' failed state before each switch.
  - `TestSwapFileResizeIntegration` (`swapfile_ops_integration_test.go`)
    parks 384 MiB of data that does not compress on `/swap.img`, the only
    swap, from eight helper processes. It grows the file from 1 GiB to
    1.5 GiB, which makes the temporary file (480 MiB for 384 MiB parked)
    and removes it; cancels a shrink, with the test's reads from the disk
    capped at 2 MiB a second, once `swapoff` has read some of the data
    back, which must end with the cancel, the file swapping at 1.5 GiB and
    no temporary file swapping or on disk; then, beside a 64 MiB zram with
    room, shrinks it back to 1 GiB with no temporary file. Each time the
    file is the same inode, `/etc/fstab` is byte for byte the same,
    `/proc/swaps` lists the new size and its unit is active, and every
    helper finds its data unchanged by its checksum.
  - `TestSwapFileLeftoverTemporaryIntegration`: a temporary file left off
    beside `/swap.img` is deleted by the next Resize, and one beside a
    second hand-made file by the next Remove of that file (the Info line
    names it); one left on is an active `temporary` row LinuxIO can remove,
    Remove of `/swap.img` beside it is refused, and Remove of the temporary
    file turns it off and deletes it with `/etc/fstab` unchanged.
  - `TestSwapFileRemoveMissingIntegration`: a row whose file is missing
    (`missing`) is removed by taking out its line alone; the rest of
    `/etc/fstab` stays byte for byte.
  - `TestSwapFileMoveIntegration`: on a btrfs root (Fedora), `/swap.img` in
    the subvolume of `/` blocks snapshots and offers the move; a snapshot of
    `/` is refused with `Text file busy`, Move makes LinuxIO's file at the
    same size and removes `/swap.img` and its line, and the snapshot then
    succeeds. On ext4 it checks that the row offers no move and blocks no
    snapshot, and skips.
  - `TestMemoryStatusAsRootIntegration` and
    `TestMemoryStatusWithoutRootIntegration`
    (`unprivileged_integration_test.go`): root's status is written to a
    file, and a system user without sudo, with `PATH=/usr/bin:/bin` and
    TuneD running, must read the same rows (active, in fstab, locked
    reason, temporary, size, filesystem, snapshots, move, resize reason)
    except that a 0600 file of root's without a swap header, which root
    reads `no_swap_header`, reads `off`; the same Create (reason, path,
    filesystem, sizes, reserve; the largest size within 256 MiB); the same
    mode and recommendation; TuneD running with its profile; and the zram
    provider, zramswap through the `/usr/sbin` fallback.
    `TestReadFolderFSWithoutRootIntegration` runs as the same user: on
    btrfs the layout ioctls answer it.
  - `TestSwapPartitionIntegration`: beside the data disk `vdb` used whole
    as a swap partition at priority 10, it is never a row, keeps its
    priority, size and fstab line through every operation, and with data
    parked only on it a drain is refused with `other_swap`. `/swap.img` is
    resized beside it without a temporary file, LinuxIO's file is made and
    removed, zram is chosen beside it and the host reads `zram`, not
    `mixed`, and the way back to disk makes no file, since the partition
    is disk swap.
  - `TestMemoryDrainIntegration` (`drain_integration_test.go`) parks data on
    `/swap.img` from eight helper processes in a cgroup of its own, which
    is told to reclaim (`memory.reclaim`, written with system calls: it
    answers EAGAIN when it reclaimed less than asked, and Go's
    `os.WriteFile` would wait on that pollable file for good) until the
    data is on the file and out of the swap cache; zram ahead of the file
    is stopped for the test and started after it. It cancels a drain once
    `swapoff` has read some of the data back, which must leave the file
    swapping with most of the data parked and its unit active, then drains
    in full: the file empty, swapping, its unit active, and every helper's
    data unchanged. It runs with 768 MiB, and with 320 MiB and the reads
    from the disk under the file capped at 2 MiB a second (`io.max` takes
    the whole disk, not the partition), where the drain must take more than
    90 seconds.
  - `TestMemoryDefaultZramIntegration` (`boot_integration_test.go`) starts
    from the zram a distribution turns on with zram-generator, the vendor
    file `/usr/lib/systemd/zram-generator.conf` alone with `zram0` swapping
    at its size, and skips on any other host. The status must read `zram`,
    or `mixed` beside `/swap.img`, with zram-generator as the provider and
    no other file listed. Without disk swap the recommendation is `zram`, a
    switch to zram writes LinuxIO's configuration for the next boot and
    keeps zram at its size (`zram_size`, `no_other_swap`), and a switch to
    zswap with `swap_file_size` 512 MiB makes LinuxIO's file at that size
    before it stops zram. Beside `/swap.img` a switch to zram removes the
    file and restarts zram at the planned size, and a switch to disk makes
    LinuxIO's file at the default size and stops zram. The suite runs it
    before and after it adds `/swap.img`.
  - `TestSwapFilesRebootIntegration` runs in three phases around two
    reboots. `create`: Create at 512 MiB adds exactly
    `/var/lib/linuxio/swap/swapfile none swap sw,nofail 0 0`, the file is
    a regular file 0600 in a folder 0755, swaps with its unit active, and
    on btrfs the folder is a subvolume labelled like `/var/lib/linuxio`, the
    file NOCOW and `swapfile_t`, and a snapshot of the subvolume holding
    `/var/lib/linuxio` (Fedora's `/var`) succeeds while it swaps;
    `/swap.img` grows to 1.5 GiB in place, beside it with no temporary
    file. `resized`: `swap.target` active, LinuxIO's file swapping, linked
    in `swap.target.wants` as `nofail` makes it, the btrfs checks again, and
    `/swap.img` at 1.5 GiB; then Remove of `/swap.img`, gone from
    `/proc/swaps`, `/etc/fstab` and the disk with its unit not active.
    `removed`: still gone after the boot, LinuxIO's file still swapping;
    then it is removed.
  - `TestSwapModesRebootIntegration` runs in three phases around two
    reboots. `zram`: from disk, a switch to zram removes `/swap.img`.
    `zswap`: after the boot `swap.target` is active, zram swaps at the
    planned size and no swap file is left; a switch to zswap with
    `swap_file_size` 768 MiB makes LinuxIO's file at that size and stops
    zram. `disk`: after the boot LinuxIO's file swaps at 768 MiB, no zram,
    zswap on; then a switch to disk and Remove of LinuxIO's file.

  On 2026-11-22, on Ubuntu 26.04 (kernel 7.0.0-44, systemd 259.9,
  util-linux 2.41.3, zram-tools 0.3.7), every test passed: the shrink cut
  between its steps swapped at the new size with the file still long, the
  cancelled resize left 333 of 373 MiB parked with the file at its old
  size, the drains read 672 MiB back in 4 seconds after the cancel and
  280 MiB capped in 2 minutes 13 seconds, and each reboot brought
  `swap.target` active with the files as the phase before left them.
  With `ZRAM_PROVIDER=zram-generator` (Ubuntu's `systemd-zram-generator`,
  whose vendor file sets up `zram0` at half of RAM) everything passed too,
  the default-zram test included. On Fedora 45 (kernel 7.3.4-300.fc45, systemd 262.1,
  util-linux 2.42.4, SELinux enforcing, btrfs root with `/var` a subvolume
  of its own) everything passed: the default `zram0` read `zram` alone at
  3898 MiB and `mixed` beside `/swap.img`; LinuxIO's folder was a subvolume
  labelled `var_lib_t` like `/var/lib/linuxio`, its file NOCOW and
  `swapfile_t`, and a snapshot of `/var` succeeded while it swapped; a
  snapshot of `/` was refused with `Text file busy` while `/swap.img`
  swapped there and succeeded after the move; the drains matched Ubuntu's.
  `make vm-test-btrfs-integration` runs `SwapfileLayout` and
  `CreateSubvolumeAt` on loop devices.
- `frontend/src/test/vm/swap-files.spec.ts` drives the real dialog against
  the real bridge in the Ubuntu test VM: it creates LinuxIO's swap file at
  256 MiB, resizes it to 320 MiB, checks that the size field refuses sizes
  outside its limits without sending a request, removes the file, and then,
  with zram-tools installed, makes it again and switches to compressed
  memory, reading `/proc/swaps`, `/etc/fstab` and the file as root after each
  step. Run it with
  `make test-vm-browser VMS=1 PLAYWRIGHT_ARGS=swap-files.spec.ts`; it does
  not run on Fedora. A cancel from the tray, a resize with data parked on
  the file, Move and a session without sudo are not in it.

## Known limits

Each is by design or unlikely to be met, or is an open item in
[the backlog](./TODO/backlog.md).

- A swap partition or LVM volume is never turned off: it is not drained, not
  resized or removed, and stays beside zram.
- A swap file that fstab does not name, whose path has a link or a folder
  others can change, or that is on the hibernation device, is listed and not
  changed.
- A btrfs filesystem with more than one device, or data that is not single,
  cannot hold a swap file, so such a root has compressed memory and whatever
  swap the admin made elsewhere.
- LinuxIO makes its swap file only in `/var/lib/linuxio/swap`. A file on
  another disk is made by hand once and then resized and removed from the
  dialog.
- A switch to compressed memory removes every swap file that passes the
  strict check; there is no way to keep one beside zram from the dialog.
- A resize needs room for the data parked on the file, in free memory, free
  disk swap or disk space for the temporary file, and is refused otherwise.
- A swap file left off and without an fstab line by a Remove that was cut
  short is not listed; at LinuxIO's own path, the next Create replaces it.
- A rollback of the subvolume that holds `/var/lib` (LinuxIO's own, snapper,
  Timeshift) brings back an empty folder in place of the swap subvolume. The
  fstab line then names a missing file, which `nofail` keeps from failing
  `swap.target` at boot; its unit fails and the system reads `degraded` until
  the row, which reads Missing, is removed. The same holds for any `nofail`
  line that names a missing file.
- A backup that copies `/var/lib` copies the swap file.
- Root can hard-link an active swap file; a Remove then deletes the one name
  it checked, and the blocks stay on disk under the other.
- A Remove of a file is refused while the temporary file of its resize swaps
  beside it; the temporary file is removed first.
- A switch to compressed memory fails and rolls back when a file it was to
  remove no longer passes the strict check by its step.
- A swap file in a btrfs subvolume of its own outside LinuxIO's folder still
  reads as blocking snapshots, and gets compressed memory recommended.
- A swap subvolume whose SELinux label could not be copied stays
  `unlabeled_t`, and swaps all the same.
- "Create a swap file" is offered while compressed memory only is the mode,
  and the host then reads `mixed` until a mode is applied.
- A Create cut short by a crash or a power loss before `mkswap` wrote the
  header leaves a file at LinuxIO's path with no swap signature, which
  Create does not take for its own: it reads `path_taken` until that file is
  deleted by hand.
- While a swap file on btrfs swaps, the block groups it is in are skipped by
  scrub and balance, other files' data in those block groups included.
- Another program that rewrites `/etc/fstab` without LinuxIO's lock
  (udisks2, cloud-init) can undo an edit of LinuxIO's; a Remove reads the
  file again before it deletes a swap file, and keeps the file when its line
  is back.
- A host that names its hibernation file only on the kernel command line,
  with `/sys/power/resume` reading `0:0`, is not detected, so the file is
  not locked; a Move or a Remove of it makes the next hibernate fail,
  without other damage.
- Switching modes many times in quick succession can meet systemd's start
  limit for the zram unit (five starts in ten seconds; zramswap's stop also
  fails whenever it cannot unload the module), and the next switch then
  fails with "Start request repeated too quickly" until the limit's interval
  has passed or `systemctl reset-failed` clears it.
- A `swapoff` has no time limit: it runs until the device is empty or the
  job is cancelled from the task tray. A cancel takes effect only when the
  process whose memory is being read back is done.
- A host that swaps to zram only keeps the size its zram has. Changing the
  size means stopping zram, which would leave the host without swap
  meanwhile, so it is done only while disk swap is active; and the dialog
  does not offer Apply for the zram row while zram is the current mode.
- The `zram-generator` path was built from upstream's documentation, source
  and unit files. It has run in the test VM on Ubuntu's
  `systemd-zram-generator` and on Fedora 45, and not on RHEL.
- zram-generator's other files are read as written, one by one, not
  evaluated or merged: a file that only sets `zram-size = 0`, whose device
  upstream discards, still counts as defining a device and keeps a switch
  away from zram refused, and a drop-in that makes another file's device a
  zram mount, or swap again, does not change how that file's section is
  counted.
- A device upstream would drop because the host has more memory than the
  section's `host-memory-limit` is still listed, and still refuses a switch
  away from zram.
- Whether a zram configuration file needs the question is decided when the
  change is planned: a file an admin saves while the change runs, between
  the plan and the write, is replaced without the question, and its copy is
  kept.
- `systemd.zram=0` on the kernel command line, which keeps the generator from
  making any device, is not read, and neither is a main file in `/run/systemd`
  that defines none and so hides LinuxIO's file: a switch to zram on such a
  host fails at `start_zram` and is rolled back.
- The dialog lives in the dashboard's Memory card, so while that card is
  hidden its URL opens nothing.
- With both zram tools installed, a change sets start-at-boot only for the
  tool that runs zram, so the other, if it is set to start at boot, brings
  zram back at the next boot. With both installed and neither running zram,
  zram-tools is used, so a host whose zram the generator ran comes back on
  zram-tools after a switch away and back, with the generator's
  configuration left without a device.
- A change whose status still cannot be read after five tries fails the
  request, with an error that says the change was made; nothing is undone.
- On a host that runs zram, `page-cluster`, `watermark_boost_factor` and
  `watermark_scale_factor` count as zram's whoever set them, so a switch
  away sets them back to the kernel defaults live even when the admin's own
  sysctl file set the same numbers; the next boot applies the admin's file
  again.
- The disk hint reads the kernel's removable flag, so a worn flash drive in
  a USB disk enclosure, which reports itself as fixed, is not steered to
  compressed memory, nor is a swap file on LVM or LUKS, whatever disk is
  under it, nor a swap partition.
- A click on Apply within half a second of an arrow key on the eagerness
  slider is lost: the click moves focus off the slider, which starts the
  save, and Apply is held while it runs.
- On a kernel where zswap failed to start, the tmpfiles line that turns zswap
  off may fail at each boot and leave a line in the journal. Not checked on a
  host.
- The overflow alert is not built
  ([Watched conditions](./TODO/watched-conditions.md)).
