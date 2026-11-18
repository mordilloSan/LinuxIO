# Memory overflow

The **Memory overflow** dialog, opened from the Swap row of the dashboard's
Memory card or by its URL, `/?dialog=memory-overflow`, says how the host
copes when RAM runs out and changes it: swap on
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
with sudo and plain text for any other, and the URL opens the dialog only for
a session with sudo. The dialog is not gated on a
capability: the status works without a zram tool or TuneD, and the zram row
links to Capabilities when the package is missing.

The status read, `memory.get_swap_status`, is not privileged. It reads
world-readable files (`/proc`, `/sys`, `/etc/fstab`, the zram tools'
configuration files, `/var/lib/dpkg/status`), asks TuneD over D-Bus whether
it runs, and returns no secret: of the zram configuration it returns paths,
device names and the size settings of zram-generator's other files, nothing
else. The dialog is withheld because it has
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
| Compressed memory only | `zram` | A zram device (half of RAM, at most 8 GiB, compressed with zstd) is the only swap. The swap file is stopped and its unit held off at boot, so nothing is written to disk and the file stays off after a restart. There is no backup: when zram fills up the kernel's OOM killer picks a victim. |
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
| A swap file that is not managed is active | `zswap`, because the file stays on whatever is chosen, so zram would end `mixed` |
| No swap file | `zram` |
| Swap file on an SD card or eMMC (`mmcblk*` under `/proc/self/mountinfo`) | `zram`, to spare the card |
| Swap file on a removable drive (`removable` is `1` for the disk under `/sys/class/block`) | `zram`, to spare the drive |
| Swap file on btrfs | `zram`, because the file blocks snapshots |
| Anything else | `zswap` |

A kernel without zswap never gets it recommended: `disk` for the first two
rows, `zram` otherwise.

`disk_hint` is `mmc` or `removable` for those two rows. The removable flag is
what a USB stick or a card in a USB reader reports; for a partition it is read
from its disk. A USB disk enclosure reports itself as fixed and is not flagged,
which is right for a USB SSD, and a swap file on a device-mapper volume (LVM,
LUKS) is not flagged whatever is under it. A swap file under a FUSE mount
(type `fuse` or `fuse.*`) is not flagged either: any user can make one
through the setuid `fusermount` and name any `/dev/...` as its source. A
`fuseblk` mount is, since the kernel opens the block device its source names.

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
| `/etc/fstab` | the swap entries, which decide what LinuxIO may stop, hold off or start |
| `/proc/self/mountinfo` | the filesystem and disk of each swap file (`fs_type`, `disk_hint`) |
| `/sys/class/block/<device>` | whether that disk is removable (`disk_hint`) |
| TuneD, through `power.GetStatus` | `tuned_active` and `tuned_profile` |
| the `zram` capability | `zram_available` |
| systemd over D-Bus, only with both zram tools installed | `zram_provider` (see [The zram provider](#the-zram-provider)) |
| the provider's configuration file, `/etc/default/zramswap` or `/etc/systemd/zram-generator.conf`, and its package's checksum of it in `/var/lib/dpkg/status` | `foreign_zram_config`, when it is not LinuxIO's (see [A zram configuration file that is not LinuxIO's](#a-zram-configuration-file-that-is-not-linuxios)) |
| zram-generator's other configuration files, only with zram-generator installed | `zram_generator_devices` (see [Other zram-generator configuration](#other-zram-generator-configuration)) |

From these it derives the mode, the recommendation, `zram_planned_size`,
`zram_unavailable_reason` (`package_missing`, or `swap_partition_active`; a
missing package wins), `can_drain` with `drain_blocked_reason`, and
`swap_file`.

LinuxIO manages one swap file. Of the fstab swap entries whose source is a
path outside `/dev`, it is the first that is active or whose unit is held
off at boot, or when there is none, the first (`fstabSwapFile`); any other
is left as it is. A unit is held off when LinuxIO's drop-in for it is there
(see [Swap units](#swap-units)) or it is masked, a link to `/dev/null` in
`/etc/systemd/system` or `/run/systemd/system`, as an admin or an older
LinuxIO leaves it (`unitHeldOff`). Holding off keeps the choice in both
directions: the file a switch to zram stopped and held off is the one the
way back starts, and a later switch does not move on to another file that
swaps. `swap_file` is the swap
file the dialog shows: that one, active or not, or else the first active swap
file. Keeping it while it is off lets the zswap and disk rows be offered while
the file is held off. `swap_file.managed` is true when LinuxIO stops, holds
off, starts and drains the file; otherwise nothing here stops, starts or drains
it, and `swap_file.unmanaged_reason` says why:

| Code | Meaning |
|---|---|
| `not_in_fstab` | fstab names no swap file and this is the first active one: it has no unit to stop, hold off or start |
| `others_can_write` | fstab names the file, but someone other than root can change a folder on its path (see "A swap file others can redirect is not managed") |

A file that is off and not managed is not counted as disk swap: the
recommendation goes by the first active swap file instead, or by none, and
a mode that needs disk swap is refused when no swap file or partition is
active.

**What fails the route.** A required file that cannot be read or parsed
(`/proc/swaps`, `/proc/meminfo`), an optional kernel file that exists and is
malformed (a non-numeric sysctl, an `mm_stat` with fewer than three fields),
and any read error other than "does not exist". A format change in the
kernel should surface rather than read as zero. **What degrades.** A missing
optional file reads as zero or absent (no zswap directory, no `mm_stat`, no
`/etc/fstab`, no mountinfo); a line of fstab or mountinfo that does not parse
is skipped; a zram device that leaves while it is read (its directory gone,
or a read that fails with `ENODEV`), as when zram's unit resets it, is
skipped; a failed TuneD read counts as TuneD not running (Debug log only);
with both zram tools installed, a unit state that cannot be read makes
zram-tools the provider (Debug log only); a host with neither zram tool is
`zram_available: false` with an empty `zram_provider`; the provider's
configuration file that cannot be read counts as not LinuxIO's, and a
zram-generator file or folder that cannot be read is left out (Debug log
only).

## Changing the mode

`memory.set_swap_mode` takes `mode`, `disk`, `zswap` or `zram`; any other
value is `ErrInvalidArgs`. `confirm_replace` names the zram configuration
file that is not LinuxIO's the user agreed the change may replace (see
[A zram configuration file that is not LinuxIO's](#a-zram-configuration-file-that-is-not-linuxios)).
It runs as a task and reports each step as it starts
(`SwapProgress`: step code, 1-based index of total, and the device for a
drain). The dialog shows the step's message while it runs. Its result
(`SwapModeResult`) is the status after the change plus `left`: what the
change left as it was and why (see
[What a change leaves as it was](#what-a-change-leaves-as-it-was)).

The status after a change, here and for `set_swappiness` and `drain_swap`, is
read up to five times, 250 ms apart (`statusAfterChange`). The route read it
fine before the change, so a read that fails now most likely met files under
`/proc` and `/sys` while the change still moved them. A read that still fails
fails the request with an error that says the change was made; nothing is
undone, and the Info line for the change is still logged.

It refuses before anything changes, with `ErrConflict`, a mode the host cannot
take: zram without a zram tool or with a swap partition active; zswap on a
kernel without it; zswap or disk with no swap file in fstab and no active
swap file or partition; with zram-generator, zswap or disk while another of
its files defines a device (see
[Other zram-generator configuration](#other-zram-generator-configuration)).
It also refuses a run that would replace the provider's configuration file
while that file is not LinuxIO's and the request does not name it in
`confirm_replace`. The plan makes these checks under the mutation lock, on
a status read for the request.

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
   - zram swaps at another size, as the device zram-tools starts when the
     package is installed: the unit is stopped, waited for and started again,
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
   swap unit starts it again. After the start the size is read again, and a
   device that came up at another size than planned is reported
   (`zram_size`, `other_size`).
3. `apply_sysctls`: write the preset live.
4. `disable_zswap`: turn zswap off and keep it off at boot (see below).
5. `stop_swap_file`: when the file is named in fstab, take it away with
   `swapoff` (see
   [Taking a disk swap device away](#taking-a-disk-swap-device-away)), then
   hold its unit off so it stays off at boot: LinuxIO's drop-in for the unit,
   a reload, and an unmask when the unit is masked (see
   [Holding a swap file off at boot](#holding-a-swap-file-off-at-boot)).
   Without a fstab swap file the step is not planned. A file that is already
   off is only held off.

To `zswap` or `disk`, the reverse:

1. `start_swap_file`: when the fstab file is not active, or its unit is held
   off, let its unit go: remove LinuxIO's drop-in, reload systemd in every
   case, since a swap entry fstab just gained has no unit until a reload and
   systemd keeps a condition whose file is gone until one, and unmask the
   unit when it is masked (which reloads again); then, unless the file
   already swaps, as a run that was cut short can leave it, start it and wait
   for the file in `/proc/swaps`.
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

Taking swap away pushes its pages into RAM. Each removal asks `fitsInMemory`
on a **freshly read status** taken just before the step, not the status from
the start of the request: the data to bring back plus a 512 MiB margin must be
below `MemAvailable`.

- Stopping the swap file: the file's used bytes.
- Stopping zram, or restarting it at the planned size: the data in the zram
  devices (the larger of the device's swap use and its stored original size,
  which also counts pages filled with one value). While zram empties, the
  kernel may also push pages to disk swap, so the free space on disk swap
  devices counts as room.
- Draining: the used bytes of the device being drained.

Stopping the swap file also needs a zram device swapping in that same fresh
read. Zram was seen at the start of the run, but if it has gone since, stopping
the file would leave no swap at all, so the file stays on and the run
still succeeds, with the host in disk mode.

### What a change leaves as it was

A step that cannot safely take a device away ends without an error and leaves
it. The request then succeeds, and its result lists each thing left in `left`,
as `what` and `reason`. The dialog shows the reasons in a warning in place of
the success message, and keeps the chosen row so Apply can be pressed again.

| `what` | `reason` | When |
|---|---|---|
| `swap_file` | `not_enough_memory` | The file's data does not fit in memory. Applying again later can finish. |
| `swap_file` | `zram_not_swapping` | No zram device swaps when the file would stop. |
| `swap_file` | `not_identified` | The fstab file is off, but an active swap file cannot be resolved and may be it, so the unit is not held off. |
| `swap_file` | `others_can_write` | The file is not managed, because a folder on its path can be changed by users other than root: no step stops it (see "A swap file others can redirect is not managed"). |
| `swap_file` | `not_managed` | A swap file other than the one LinuxIO manages still swaps: no step stops it. |
| `zram` | `not_enough_memory` | zram's data does not fit in memory and free disk swap. |
| `zram` | `not_managed` | The provider's unit does not run that zram (another tool set it up, the unit is `inactive` or `failed`, with `zram-generator` a device other than `zram0` swaps), or no provider is installed. |
| `zram_size` | `not_enough_memory`, `not_managed` | zram runs at another size than planned and cannot be restarted for that reason. |
| `zram_size` | `no_other_swap` | The same, and no swap file or partition is active: a restart would leave the host without swap meanwhile, and with none at all if the start failed. |
| `zram_size` | `other_size` | zram came up at another size than planned: a drop-in overrides the size, or the device could not be reset. Every later change would restart it again. |

A device left active beside the other kind makes the mode `mixed`, and a
`mixed` result always has a reason: `not_managed`, for the swap file or for
zram, is added after the run when no step left the device that stayed, or
`others_can_write` for a swap file the status reported active with that
reason. `zram_size`
does not make the mode `mixed`. The success log line says what stayed
(`swap_file_left_active`, `zram_left_active`, `zram_size_kept`). Applying
again helps only for `not_enough_memory`.

### Sysctls follow the mode the host ends in

The run writes the target mode's sysctls, and a rollback puts back the ones
from before. Neither is right when the host ends in another mode, which
happens because swap is never undone: zram that went away before the swap
file would stop leaves disk swap only, and a switch away from zram that fails
after zram stopped does too. So after the last step, and after a rollback, the
status is read again, and when the host is in `disk`, `zswap` or `zram` and
the live sysctls are another mode's, that mode's preset is written live
(`settleSysctls`). This keeps swappiness 180 and zram's readahead off a host
that swaps to disk only, and disk's values off a host that ended on zram
only. A host left `mixed` keeps the sysctls it has.

Only the live values are settled. The drop-in stays as the run or the
rollback left it, because it belongs with what the next boot starts: the
zram unit's enablement, its configuration and the swap unit's hold-off
drop-in, as the run set them for the target or the rollback left them. After a successful run
a failure to write the live values is logged and does not fail the request;
after a rollback it is joined into the error.

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
  mode, or is removed if it did not exist. The copy kept of a zram
  configuration file that was not LinuxIO's stays: it holds what the file
  held, which the rollback put back. A hold-off drop-in `stop_swap_file`
  wrote is removed, with a reload, so the next boot starts the file as it did
  before the run. One `start_swap_file` removed is not put back, nor a mask:
  letting the file go is adding swap, which a rollback never undoes, and the
  run may already have turned zram off at boot, so holding the file off
  again would leave the next boot with no swap.

The previous content of a file, and the previous value of a parameter, is
recorded **before** it is written: a write can land and still report an error
(the directory sync after a rename, the close of a kernel file), and putting
the old content back is harmless when the write did not land.

Rollback **never undoes added swap**. Zram that is swapping and a swap file that
was started stay, and swap that a passed room check removed is not started
again; only a zram stop the step gave up on before systemd reported it
over has a start queued behind it, by the step and not the rollback (see
`stop_zram`). A failed run therefore
ends with swap: what it started with, less
what a room check let it take away while other swap was active. The sysctls
then follow the mode the host ended in (see above).

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

TuneD tells any user whether it runs (`is_running`) and its profile
(`active_profile`); only a profile switch asks for authentication. So the
bridge of a session without sudo, which reads the status as its user, sees
TuneD as root does (TuneD 2.28 on Fedora 45 and 2.25 on Ubuntu 26.04, in the
test VM). The changes do
not rest on that read: all three are privileged and run as root, which reads
TuneD again.

## Draining

"Bring back to memory" (`memory.drain_swap`) empties data parked on disk swap
by taking a swap device away, which makes the kernel read its pages into RAM
(`swapoff`), and starting it again. zram is never drained: its data is already
in memory.

Only devices that fstab names are drained, and only those holding data, one at
a time in fstab order, for each: a fresh status and the room check (the
device's used bytes with the margin), `swapoff` (see
[Taking a disk swap device away](#taking-a-disk-swap-device-away)), start
the unit and wait for the device to return. A failure at a later device says that the ones before it
stay drained. The dialog reports the device in progress.

The device's unit is used only to start it again, and its state before the
`swapoff` is not read: a swap file systemd tracks under another unit name,
and a partition whose unit is an alias named after its `/dev/disk/by-*` link,
are drained like any other. The test VM shows both (systemd 259 on Ubuntu
26.04, 262 on Fedora 45). The alias of a disk fstab names by `UUID=` reads
inactive within milliseconds of the `swapoff`, and its start brings the disk
back. A swap file fstab names through a link comes back with a start of the
link's unit, and stays off at boot while that unit is held off. systemd 259
reads the link's unit inactive as soon as another swap device comes or goes,
while the file still swaps, and a start of it then fails; LinuxIO starts it
only once the file has left, so that never reaches it.

The route refuses a drain with `ErrConflict`, and the status predicts it with
`can_drain` and `drain_blocked_reason`, one of these, checked in this order:

| Code | Meaning |
|---|---|
| `nothing_parked` | no disk swap device has data on it |
| `others_can_write` | no managed device holds data, and an fstab swap entry that is not managed because others can change its folder does |
| `unmanaged_device` | the data is only on devices fstab does not name, whose units could not be started again |
| `unit_held_off` | a device holding data has a unit held off at boot, so a start of it would not bring it back |
| `not_enough_memory` | the data on the fstab-named devices does not fit in memory with the margin |

`unit_held_off` is read from disk: LinuxIO's drop-in for the unit, or a
symlink to `/dev/null` at `/etc/systemd/system/<unit>` or
`/run/systemd/system/<unit>`. The route asks systemd about each unit's mask
as well before it stops anything, as a second check.

**A `swapoff` that fails or is cancelled leaves the device swapping**, so
nothing is started and the error names the device. Once a device is off,
bringing it back is not cancelled with the request, and the drain reports the
device off when its unit does not bring it back within a minute.

## Swap units

systemd turns each fstab swap entry into a unit named after the path it swaps
on, and that unit is what LinuxIO holds off at boot, lets go and starts, through
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
tied to an entry is ever stopped, held off or drained; any other is unmanaged.

**A swap file others can redirect is not managed.** A start has systemd swap
on the path fstab names, and a stop or a drain gives `swapoff` the
`/proc/swaps` path; both look the path up again. Where someone other than
root can change a folder on that path, they can put another entry at it, so
root would take another swap device away without the room check, or start
swapping on a file of theirs. `matchFstabSwaps` therefore marks an fstab
swap entry (`othersCanWrite`) when a folder is not one root owns that no
group or everyone may write, the test `rootOnlyPath` applies, on any of:

- the path fstab names;
- the path of every link followed from it, and of every folder a link
  target leaves by `..`, since either could be pointed elsewhere after the
  check, whatever it names now;
- the path the links lead to. Links that lead nowhere cannot be judged and
  count;
- the `/proc/swaps` path of the active device it is.

Such an entry gets no `stop_swap_file` or `start_swap_file` step, is left out
of a drain and of the data a drain counts, and the status reports the swap
file with `managed` false and `unmanaged_reason` `others_can_write`. Only
root can change a folder that passed, so what the status found still holds
when a step starts the file; `swapoff` repeats the test all the same. A
missing folder passes, since nothing can be redirected there. A folder the
reader may not look into is not judged: a session without sudo reads the
status too, and root, which makes the changes, can look into every folder.
LinuxIO does not move the file: it creates no swap file, and moving one
starts with stopping it.

`stop_swap_file` also does not hold off the unit of a file that is not
active when some active swap file cannot be resolved, because that file
might be it under another name.

### Holding a swap file off at boot

`stop_swap_file` keeps the file off at boot with a drop-in for its unit,
`/etc/systemd/system/<unit>.d/90-linuxio-memory.conf` (`holdOffDropInPath`),
followed by a reload, and an unmask when systemd reads the unit masked:

```ini
# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.
# Keeps this swap off at boot. LinuxIO removes this file to turn it on again.
[Unit]
ConditionPathExists=!/
```

`/` always exists, so the condition never holds, and systemd skips the unit
at boot and for any start, which still succeeds and starts nothing
(`systemd.unit(5)`: a failed condition does not put the unit in the
`failed` state). The unit is not masked, because the fstab generator makes
`swap.target` require the unit of a swap entry without `nofail`, and
systemd starts no target that requires a masked unit (`Failed to start
swap.target: Unit swap.img.swap is masked.`): the boot then leaves
`swap.target` out, and zram-generator's `dev-zram0.swap`, which only
`swap.target` wants, with it, so a zram-generator host in zram mode would
boot with no swap at all. A skipped unit does not hold the target back. The
test VM showed both, on Fedora 45 (systemd 262) and Ubuntu 26.04 (systemd
259): with the mask, no zram after the reboot; with the drop-in,
`swap.target` active, zram swapping and the file off, with
`swap.img.swap - /swap.img skipped, unmet condition check
ConditionPathExists=!/` in the journal. The drop-in applies to the unit the
generator makes in `/run/systemd/generator`, a file path's as well as an
escaped `dev-disk-by\x2duuid-….swap`.

The unit name comes from the fstab entry through `systemd.EscapePath`, never
from the request, and `/etc/systemd/system` is root's alone. The drop-in is
written atomically and never through a symlink, as the other managed files
are. Its folder, `<unit>.d`, is made mode 0755 whatever the umask: the job
host runs with umask 077, and the bridge of a session without sudo, whose
status reads whether the drop-in is there, must be able to look into it. A
folder an earlier run left another mode with nothing in it but LinuxIO's
drop-in is made 0755 too; an admin's folder with drop-ins of its own keeps
its mode (`makeHoldOffDropInDir`).

`start_swap_file` lets the unit go before it starts it: it removes the
drop-in, reloads systemd in every case, and unmasks the unit when systemd
reads it masked, which reloads again. The status and the drain count a unit
as held off when the drop-in is there or the unit is masked, so a mask an
admin set still keeps the file the managed one and blocks a drain
(`unitHeldOff`).

A host an older LinuxIO switched to zram has the unit masked instead. On
zram-generator such a host reads `none` after a reboot, since the mask kept
`swap.target`, and zram with it, from starting, and the dialog offers zram;
one that has not rebooted, or runs zram-tools, reads `zram`. Either way any
mode change now clears the mask: a switch to zram holds the file off with
the drop-in and unmasks the unit, and a switch to disk or zswap unmasks it
and starts the file. After that `swap.target`, and with zram-generator
zram, start at boot again. So such a host should be switched once, to zram
again or to another mode.

The file itself is never created, grown or deleted.

## Taking a disk swap device away

A mode change and a drain take a swap file or partition away by running
`swapoff` on its `/proc/swaps` path themselves (`swapOffDevice`,
`swapoff.go`), and not by stopping its unit. systemd runs the same `swapoff`
for a stop, but kills it after the unit's timeout, 90 seconds by default,
and a full swap file on a slow disk takes longer than that to read back. Run
by LinuxIO it has as long as the disk needs. Nothing bounds it but the
request.

- **It is a process of its own**, util-linux's `swapoff`, and not a
  `swapoff(2)` call from the job host. While the pages come back the kernel
  makes the caller the first process it kills when memory runs out
  (`set_current_oom_origin` in the kernel's `mm/swapfile.c`), and that has to
  be a helper and not the job host, which runs every job. The room check is
  made once, before the call, so memory other programs take during a long
  `swapoff` can still run out; the helper is then killed and the change
  fails.
- **A `swapoff` that is killed, or cancelled with the request, leaves the
  device swapping.** `swapoff(2)` gives up when a signal is pending and the
  kernel puts the device back, with the pages already read in memory
  (`try_to_unuse` and the reinsert in `mm/swapfile.c`). A cancelled request
  kills the helper. The kernel looks for the signal between one process's
  memory and the next, not inside one, so the cancel takes effect when the
  process being read back is done: at once for many small processes, after
  all of its parked data for one large one.
- **systemd follows.** It watches `/proc/swaps`, and marks the unit of a
  device that left inactive (`swap_process_proc_swaps` in systemd's
  `src/core/swap.c`). The unit is still what keeps the file off at boot
  (held off by LinuxIO's drop-in) and what starts a device again. Once a
  swap file is off, holding it off is not cancelled with the request.
- **A start is sent until the device is back.** systemd does nothing for a
  start of a unit it still holds active. So before a start of a device that
  was taken away, LinuxIO waits up to 15 seconds for the unit to read
  something other than `active` or `deactivating`, and while the device has
  not returned it sends the start again every 15 seconds, for a minute
  (`waitUnitSeenOff`, `startUntilBack`). That also outlasts systemd's start
  limit, which refuses a unit's sixth start within 10 seconds
  (`start-limit-hit`), as a quick series of changes in the test VM met.
- **A device that left counts as stopped**, whatever `swapoff` returned.
- **No unit state is read first.** `swapoff` takes the device away whatever
  systemd says of its unit.
- **The path must be one only root can redirect** (`rootOnlyPath`).
  `swapoff(2)` looks the path up again, so in a folder another user can write,
  a link put at the path, or at the name a deleted file is listed under
  (`<path> (deleted)`), would send the call to another active swap device,
  which would leave without the room check made for this one. Every folder on
  the path must be a folder, not a link, that root owns and no group or
  everyone may write; otherwise `swapoff` is not run and the device keeps
  swapping. The status applies the same test first, so such a swap file is
  not managed and no step or drain reaches this point; the check here is
  what holds when a folder changes between the status and the call, and
  then a mode change leaves the swap file on and reports `others_can_write`,
  and a drain is refused with 409. `/swap.img`, `/swapfile` and everything
  under `/dev` pass.
- The path must be absolute, as every `/proc/swaps` path is: `swapoff` reads
  `LABEL=` and `UUID=` arguments as tags.

The job host runs as root with no capability or system call filter
(`packaging/systemd/linuxio-jobs@.service`), and its `OOMPolicy=continue`
keeps a killed helper from taking the other jobs with it.

## Waiting on systemd

zram is still stopped through its provider's unit, since the unit also
resets the device, or tries to (see `start_zram`). Before a mode change stops zram, it reads the unit's
state. A unit that reads `inactive` or `failed` would not take zram away, so
no stop is issued and zram stays; this covers zram set up by another tool. A
state that cannot be read does not hold the stop back.

Waits are bounded: a minute for a device to appear in `/proc/swaps`, ten
minutes for zram to leave, polled every 250 ms. systemd's own stop timeout
for zram's unit (90 seconds by default) is not raised: emptying zram moves
pages inside memory.

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

The five kinds of files LinuxIO manages start with
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
| `/etc/systemd/system/<unit>.d/90-linuxio-memory.conf`, for the managed swap file's unit | `zram` (`stop_swap_file`), removed by `zswap` and `disk` (`start_swap_file`) | `[Unit]` with `ConditionPathExists=!/`, which never holds, so systemd skips the unit and the file stays off at boot (see [Holding a swap file off at boot](#holding-a-swap-file-off-at-boot)). Followed by a reload each way. Its folder is made 0755. |
| `<path>.before-linuxio`, beside either zram file | a run that replaces that file while it is not LinuxIO's | The file as it was, with its mode. Not undone by a rollback, and replaced by the next such copy. |

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

Besides these, with zram-tools `zramswap.service` is enabled or disabled, and
`start_swap_file` removes a mask of the swap file's unit, a symlink in
`/etc/systemd/system` or `/run/systemd/system`, that an older LinuxIO or an
admin left.

Never touched: the kernel command line (zswap persists through tmpfiles, not
`zswap.enabled=`), the swap file's own content, creation or size, and swap
partitions. zram writeback is not exposed.

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
then read `mixed`. The refusal says how to turn the file off: a package's
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
service step, since `start_zram` does that. See
[Capabilities](./capabilities.md#zram).

## The dialog

- A banner, worst condition first, with at most one action: no swap and no zram
  ("If memory runs out, Linux will close programs"); mixed; data parked on
  disk, with Bring back to memory, disabled with the reason when `can_drain` is
  false; an active swap file on btrfs, with Switch to compressed memory when
  zram can be chosen and the file is managed (a switch could not take any
  other file away); or "Memory is fine".
- Under the banner, a warning for a swap file that is not managed
  (`swapFileNote`): its path, why (`unmanaged_reason`), that LinuxIO will
  not turn it off or empty it, or will not turn it on, whatever is chosen,
  and for `others_can_write` to move it to a folder only root can write. It
  is there before anything is asked of the file.
- Under it, while `zram_generator_devices` lists any, a note
  (`generatorFilesNote`) with each file, its device and the size settings it
  writes (`zram-size`, `zram-fraction`, `max-zram-size`), or that it sets no
  size, since LinuxIO's size then applies to `zram0` after a switch to zram.
  With zram-generator as the provider the note says that what these files set
  wins over LinuxIO's settings; with zram-tools as the provider it says only
  that zram-generator is installed too and sets up compressed memory from
  them.
- A single-choice list of three rows, in the order zswap, zram, disk, each with
  a plain name, its technical name once, a description, a caution where one
  applies, and chips for the recommended and the current row. A row that cannot
  be chosen shows why; the zram row shows a link to Capabilities instead of a
  choice while no zram tool is installed. The zswap and disk rows need disk
  swap, as the plan counts it: a managed swap file, or an active swap file
  or partition; with zram-generator as the provider they also say why the
  plan refuses them while another of its files defines a swap device, naming
  the first, and how to turn it off, as the refusal does. Apply is enabled
  for a different row, or
  for the current one while the host is `mixed`.
- An Apply that needs agreement asks first, as a second step of the same
  dialog and not a dialog on top of it, which lists all of it
  (`confirmView`): choosing zram while a managed swap file
  (`swap_file.managed`) is active, with what will happen (data parked in the
  file comes back, the file is turned off and stays off after a restart, and
  the file is not deleted); and any Apply that replaces the provider's
  configuration file while it is not LinuxIO's (to zram with either tool, to
  disk or zswap with zram-generator), with the file and where its copy is
  kept. That step's Apply sends the file in `confirm_replace`. The step shows
  what it showed when Apply was pressed, while a refresh or the change itself
  moves the host. It opens with focus on Back, and Back returns focus to
  Apply.
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
- Close and Escape work while a change runs. A mode change and a drain are
  jobs ([Jobs](./jobs.md)): the task tray shows their progress and can cancel
  them. The other controls stay held, since the host refuses a second
  change. Closing drops an unapplied choice and keeps the one a mode change
  is applying, so the dialog shows that change when it is opened again, on
  its first step and not on the confirmation. A step of the slider made
  while its save runs is still sent.
- Whatever follows the job reports its result, once
  (`frontend/src/tasks/memoryFeedback.ts` holds the texts):
  - The dialog that started it follows it for as long as the tab is not
    reloaded, also after the dashboard is left, and toasts the result. A
    dialog opened meanwhile sees that change in the mutation cache, holds its
    controls and does not follow the job a second time.
  - After a reload, or on another tab, a dialog that opens finds the running
    job and takes it up (`useActiveTaskRecovery`, then the mutation's
    `watch`): it shows the step, holds its controls and toasts the result.
    The job's metadata carries the requested mode as `action` for that.
  - When no dialog follows the job, the task tray reports it, from the
    entries for the two routes in `terminalTaskFeedback.ts`. A dialog marks
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

## Tests

- Go unit tests in `backend/bridge/handlers/memory` run against fixture roots
  (a temporary tree standing for `/proc`, `/sys` and `/etc`) and a fake
  `hostSystem` that stands for systemd, files and kernel parameters. They cover
  the parsers, mode derivation and recommendation, the planner for every
  transition and refusal, each step, rollback (including a cancelled request, a
  write that lands and then errors, and a stop that never completes), the room
  checks, drain, the fail-fast and slow-success waits, a stop left out for a
  unit systemd does not report running, the reason reported for each thing
  left, zram restarted at the planned size or kept at its own (and brought
  back after a cancel or a failed stop, as `stop_zram` brings it back), the
  live sysctls after a run that ended in another mode and the kernel defaults
  written only over LinuxIO's values, a `swapoff` that fails or is cancelled,
  the wait for systemd to see a drained device leave and a start sent again,
  the status read again after each route's change, the swap file chosen
  among several in fstab, no disk hint under a FUSE mount, the provider
  chosen by the unit that runs zram, the swap file held off by LinuxIO's
  drop-in (its path and content, never a mask, its folder 0755 under umask
  077) and let go again with a reload in every case, a mask an older
  version left removed by a switch either way, a rollback that removes a
  drop-in the run wrote and does not put back one it removed, and the status
  and route agreement for held-off units, by the drop-in or a mask. `zram_config_test.go` covers a zram configuration file that
  is not LinuxIO's (what counts as one, with `;`, a byte order mark and CRLF;
  a package's unchanged conffile against a fake dpkg status, changed, marked
  obsolete or not recorded; one that cannot be read; the refusal without
  `confirm_replace` with nothing changed, and a `confirm_replace` naming
  another file; the copy with its mode over an older one, a folder or a link
  at its name, the copy kept after a failed run, and the Info line) and
  zram-generator's other files (which are read and in what order, a
  same-named file in a later folder, a link to `/dev/null`, the vendor main
  file, zram mounts, a header with a comment, the older size keys, one that
  cannot be read in the status and in a change, and the refusal to leave
  zram with how to turn the file off). `swapoff_test.go` runs a
  script in place of `swapoff`: its arguments, what it printed in the error,
  and a cancel that ends it. `escape_test.go` pins the unit-name
  escaping. Run with `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/memory`.
- Frontend: `memoryStatusView.test.ts` and `MemoryOverflowDialog.test.tsx`
  (the real generated client with the calls mocked), and `Memory.test.tsx` for
  the pencil on the dashboard card and the dialog's URL, all under jsdom.
  `src/test/browser/memory-overflow.spec.ts` runs in Chromium against the
  Memory card in the browser fixture (`fixture/routes/MemoryOverflowPage.tsx`,
  which has no bridge, so nothing is applied): the pencil and the URL, Escape,
  a reload with the URL, the rows as one choice with the arrow keys, the
  slider and its caption, and the confirm step's focus and the configuration
  file it names. Run it with
  `make test-frontend-browser-quiet PLAYWRIGHT_ARGS=src/test/browser/memory-overflow.spec.ts`.
- The root tests change `/etc` and swap on the machine they run on, and put
  it back only through the test's own cleanup, so they are for the disposable
  test VM only.
- `make vm-test-memory-integration` runs `TestMemoryIntegration` as root in the
  [test VM](./development.md#test-vm). It switches the real host to zram and
  back through systemd, the kernel and `/etc`, and checks: after zram, mode
  `zram` with zswap off, swappiness 180 (unless TuneD runs), the three zram
  sysctls, the swap file's unit held off by LinuxIO's drop-in and not
  masked, `zramswap.service` enabled and the configuration holding
  `ALGO=zstd`; after the return, the swap file active, no zram swapping, the
  drop-in gone and `zramswap.service` disabled. After
  zram it also checks that zram has the planned size and that systemd no
  longer reads the swap file's unit active, since the file left through
  `swapoff` and not a stop. It then drains the swap file (`swapoff`, the
  wait for systemd, a start of the unit) and checks the file and its unit
  are active again, starts a 64 MiB zram beside the swap file and switches to
  zram again, which must end at the planned size. It holds `zram0` open while
  the provider's unit stops, so the reset at the stop fails as the test VM saw
  zram-generator's fail, and checks that a switch to zram resets the device
  and starts zram at the planned size. It then writes an admin's
  configuration for the provider, mode 0640, and checks that a switch to zram
  is refused without `confirm_replace`, that a folder at the copy's name stops
  a confirmed switch with the file as it was, and that a confirmed switch
  keeps the copy with mode 0640 and replaces the file. With zram-tools it
  then makes the stop of `zramswap.service` hang past a `TimeoutStopSec` of
  5 seconds (a drop-in in `/run`) and checks that systemd leaves the unit
  `failed` with zram swapping, and that a switch to disk fails within a
  minute, the stop given up, and not after the wait's ten minutes; zram is
  then taken away with `zramswap stop`. With zram-generator it writes a
  drop-in that defines `zram1` and checks that the status lists it and that
  a switch to disk is refused naming it; the drop-in is removed before any
  reload. It then writes drop-ins in the four folders the generator reads,
  one hidden by a link to `/dev/null`, two of the same name and a zram
  mount, reloads, and checks that the generator made a swap unit for exactly
  the devices the status lists and a setup unit for those and the mount (the
  units systemd lists, as `systemctl list-units --all` does); the files go,
  systemd is reloaded, and the empty zram devices the reload added are
  removed. Last it switches to zswap and back to disk, and checks zswap live
  and its tmpfiles entry each way; with zswap on, a compressor the kernel
  does not have must be refused with `ENOENT` or `EINVAL` and leave the
  compressor as it was. A host that starts `mixed`, as a fresh zram-generator
  install leaves it, goes to disk first. Cleanup puts the host back as it
  was: the starting mode, the three files LinuxIO manages, the zram
  configuration file and the copies, `zramswap.service`'s start at boot, zram
  beside the swap file for a host that started `mixed`, and the four live
  sysctls and zswap; the test files a cut run left are removed when the test
  starts. The same target then runs `TestMemoryDrainIntegration`
  (`drain_integration_test.go`) on a data disk used as swap. zram swapping
  ahead of the disk, as zram-generator's does at priority 100, is stopped
  through its provider's unit for the test and started again after it; any
  other device ahead of the disk skips the test, since the data would not
  park there. Each wait is bounded (10 minutes to park, 10 to cancel, 15 for
  the full drain), and data that goes to another device fails the test at
  once. The cgroup's `memory.reclaim` is written with system calls: it
  answers EAGAIN when it reclaimed less than asked, and Go's `os.WriteFile`
  would wait on that pollable file for good, which is how an earlier run
  hung. Eight helper
  processes hold data that does not compress, in a cgroup of the test's own,
  which is told to reclaim until the data is on the disk and out of the swap
  cache. The test then cancels a drain once `swapoff` has read some of it
  back, and checks that the device still swaps with most of the data parked
  and its unit active; drains in full, and checks that the device is empty
  and swapping again, its unit active, and that each helper finds its data
  unchanged by its checksum. It runs with 2 GiB, and again with 320 MiB and
  the cgroup's reads from the disk capped at 2 MiB a second (`io.max`), where
  the drain must take more than 90 seconds: nothing ends a `swapoff` at the
  timeout systemd gives a swap unit. On 2026-11-16, on kernel 7.0.0-38 in the
  test VM, the cancel left 1791 of 2047 MiB parked and the rest drained in
  11 seconds, and the capped run drained 280 MiB in 2 minutes 19 seconds.
  `make vm-test-memory-integration ZRAM_PROVIDER=zram-generator`, after
  `make vm-reset`, runs the same test with Ubuntu's `systemd-zram-generator`
  in place of zram-tools: after zram it checks the `[zram0]` configuration
  with `zstd` and that `systemd-zram-setup@zram0.service` is active, and after
  the return that the configuration holds no device. On 2026-11-18, on
  kernel 7.0.0-43, both providers ran to the end (zram-tools 0.3.7 and
  Ubuntu's `systemd-zram-generator` 1.2.1): with `zram0` held open the
  generator's stop left the device set up and its setup service `failed`,
  and the next switch to zram reset the device and started zram at the
  planned size; the drains matched the earlier numbers.
- The same target runs more root tests around those, in this order
  (`suite_memory` in `packaging/vm/linuxio-vm.sh`, and
  [Test VM](./development.md#test-vm) for what the suite sets up):
  - `TestMemoryDefaultZramIntegration` (`boot_integration_test.go`) starts
    from the zram a distribution turns on with zram-generator, the vendor
    file `/usr/lib/systemd/zram-generator.conf` alone with `zram0` swapping
    at the size it sets, and skips on any other host. The status must read
    `zram`, or `mixed` beside an active swap file, with zram-generator as the
    provider and no other file listed. Without disk swap a switch to disk is
    refused, and a switch to zram writes LinuxIO's configuration for the next
    boot and keeps zram at its size (`zram_size`, `no_other_swap`); beside a
    swap file a switch to zram restarts it at the planned size and a switch
    to disk stops it. Cleanup puts the default back. On Fedora the suite runs
    it before and after it adds `/swap.img`.
  - `TestMemoryStatusWithoutRootIntegration`
    (`unprivileged_integration_test.go`) reads the status as a system user
    without sudo, with `PATH=/usr/bin:/bin`, while TuneD runs and a switch to
    zram made as root with the job host's umask 077
    (`TestMemorySwitchIntegration`) holds the swap file off: TuneD must read
    running with the profile `tuned-adm active` names, the zram provider the
    suite installed must be found, zram-tools' `zramswap` through the
    `/usr/sbin` fallback, and the swap file must read as held off. The root
    tests all run with umask 077, as the job host does, and check that the
    drop-in's folder is 0755.
  - `TestMemoryDrainAliasIntegration` drains the data disk while fstab names
    it by `UUID=`: its alias unit must read off within the 15 seconds a
    drain waits, one start of it must bring the disk back, and a drain must
    take less than those 15 seconds.
  - `TestMemoryRebootIntegration` (`boot_integration_test.go`) runs in two
    phases around a reboot: a switch to zram; after the boot, the swap file
    off, its unit held off by the drop-in and not active, `swap.target`
    active and zram the only swap, then a switch to disk, the drop-in gone,
    the unit back and active where fstab names the file by its own path, and
    a drain. The suite runs it with fstab naming `/swap.img`, then naming it
    through the link `/swap-link`.

  On 2026-11-18, on Ubuntu 26.04 (kernel 7.0.0-43, systemd 259.9,
  zram-tools 0.3.7, TuneD 2.25.1) everything passed: systemd killed the hung
  stop 5 seconds after it began and left `zramswap.service` `failed`
  (`Stopping timed out. Terminating.`), and the switch ended at once, 6
  seconds in all (21 in a second run, where the swap file's start first met
  systemd's start limit and was sent again), the alias read
  off 1 ms after the `swapoff`, and after each reboot zram swapped with the
  file off, under either name. On Fedora
  45 (kernel 7.3.4-300.fc45, systemd 262.1, zram-generator 1.2.1, TuneD
  2.28.0) everything before the reboot passed: the default zram read `zram`
  alone and `mixed` beside the swap file at 3898 MiB (`min(ram, 8192)`), a
  switch to zram there kept that size with `no_other_swap` and beside the
  swap file restarted it at the planned 1949 MiB; the generator's units
  matched the status; the drains matched Ubuntu's (the cancel left 1791 MiB,
  drained in 15 seconds; capped, 280 MiB in 2 minutes 13 seconds); the alias
  read off 2 ms after the `swapoff`. With `ZRAM_PROVIDER=zram-generator` on
  Ubuntu everything passed as well; Ubuntu's package also brings a default
  `zram0`, at half of RAM, which the default-zram test passed on. The first
  runs held the swap file off with a mask, and on both generator hosts zram
  did not start after the reboot (see
  [Holding a swap file off at boot](#holding-a-swap-file-off-at-boot)). With
  the drop-in in its place all three runs passed end to end, each reboot
  ending with `swap.target` active and zram the only swap: Ubuntu with
  zram-tools and with `systemd-zram-generator`, and Fedora 45, where the
  drains read 1806 MiB back in 12 seconds after the cancel and 279 MiB
  capped in 2 minutes 14 seconds.
  Both kernels refused an
  unknown zswap compressor with `ENOENT`. Install from Capabilities in the
  browser (`capability-install.spec.ts`, [Capabilities](./capabilities.md#zram))
  passed on Ubuntu, where the dialog then named zram-tools, and on Fedora as
  installed, with SELinux enforcing, where PackageKit installed
  `zram-generator` and the dialog named zram-generator as the provider
  ([Capabilities](./capabilities.md#zram) has the runs).

## Known limits

Each is by design or unlikely to be met, or is an open item in
[the backlog](./TODO/backlog.md).

- A `swapoff` has no time limit: it runs until the device is empty or the
  job is cancelled from the task tray. A cancel takes effect only when the
  process whose memory is being read back is done.
- A swap partition or a swap device fstab does not name is never stopped, so
  such a host cannot choose compressed memory only (partition) or have the
  device drained (unnamed).
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
  zram back at the next boot and the mode then reads `mixed`. With both
  installed and neither running zram, zram-tools is used, so a host whose
  zram the generator ran comes back on zram-tools after a switch away and
  back, with the generator's configuration left without a device.
- A swap file in a folder that someone other than root can write, or below
  one, is not managed: LinuxIO never stops, starts or drains it and does
  not move it (see "A swap file others can redirect is not managed"). A
  folder on its path that is a link counts as one others can change, even
  where only root could have made the link. When it is the swap file
  LinuxIO would manage (see the next limit), another is not managed in its
  place. Off, it is not held off either, so the next boot turns it on,
  beside zram if that is the mode.
- LinuxIO manages one swap file: the first fstab names that is active or
  whose unit is held off, or the first when there is none. Another is never
  stopped, started or held off: one that swaps stays beside zram (`mixed`,
  `not_managed`), and one that is off is not held off, so a boot that starts
  it puts it beside zram. A swap file the admin masked themselves, before the
  one that swaps, is taken as the managed one: the one that swaps then stays
  beside zram, and a switch to disk or zswap unmasks and starts the masked
  one. When two fstab swap files are held off the first is the managed one,
  so with a swap file the admin masked listed before the one a switch to zram
  held off, the way back starts the admin's and leaves the other held off.
- A mask on the managed swap file's unit, an admin's or an older
  LinuxIO's, does not outlast a mode change: a switch to zram holds the file
  off with LinuxIO's drop-in in its place, and a switch to disk or zswap
  removes it and starts the file (see
  [Holding a swap file off at boot](#holding-a-swap-file-off-at-boot)).
- LinuxIO's hold-off drop-in stays when its fstab entry is deleted or
  another swap file becomes the managed one, and holds that unit off again
  if the entry returns.
- Removing the hold-off drop-in leaves its `<unit>.d` folder, empty.
- A condition an admin put on a swap unit themselves is not seen by the
  drain's check, so a drain takes that device away and its start starts
  nothing; the drain then reports the device off.
- A change whose status still cannot be read after five tries fails the
  request, with an error that says the change was made; nothing is undone.
- On a host that runs zram, `page-cluster`, `watermark_boost_factor` and
  `watermark_scale_factor` count as zram's whoever set them, so a switch
  away sets them back to the kernel defaults live even when the admin's own
  sysctl file set the same numbers; the next boot applies the admin's file
  again.
- A swap file on a filesystem that is not mounted is judged by the folders
  that are there, not by those of the filesystem a start would mount. Where
  that filesystem's folders can be changed by other users, a start swaps
  there, as every boot does.
- The disk hint reads the kernel's removable flag, so a worn flash drive in
  a USB disk enclosure, which reports itself as fixed, is not steered to
  compressed memory, nor is a swap file on LVM or LUKS, whatever disk is
  under it.
- A click on Apply within half a second of an arrow key on the eagerness
  slider is lost: the click moves focus off the slider, which starts the
  save, and Apply is held while it runs.
- On a kernel where zswap failed to start, the tmpfiles line that turns zswap
  off may fail at each boot and leave a line in the journal. Not checked on a
  host.
- The overflow alert is not built
  ([Watched conditions](./TODO/watched-conditions.md)).
