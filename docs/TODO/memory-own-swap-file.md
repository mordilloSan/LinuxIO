# Memory overflow: swap files LinuxIO creates, resizes and removes

## Status

Not built. Rewritten with the owner on 2026-11-21. It replaces the plan of 2026-11-18 (LinuxIO manages only a file of its own and shows every other swap read-only), which is in git history at `34305478`.

Why it was rewritten:

- **The old plan sent most hosts to a terminal.** A swap file LinuxIO did not make was read-only, with text saying which commands remove it. Ubuntu puts `/swap.img` on every install, so that text would be the first thing most admins met.
- **Its fixed size was wrong for the owner's own host.** It made one file of at most 8 GiB. The planning host has 19 GiB of RAM and 24 GiB of swap in two files, because 8 GiB was not enough.
- **Adoption is the expensive part of today's code, not swap files.** A swap file is named in `/etc/fstab` by its plain path, never by a tag, and distributions put it at the top of the system disk where only root can write. The tag resolution, link following, unit names and hold-off drop-ins all serve cases a strict check can simply refuse.

Nothing is built until the owner approves this document, and Phase 0 checks the facts marked "from memory" before any code is written.

## Sources

Every statement about upstream behaviour carries one of two marks:

- **(read: …)** It was read on the planning host from the named file or command. The host runs Ubuntu 26.04.1 with kernel 7.0, systemd 259.9, util-linux 2.41.3 and btrfs-progs 6.17.1, on a btrfs root that is the top-level subvolume (`subvolid=5`).
- **(from memory)** It was not checked. Phase 0 checks each one on the test VMs (Ubuntu 26.04 and Fedora 45) and corrects this plan.

## The design in short

LinuxIO does three things to swap files and nothing else:

| Operation | Where | What it does |
|---|---|---|
| **Create** | Only at `/var/lib/linuxio/swap/swapfile` | Makes a swap file at a size the admin sets, adds its `/etc/fstab` line and turns it on |
| **Resize** | In place: same folder, same name | Turns the file off, changes its size, turns it on. A temporary swap file beside it stands in when the host would otherwise be without swap |
| **Remove** | In place | Turns the file off, removes its `/etc/fstab` line and deletes it |

- **Resize and Remove are offered only on a swap file that passes one strict check** (section 1). Any other swap is listed and never touched: a partition, an LVM volume, zram another tool set up, a swap file on an unusual path.
- **A file stays where the admin put it.** LinuxIO never moves a swap file on its own. One offer, on btrfs, moves a file into LinuxIO's folder when the admin asks (section 6).
- **The three modes stay** as the simple choice on top, built from the same operations (section 7).
- **Nothing is held off, started or stopped through systemd units.** A swap file is either there and on, or removed.

## What changes

| | Today | After |
|---|---|---|
| Swap files LinuxIO acts on | One: the first `/etc/fstab` names that is active or held off | Every swap file that passes the strict check |
| How a swap file is found | fstab is parsed, tags become `/dev/disk/by-*` links, links are followed, unit names are derived (`fstab.go`) | An fstab line whose first field is a plain absolute path, compared with `/proc/swaps` as written |
| A swap file behind a link, or on a path others can write | Followed and judged, link by link (`othersCanWrite`) | Refused by the strict check, and listed |
| Creating a swap file | Never | Create, in LinuxIO's folder |
| Changing a swap file's size | Never | Resize, in place |
| Removing a swap file | Never; a switch to compressed memory stops it and holds its unit off | Remove; a switch to compressed memory removes it, after a confirm that names it |
| Keeping a swap file off at boot | A condition drop-in on its unit, `90-linuxio-memory.conf` | Not done. Its fstab line is removed with the file |
| Turning a swap file on | Starting its systemd unit and waiting (`startUntilBack`, `waitUnitSeenOff`) | LinuxIO runs `swapon` itself |
| A second swap file | Not managed, stays beside zram | A row like the first |
| A swap partition or LVM volume | Never stopped by a mode change; drained when fstab names it; zram cannot be chosen beside it | Listed, never turned off, not drained; zram can be chosen beside it |

## 1. Which swap LinuxIO acts on

### The list

The status lists every swap device in `/proc/swaps` (as today), and every swap file `/etc/fstab` names, active or not. A swap file fstab names is a line whose third field is `swap` and whose first field, after `\ooo` unescaping (`unescapeOctal`, exists), is an absolute path that is not under `/dev`.

### The strict check

Resize and Remove are offered on a swap file only when all of this holds:

1. **One or more fstab lines name it by its exact path.** The path is compared as a string with the `/proc/swaps` path. No tag, no link, no second name.
2. **Only root can change the path.** Every folder from `/` down is a folder, not a link, that root owns and that no group or everyone may write. This is today's `rootOnlyPath` (`swapoff.go`, exists), which `/swap.img` and `/swapfile` pass.
3. **It is a regular file that root owns**, opened through its folder's handle with `O_NOFOLLOW`.
4. **It is not the host's hibernation file** (see below).

A row that fails carries one `locked_reason` and no buttons:

| `locked_reason` | When | What the dialog says |
|---|---|---|
| `not_in_fstab` | An active swap file no fstab line names: started by hand, by a `.swap` unit file, or by a tool such as dphys-swapfile | Something other than `/etc/fstab` turns this on, so LinuxIO cannot keep it off |
| `not_root_only` | A folder on the path is a link or can be changed by users other than root, or the file is not a regular file root owns | Another user could put a file of their own at this name |
| `hibernation` | The host resumes from this filesystem at a file offset | Changing this file would break hibernation |

A partition, an LVM or LUKS volume and a zram device are not swap files and have no buttons either. They show their kind, size and use.

Nothing is "managed" or "unmanaged" any more. There is no chosen file: two swap files that pass the check are two rows with the same buttons.

### The hibernation file

A swap file on the device `/sys/power/resume` names, while `/sys/power/resume_offset` is not zero, is locked (both files from memory). The offset names where the file's first block is on disk, so a resize or a new file breaks it. LinuxIO does not work out which of several files on that filesystem the offset belongs to; it locks them all.

## 2. The building blocks

Every operation is built from five steps. Each one already exists or is small.

| Block | What it does | Status |
|---|---|---|
| **Make a swap file** | Through a folder handle: create `name` with `O_CREAT\|O_EXCL\|O_NOFOLLOW`, mode `0600`; on btrfs set NOCOW on the empty file; `fallocate` to the size; `fsync`; `mkswap` | New (section 3) |
| **Turn on** | Run `swapon -- <path>`, with `-p <n>` and `-d[<policy>]` when the file's fstab line has `pri=` or `discard`. Then wait for the path in `/proc/swaps` | New. `swapon` has `-p` and `-o` (read: `swapon --help`) |
| **Turn off** | The room check on a freshly read status, then `swapoff` as a process of its own on the `/proc/swaps` path | Exists (`swapOffDevice`, `rootOnlyPath`) |
| **Edit `/etc/fstab`** | Add or remove the lines whose first field is the path, under the fstab lock the storage pages use (`btrfs.FstabLockPath`), written with `atomicfile.Write`. Then a systemd reload, so the generated unit follows | New for memory. The lock and the atomic write exist (read: `storage/fstab.go`) |
| **Delete** | Read `/proc/swaps` again; `unlinkat` through the folder handle, only when the path is not listed and the name still holds the file that was checked (same device and inode) | New |

**Why LinuxIO runs `swapon` itself.** Today a start goes through the file's systemd unit, which is why LinuxIO derives unit names and, because it runs `swapoff` itself, waits for systemd to notice the device left before a start means anything (`waitUnitSeenOff`, `startUntilBack`). Running `swapon` removes the unit name and both waits. systemd follows `/proc/swaps` either way (read: `docs/memory.md`, "Taking a disk swap device away"). What boot does is still systemd's, from fstab, and the reboot tests cover it.

**The room check for turning a file off.** The file's used bytes, plus `swapReturnMargin` (512 MiB, exists), must be less than `MemAvailable` plus the free space on the other active disk swap devices. Today's `fitsInMemory` counts memory only; counting free disk swap follows what `zramFits` already does for zram, for the same reason: while the kernel reads the pages back it can push others out to the swap that remains.

## 3. Create

### Path

`/var/lib/linuxio/swap/swapfile`, in its own folder. One file at a time: Create is offered while nothing is at that path.

- `/var/lib/linuxio` is LinuxIO's state folder, `0755 root root`, made by `packaging/systemd/linuxio-tmpfiles.conf` (read, in the earlier plan).
- The folder `swap` is `0755 root root`, so a session without sudo can stat the file for the status. The file is `0600 root root`.
- The admin does not choose the folder. Someone who wants the file on another disk makes it there by hand once, with its fstab line; from then on LinuxIO resizes and removes it there (section 1).

### Filesystems

LinuxIO makes a swap file only on `ext4`, `xfs` and `btrfs`. This applies to Create and to the temporary file of a Resize.

- The type comes from mountinfo for the folder, and is checked again with `fstatfs` on the folder handle against `EXT4_SUPER_MAGIC`, `XFS_SUPER_MAGIC` and `BTRFS_SUPER_MAGIC` (in `golang.org/x/sys/unix`). Mountinfo is needed because ext2, ext3 and ext4 share one magic number.
- ext4 and xfs accept a fallocated swap file (from memory; the test VM already swaps on a fallocated `/swap.img` on ext4).
- Anything else is refused with `unsupported_filesystem`, a read-only filesystem with `read_only`.

### btrfs

What upstream requires (btrfs(5), "SWAPFILE SUPPORT", from memory): one device, a single data profile, a NOCOW file with no holes; a subvolume holding an active swap file cannot be snapshotted; balance skips the file's block groups.

Before creating, in this order:

1. `BTRFS_IOC_FS_INFO` reports one device, and `BTRFS_IOC_SPACE_INFO` reports every data block group type as single. Otherwise the reason is `btrfs_layout`. `backend/common/btrfs` wraps both ioctls already; this plan adds `btrfs.SwapfileLayout(dir *os.File)`.
2. `/var/lib/linuxio/swap` is a subvolume root (inode 256) or absent. LinuxIO makes it a subvolume through the handle of `/var/lib/linuxio`, with a new `btrfs.CreateSubvolumeAt(dir *os.File, name string)` on the existing `createSubvolumeOn`. An empty plain folder at the name is replaced; a folder with anything in it gives `path_taken`.
3. On the new, empty file, `FS_IOC_SETFLAGS` sets `FS_NOCOW_FL` (`0x00800000`) and `FS_NOCOMP_FL` (`0x400`) and clears `FS_COMPR_FL` (`0x4`) (values from memory; a test pins them). `FS_IOC_GETFLAGS` must then read NOCOW set, or the file is removed and the step fails.

A snapshot leaves nested subvolumes out (read: `docs/btrfs.md`), so a swap file in this subvolume does not block a snapshot of the subvolume that holds `/var/lib`.

### Size

- **The admin sets it**, in the dialog, in whole MiB, at least 256 MiB.
- **The default** is half of `MemTotal`, at least 1 GiB and at most 8 GiB, the same as zram's planned size. It is a starting value, not a limit.
- **The limit is the disk.** The filesystem's free space (`f_bavail * f_bsize`) must hold the file and still leave `max(1 GiB, 5% of the filesystem)`. Otherwise the reason is `not_enough_space`, and the status carries the free space and the reserve so the dialog can show the largest size.
- On btrfs `statfs` is an estimate. A `fallocate` that returns `ENOSPC` removes the file and fails the step.

### Steps

1. Walk from `/` to `/var/lib/linuxio` with `O_DIRECTORY|O_NOFOLLOW`, applying the root-only rule to each folder.
2. Make or open `swap` (a subvolume on btrfs), set it `0755`.
3. Make the swap file (section 2).
4. Add the fstab line `/var/lib/linuxio/swap/swapfile none swap sw,nofail 0 0`, and reload systemd.
5. Turn it on.

- **`nofail`** makes `swap.target` want the file's unit instead of requiring it (the generator requires the units of entries without it, read in the earlier plan from `/run/systemd/generator/swap.target.requires`). A file that is missing after a btrfs rollback then does not keep `swap.target`, and zram with it, from starting.
- **`mkswap` on the path**, not a header written in Go: it is in util-linux everywhere and, where SELinux is on, labels the file `swapfile_t` (from memory).
- **Something already at the path:** a regular file root owns that holds a swap signature (`holdsSwap`, exists) and has no fstab line is LinuxIO's own from a create that was cut short; it is deleted and made again. Anything else gives `path_taken`.

## 4. Resize

The kernel cannot change the size of a swap file while it is on, and refuses to rename or delete one (from memory: `may_delete` in `fs/namei.c` returns `EPERM` for a swap file). So a resize turns the file off, changes it and turns it on again, and the question is only what the host swaps on meanwhile.

### The steps

For a file `F` in folder `D`, read from a fresh status:

1. **Temporary file, when needed.** When `F` is the only active swap device, or its data does not pass the room check without help, LinuxIO makes `D/<name>.linuxio-resize` and turns it on. Its size is the data parked on `F` plus a quarter, at least 256 MiB, at most the size of `F`. It has no fstab line.
2. **Turn `F` off**, after the room check on a fresh status, which now counts the temporary file's free space.
3. **Change the size** through the file's handle: `fallocate` to grow, `ftruncate` to shrink, `fchmod 0600`, `fsync`, then `mkswap` on the path.
4. **Turn `F` on.**
5. **Turn the temporary file off** after its own room check, and delete it.

- **Nothing moves and nothing is renamed.** The path, the folder and the fstab line are the same before and after, so a resize does not write `/etc/fstab`.
- **In place, not delete and make again**, so that at no point is the file missing while its fstab line stands. On btrfs the file keeps its NOCOW flag; LinuxIO reads it with `FS_IOC_GETFLAGS` before step 3 and refuses a file that lost it.
- **Disk room** is checked before step 1: the temporary file, plus the growth, plus the reserve of section 3. A resize needs little more than the growth itself.
- **The new size** follows the rules of section 3: whole MiB, at least 256 MiB, limited by the disk.
- **A file that is off** (in fstab but not swapping) is not resized. Its row offers Remove only.

### What a failure leaves

| Fails | Left |
|---|---|
| Making or turning on the temporary file | Nothing changed. The temporary file is deleted |
| Turning `F` off: no room, or cancelled | `F` swaps at its old size. The temporary file is turned off and deleted |
| Growing (`ENOSPC`) | `F` is unchanged, with its old swap header. It is turned on again at its old size, then step 5 |
| `mkswap` after a shrink | The file is at the new length without a header. `mkswap` is run once more; if that fails too the temporary file stays on, the error says so, and the row reads "not a swap file" with Remove |
| Turning `F` on | The temporary file stays on, because added swap is never undone, and the error says the host swaps on it |
| A crash or power loss | At most a temporary file is left |

**A leftover temporary file** is recognised by its name beside a row's file. Off, it is deleted at the start of the next Resize or Remove of that file. On, it is listed with a Remove button of its own, which turns it off and deletes it; there is no fstab line to remove.

## 5. Remove

For a file `F`, read from a fresh status:

1. Turn `F` off, after the room check. A file that is already off skips this.
2. Remove its fstab lines, and reload systemd.
3. Delete the file.

- **The fstab line goes before the file.** A crash between the two leaves a file that is off and that nothing turns on, which wastes disk and nothing else. The other order would leave a line naming a missing file, and a unit that fails at every boot.
- **Removing the last swap is allowed.** The confirm says that the host will then have no swap and that Linux closes programs when memory runs out.
- **The confirm names the file and its size**, and says that data parked on it is brought back to memory first.
- The file's folder is left as it is, LinuxIO's subvolume included.

## 6. Move into LinuxIO's folder, on btrfs

A swap file on btrfs blocks snapshots of the subvolume that holds it while it is on, and this cannot be fixed in place: a subvolume is a folder, and a snapshot taken while the file is off shares its blocks, which a swap file may not have (all from memory). Ubuntu puts `/swap.img` in the subvolume of `/`, so on a btrfs root the system cannot be snapshotted while it swaps to disk. This is why the dialog exists (read: `docs/memory.md`).

The dialog therefore says, for a swap file on btrfs that passes the strict check, is on the same filesystem as `/var/lib/linuxio`, and is not already in LinuxIO's folder:

> `/swap.img` blocks snapshots of the subvolume it is in. LinuxIO can make it again in a subvolume of its own, at the same size.

The action is Create at the file's size, then Remove of the old file, in that order, so swap is added before any is removed. It is offered only while LinuxIO's path is free, and needs disk room for both files for the moment both exist.

Nothing moves unless the admin presses it. On a file on another filesystem the line is shown without the action, because a move would take the file off the disk the admin chose.

## 7. The three modes

### What each means

| Mode | Disk swap | zram its provider runs | zswap |
|---|---|---|---|
| `disk` | On | Off at boot, stopped | Off now and at boot |
| `zswap` | On | Off at boot, stopped | On now and at boot |
| `zram` | Every swap file that passes the strict check is removed | On at boot, swapping at the planned size | Off now and at boot |

**How the status names the host** (`deriveMode`), first match wins:

1. zram is active and so is a swap file LinuxIO can remove: `mixed`. Apply finishes it.
2. zram is active: `zram`, whatever other swap stays beside it.
3. Any disk swap is active, file or partition: `zswap` or `disk`, by zswap's `enabled`.
4. Otherwise: `none`.

Today any zram beside any disk swap is `mixed`, which a host with a swap partition could never leave, and zram is refused there (`swap_partition_active`). That refusal goes: zram can be chosen beside a partition, and the dialog says the partition stays on and takes what compressed memory cannot hold.

### Order of steps

| To | Steps |
|---|---|
| `zram` | `write_config`, `start_zram`, `apply_sysctls`, `disable_zswap`, then `remove_swap_file` for each swap file that passes the strict check |
| `zswap`, `disk` | `create_swap_file` when no disk swap is active and none is in fstab; `stop_zram` when a provider is installed; `apply_sysctls`; `enable_zswap` or `disable_zswap` |

- **A switch to `zram` removes files, so it asks first.** The confirm step lists each file and its size. The request carries the paths the admin confirmed (`confirm_remove`), and the plan refuses with `ErrConflict` when they are not the files it would remove, as `confirm_replace` works today.
- **A switch to `zswap` or `disk` on a host with no disk swap makes LinuxIO's file.** The row shows the size field; the request carries the size.
- **A swap file that is in fstab but off**, as an older LinuxIO's hold-off leaves it, is not turned on by a switch to disk. The row says it is off, and LinuxIO's file is not made while it is there. The admin removes it and applies again (section 12).
- **`remove_swap_file` keeps today's conditions for stopping a file:** a zram device swaps in the fresh status and the room check passes. Otherwise the step records `left` (`zram_not_swapping`, `not_enough_memory`) and the file stays, with its fstab line.
- `start_zram`, `stop_zram`, `write_config`, the sysctls and zswap are unchanged.

### Recommendation

First match wins. It is today's table without the rows about unmanaged swap:

| Host | Recommended |
|---|---|
| No disk swap, and LinuxIO cannot make its file | `zram` |
| No disk swap | `zram` |
| Disk swap on an SD card, eMMC or removable disk (`disk_hint`) | `zram` |
| A swap file on btrfs outside a subvolume of its own | `zram` |
| The kernel has no zswap | `zram` when there is no disk swap, `disk` otherwise |
| Anything else | `zswap` |

### The standing rules

| Rule (AGENTS.md, `docs/memory.md`) | After |
|---|---|
| Add swap before removing any | Stays. A resize of the only swap device adds the temporary file first; a move creates before it removes |
| Remove swap only after the room check on a freshly read status | Stays, for Resize, Remove, the modes and a drain |
| Restart zram at a new size only while disk swap is active | Stays |
| Never undo added swap in a rollback | Stays. A file Create made, and a temporary file that is the only swap, stay on after a failure |
| Stop or hold off a device only when it is positively matched to an fstab entry | Becomes the strict check: an exact path in fstab, nothing resolved |
| A condition drop-in, never a mask | Goes. Nothing is held off |
| Act on a swap file only where root alone can write the folders (`othersCanWrite`, `rootOnlyPath`) | Narrows to `rootOnlyPath` on one plain path. `othersCanWrite` and the link rules go |

## 8. Draining

"Bring back to memory" stays, for swap files that pass the strict check: for each one holding data, the room check, turn off, turn on. It is Resize without the size change, and uses the same two blocks.

A partition or LVM volume is no longer drained. Today one that fstab names is, which needs the tag and alias matching this plan deletes. When the parked data is only on such a device, `drain_blocked_reason` is `other_swap` and the dialog says LinuxIO does not turn that device off.

## 9. What is deleted and what stays

The names below were checked to exist on `34305478`; read the plan against the code before building.

### Deleted

| What | Where |
|---|---|
| Tag and link resolution, unit-name derivation, positive matching of devices to entries, the "others can redirect" walk: `fstabSwaps` as it is, `matchFstabSwaps`, `othersCanWrite` and their helpers | `fstab.go` (302 lines) shrinks to a plain-path reader; `fstab_test.go` (324 lines) with it |
| The managed-file choice: `managedSwapFile`, `describeSwapFiles` as it is, `unmanaged_fstab_swaps` | `plan.go`, `status.go`, `apischema/memory.go` |
| Holding off and letting go: `holdOff`, `letGo`, `unmaskIfMasked`, the hold-off drop-in's path, folder and render | `mode.go`, `persist.go` |
| Starting through units: `startUntilBack`, `waitUnitSeenOff` | `mode.go`, `drain.go`, `executor.go` |
| `drainTargets` and the masked-unit and alias handling of a drain | `drain.go` |
| `SwapFile`, `SwapFileUnmanagedReason`, `UnmanagedFstabSwap`, `ZramSwapPartitionActive`, the drain reasons `unmanaged_device`, `unit_held_off`, `others_can_write`, the left reasons `not_identified`, `others_can_write`, `not_managed` for a swap file | `apischema/memory.go`, `contracts.go` |
| The unmanaged-swap notes and texts of the dialog | `memoryStatusView.ts`, `memoryFeedback.ts` |
| Root tests of aliases, links and the hold-off across a reboot, and their rig helpers | `drain_integration_test.go`, `boot_integration_test.go`, `packaging/vm/linuxio-vm.sh` |
| "Swap units", "A swap file others can redirect is not managed", "Holding a swap file off at boot", the alias paragraphs of "Draining", and about half of Known limits | `docs/memory.md` |

### Stays

- **`systemd.EscapePath`.** The earlier plan deleted it; `backend/common/btrfs` uses it now (`fstab_bound.go`, `unready.go`).
- **The zram provider**, zram-tools' conffile handling, zram-generator's other files, the sysctls, zswap and the TuneD handling.
- **`swapOffDevice`, `rootOnlyPath`, `foldersOnlyOf`**, the room margin, `statusAfterChange` and the mutation lock.
- **The readers:** `readMounts`, `mountHolding`, `diskHint`, `removable`, `holdsSwap`, `unescapeOctal`.
- **`waitStopped`**, for the zram provider's unit.

### How large the change is

Rough, from the earlier plan's counts and not measured again:

| | Removed | Added |
|---|---|---|
| Go, not tests | ~1,100 | ~1,300 |
| Go tests, unit and root | ~2,400 | ~2,900 |
| Frontend with its tests | ~600 | ~900 |
| VM rig | ~150 | ~200 |
| Docs | ~500 | ~500 |
| Total | ~4,750 | ~5,800 |

It is about 1,000 lines more than today, across about 75 files, with four new privileged routes. The reason is that Create, Resize and Remove are three things LinuxIO cannot do at all today. What shrinks is the number of rules that each needed a review: links, tags, unit names, hold-offs, masks, the second swap file. What replaces them is one string comparison and one folder rule.

## 10. API and UI

### The contract

- **`SwapDevice`** (a `/proc/swaps` row, as today) is joined by **`SwapFileRow`**, one per swap file in `/proc/swaps` or fstab:

  ```go
  type SwapFileRow struct {
      Active              bool                 `json:"active"`
      BlocksSnapshots     bool                 `json:"blocks_snapshots,omitzero"`
      CanMove             bool                 `json:"can_move,omitzero"`
      DiskHint            SwapDiskHint         `json:"disk_hint,omitzero"`
      FreeSpace           uint64               `json:"free_space"`
      FSType              string               `json:"fs_type"`
      InFstab             bool                 `json:"in_fstab"`
      LockedReason        SwapFileLockedReason `json:"locked_reason"`
      MaxSize             uint64               `json:"max_size"`
      Path                string               `json:"path"`
      ResizeBlockedReason SwapFileResizeReason `json:"resize_blocked_reason"`
      Size                uint64               `json:"size"`
      Temporary           bool                 `json:"temporary,omitzero"`
      Used                uint64               `json:"used"`
  }
  ```

  `locked_reason` is empty, `not_in_fstab`, `not_root_only` or `hibernation`. `resize_blocked_reason` is empty, `off`, `unsupported_filesystem`, `read_only` or `btrfs_layout`.
- **`SwapStatus`** gains `swap_files`, and `create` (the path, the default size, the largest size, and why Create is not offered: `exists`, `unsupported_filesystem`, `read_only`, `btrfs_layout`, `not_enough_space`, `folder_not_root_only`, `path_taken`). It loses `swap_file` and `unmanaged_fstab_swaps`.
- **`SwapModeRequest`** gains `confirm_remove []string` and `swap_file_size uint64`.
- **`SwapStep`** gains `create_swap_file`, `resize_swap_file`, `remove_swap_file`, `make_temporary_swap` and `remove_temporary_swap`, and loses `stop_swap_file` and `start_swap_file`.

### Routes

Four new routes, each a `MutationTask` with `Privileged()` and `SwapProgress`, under the lock that allows one memory change at a time:

| Route | Request |
|---|---|
| `memory.create_swap_file` | `size` |
| `memory.resize_swap_file` | `path`, `size` |
| `memory.remove_swap_file` | `path` |
| `memory.move_swap_file` | `path` |

A `path` from a request is never used as given. It selects the row of a freshly read status with that exact path, and the operation runs on that row or refuses with `ErrNotFound`; a row with a `locked_reason` refuses with `ErrConflict`.

### The dialog

- **A "Swap files" section** under the modes: one row per swap file with its path, size and use, and Resize and Remove buttons, or the reason it has none. Other swap devices follow as plain rows. Below them, "Create a swap file" while LinuxIO's path is free, or the reason it cannot be made.
- **Resize** opens a size field with the current size, the largest size the disk allows, and a line saying that the file is turned off and on again and that data parked on it is brought back to memory first.
- **Remove** and the switch to compressed memory use the confirm of section 5.
- **The btrfs line** of section 6 sits on the row it is about.
- **These are long jobs.** Each dialog closes when its job starts, and the task tray holds the progress and the cancel (`docs/jobs.md`).
- **A session without sudo** sees what it sees today: the status is unprivileged and now also reads `/etc/fstab`, which everyone may read. A folder the reader may not look into is not judged, as today; the change, running as root, checks again.

## 11. Privilege and safety review

- **Request input.** A size, validated against the status's limits, and a path that only selects a row. Neither reaches a command line unchecked: `mkswap`, `swapon` and `swapoff` get the row's path after `--`, and that path is absolute.
- **Another local user.** Every folder on a path LinuxIO acts on is one only root can change, so no name on it can be replaced between the check and the action. This is today's argument (`docs/memory.md`, "Taking a disk swap device away"), applied without the link cases.
- **Files are reached through handles.** A file is made with `O_EXCL|O_NOFOLLOW` through its folder's handle; a resize opens the file with `O_NOFOLLOW` and works on the handle; a delete checks device and inode before `unlinkat`.
- **`/etc/fstab`** is written by root already (`storage/fstab.go`, `btrfs/fstab_bound.go`). Memory takes the same lock, removes or adds only lines whose first field is the row's path and whose type is `swap`, leaves every other byte as it was, and writes atomically.
- **Deleting** happens only when `swapoff` succeeded and a fresh `/proc/swaps` does not list the path. A swap file that is off holds nothing, so a delete loses no data.
- **Disk filling** is bounded by the reserve of section 3, for Create, Resize and the temporary file.
- **SELinux.** `mkswap` labels the file `swapfile_t` (from memory). An `optional` `filecon` for `/var/lib/linuxio/swap/swapfile` in `packaging/selinux/linuxio.cil` keeps a relabel from undoing that; it gives `linuxio_auth_t` nothing. It needs the policy review and a Fedora run as the installer puts it and after a reboot.
- **No new socket, binary or word.** Four new routes, all `Privileged()`.
- **Uninstall.** Without `--remove-data` the file and its fstab line stay and the host keeps its swap. With it, `uninstall.sh` turns the file off, removes the fstab line whose first field is LinuxIO's path, and then removes the folder; a `swapoff` that fails stops the removal of that folder and says so.

**For the privilege-boundary review and the independent storage review:** root writing `/etc/fstab` from the memory handler; root resizing and deleting a file it did not make; the btrfs subvolume and NOCOW file; `mkswap` and `swapon` run on a path; the SELinux `filecon`.

## 12. Migration

The owner is the only user and accepts breaking saved state. There is no migration code; the release note and `docs/memory.md` say what to clean up by hand.

| Host before the upgrade | After |
|---|---|
| On disk or zswap, swap files in fstab (the planning host: `/swap.img` and `/swap2.img`) | Nothing changes. Each file is a row with Resize and Remove |
| On zram, `/swap.img` held off by `swap.img.swap.d/90-linuxio-memory.conf` | The row reads "off". It stays off at every boot through the old drop-in. Remove deletes the file and its line; the drop-in and its folder are left: `rm -r /etc/systemd/system/swap.img.swap.d` |
| On zram, `/swap.img` masked by an older LinuxIO | As above, and `systemctl unmask swap.img.swap` |

## 13. Tests

### Unit tests

`make test-go-quiet GO_TEST_PKGS=./bridge/handlers/memory`, on fixture roots and the fake `hostSystem`.

- **The strict check:** a plain path passes; a link on the path, a folder that is `0775` or not root's, a file that is a link, a tag source, a `/dev` source, an escaped path, two lines for one path, and the hibernation file, each with its `locked_reason`.
- **Create:** each refusal reason; the size limits and the reserve; `ENOSPC` and a failed `mkswap` remove the file; the fstab line's exact text; a leftover file at the path.
- **Resize:** with and without the temporary file; its size; the room check counting it; each row of the failure table; a leftover temporary file, off and on; fstab byte-for-byte unchanged.
- **Remove:** the order (off, fstab, delete); no delete while `/proc/swaps` lists the path; a changed inode refuses; the last swap; a file that is off.
- **fstab edits:** only the matching swap lines change; comments, spacing and other lines are kept byte for byte; two concurrent edits under the lock.
- **Modes:** `deriveMode` and `recommendMode` tables; the `zram` plan with one, two and no removable files; `confirm_remove` that does not match; `zswap` on a host with no disk swap makes the file; a partition is never named in any step.
- **Drain:** only swap files; `other_swap`.
- **btrfs:** `SwapfileLayout` and `CreateSubvolumeAt` against the fake kernel, request numbers pinned.
- **Frontend:** the rows and their reasons, the size field's limits, the confirms, the btrfs line, and the browser spec `memory-overflow.spec.ts`.

### Root tests in the VM

`make vm-test-memory-integration`, on Ubuntu (ext4, with `/swap.img`), Ubuntu with `ZRAM_PROVIDER=zram-generator`, and Fedora 45 (btrfs root, SELinux, no swap file).

- **Create**, then a reboot: the file swaps through its fstab line and `swap.target` is active. On Fedora: the folder is a subvolume, the file is NOCOW and labelled `swapfile_t`, and a snapshot of the subvolume holding `/` succeeds while it swaps.
- **Resize** of `/swap.img` up and down, as the only swap (with the temporary file) and beside zram (without): the path and `/etc/fstab` are unchanged, the new size is in `/proc/swaps`, parked data survives, and a reboot brings it back at the new size.
- **Remove** of `/swap.img`: gone from `/proc/swaps`, from fstab and from disk, and still gone after a reboot.
- **Move** on a btrfs root that has a swap file in the subvolume of `/`: a snapshot is refused before and succeeds after.
- **The modes:** to zram (files removed), a reboot, back to zswap (LinuxIO's file made), a reboot.
- **A partition** as swap on `vdb`: untouched through every operation, and zram can be chosen beside it.
- **`make vm-test-btrfs-integration`** for the two new btrfs functions, as a change under `backend/common/btrfs` that issues ioctls requires.

## 14. Phases

Each phase is one commit that leaves the feature working, with its docs.

| Phase | What | Verification |
|---|---|---|
| 0 | Check the facts marked "from memory" in the VM; correct this plan. No code | By hand, through `make vm-ssh` |
| 1 | The blocks and Create: make a swap file, turn on, the fstab edit, delete; the two btrfs functions; `swap_files` and `create` in the status; `memory.create_swap_file` | Unit tests, `make generate`, `make test-quiet`; the memory suite on Ubuntu and Fedora; `vm-test-btrfs-integration` |
| 2 | Resize, Remove and Move, their routes, and the "Swap files" section of the dialog | `make test-quiet`, the browser spec; the memory suite on Ubuntu and Fedora |
| 3 | The modes and the drain on the blocks; the old adoption code deleted | `make test-quiet`; the memory suite on all three VM setups |
| 4 | Uninstall, the SELinux `filecon`, the docs and the backlog; this file deleted | `shellcheck`, `make check-backend-quiet`; an uninstall with and without `--remove-data` and a Fedora install and reboot in the VM |

**Phase 0 checks**, each a few commands as root in a VM:

1. The kernel refuses to rename and to delete an active swap file.
2. btrfs refuses a snapshot of a subvolume with an active swap file, allows it once the file is off, and then refuses `swapon` on that file.
3. A snapshot of a subvolume succeeds while a swap file swaps in a nested subvolume.
4. A swap file on btrfs grown with `fallocate` and formatted again with `mkswap` turns on; so does one shrunk with `ftruncate`. The same on ext4 and xfs.
5. `/sys/power/resume` and `/sys/power/resume_offset` read as section 1 assumes.
6. `swapon -p` and `-d` set what `pri=` and `discard` in fstab set at boot.
7. An fstab swap line with `nofail` is wanted, not required, by `swap.target`, and a missing file does not fail the target.
8. `mkswap` labels a file `swapfile_t` on Fedora.
9. systemd shows the generated unit active after LinuxIO's own `swapon`, and inactive after its `swapoff`.

AGENTS.md's memory rule becomes, in Phase 3:

> A memory-overflow change adds swap before it removes any (a resize of the only swap device adds a temporary swap file beside it first), removes swap only after the room check on a freshly read status, restarts zram at a new size only while disk swap is active, and never undoes added swap in a rollback. It turns off, resizes, drains or deletes only a swap file that an `/etc/fstab` line names by its plain path, where every folder on that path is one root owns that no group or everyone may write (`rootOnlyPath`), and the zram its provider runs; it creates a swap file only at `/var/lib/linuxio/swap/swapfile`; a partition, an LVM volume and any other swap are shown and never turned off. See `docs/memory.md`.

## 15. Limits to write under Known limits when this is built

- A swap partition or LVM volume is never turned off, so it is not drained and stays beside zram.
- A swap file that fstab does not name, or whose path has a link or a folder others can change, is listed and not changed.
- A btrfs filesystem with more than one device, or data that is not single, cannot hold a swap file, so such a root has compressed memory and whatever swap the admin made elsewhere.
- LinuxIO makes its file only in `/var/lib/linuxio/swap`. A file on another disk is made by hand once and then resized and removed from the dialog.
- A switch to compressed memory removes every swap file that passes the check; there is no way to keep one beside zram from the dialog.
- A resize needs free memory or free swap for the data parked on the file, and is refused otherwise.
- A file left off and without an fstab line by a Remove that was cut short is not listed.
- A rollback of the subvolume holding `/var/lib` (LinuxIO's own, snapper, Timeshift) brings back an empty folder in place of the swap subvolume. The fstab line then names a missing file, which `nofail` makes harmless at boot; the row reads "missing" with Remove.
- A backup that copies `/var/lib` copies the swap file.

## Decisions for the owner

Decided in conversation on 2026-11-20 and 2026-11-21:

1. LinuxIO acts on swap files only, never on partitions.
2. Three operations: create, resize, remove.
3. Create goes in a locked folder of LinuxIO's own.
4. Resize is in place, through a temporary file beside it; the file is never moved or renamed.
5. The admin sets the size; 8 GiB is not a limit.
6. No picker of disks or folders.
7. On btrfs the dialog offers to make the file again in LinuxIO's folder; nothing moves unless asked.

Calls made while writing this, each to confirm or change:

1. **LinuxIO's file is turned on at boot by an fstab line, with `nofail`,** not by a unit file of its own as the earlier plan had it. Every swap file is then one kind of row, and Remove has one path. Recommended.
2. **LinuxIO runs `swapon` itself** and no longer starts swap units. Recommended; it deletes the unit names and the waits.
3. **Resize changes the file in place** (`fallocate` or `ftruncate`, then `mkswap`) and does not delete and remake it. Recommended.
4. **The temporary file is made only when the file is the only swap or its data needs the room,** and is sized to the data parked, not to the file. Recommended.
5. **A switch to compressed memory removes every removable swap file, after a confirm that names them,** with no way to keep one. Recommended; keeping one needs the hold-off this plan deletes, or a mode the dialog does not have.
6. **A drain no longer covers a partition fstab names.** This is a loss against today, on Debian and RHEL defaults. Recommended, because it is what lets the tag matching go.
7. **A swap file that is in fstab but off is not turned on by LinuxIO;** it can only be removed. Recommended.
8. **The room check counts free space on other disk swap,** as `zramFits` does. Recommended.
9. **Removing the last swap is allowed, with a warning.** Recommended.
10. **The smallest size is 256 MiB; the reserve is 1 GiB or 5% of the filesystem.** Recommended.
11. **No migration code** for the old hold-off drop-ins and masks. Recommended.
12. **The earlier plan's "one bus connection per wait" item is left out.** Of the two waits it covered, `waitUnitSeenOff` is deleted here; it can be done for `waitStopped` on its own. Recommended.
13. **Recommendation on btrfs stays `zram`** for a swap file outside a subvolume of its own. To confirm, since the owner's own host runs zswap on btrfs.
