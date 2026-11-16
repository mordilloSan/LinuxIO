# Memory overflow

The **Memory overflow** dialog, opened from the Swap row of the dashboard's
Memory card, says how the host copes when RAM runs out and changes it: swap on
disk, zswap in front of a swap file, or zram with no swap file. It also sets
how eagerly the kernel moves data out of RAM (`vm.swappiness`) and brings data
parked on disk back into memory. The backend is
`backend/bridge/handlers/memory`, the contract is
`backend/bridge/apischema/memory.go`, and the dialog is
`MemoryOverflowDialog.tsx` with its view logic in `memoryStatusView.ts`, both
beside the card in `routes/_authenticated/-dashboard`.

It is a dialog on the dashboard and not a Settings tab because Settings holds
LinuxIO's own settings; a host setting is edited where the host shows it, as
the hostname and the clock are on the System Overview card.

Why it exists: a swap file on btrfs blocks snapshots of the subvolume that
holds it and cannot be balanced, and Ubuntu puts `/swap.img` in the root
subvolume, so on a btrfs root the system drive cannot be snapshotted. The
dashboard shows swap use and nothing more, and nothing else in LinuxIO writes
these settings.

## Who sees it

The dialog is **administrator-only**: every action in it is privileged, so
the Swap row of the Memory card is an edit button with a pencil for a session
with sudo and plain text for any other. The dialog is not gated on a
capability: the status works without a zram tool or TuneD, and the zram row
links to Capabilities when the package is missing.

The status read, `memory.get_swap_status`, is not privileged. It reads
world-readable files (`/proc`, `/sys`, `/etc/fstab`), asks TuneD over D-Bus
whether it runs, and returns no secret. The dialog is withheld because it has
nothing a session without sudo could do, not because the read is sensitive;
see [Privilege Pattern](./privilege_pattern.md#in-the-frontend).

The three changes are privileged and declare `Privileged()`:
`memory.set_swap_mode` and `memory.drain_swap` are mutations with progress,
`memory.set_swappiness` a plain mutation. All three return the status read
after the change.

## The three modes

| Name in the dialog | Mode | What the host does |
|---|---|---|
| Compressed memory with the disk as backup | `zswap` | The kernel compresses rarely used pages into a pool in RAM in front of the swap file; when the pool is full the oldest pages go to the file. The swap file stays active and zswap is enabled. |
| Compressed memory only | `zram` | A zram device (half of RAM, at most 8 GiB, compressed with zstd) is the only swap. The swap file's unit is stopped and masked, so nothing is written to disk and the file stays off after a restart. There is no backup: when zram fills up the kernel's OOM killer picks a victim. |
| Disk only | `disk` | The swap file (or an active swap partition) is the only swap, zswap is off and zram is stopped. |

Two more values describe a host and cannot be chosen. `none` is a host with
no active swap device. `mixed` is zram and a disk device active together,
which is what a switch leaves when it could not take the last device away (see
[Room](#room-for-what-comes-back)).

### How the current mode is derived

From the devices in `/proc/swaps` and zswap's `enabled` parameter (`deriveMode`):

1. a zram device and a disk device (swap file or partition) both active: `mixed`;
2. only zram: `zram`, whatever zswap says;
3. no device: `none`;
4. disk only with zswap enabled: `zswap`; with zswap off: `disk`.

### Recommendation and presets

The backend picks the recommended mode, first match wins (`recommendMode`):

| Host | Recommended |
|---|---|
| A swap partition is active | `zswap`, because this feature never stops a partition and zram cannot be chosen there |
| No swap file | `zram` |
| Swap file on an SD card or eMMC (`mmcblk*` under `/proc/self/mountinfo`) | `zram`, to spare the card |
| Swap file on a removable drive (`removable` is `1` for the disk under `/sys/class/block`) | `zram`, to spare the drive |
| Swap file on btrfs | `zram`, because the file blocks snapshots |
| Anything else | `zswap` |

A kernel without zswap never gets it recommended: `disk` where a swap
partition is active, `zram` otherwise.

`disk_hint` is `mmc` or `removable` for those two rows. The removable flag is
what a USB stick or a card in a USB reader reports; for a partition it is read
from its disk. A USB disk enclosure reports itself as fixed and is not flagged,
which is right for a USB SSD, and a swap file on a device-mapper volume (LVM,
LUKS) is not flagged whatever is under it.

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

`memory.get_swap_status` reads, with `os.ReadFile` and no subprocess:

| Source | Gives |
|---|---|
| `/proc/swaps` | active devices: path, kind (`file`, `partition` or `zram`), size, used, priority |
| `/proc/meminfo` | `MemTotal`, `MemAvailable`, `SwapTotal`, `SwapFree` |
| `/proc/sys/vm/{swappiness,page-cluster,watermark_boost_factor,watermark_scale_factor}` | the four sysctls |
| `/sys/module/zswap/parameters/{enabled,compressor,max_pool_percent}` | zswap; no directory means `zswap.available` is false |
| `/sys/block/zram<N>/{disksize,mm_stat}` | each zram device's size, stored data, compressed size and memory used |
| `/etc/fstab` | the swap entries, which decide what LinuxIO may stop, mask or start |
| `/proc/self/mountinfo` | the filesystem and disk of each swap file (`fs_type`, `disk_hint`) |
| `/sys/class/block/<device>` | whether that disk is removable (`disk_hint`) |
| TuneD, through `power.GetStatus` | `tuned_active` and `tuned_profile` |
| the `zram` capability | `zram_available` |

From these it derives the mode, the recommendation, `zram_planned_size`,
`zram_unavailable_reason` (`package_missing`, or `swap_partition_active`; a
missing package wins), `can_drain` with `drain_blocked_reason`, and
`swap_file`.

`swap_file` is the swap file the dialog shows: the first fstab swap entry whose
source is a path outside `/dev`, active or not, or else the first active swap
file. Keeping it while it is off lets the zswap and disk rows be offered while
the file is masked. `swap_file.managed` is true when fstab names the file:
only then does it have a unit that LinuxIO stops, masks and starts. The first
active file shown when fstab names none is not managed, and nothing here
stops or starts it.

**What fails the route.** A required file that cannot be read or parsed
(`/proc/swaps`, `/proc/meminfo`), an optional kernel file that exists and is
malformed (a non-numeric sysctl, an `mm_stat` with fewer than three fields),
and any read error other than "does not exist". A format change in the
kernel should surface rather than read as zero. **What degrades.** A missing
optional file reads as zero or absent (no zswap directory, no `mm_stat`, no
`/etc/fstab`, no mountinfo); a line of fstab or mountinfo that does not parse
is skipped; a failed TuneD read counts as TuneD not running (Debug log only);
a host with neither zram tool is `zram_available: false` with an empty
`zram_provider`.

## Changing the mode

`memory.set_swap_mode` takes `disk`, `zswap` or `zram`; any other value is
`ErrInvalidArgs`. It runs as a task and reports each step as it starts
(`SwapProgress`: step code, 1-based index of total, and the device for a
drain). The dialog shows the step's message while it runs.

It refuses before anything changes, with `ErrConflict`, a mode the host cannot
take: zram without a zram tool or with a swap partition active; zswap on a
kernel without it; zswap or disk with no swap file in fstab and no active
swap file or partition.

### Order of steps

The planner (`planSwapMode`) always **adds swap before it removes any**, and
checks each step before the next, so a failure leaves the host no less safe
than it was.

To `zram`:

1. `write_config`: write the zram provider's configuration and the sysctl
   drop-in.
2. `start_zram`: set zram to start at boot, start its unit and wait for a
   zram device in `/proc/swaps` (up to a minute). Already-running zram is not
   restarted, since a restart empties it, so the configuration just written
   applies at the next restart of the unit. What "at boot" and "its unit" mean
   depends on the [zram provider](#the-zram-provider).
3. `apply_sysctls`: write the preset live.
4. `disable_zswap`: turn zswap off and keep it off at boot (see below).
5. `stop_swap_file`: when the file is named in fstab, stop its unit, wait for
   the file to leave `/proc/swaps`, then mask the unit. Without a fstab swap
   file the step is not planned. A file that is already off is only masked. A
   unit systemd does not report running is not stopped, and the file stays on
   (see [Waiting on systemd](#waiting-on-systemd)).

To `zswap` or `disk`, the reverse:

1. `start_swap_file`: when the fstab file is not active, unmask its unit
   (which also brings back the unit fstab generates), start it and wait for the
   file in `/proc/swaps`.
2. `stop_zram`: when a zram provider is installed, stop zram's unit, wait for
   zram to leave `/proc/swaps` and turn zram off at boot. zram that is not
   running is only turned off at boot. zram the provider does not run, because
   another tool set it up or systemd reports the provider's unit `inactive` or
   `failed`, is left swapping and nothing is turned off. Without a provider
   the step is not planned and zram is left alone.
3. `apply_sysctls`: write the preset to the drop-in and live.
4. `enable_zswap` (zswap) or `disable_zswap` (disk).

Details that follow from the code:

- zswap's compressor is requested as `zstd`. The kernel refuses one it does
  not have with `ENOENT` or `EINVAL`; the mode then keeps the compressor the
  kernel has. After enabling, the compressor is read back and that value is
  persisted.
- zswap is turned off only when it reads enabled. On a kernel where zswap
  failed to start, a write of `N` fails for nothing and would take the whole
  switch down.
- `zram` and `disk` write the tmpfiles entry as `enabled` `N`, whatever zswap
  reads at the time. A kernel built with zswap on, or booted with
  `zswap.enabled=1`, would otherwise return to zswap at the next boot, in
  front of zram too, and zswap reading off now says nothing about that. A file
  that already says `N` is not written again. On a kernel without zswap there
  is nothing to turn off and the file is removed.
- Leaving zram, the three sysctls only zram sets go back to the kernel's
  defaults (3, 15000, 10) when their live value is still zram's, and their
  keys leave the drop-in. This overrides a value an admin set by hand to the
  same number as the preset, until a reboot.
- Every kernel parameter written is read back and must equal what was written.
- A vm sysctl the kernel does not have (`watermark_boost_factor` came in
  Linux 5.0) is not written live, as a preset or as a kernel default; the
  drop-in keeps its key.

### Room for what comes back

Taking swap away pushes its pages into RAM. Each removal asks `fitsInMemory`
on a **freshly read status** taken just before the step, not the status from
the start of the request: the data to bring back plus a 512 MiB margin must be
below `MemAvailable`.

- Stopping the swap file: the file's used bytes.
- Stopping zram: the data in the zram devices (the larger of the device's
  swap use and its stored original size, which also counts pages filled with
  one value). While zram empties, the kernel may also push pages to disk swap,
  so the free space on disk swap devices counts as room.
- Draining: the used bytes of the device being drained.

Stopping the swap file also needs a zram device swapping in that same fresh
read. Zram was seen at the start of the run, but if it has gone since, stopping
the file would leave no swap at all, so the file stays on and the run
still succeeds, with the host in disk mode.

When a removal is refused for lack of room the step ends without an error and
the swap device stays active: the result of the request is then `mixed`, the
success log line says which one stayed (`swap_file_left_active`,
`zram_left_active`), and the dialog shows a warning and keeps the chosen row so
Apply can be pressed again later. `mixed` carries no reason beyond the
devices; the dialog's warning says the swap file is not in `/etc/fstab` when the
result has an active swap file that is not managed, and that memory was short
otherwise. It is also what a switch leaves when the device's unit is not one
systemd runs: a switch to zram when the active swap file is not named in fstab,
or when systemd does not report the file's unit running, and a switch away from
zram when zram is not run by the provider's unit, or, with `zram-generator`,
when a zram device other than `zram0` swaps. Applying again changes nothing
in those cases.

### Rollback

When a step fails the run undoes what it changed, last change first, on a
context that outlives a cancelled request, within 30 seconds, and joins every
failed undo into the error it returns:

- if the run set zram to start at boot, that is undone, so the next boot does
  not start zram with the configuration the rollback put back: `zramswap.service`
  is disabled again, and with `zram-generator` the restored configuration is
  followed by a systemd reload, so no generated unit outlives its file. zram's
  unit is also stopped when no zram device swaps, and zram that swaps stays;
- each kernel parameter the run wrote goes back to the value it had before;
- each file the run wrote or removed goes back to its previous content and
  mode, or is removed if it did not exist.

The previous content of a file, and the previous value of a parameter, is
recorded **before** it is written: a write can land and still report an error
(the directory sync after a rename, the close of a kernel file), and putting
the old content back is harmless when the write did not land.

Rollback **never undoes added swap**. Zram that is swapping and a swap file that
was started stay, and swap that a passed room check removed is not started
again. A failed run therefore ends with at least the swap it started with.
One consequence: when a switch away from zram fails after zram was stopped,
the live sysctls go back to zram's values, so the host runs disk swap with
swappiness 180 until a mode is applied again.

### One change at a time

The three routes share a mutex (`mutationMu` in `executor.go`); a second
request while one runs fails with `ErrConflict` ("another memory change is
running"), and the dialog disables Apply, the drain button and the slider while
any change is pending. The lock covers the whole host: the three routes are
privileged mutations, a mutation runs only in a job host, and every admin
login shares the root job host ([Jobs](./jobs.md#terms)), so every memory
change, from any session, runs in that one process.

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

## Draining

"Bring back to memory" (`memory.drain_swap`) empties data parked on disk swap
by stopping a swap device, which makes the kernel read its pages into RAM
(`swapoff`), and starting it again. zram is never drained: its data is already
in memory.

Only devices that fstab names are drained, and only those holding data, one at
a time in fstab order, for each: a fresh status and the room check (the
device's used bytes with the margin), stop the unit, wait for the device to
leave `/proc/swaps`, start the unit and wait for the device to return. A
failure at a later device says that the ones before it stay drained. The dialog
reports the device in progress.

Before it stops a swap file, the drain reads the state of the file's unit. That
unit has the file's own name, so systemd reports it running while the file
swaps; one that reads `inactive` or `failed`, as for a file fstab names through
a symlink, could not stop the file and start it again, so the drain refuses at
that device with `ErrConflict`. A partition's unit is named after its
`/dev/disk/by-*` link, an alias of the device's own unit, and its state is not
read before the stop: the wait that follows decides.

The route refuses a drain with `ErrConflict`, and the status predicts it with
`can_drain` and `drain_blocked_reason`, one of these, checked in this order:

| Code | Meaning |
|---|---|
| `nothing_parked` | no disk swap device has data on it |
| `unmanaged_device` | the data is only on devices fstab does not name, whose units could not be started again |
| `unit_masked` | a device holding data has a masked unit, so it could not be started again |
| `not_enough_memory` | the data on the fstab-named devices does not fit in memory with the margin |

`unit_masked` is read from disk: a symlink to `/dev/null` at
`/etc/systemd/system/<unit>` or `/run/systemd/system/<unit>`. The route asks
systemd about each unit as well before it stops anything, as a second check.

**A stop that fails still queues a start.** A D-Bus call cancelled after
systemd took the job still stops the unit, and a `swapoff` goes on after the
route gives up waiting. So when the stop call fails or is cancelled, or the
wait for the device to leave fails, a start of the unit is queued anyway, on a
context that survives the cancel and with a bound of its own. A start on a unit
still active does nothing. The device's return after a successful stop is also
not cancelled with the request. The error names the unit and says that a
start was queued, or, when queueing it failed too, that the device may be off
and why the start failed.

## Swap units

systemd turns each fstab swap entry into a unit named after the path it swaps
on, and that unit is what LinuxIO stops, masks, unmasks and starts, through
`common/systemd`. `fstab.go` names it the way `systemd-fstab-generator` and
`systemd-escape --path` do (`systemd.EscapePath` in
`backend/common/systemd/escape.go`): `/swap.img` is `swap.img.swap`. A source
that is a path is used as written; `UUID=`, `LABEL=`, `PARTUUID=` and
`PARTLABEL=` go through their `/dev/disk/by-*` link, with `/`, blanks and
control characters escaped as udev does. Any other source, and a path with a
`..` component, names no unit and is skipped.

**Matching is positive.** A `/proc/swaps` device is tied to a fstab entry only
when its path equals the entry's path, or both resolve to the same path after
following symlinks the way the kernel would with `/` as root (a symlinked swap
file included), and each device is tied to at most one entry. Only a device
tied to an entry is ever stopped, masked or drained; any other is unmanaged.
`stop_swap_file` also does not mask the unit of a file that is not active when
some active swap file cannot be resolved, because that file might be it under
another name.

Masking makes systemd ignore the fstab generator's unit, so the file does not
come back at boot until `start_swap_file` unmasks it. The file itself is never
created, grown or deleted.

## Waiting on systemd

Before a mode change stops the swap file or zram, it reads the unit's state.
A unit that reads `inactive` or `failed` would not take the device away, so
no stop is issued: the device stays and the result is `mixed`. This covers
zram set up by another tool and a swap file systemd tracks under another unit
name. A drain does the same for a swap file and refuses instead (see
[Draining](#draining)). A state that cannot be read does not hold the stop
back.

A swap unit's stop runs `swapoff`, which reads every swapped page back and can
take minutes. Waits are bounded: a minute for a device to appear in
`/proc/swaps`, ten minutes for one to leave, polled every 250 ms. LinuxIO does
not raise systemd's own stop timeout for the unit (90 seconds by default),
which kills a `swapoff` or a `zramswap stop` that outlives it.

While waiting for a device to leave, the unit's state is watched too, and the
wait ends early with an error, the device still in `/proc/swaps`, when:

- the unit is `failed`, or `active` again after having been seen
  `deactivating`: systemd stopped waiting, as it does when it kills a
  `swapoff` or a stop command that outlived the timeout;
- the unit is `inactive`: the stop finished without taking the device away,
  as when `zramswap.service` stops and a zram device another tool set up still
  swaps;
- the unit stays `active` for 15 seconds without ever being seen
  `deactivating`: systemd did not carry out the stop, as when it ends at once
  because the kernel refused the `swapoff`.

A device that has left always counts as stopped, so the device is read once
more before giving up, and a state that cannot be read is waited for. The
error for a swap file or zram stop in a mode change goes through rollback; for
a drain it queues the start described above.
A swap file whose `swapoff` outlasts the timeout cannot be stopped from the UI
([backlog](./TODO/backlog.md)).

## Files written on the host

Each file is LinuxIO's alone and starts with
`# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.`.
Writes are atomic (`atomicfile.Write`, which refuses to write through a
symlink) and mode `0644`.

| Path | Written by | Content |
|---|---|---|
| `/etc/sysctl.d/90-linuxio-memory.conf` | every mode, and `set_swappiness` | `vm.<name> = <value>` lines. Keys the admin added stay. Leaving zram drops `vm.page-cluster`, `vm.watermark_boost_factor` and `vm.watermark_scale_factor`. Under TuneD `vm.swappiness` is neither written nor dropped. |
| `/etc/tmpfiles.d/linuxio-zswap.conf` | every mode, on a kernel with zswap | For `zswap`, `w` lines setting zswap's `compressor` and `enabled` (`Y`) at boot. For `zram` and `disk`, one `w` line setting `enabled` to `N`, so a kernel that starts with zswap on does not return to it. |
| `/etc/default/zramswap` | `zram`, with zram-tools | zram-tools' configuration: `ALGO=zstd`, `SIZE=<MiB>`, `PRIORITY=100`. It is a package conffile, so the next package upgrade asks about the change. |
| `/etc/systemd/zram-generator.conf` | `zram`, and leaving zram, with zram-generator | For `zram`: one `[zram0]` section with `zram-size = <MiB>`, `compression-algorithm = zstd` and `swap-priority = 100`. Leaving zram: comments only, so the generator makes no device. Either way it replaces what was there, and takes the place of the file a package puts in `/usr/lib/systemd`. |

Besides these, systemd keeps the mask of the swap unit as a symlink in
`/etc/systemd/system`, and with zram-tools `zramswap.service` is enabled or
disabled.

Never touched: the kernel command line (zswap persists through tmpfiles, not
`zswap.enabled=`), the swap file's own content, creation or size, and swap
partitions. zram writeback is not exposed.

## The zram provider

Two tools set zram up, and the status names the one installed in
`zram_provider`. The mode change does the same steps with either; what differs
is the file and the unit.

| | `zram-tools` | `zram-generator` |
|---|---|---|
| Where | Debian and Ubuntu | Fedora, RHEL 9 and later, Arch; `systemd-zram-generator` on Debian and Ubuntu |
| Found by | the `zramswap` command (`hostcmd.LookPath`, which also looks in `/usr/sbin`) | the file `/usr/lib/systemd/system-generators/zram-generator` |
| Configuration | `/etc/default/zramswap` | `/etc/systemd/zram-generator.conf` |
| Start at boot | enable `zramswap.service` | the configuration holds a device; a systemd reload makes `dev-zram0.swap`, wanted by `swap.target` |
| Start now | start `zramswap.service` | start `dev-zram0.swap`, which requires `systemd-zram-setup@zram0.service` |
| Stop now | stop `zramswap.service` | stop `dev-zram0.swap`; the setup service is bound to it and resets the device |
| Off at boot | disable `zramswap.service` | write a configuration with no device, then reload |
| "The provider runs it" | `zramswap.service` is not `inactive` or `failed` | `systemd-zram-setup@zram0.service` is `active`, and no zram device but `zram0` swaps |

zram-tools is used when both are installed, so a host that had it keeps
working as before. This follows upstream: the generator's README applies a new
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
- Drop-ins in `zram-generator.conf.d` and a file in `/run/systemd` are neither
  read nor changed. They override LinuxIO's file, so a device defined there
  comes back at boot, and one that swaps beside `zram0` keeps `stop_zram` from
  stopping anything.

## The zram capability

`zram` is available when either provider is installed
(`CheckZramAvailability`). Install goes through PackageKit with the Debian
package `zram-tools` or the RHEL package `zram-generator`; neither has a
service step, since `start_zram` does that. See
[Capabilities](./capabilities.md#zram).

## The dialog

- A banner, worst condition first, with at most one action: no swap and no zram
  ("If memory runs out, Linux will close programs"); mixed; data parked on
  disk, with Bring back to memory, disabled with the reason when `can_drain` is
  false; an active swap file on btrfs, with Switch to compressed memory when
  zram can be chosen and the file is managed (a switch could not take any
  other file away); or "Memory is fine".
- A single-choice list of three rows, in the order zswap, zram, disk, each with
  a plain name, its technical name once, a description, a caution where one
  applies, and chips for the recommended and the current row. A row that cannot
  be chosen shows why; the zram row shows a link to Capabilities instead of a
  choice while no zram tool is installed. Apply is enabled for a different row, or
  for the current one while the host is `mixed`.
- Choosing zram while a managed swap file (`swap_file.managed`) is active asks
  first, as a second step of the same dialog and not a dialog on top of it,
  and lists what will happen: data parked in the file comes back, the file is
  turned off and stays off after a restart, and the file is not deleted. The
  step opens with focus on Back, and Back returns focus to Apply.
- Advanced, collapsed: a slider for swappiness from 0 to 200, with the caption
  "Recommended for <mode>: N". A drag is saved when the pointer is released
  or focus leaves the slider. Arrow keys are saved half a second after the
  last key, so a run of them is one write and one toast; leaving the slider or
  closing the dialog with Escape saves a waiting key press at once. A step
  made while a save runs is sent when that save succeeds, a step waiting when
  a mode change or a drain starts is dropped, and after a failed save the
  slider goes back to the host's value.
- The status is read only while the dialog is open, refreshes every 15
  seconds, and each of the three mutations invalidates it.
- Close and Escape are held while a change runs, since the running step and
  the result are shown in the dialog. Closing drops an unapplied choice.
- The dialog's code loads the first time it is opened. It has no URL: it
  cannot be linked to or bookmarked, like the hostname and clock dialogs.

## Tests

- Go unit tests in `backend/bridge/handlers/memory` run against fixture roots
  (a temporary tree standing for `/proc`, `/sys` and `/etc`) and a fake
  `hostSystem` that stands for systemd, files and kernel parameters. They cover
  the parsers, mode derivation and recommendation, the planner for every
  transition and refusal, each step, rollback (including a cancelled request, a
  write that lands and then errors, and a stop that never completes), the room
  checks, drain, the fail-fast and slow-success waits, a stop left out for a
  unit systemd does not report running, and the status and route agreement for
  masked units. `escape_test.go` pins the unit-name
  escaping. Run with `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/memory`.
- Frontend: `memoryStatusView.test.ts` and `MemoryOverflowDialog.test.tsx`
  (the real generated client with the calls mocked), and `Memory.test.tsx` for
  the pencil on the dashboard card, all under jsdom. There is no browser spec
  for the dialog.
- `make vm-test-memory-integration` runs `TestMemoryIntegration` as root in the
  [test VM](./development.md#test-vm). It switches the real host to zram and
  back through systemd, the kernel and `/etc`, and checks: after zram, mode
  `zram` with zswap off, swappiness 180 (unless TuneD runs), the three zram
  sysctls, the swap file's unit masked, `zramswap.service` enabled and the
  configuration holding `ALGO=zstd`; after the return, the swap file active, no
  zram swapping, its unit unmasked and `zramswap.service` disabled. Cleanup
  switches back to the starting mode. It does not cover zswap's persistence,
  swappiness, drain, rollback, systemd's stop timeout, TuneD or a reboot.
  `make vm-test-memory-integration ZRAM_PROVIDER=zram-generator`, after
  `make vm-reset`, runs the same test with Ubuntu's `systemd-zram-generator`
  in place of zram-tools: after zram it checks the `[zram0]` configuration
  with `zstd` and that `systemd-zram-setup@zram0.service` is active, and after
  the return that the configuration holds no device.

## Known limits

Each is by design or unlikely to be met, or is an open item in
[the backlog](./TODO/backlog.md).

- A swap file whose `swapoff` outlasts systemd's stop timeout cannot be
  stopped or drained from the UI.
- A `mixed` result gives no reason.
- A swap partition or a swap device fstab does not name is never stopped, so
  such a host cannot choose compressed memory only (partition) or have the
  device drained (unnamed).
- A host with zram already running keeps its old zram size until the service
  restarts.
- The `zram-generator` path was built from upstream's documentation and unit
  files and has not run on Fedora or RHEL; drop-ins in
  `zram-generator.conf.d` are not read.
- A click on Close or Apply within half a second of an arrow key on the
  eagerness slider is lost: the click moves focus off the slider, which starts
  the save, and the buttons are held while it runs.
- On a kernel where zswap failed to start, the tmpfiles line that turns zswap
  off may fail at each boot and leave a line in the journal. Not checked on a
  host.
- The overflow alert is not built
  ([Memory overflow](./TODO/memory-overflow.md)).
