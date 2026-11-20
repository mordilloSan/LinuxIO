# Memory overflow: LinuxIO's own swap file

## Status

Not built. Planned on 2026-11-18 for v0.54.0, and moved out of that release by the owner the same day: it is built afterwards, when the owner approves it. It comes from the backlog item under "Later, no plan yet": LinuxIO makes its own swap file in a fixed folder only root can write, manages only that file, and shows any other swap as read-only.

The owner also folded in two smaller items:
- read a unit's state over one system bus connection for a whole wait;
- tell the admin about swap LinuxIO does not manage, and why.

The plan is written against `fb74b5b9` (branch `dev/v0.54.0`). Three changes landed after it and before any of it is built: `/etc/fstab` is read once per memory change, the success log line is exact, and an fstab swap entry without a systemd unit LinuxIO can use is left alone and listed (`unmanaged_fstab_swaps`). Section 6, "Where the two earlier changes meet this plan", covers the first two; the third is replaced by section 4, "Swap LinuxIO did not make". Read the plan against the code before building it.

## Sources

The man pages named for this plan are not installed on the planning host: `systemd.swap(5)`, `swapon(8)`, `mkswap(8)`, `fallocate(1)`, `btrfs-filesystem(8)` and `systemd.unit(5)`. Every statement about upstream behaviour carries one of two marks:

- **(read: …)** It was read on the planning host from the named file, command or repository file. The host runs Ubuntu with kernel 7.0, systemd 259.9, util-linux 2.41.3 and btrfs-progs 6.17.1.
- **(from memory)** It was not checked here. Phase 2 starts by checking each such statement against the man page on the test VMs (Ubuntu 26.04 and Fedora 45) and correcting this plan.

## What changes

| | Today | After |
|---|---|---|
| Disk swap LinuxIO stops, starts and drains | The first swap file `/etc/fstab` names that is active or held off, when only root can change its folders | LinuxIO's own file, `/var/lib/linuxio/swap/swapfile`, and nothing else |
| How it is found | fstab is parsed, tags become `/dev/disk/by-*` links, links are resolved, unit names are derived (`fstab.go`) | A constant path, compared with `/proc/swaps` exactly as written |
| How it stays off in zram mode | A condition drop-in on the fstab unit, `90-linuxio-memory.conf`, never a mask | The file, its unit and the unit's `swap.target` link are removed |
| How it comes on at boot | The fstab generator's unit | LinuxIO's unit `var-lib-linuxio-swap-swapfile.swap`, with `WantedBy=swap.target` |
| Other swap (a partition, `/swap.img`, LVM, zram set up by other means) | Never stopped when not matched. Reported only as "the swap file" with `unmanaged_reason`, or not at all | Listed device by device (`managed: false`). Never stopped, drained or held off. The dialog says how to remove it |
| zram beside other disk swap | Refused beside a partition (`swap_partition_active`); `mixed` beside a file | Allowed. The mode reads `zram` and the other swap is listed |
| `mixed` | Any zram and any disk swap active together | LinuxIO's file and the zram its provider runs, both active |
| Creating a swap file | Never | LinuxIO makes its file when disk or zswap is chosen, and deletes it when zram is chosen |
| Unit state during a wait | A new bus connection for every poll, about four a second | One connection for the whole wait |

## 1. LinuxIO's swap file

### Path

The file is `/var/lib/linuxio/swap/swapfile`, in its own folder `/var/lib/linuxio/swap`.

| Candidate | Verdict | Why |
|---|---|---|
| `/var/lib/linuxio/swap/swapfile` | Chosen | It is LinuxIO's state folder, made `0755 root root` by `packaging/systemd/linuxio-tmpfiles.conf` (read). No admin or distribution uses the name. The unit name needs no escapes. `uninstall.sh` keeps it unless given `--remove-data` (read). |
| `/swapfile`, `/swap.img` | Rejected | These are distribution and guide defaults, and an admin's fstab may already name them. |
| `/var/swap` | Rejected | dphys-swapfile keeps its file there on Raspberry Pi OS (from memory). |
| `/swap/swapfile` on a top-level `@swap` subvolume | Rejected | It needs a mount (an fstab line or a `.mount` unit) and a new folder in `/`. |
| A folder the admin picks | Later | The unit name stops being a constant, and the path rules this redesign deletes come back for that path. |
| `tmpfiles.d` type `v` to make the folder | Rejected | It makes a plain folder wherever `/` is not itself a subvolume (tmpfiles.d(5), from memory), and it runs on every host. |

- **Unit name:** `var-lib-linuxio-swap-swapfile.swap` (read: `systemd-escape --path /var/lib/linuxio/swap/swapfile`, systemd 259.9).
  - A swap unit must be named after the file it swaps on (systemd.swap(5), from memory).
  - systemd will not load a swap unit whose `What=` does not match its name (from memory). A wrong constant would therefore fail loudly in the VM tests.
- **Modes:**
  - The folder is `0755 root root`. A session without sudo reads the status, which stats the file; today's drop-in folder is `0755` for the same reason.
  - The file is `0600 root root`.

### Filesystems

LinuxIO makes the file only on `ext4`, `xfs` and `btrfs`.

- The type comes from mountinfo for the folder. Before the folder exists, it comes from the nearest existing ancestor.
- It is checked again with `fstatfs` on the folder handle just before creating, against `EXT4_SUPER_MAGIC`, `XFS_SUPER_MAGIC` and `BTRFS_SUPER_MAGIC`. All three constants are in `golang.org/x/sys/unix` v0.50.0 (read).
- Mountinfo is still needed, because ext2, ext3 and ext4 share one magic number.

| Filesystem under `/var/lib` | What LinuxIO does | Why |
|---|---|---|
| ext4, xfs | Makes the file | Swap through iomap accepts allocated and unwritten (fallocated) extents (from memory). The test VM already swaps on a fallocated `/swap.img` on Ubuntu's ext4 (read: `swap_file` in `packaging/vm/linuxio-vm.sh`). |
| btrfs | Makes it in its own subvolume, after the checks in the next section | |
| ZFS, tmpfs, overlayfs, NFS, CIFS, FUSE, 9p, virtiofs, f2fs, bcachefs, ext2, ext3, anything else | Refuses with `unsupported_filesystem` | These have no swap file support, or need more than LinuxIO does: f2fs pins its file, NFS needs `CONFIG_NFS_SWAP` (from memory). A whitelist keeps LinuxIO out of all of them. |
| Read-only (`ST_RDONLY`) | Refuses with `read_only` | |

### btrfs

**What upstream requires** (btrfs(5), "SWAPFILE SUPPORT", from memory):
- the filesystem has one device;
- its data profile is single (metadata may use any profile);
- the file is NOCOW, which also means no checksums and no compression;
- the file is fully allocated, with no holes;
- a subvolume that holds an active swap file cannot be snapshotted;
- balance skips the block groups that hold the file;
- while an exclusive operation runs (balance, device remove, shrink), a swap file cannot be activated.

That a snapshot leaves nested subvolumes out is read in `docs/btrfs.md` ("Nested subvolumes are not included in a snapshot").

**What LinuxIO checks before it creates anything, in this order:**

1. `BTRFS_IOC_FS_INFO` reports one device. Otherwise the reason is `btrfs_layout`.
2. `BTRFS_IOC_SPACE_INFO` reports every data (and data+metadata) block group type as single. Otherwise the reason is `btrfs_layout`.
3. `/var/lib/linuxio/swap` is either a subvolume root (inode 256 on btrfs) or absent. LinuxIO makes it a subvolume through the handle of `/var/lib/linuxio`.
   - An empty plain folder at that name, as a rollback of the parent leaves it, is removed and made a subvolume.
   - A folder with anything in it gives `path_taken`.
4. On the new, empty file, LinuxIO calls `FS_IOC_SETFLAGS` to set `FS_NOCOW_FL` and `FS_NOCOMP_FL` and clear `FS_COMPR_FL`. `FS_IOC_GETFLAGS` must then read NOCOW set and COMPR clear. Otherwise the file is removed and the step fails. NOCOW has to be set while the file is still empty (from memory).

**Where the code goes:**
- `backend/common/btrfs` already wraps the ioctls of checks 1 and 2 (`fsInfoArgs`, `spaceInfos`, read). This plan adds two exported functions that work on an open folder:
  - `btrfs.SwapfileLayout(dir *os.File) (devices uint64, singleData bool, err error)`;
  - `btrfs.CreateSubvolumeAt(dir *os.File, name string) error`, which creates a subvolume through the folder handle without mounting the top level. It sits on the existing `createSubvolumeOn` (read).
- x/sys/unix has `FS_IOC_GETFLAGS` and `FS_IOC_SETFLAGS`, but not the flag values (read). The memory package defines them from `linux/fs.h` (from memory):
  - `FS_NOCOW_FL = 0x00800000`
  - `FS_COMPR_FL = 0x4`
  - `FS_NOCOMP_FL = 0x400`
- A test pins those values. The ioctl reads and writes an `int`, even though it is declared with `long` (from memory).

The kernel's own check at `swapon` has the last word. If it refuses, the start fails and the journal gives the reason (from memory: "swapfile must ...").

### Creation

The step `create_swap_file` runs as root in the job host. Everything goes through handles, starting from `/`:

1. Open each folder down to `/var/lib/linuxio` with `O_DIRECTORY|O_NOFOLLOW`. Each one must be a folder that root owns and that no group or everyone may write; this is today's `foldersOnlyOf` rule. If `/var/lib/linuxio` is missing, it is made `0755 root` (tmpfiles normally makes it).
2. Make or open `swap` inside it: a subvolume on btrfs, `mkdirat` otherwise. Set it to `0755` with `fchmod`, and apply the same ownership check.
3. Create the file with `openat(swap, "swapfile", O_CREAT|O_EXCL|O_WRONLY|O_NOFOLLOW|O_CLOEXEC, 0600)`, then `fchmod 0600`. If it already exists (`EEXIST`), see the "What is at the path" table below.
4. On btrfs, set the flags as described above.
5. Allocate with `fallocate(fd, 0, 0, size)`. On `ENOSPC` or any other error, remove the file with `unlinkat` (it never swapped) and fail.
6. `fsync` the file and the folder.
7. Run `mkswap -- /var/lib/linuxio/swap/swapfile` through `hostcmd`, with a time limit.
   - Where SELinux is on, mkswap labels a regular file `swapfile_t` (from memory). The VM rig's note says `btrfs filesystem mkswapfile`, unlike mkswap, does not label the file (read).
   - If mkswap fails, the file is removed.

The unit is written only after the file is complete, so no boot ever sees a unit pointing at a half-made file.

| Alternative | Verdict | Why |
|---|---|---|
| Write zeros instead of `fallocate` | Rejected | It takes minutes on an SD card or a slow disk and wears flash for nothing. It is only needed where swap refuses unwritten extents, and the whitelist already leaves those filesystems out. |
| `mkswap --file --size` | Rejected | Needs util-linux 2.39 or later (2.41.3 here has it, read). RHEL 9 ships 2.37 (from memory). |
| `btrfs filesystem mkswapfile` | Rejected | btrfs only, needs btrfs-progs 6.1 or later (from memory), and sets no SELinux label (read: VM rig). |
| Write the swap header in Go | Rejected | mkswap is in util-linux everywhere and also sets the SELinux label. A Go header writer would have to do both. |

**What is at the path**, checked before a create or a start:

| Found | Done |
|---|---|
| Nothing | Create the file |
| A regular file owned by root, holding a swap signature (`holdsSwap`), not active | Treated as LinuxIO's and reused. Its mode is set back to `0600` and its owner to root, through the handle. |
| The same, but active | Already swapping; nothing to do |
| Anything else: a link, a folder, a file without a swap signature | `path_taken`. Refused; the admin removes it. |

### Size

- **The size:** half of `MemTotal`, at least 1 GiB, at most 8 GiB, rounded down to whole MiB. This matches zram's planned capacity (`zramPlannedSize`: half of RAM, up to 8 GiB).
- **When it is set:** when the file is made, and never changed in place.
  - The kernel refuses to grow or shrink an active swap file (from memory).
  - With one fixed path, a resize would have to remove swap before adding it.
- **After a memory change:** the new size applies the next time the file is made, for example after a switch to zram and back.
- **What the status reports:** the file's size, or the size it would be made.

| Alternative | Verdict |
|---|---|
| Equal to RAM | Rejected: large files on small disks, and hibernation is not set up anyway. |
| A size the admin picks | Later (owner decision 3). |

### Room

- **Before creating**, the filesystem's free space (`f_bavail * f_bsize` from `fstatfs`) must hold the file and still leave `max(1 GiB, 5% of the filesystem)`.
- **If it does not**, the reason is `not_enough_space` and the status carries `free_space`. The plan refuses with `ErrConflict`, naming the size, the reserve and the free space.
- **On btrfs** `statfs` is only an estimate. If `fallocate` returns `ENOSPC` anyway, the step fails as described above and removes the file.

## 2. Activation at boot

### The unit

The unit file is `/etc/systemd/system/var-lib-linuxio-swap-swapfile.swap`, written atomically with `atomicfile.Write`, mode `0644`:

```ini
# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.
# LinuxIO's swap file. LinuxIO removes this unit and the file when compressed memory only is chosen.
[Unit]
Description=LinuxIO swap file

[Swap]
What=/var/lib/linuxio/swap/swapfile

[Install]
WantedBy=swap.target
```

- **Enabling and starting.** `systemd.EnableUnit` (EnableUnitFiles, then a reload) links the unit into `swap.target.wants`. `systemd.StartUnit` then starts it, followed by the existing wait for `/proc/swaps` (`startUntilBack`). LinuxIO does not run `swapon` itself, so boot and Apply take the same path.
- **Dependencies come from systemd.**
  - A generated fstab swap unit for a file shows `Requires=-.mount`, `RequiresMountsFor=<path>`, `After=-.mount systemd-remount-fs.service`, `Conflicts=` and `Before=umount.target`, and `TimeoutUSec=1min 30s` (read: `systemctl show swap.img.swap`, systemd 259.9).
  - A unit file gets the same implicit and default dependencies (from memory).
  - `swap.target` orders itself after what it wants (systemd.target(5), from memory). On this host `swap.target` shows `After=` its swap units (read).
  - Phase 2 records `systemctl show` for LinuxIO's unit on both VMs in `docs/memory.md`.
- **`WantedBy`, not `RequiredBy`.** A file that fails to start does not keep `swap.target` from starting, nor zram-generator's `dev-zram0.swap`, which `swap.target` wants.
  - The fstab generator makes `swap.target` require the units of entries without `nofail` (read: `/run/systemd/generator/swap.target.requires`).
  - That requirement is why today's mask stopped `swap.target` (read: `docs/memory.md`).
- **No `Priority=`.** The kernel's default applies, as for any swap without one; kernel 7.0 reads `-1` for two such files (read: `/proc/swaps` on the planning host). So LinuxIO does not reorder swap it did not make. Devices of equal priority share writes round-robin (from memory).
- **fstab precedence.** A unit file for a path that is also in `/etc/fstab` takes precedence over the fstab line (systemd.swap(5), from memory). This is not expected to happen, since nothing at that path is in anyone's fstab.

### zram mode

`stop_swap_file` does, in order:

1. Turns the file off with `swapoff` (`swapOffDevice`, unchanged: a process of its own, no time limit, the root-only path check).
2. `DisableUnit`, which removes the `swap.target.wants` link and reloads systemd.
3. Removes the unit file and reloads again.
4. Reads `/proc/swaps` again and deletes the file through the folder handle (`unlinkat`), only when the file is no longer listed.

The folder (or subvolume) stays.

There is therefore nothing to "hold off", because there is no unit left to hold. The condition drop-in, its folder handling, the unmask, and the rule "never a mask" no longer apply to anything.

| Alternative | Verdict | Why |
|---|---|---|
| Remove the unit and the file (above) | Chosen | One state per mode: the file exists exactly when LinuxIO uses disk swap. It frees the space, and the way back makes a new file at the current size. |
| Disable the unit, keep the unit file and the file | Rejected (owner decision 5) | The way back is quicker and needs no free space, but gigabytes stay unused and a stale unit file is left. |
| Mask LinuxIO's unit | Rejected | Pointless for a unit that is only wanted, and a mask is what broke `swap.target`. |
| A condition drop-in on LinuxIO's unit | Rejected | Nothing is left for it to do once the unit itself can be removed. |

## 3. The three modes

### What each means

| Mode | LinuxIO's swap file | zram its provider runs | zswap |
|---|---|---|---|
| `disk` | Made, on at boot, active | Off at boot, stopped | Off now and at boot |
| `zswap` | Made, on at boot, active | Off at boot, stopped | On now and at boot |
| `zram` | Absent: no file, no unit | On at boot, swapping at the planned size | Off now and at boot |

Swap LinuxIO did not make plays no part. No mode needs it, and no mode stops it.

**How the status names the host** (`deriveMode`), counting only the devices LinuxIO manages, first match wins:

1. LinuxIO's file and managed zram are both active: `mixed`. This is a change that was cut short, and Apply finishes it.
2. Managed zram is active: `zram`.
3. LinuxIO's file is active: `zswap` or `disk`, depending on zswap's `enabled`.
4. Otherwise: `none`, whether or not other swap is active.

**Which devices are managed** (`SwapDevice.managed`):
- LinuxIO's file, when its `/proc/swaps` path equals the constant;
- `/dev/zram0`, while a zram provider is installed.

The status cannot tell from files whether the provider's unit actually runs that `zram0`. The change asks systemd (`providerRunsZram`, unchanged), and leaves alone zram that another tool set up, reporting it as `zram: not_managed`, as today.

### Recommendation

First match wins:

| Host | Recommended |
|---|---|
| LinuxIO cannot make its file (any `unavailable_reason`) and the file does not exist | `zram` |
| Its filesystem is on an SD card, eMMC or removable disk (`disk_hint`) | `zram` |
| Its filesystem is btrfs | `zram` (owner decision 10). The file no longer blocks snapshots, but its block groups stay out of balance, profile conversion and device removal (from memory). |
| The kernel has no zswap | `zram` |
| Anything else | `zswap` |

Other swap no longer changes the recommendation. Today an active partition or an unmanaged file forces `zswap`.

### Order of steps

| To | Steps |
|---|---|
| `zram` | `write_config`, `start_zram`, `apply_sysctls`, `disable_zswap`, then `stop_swap_file` when LinuxIO's file, its unit file or its `swap.target.wants` link exists |
| `zswap`, `disk` | `create_swap_file` when the file does not exist; `start_swap_file` when the file is not active, or its unit is not installed and enabled; `stop_zram` when a provider is installed; `apply_sysctls`; `enable_zswap` or `disable_zswap` |

- `create_swap_file`: see section 1.
- `start_swap_file`: write the unit, enable it, start it, and wait for the path in `/proc/swaps`. The wait is up to a minute, with the start sent again every 15 seconds (`startUntilBack`, unchanged).
- `stop_swap_file`:
  1. Read a fresh status.
  2. A zram device must be swapping in it, and the file's used bytes must fit in memory (`fitsInMemory`). Otherwise the step records `left` (`zram_not_swapping` or `not_enough_memory`), and the file stays with its unit.
  3. Then the steps in section 2.

  A file that is not active is simply removed together with its unit.
- `start_zram`, `stop_zram`, `write_config`, the sysctls and zswap: unchanged.

### The standing rules

| Rule (AGENTS.md, `docs/memory.md`) | After |
|---|---|
| Add swap before removing any | Stays. The file is made and started before zram stops; zram starts before the file stops. |
| Remove swap only after the room check on a freshly read status | Stays, for LinuxIO's file, for zram and for a drain. |
| Restart zram at a new size only while disk swap is active | Stays. Any active disk swap counts, LinuxIO's or not, since it is capacity while zram restarts. |
| Never undo added swap in a rollback | Stays, and covers the new steps (see Rollback below). |
| Stop or hold off a device only when it is positively matched to an fstab entry | Goes. LinuxIO stops only its constant path and the zram its provider runs. |
| A condition drop-in, never a mask | Goes. Nothing is held off any more. |
| Stop, start, drain or `swapoff` a swap file only where root alone can write the folders (`othersCanWrite`, `rootOnlyPath`) | Narrows. `rootOnlyPath` guards LinuxIO's path before create, start, `swapoff` and delete. `othersCanWrite` and the link rules go. |
| The swap file stops only while zram swaps in the fresh read | Stays. |

### Rollback

Rollback is unchanged for files, kernel parameters and zram at boot. For the new steps:

- **A file `create_swap_file` made** is not deleted by the rollback. Deleting it by name after a failure could race a start that is still queued. The next Apply reuses the file, or a switch to zram removes it.
- **The unit `start_swap_file` wrote and enabled** is disabled and removed, with a reload, only when the file is not in `/proc/swaps` as read at rollback time. A file that swaps keeps its unit, because it is added swap.
- **A file `stop_swap_file` turned off** is not started again, and its unit is not put back.

## 4. Swap LinuxIO did not make

This is every `/proc/swaps` device that is not managed:
- a swap file, whether from fstab or started by hand;
- a partition;
- swap on LVM or LUKS (`/dev/dm-N`);
- zram set up by another tool, or by a zram-generator file other than LinuxIO's (`zram1` and so on).

What LinuxIO does with it:

- **Never stopped, drained or held off**, in any mode or rollback. No unit of its is read or written, and `/etc/fstab` is not read.
- **Shown device by device**: path, kind, size and used.
  - For a swap file, also its filesystem (`fs_type`, `disk_hint`, kept from today's `describeSwapFiles`).
  - For a swap file, also `others_can_write`. This applies the folder rule (`foldersOnlyOf`) to its `/proc/swaps` path, only to warn (owner decision 7).
- **How to remove it**, said in the dialog: turn it off with `swapoff <path>`, remove what turns it on at boot (usually its line in `/etc/fstab`), and delete the file. For zram: it was set up by another tool, and LinuxIO leaves it as it is.
- **Counted as room where it is today.** Free space on any disk swap counts for zram's room check (`zramFits`), and any active disk swap allows a zram restart.
- **Drain.** If the parked data is only on other swap, `drain_blocked_reason` is `other_swap`.

**Choosing zram while other disk swap is active** is allowed.
- The host reads `zram`, not `mixed`, because Apply cannot change what LinuxIO does not manage.
- The confirm step and the result both say the other device stays on and takes whatever zram cannot hold; zram swaps at priority 100, ahead of it.

**Taking over or moving another swap file is not offered** (owner decision 7).
- It would need `swapoff` on a path LinuxIO did not make, and keeping that file off at boot. That means editing `/etc/fstab` or adding a drop-in on a unit named after the file, which brings back the link and folder rules this plan deletes.
- A swap file in a folder others can write instead gets a warning that says why it matters: another user could put a file of their own at that name and read what root swaps to it. The warning also says how to remove the file.
- One line goes under Rejected in the backlog.

## 5. Migration

The owner is the only user and accepts breaking saved state. There is no migration code. The release note and `docs/memory.md` say what to clean up by hand (owner decision 9).

**Host on disk or zswap, with an fstab swap file LinuxIO managed (`/swap.img`):**
- First status read after the upgrade: mode `none`. `/swap.img` is listed as other swap; zswap and the sysctls are as they were.
- First switch to disk or zswap: LinuxIO makes its own file beside `/swap.img`, so the host has two disk swap files.
- First switch to zram: zram starts, `/swap.img` stays on, and the mode reads `zram` with `/swap.img` listed.
- Left behind: `/swap.img` and its fstab line, for the admin to remove.

**Host on zram, with `/swap.img` held off by `/etc/systemd/system/swap.img.swap.d/90-linuxio-memory.conf`:**
- First status read: mode `zram`. Nothing is listed, because the file does not swap.
- First switch to disk or zswap: LinuxIO's file is made and started, and zram is stopped. `/swap.img` stays off at every boot, through the old drop-in.
- Left behind: the drop-in, its `.d` folder, `/swap.img` and its fstab line. To clean up:
  1. `rm -r /etc/systemd/system/swap.img.swap.d`
  2. Remove the fstab line.
  3. `rm /swap.img`
  4. `systemctl daemon-reload`

**Host on zram, with `/swap.img` masked by an older LinuxIO:**
- First status read: as above where `swap.target` started. Where the mask kept `swap.target` from starting (and zram-generator's zram with it), the mode reads `none`.
- First mode change: `swap.target` still does not start at boot, so LinuxIO's unit, which `swap.target` wants, is not started at boot either.
- Left behind: the mask. Run `systemctl unmask swap.img.swap`, then clean up as above.

**Any host, for the sysctl drop-in, the tmpfiles entry, the zram configuration and its copy:** read and written as today; nothing is left behind.

Nothing is cleaned up automatically. Removing the old drop-in would bring `/swap.img` back at the next boot, on a host the admin put on zram.

The alternative is a status note that lists leftover `/etc/systemd/system/*.swap.d/90-linuxio-memory.conf` files, found by a glob with no fstab reading. It is about 40 lines with its test.

## 6. What is deleted and what stays

### Deleted

| Item | Where |
|---|---|
| `fstabSwap`, `fstabTagDirs`, `fstabSwaps`, `fstabNodePath`, `maxSymlinks`, `resolve`, `resolveLinks`, `sameSwap`, `matchFstabSwaps`, `othersCanWrite`, `maskDirs`, `unitHeldOff`, `managedDiskUsed` | `fstab.go`, the whole file (246 lines), and `fstab_test.go` (292 lines) |
| `fstabSwapFile`, `isSwapFilePath` | `read.go`; `TestFstabSwapFile` in `read_test.go` |
| `statusSwapFile`, `usableSwapFile`, the fstab read in `readSwapStatus` | `status.go` |
| `managedSwapFile`, the `others_can_write` refusal, `swapStep.unit` and `swapStep.path` | `plan.go` |
| `holdOff`, `letGo`, `unmaskIfMasked`, `swapFilesResolve`, `findSwap` (an exact match replaces it), the hold-off reload in `rollback`, the swap file half of `explainMixed` | `mode.go` |
| `drainTargets`, `holdsData`, `holdingData`, `leftOutHoldsData`, the masked-unit loop, the per-device loop | `drain.go` |
| `holdOffDropInPath`, `makeHoldOffDropInDir`, `isHoldOffDropIn`, `renderHoldOffDropIn`, `holdOffDropInName` | `persist.go` |
| `swapView.fstab`, the fstab read in `readView` | `executor.go` |
| `hostSystem.unmaskUnit` | `system.go`, `fake_test.go` |
| `systemd.EscapePath`, whose only caller is `fstab.go` | `backend/common/systemd/escape.go`, `escape_test.go`, and the Code cell of the Memory row in `docs/README.md` |
| `SwapFile`, `SwapFileUnmanagedReason` (`not_in_fstab`, `others_can_write`), `SwapStatus.swap_file` | `apischema/memory.go`, `contracts.go` |
| `ZramSwapPartitionActive` | same |
| `SwapDrainUnmanagedDevice`, `SwapDrainUnitHeldOff`, `SwapDrainOthersCanWrite` | same |
| `SwapLeftNotIdentified`, `SwapLeftOthersCanWrite`; `not_managed` no longer applies to a swap file | same |
| `swapFileNote`, `UNMANAGED_CAUSE`, `NEEDS_SWAP_FILE`, `SWAP_FILE_NOT_STARTED`, `hasDiskSwap`, the banner action `switch_zram`, three drain texts, three `LEFT_TEXT` entries | `memoryStatusView.ts`, `memoryFeedback.ts` |
| Unit tests about fstab, links, folders others can write, masks, drop-ins, two swap files and unit aliases | `fstab_test.go`, `status_test.go` (6 tests), `plan_test.go` (3), `mode_test.go` (about 20), `drain_test.go` (6) |
| `TestMemoryDrainAliasIntegration`, and the reboot run through `/swap-link` | `drain_integration_test.go`, `boot_integration_test.go` |
| `link_swap_file`, `vdb_by_uuid`, `vdb_by_path`, `vdb_fstab_source`, and the swap setup of `vdb` | `packaging/vm/linuxio-vm.sh` |
| "LinuxIO manages one swap file", the unmanaged-reason table, "Swap units", "A swap file others can redirect is not managed", "Holding a swap file off at boot", the alias paragraphs of "Draining", the drop-in row of "Files written", and nine Known limits | `docs/memory.md` |
| Backlog items: the unusual label and 256-byte path item, this redesign, the bus connection item, and the fstab-once and log items once those land | `docs/TODO/backlog.md` |

### Stays

- **The zram provider:** `zramProvider`, `providerRunsZram`, `generatorMadeZram`, `stopZramUnit`, `resetStaleZram` and the stop waits.
- **zram-tools' conffile handling:** `asksToReplace`, the dpkg checksum, `confirm_replace`, and the copy.
- **zram-generator's other files:** `generator_config.go`.
- **Sysctls and zswap:** the render functions in `persist.go`, `liveSysctls`, `settleSysctls`, and the TuneD handling.
- **Swap removal and waits:** `swapOff`, `swapOffDevice`, `rootOnlyPath`, `foldersOnlyOf` (now for LinuxIO's path, and for the warning on other swap files), `waitUnitSeenOff`, `startUntilBack` (now for LinuxIO's unit), `fitsInMemory`, `zramFits`, `statusAfterChange`, and the mutation lock.
- **Readers:** `readMounts`, `mountHolding`, `diskHint`, `removable`, `holdsSwap`, `unescapeOctal`. The backlog item about hoisting the `\ooo` unescape helper stays.
- **Draining**, now for LinuxIO's file only.

### Where the two earlier changes meet this plan

**Read `/etc/fstab` once per change.**
- It touches `readSwapStatus` (`status.go`, returning the matched entries with the status), `readView` and `swapView` (`executor.go`), `drainTargets` (`drain.go`), and the fake's `status`.
- Phase 3 deletes all of its fstab half.
- If that change makes `hostSystem.status` return a view (the status plus what the API does not carry), Phase 3 keeps that shape and drops the fstab field.
- Phase 2 needs no field there, because every fact about LinuxIO's file is in the API status.

**Exact success log.**
- It touches the `swap mode changed` line in `setSwapMode` (`mode.go`) and the `swap drained` line in `drainSwap` and `drainDevice` (`drain.go`).
- In Phase 3, `swap_file_left_active` comes to mean LinuxIO's file only, read from the final status. Three attributes are added:
  - `other_swap_active`: the number of unmanaged active devices;
  - `swap_file_created`;
  - `swap_file_deleted`.
- The drain line names one `path` and is logged only when the file was actually drained.
- That change's count of several drained devices goes away with the loop. Its "a vanished device is not counted" test becomes "a drain whose file vanished logs no drain".

### Size

| | Removed | Added |
|---|---|---|
| Go, not tests | ~1,050 | ~880 |
| Go tests, unit and root | ~2,400 | ~2,150 |
| Frontend with its tests | ~600 | ~620 |
| VM rig | ~150 | ~150 |
| Docs | ~500 | ~450 |
| Total | ~4,700 | ~4,250 |

The value is not in the line count. What goes are the rules that each needed a review: links, folder ownership on someone else's path, unit-name derivation, hold-offs, masks, second swap files and unmounted filesystems. What replaces them is one constant path and one constant unit.

## 7. API and UI

### apischema/memory.go after the change

```go
// OwnSwapFileUnavailableReason says why LinuxIO cannot make its swap file,
// or is empty when it can.
type OwnSwapFileUnavailableReason string

const (
	OwnSwapFileUnsupportedFilesystem OwnSwapFileUnavailableReason = "unsupported_filesystem"
	OwnSwapFileReadOnly              OwnSwapFileUnavailableReason = "read_only"
	OwnSwapFileBtrfsLayout           OwnSwapFileUnavailableReason = "btrfs_layout"
	OwnSwapFileNotEnoughSpace        OwnSwapFileUnavailableReason = "not_enough_space"
	OwnSwapFileFolderNotRootOnly     OwnSwapFileUnavailableReason = "folder_not_root_only"
	OwnSwapFilePathTaken             OwnSwapFileUnavailableReason = "path_taken"
)

// OwnSwapFile is LinuxIO's swap file, as made or as it would be made.
// UnavailableReason is set only while the file does not exist.
type OwnSwapFile struct {
	Active            bool                         `json:"active"`
	DiskHint          SwapDiskHint                 `json:"disk_hint"`
	Exists            bool                         `json:"exists"`
	FreeSpace         uint64                       `json:"free_space"`
	FSType            string                       `json:"fs_type"`
	OnAtBoot          bool                         `json:"on_at_boot"`
	Path              string                       `json:"path"`
	Size              uint64                       `json:"size"`
	UnavailableReason OwnSwapFileUnavailableReason `json:"unavailable_reason"`
	Used              uint64                       `json:"used"`
}
```

Other changes to the contract:

- `SwapDevice` gains:
  - `Managed bool` (`managed`);
  - `OthersCanWrite bool` (`others_can_write,omitzero`), set only on swap files that are not managed.
- `SwapStatus.SwapFile` is replaced by `OwnSwapFile OwnSwapFile` (`own_swap_file`, always present).
- `ZramUnavailableReason` keeps only `package_missing`.
- `SwapDrainBlockedReason` becomes `nothing_parked`, `not_enough_memory` and `other_swap`.
- `SwapLeftReason` becomes `not_enough_memory`, `zram_not_swapping`, `not_managed` (zram only), `no_other_swap` and `other_size`.
- `SwapStep` gains `create_swap_file`.
  - The order becomes `write_config, create_swap_file, start_swap_file, start_zram, stop_zram, apply_sysctls, disable_zswap, enable_zswap, stop_swap_file, drain_device`.
  - New and changed messages: "Making LinuxIO's swap file", "Turning on LinuxIO's swap file", "Turning off and deleting LinuxIO's swap file".
- Unchanged: `SwapModeRequest`, `SwapModeResult`, `SwapProgress`, `SwappinessRequest`.
- `SwapMode` keeps its values; `none` now means "LinuxIO manages no active swap".
- The enums in `contracts.go` follow. Run `make generate`, then `make test-quiet`.

### The dialog

**Banner**, worst condition first:

| State | Severity | Text | Action |
|---|---|---|---|
| No active swap at all | warning | If memory runs out, Linux will close programs. | Turn on compressed memory, or the Capabilities link |
| `mixed` | warning | Compressed memory and LinuxIO's swap file are both on. Choose one below and apply to finish. | none |
| Data parked on disk swap | info | `<X>` of memory is parked on disk… | Bring back to memory, enabled when LinuxIO's file holds data. Otherwise disabled with "The parked data is on swap LinuxIO did not make, which it does not empty", or the not-enough-memory text. |
| Another swap file on btrfs is active | warning | `<path>` blocks snapshots of the system drive. LinuxIO did not make it and does not turn it off. | none |
| `none`, other swap active | info | This host swaps only to swap LinuxIO did not make. Choose below how LinuxIO handles memory running out. | none |
| Otherwise | success | Memory is fine. Nothing is parked. | none |

**Under the banner:**

- **Other swap** (`otherSwapNote`, which replaces `swapFileNote`), shown while any device is not managed.
  - It reads: "Swap LinuxIO did not make. LinuxIO leaves it as it is whatever is chosen here:"
  - Then each device with its kind, size and used. For a file whose folder others can write, it also says why that matters.
  - It ends: "To stop using one: turn it off with swapoff, remove what turns it on at boot (usually its line in /etc/fstab), and delete the file."
  - It is a warning when any file has `others_can_write`, and info otherwise.
- **zram-generator files:** unchanged.
- **Rows:**
  - zswap and disk name LinuxIO's file. While it does not exist they say, for example, "…spills to LinuxIO's swap file (`<size>` at `<path>`), made when you apply".
  - Their disabled reason is one of: the file's `unavailable_reason` in words (table below), the kernel's missing zswap, or zram-generator's refusal (as today).
  - The disk row's caution "Blocks snapshots of the system drive while in use" goes.
  - The zram row is disabled only while no zram tool is installed (the Capabilities link). Its description adds "LinuxIO's swap file, if any, is deleted".
- **Confirm step**, for a switch to zram while LinuxIO's file is active:
  - "Compressed memory is turned on."
  - "Data parked in LinuxIO's swap file is brought back to memory."
  - "The swap file is turned off and deleted."
  - For each other active disk swap device: "`<path>` stays on: LinuxIO did not make it. It takes what compressed memory cannot hold."
  - The item about a zram configuration file that is not LinuxIO's is unchanged.
- **Result:**
  - `LEFT_TEXT` loses the swap file's `not_identified`, `others_can_write` and `not_managed` texts.
  - A switch to zram that ends with other disk swap still active adds "`<path>` still swaps: LinuxIO did not make it." to the toast.

**Row text for each `unavailable_reason`:**

| `unavailable_reason` | Row text |
|---|---|
| `unsupported_filesystem` | LinuxIO makes its swap file in `<folder>`, which is on `<fs>`. A swap file needs ext4, XFS or btrfs. |
| `read_only` | `<folder>` is on a read-only filesystem. |
| `btrfs_layout` | The btrfs filesystem holding `<folder>` has more than one device, or its data is not single, which a swap file needs. |
| `not_enough_space` | A `<size>` swap file needs more free space in `<folder>`: `<free>` is free and LinuxIO leaves `<reserve>`. |
| `folder_not_root_only` | A folder on `<path>` is a link, or users other than root can change it. |
| `path_taken` | Something that is not LinuxIO's swap file is at `<path>`. Remove it to choose this. |

### A session without sudo

- **What the user sees** is unchanged: the Swap row is plain text, and the URL opens nothing.
- **The status** stays unprivileged and returns paths, sizes, filesystem types and flags only.
- **What it now reads:**
  - It stats LinuxIO's file (the folder is `0755`).
  - It runs `statfs`, and on btrfs `BTRFS_IOC_FS_INFO` and `BTRFS_IOC_SPACE_INFO` on `/var/lib/linuxio`. These need no capability (from memory); the without-root test on Fedora checks it.
  - It no longer reads `/etc/fstab`.
- **When a check fails for the session's user**, `unavailable_reason` is left empty. The change, running as root, checks again.

## 8. Privilege and safety review

- **No request input reaches a path, size, unit name or command argument.** The request carries `mode` and `confirm_replace`, as today. The path and unit are constants, and the size comes from `/proc/meminfo`.
- **The folder chain.**
  - Every folder from `/` to `/var/lib/linuxio/swap` is reached with `O_NOFOLLOW|O_DIRECTORY`.
  - Each must be a folder that root owns and no group or everyone may write. This is checked before create, start, `swapoff` and delete.
  - Only root can change such a folder, so the check still holds when the operation runs. This is today's argument, now applied to one path LinuxIO chose.
  - A link anywhere on the path (for example a `/var` that links elsewhere) refuses with `folder_not_root_only`. This also keeps the `/proc/swaps` path equal to the constant.
- **Another local user** can write none of those folders, so cannot plant a link or a file at the name.
  - The file is made with `O_EXCL|O_NOFOLLOW` through the folder handle, and is `0600`, so no one else can read what is swapped to it.
  - A file already there is reused only if it is a regular file owned by root that holds a swap signature. Its mode is set back to `0600`.
- **Subprocesses:** `mkswap` and `swapoff` (as today), with the constant path, through `hostcmd`, either bounded or cancellable.
- **Kernel calls on LinuxIO's own handles only:** `fallocate`, `FS_IOC_GETFLAGS` and `SETFLAGS`, and `BTRFS_IOC_FS_INFO`, `SPACE_INFO` and `SUBVOL_CREATE`. The last goes through the new handle-based btrfs function, with no top-level mount.
- **Deleting** happens only after all three hold: `swapoff` succeeded, the unit is gone, and a fresh `/proc/swaps` read does not list the file.
  - Unlinking an active swap file would leave swap that `swapoff` can no longer name; `/proc/swaps` would list it as `<path> (deleted)` (read: `docs/memory.md`).
  - That such an unlink is allowed at all is from memory.
- **Disk filling** is bounded: at most 8 GiB, and a reserve is always left free.
- **SELinux:**
  - mkswap labels the file `swapfile_t`.
  - An `optional` `filecon` mapping `/var/lib/linuxio/swap/swapfile` to `swapfile_t` in `packaging/selinux/linuxio.cil` stops a relabel from undoing that. It gives `linuxio_auth_t` nothing.
  - It needs the policy review, and a Fedora run both as the installer puts it and after a reboot (AGENTS.md).
- **No new socket, binary, word or route.**
  - The unit runs no LinuxIO program, so `rewrite_saved_unit_commands` has nothing to rewrite.
  - The three routes keep `Privileged()`.
- **Uninstall.** `uninstall.sh` removes `/etc/systemd/system/linuxio*`, and with `--remove-data` it removes `/var/lib/linuxio` (read). LinuxIO's unit name does not match that glob. The plan (owner decision 12):
  - Without `--remove-data`, the file and unit stay and keep working, so the host keeps its swap.
  - With `--remove-data`:
    1. `swapoff` the file. A failure stops the removal of that folder and says so.
    2. `systemctl disable` the unit and remove it.
    3. Remove the folder.

**For the privilege-boundary review:**
- the SELinux `filecon`;
- root creating and deleting a file and a btrfs subvolume under `/var/lib/linuxio`;
- `mkswap` run on a path;
- the new unprivileged status reads.

## 9. Tests

### Unit tests

Run with `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/memory`. They use fixture roots and the fake `hostSystem`. The fake gains `makeSwapFile`, `removeSwapFile` and `unitStates`, and loses `unmaskUnit`.

**LinuxIO's file:**
- The size: the 1 GiB floor, the 8 GiB cap, MiB rounding, and the reserve.
- Each `unavailable_reason`:
  - filesystem type from mountinfo, with ext2, ext3 and ext4 told apart;
  - read-only;
  - btrfs layout, through an injected checker;
  - free space, through an injected `statfs`;
  - a folder that is `0775` or not root's, using the fixture's owner, as `foldersOnlyOf` is tested today;
  - a link at `swap`, a folder at the file's name, and a file without a swap signature.
- A file that exists and holds swap is reused, with its mode put back to `0600`.
- The unit's exact content.

**Status and mode:**
- `managed`: the constant path is managed; `<path> (deleted)` is not; `zram0` with and without a provider; `zram1`; `others_can_write` set only on an unmanaged file.
- `deriveMode`: `zram` beside another file or partition; `mixed` only for LinuxIO's two; `none` beside other swap.
- `recommendMode` and `drainBlockedReason`.

**Plan and steps:**
- `planSwapMode` for every transition, with and without the file, its unit and its wants link. A refusal for each reason. zram allowed beside a partition.
- `create_swap_file` then `start_swap_file`.
- A `fallocate` `ENOSPC` that removes the file, and a failed `mkswap`.
- A start that fails, followed by a rollback that removes the unit of a file that never swapped and keeps the unit of one that swaps.
- `stop_swap_file`:
  - the room check on a fresh status;
  - zram gone by the time of the check;
  - `swapoff` failing or cancelled, and a cancel at the end of `swapoff`;
  - the unit removed before the file is deleted;
  - no delete while `/proc/swaps` still lists the file.
- Other swap is never touched: in every transition, rollback and drain, no call names its path or any unit of its.

**Drain:**
- Only LinuxIO's file is drained; `other_swap` is reported.
- The unit is written first when it is missing.
- Cancel and start-again: today's tests, moved to the new target.

**One bus connection per wait (Phase 1):**
- The fake counts sessions. A stop polled forty times opens one session.
- A read that fails on the held session falls back to a single read.
- `backend/common/systemd`: `UseUnitStates` on `testdbus`, with several reads in one session.

**btrfs:** `SwapfileLayout` and `CreateSubvolumeAt` against the fake kernel, with request numbers pinned as the package already does.

**Frontend:**
- `memoryStatusView.test.ts`: banner states, rows and their reasons, the other-swap note, the confirm items.
- `MemoryOverflowDialog.test.tsx` and the `memoryFeedback` texts.
- The fixture `memoryStatusFixture.ts`, the browser fixture, and `memory-overflow.spec.ts`.

### Root tests in the VM (`make vm-test-memory-integration`)

The rig keeps `/swap.img` in fstab, now as **other swap**. After every switch, drain and reboot it checks that `/swap.img` still swaps and that nothing was written for it: no drop-in under `swap.img.swap.d`, and fstab unchanged.

| Test | Ubuntu (ext4, zram-tools) | Ubuntu, `ZRAM_PROVIDER=zram-generator` | Fedora 45 (btrfs root, SELinux) |
|---|---|---|---|
| `TestMemoryOwnSwapFileIntegration` (new, Phase 2, details below the table) | yes | yes | yes, with the Fedora checks below |
| `TestMemoryIntegration` (rewritten) | yes | yes | yes |
| `TestMemoryDefaultZramIntegration` (rewritten: reads `zram` beside `/swap.img`, not `mixed`; a switch to disk makes LinuxIO's file on btrfs) | skips | yes (Ubuntu's package brings a default `zram0`) | yes |
| `TestMemoryStatusWithoutRootIntegration` (rewritten: LinuxIO's file active or absent, the `managed` flags, `unavailable_reason` empty) | yes | yes | yes |
| `TestMemoryDrainIntegration` (rewritten, details below) | yes | yes | yes, which also gives XFS coverage for RHEL's default |
| `TestMemoryRebootIntegration` (rewritten, details below) | yes | yes | yes |

**`TestMemoryOwnSwapFileIntegration`:**
- Makes, starts, stops and deletes the file through the executor's steps.
- Checks the folder is `0755` and the file `0600`, and checks the unit's content and its `systemctl show` dependencies.
- A tmpfs mounted on the folder is refused, with nothing written.
- A folder set to `0775` is refused.
- On Fedora it also checks:
  - the folder is a subvolume (inode 256);
  - the file is NOCOW and not compressed, by `FS_IOC_GETFLAGS`;
  - the file is labelled `swapfile_t`;
  - a snapshot of the subvolume holding `/` (`btrfs.CreateSnapshot`) succeeds while the file swaps and `/swap.img` is off.

**`TestMemoryDrainIntegration`:**
- LinuxIO's file sits on `vdb`, formatted xfs and mounted at the folder. `/swap.img` is turned off for the test.
- It parks 1.5 GiB, then 320 MiB with reads from `vdb` capped at 2 MiB/s.

**`TestMemoryRebootIntegration`:**
1. Switch to zswap and reboot. LinuxIO's file must be active through its unit, `swap.target` active, and `/swap.img` on.
2. Switch to zram and reboot. The file and unit must be gone, zram the only managed swap, and `swap.target` active.

**Changes to the rig:**
- The drain no longer uses `vdb` whole as swap. The rig formats it xfs with a label of its own, refuses a `vdb` that holds anything else, and unmounts it before the reboots.
- The VM's 4 GiB of RAM (`VM_MEMORY_GIB=4`, read) gives a planned size of about 1.9 GiB, hence 1.5 GiB parked.
- Dropped: `TestMemoryDrainAliasIntegration` and the `/swap-link` reboot run.
- Kept inside `TestMemoryIntegration`: the zram reset, the hung zramswap stop, the zram configuration file that is not LinuxIO's and its copy, zram-generator's other files, and zswap's round trip.
- `TestMemorySwitchIntegration` stays. `LINUXIO_MEMORY_EXPECT_HELD_OFF` becomes `LINUXIO_MEMORY_EXPECT_OWN_FILE`.
- The rig's `default_zram` keeps its unmask and drop-in cleanup of `swap.img.swap` until every VM has been reset once.

**Browser:**
- `make test-frontend-browser-quiet PLAYWRIGHT_ARGS=src/test/browser/memory-overflow.spec.ts`.
- `capability-install.spec.ts` once on Ubuntu and once on Fedora, since the dialog it opens changes its texts.

## 10. Phases

Each phase is one commit that leaves the feature working, with its docs.

| Phase | What | Files | Lines | Verification | VM runs |
|---|---|---|---|---|---|
| 1 | One bus connection per wait | 6 | +160 −25 | `make test-go-quiet` for `./common/systemd` and `./bridge/handlers/memory`, then `make check-backend-quiet` | 1: the memory suite on Ubuntu (the hung zramswap stop exercises the fail-fast) |
| 2 | LinuxIO's swap file, made and tested on its own; the status reports it (additive API) | ~20 | +1,400 −20 | unit tests, `make generate`, `make test-quiet` | 3: `vm-test-btrfs-integration`; the memory suite on Ubuntu; the memory suite on Fedora (install, SELinux, reboot) |
| 3 | Modes on LinuxIO's file; other swap read-only; fstab code deleted | ~45 | +2,600 −4,650 | `make test-quiet`, the browser spec | 3: Ubuntu; `vm-reset` then `ZRAM_PROVIDER=zram-generator`; Fedora |
| 4 | Uninstall and leftovers | ~5 | +60 −10 | `shellcheck`, `make check-backend-quiet`, and an uninstall with and without `--remove-data` in the VM through `make vm-ssh` | 1 |

### Phase 1: one bus connection per wait

- In `backend/common/systemd`, add `UseUnitStates(ctx, fn func(state func(name string) (string, error)) error) error`, built on one `UseSessionWithOptions(…, NoRetry: true)`. `GetActiveState` and the new function share `activeStateIn(session, name)`.
- Add `hostSystem.unitStates(ctx, fn)`. `waitStopped` and `waitUnitSeenOff` run their whole wait inside one session.
- A read that fails on the held connection is answered by one `GetActiveState` for that poll. A bus restart in the middle of a wait then costs today's connection rate, rather than losing the fail-fast checks.
- Single reads (`unitNotRunning` and the provider checks) stay as they are.
- Docs: the "Waiting on systemd" section says it. The backlog item is deleted.

### Phase 2: LinuxIO's swap file

1. **Check the facts first.** Check the "from memory" statements of sections 1 and 2 on the VMs (`man btrfs`, `man 5 systemd.swap`, `man 8 swapon`, `man 8 mkswap`, `man 5 tmpfiles.d`), and correct this plan before building.
2. **New `ownswap.go`:** the constants, the folder walk, the checks and `unavailable_reason`, creation, the unit's render, and removal.
3. **`system.go`:** `makeSwapFile` and `removeSwapFile` on `liveSystem`.
4. **`status.go`:** `own_swap_file`, and `SwapDevice.managed` and `others_can_write`. These are additive; mode, plan and drain are unchanged in this phase.
5. **`backend/common/btrfs`:** `SwapfileLayout` and `CreateSubvolumeAt`. `docs/btrfs.md` gets a line.
6. **`apischema/memory.go` and `contracts.go`:** the additive types. Run `make generate`. `memoryStatusFixture.ts` gains the new fields.
7. **`linuxio.cil`:** the `optional` `filecon`. `docs/process-systemd-architecture.md`, "SELinux", says so.
8. **`ownswap_integration_test.go`**, with a reboot pair. `suite_memory` runs it.
9. **`docs/memory.md`** gains a section "LinuxIO's swap file": path, filesystems, btrfs, creation, unit, and the status fields. It describes what the status reports, and the steps that no mode uses yet.

### Phase 3: modes on LinuxIO's file

The build order inside the commit, which is also the order to review it:

1. apischema and `contracts.go`, then `make generate`.
2. `modes.go`: derive, recommend, and the drain reason.
3. `status.go`.
4. `plan.go`.
5. `mode.go`: the three steps, rollback, `explainMixed`, and the log line.
6. `drain.go`.
7. Deletions: `fstab.go`, the hold-off code in `persist.go`, `escape.go`.
8. Unit tests.
9. Frontend: `memoryStatusView.ts`, the dialog, `memoryFeedback.ts`, the fixtures, and their tests.
10. Root tests and `suite_memory`.
11. Docs: `memory.md`, the memory rule in AGENTS.md, `privilege_pattern.md`, `capabilities.md`, `development.md`, `docs/README.md`, and the backlog.

AGENTS.md's memory rule becomes:

> A memory-overflow change adds swap before it removes any, removes swap only after the room check on a freshly read status, restarts zram at a new size only while disk swap is active, and never undoes added swap in a rollback. It stops, starts, drains and deletes only LinuxIO's own swap file (`/var/lib/linuxio/swap/swapfile`, through its own unit) and zram its provider runs, and only where root alone can write the folders on that path (`rootOnlyPath`); any other swap is shown and never stopped, drained or held off. See `docs/memory.md`.

The Phase 2 reboot pair is dropped in this phase, because the mode reboot test covers it.

### Phase 4: uninstall and leftovers

- `uninstall.sh` as described in section 8, with its fixture test.
- Backlog:
  - one Rejected line for taking over another swap file;
  - the later items (an adjustable size, a folder the admin picks), if the owner wants them listed.
- `docs/TODO/e2e-testing.md`, "Distributions": record the runs.
- Delete this plan's file. What it built is described in `docs/memory.md`.

## 11. Risks and open questions

- **Upstream behaviour from memory.** The btrfs constraints, systemd.swap's naming and precedence, ext4's and XFS's acceptance of unwritten extents, mkswap's SELinux label and the flag values were not read here. Phase 2's first step checks them. The kernel's `swapon` has the last word, and a refusal fails the start with its reason.
- **btrfs rollbacks.** A rollback of the subvolume holding `/var/lib` (LinuxIO's own Rollback, snapper, Timeshift) brings back an empty folder, because snapshots leave nested subvolumes out (read: `docs/btrfs.md`).
  - The unit then fails at boot, but `swap.target` does not wait on it.
  - The status reads the file as missing, and Apply makes it again; btrfs check 3 turns the empty folder back into a subvolume.
  - LinuxIO's Rollback keeps the old subvolume as a leftover, which cannot be deleted while it still holds the nested swap subvolume.
  - This becomes one Known limit, plus a line in `docs/btrfs.md`, "Rollback".
- **Two disk swap files after the upgrade**, on every host that had `/swap.img` and then picks disk or zswap. The dialog says so, and says how to remove the other one.
- **Hosts that lose disk and zswap**: `/var/lib` on ZFS, overlayfs or NFS, a multi-device btrfs, a full disk, or a `/var` that is a link. These hosts keep zram and their own swap.
- **Backups** that copy `/var/lib` also copy the file, up to 8 GiB. This becomes a Known limit. `btrfs send` of the parent subvolume leaves it out.
- **XFS** is tested only on the VM's `vdb`. RHEL itself is not run.
- **A hung `swapon` at boot**, for a file on a dead disk, ends at systemd's timeout (90 s, read), as for any swap unit.

## Decisions for the owner

1. **Path:** `/var/lib/linuxio/swap/swapfile`. Recommended. A folder the admin picks can come later.
2. **Filesystems:** ext4, XFS, and btrfs with one device and single data, in its own subvolume; refuse the rest with a reason. Recommended.
3. **Size:** half of RAM, 1 to 8 GiB, set when the file is made and shown in the dialog, not adjustable in this release. Recommended.
4. **Free space left:** at least 1 GiB or 5% of the filesystem, whichever is larger. Recommended.
5. **zram mode:** delete the file, its unit and its link, rather than keeping them disabled. Recommended.
6. **Disk or zswap beside other active disk swap:** still make LinuxIO's file, so each mode means one thing. Recommended.
7. **Other swap files:** read-only, with how to remove them and a warning for a folder others can write; no take-over or move. Recommended.
8. **State with only other swap:** `none` plus the device list, with no new mode value. Recommended.
9. **Migration:** no code; document the hand clean-up of old drop-ins and masks. Recommended.
10. **Recommendation on btrfs:** keep `zram`. Recommended.
11. **`systemd.EscapePath`:** delete it together with its last caller. Recommended.
12. **Uninstall:** keep the swap file without `--remove-data`; turn it off and remove it with `--remove-data`. Recommended.
13. **Unit priority:** none set. Recommended.
14. **SELinux:** an `optional` `filecon` for `swapfile_t`. Recommended.
15. **Format:** `mkswap` on the path, rather than a swap header written in Go. Recommended.
16. **Confirm before making the file:** no; the row already says where the file goes and how large it is. Recommended.
