# Memory overflow: swap, zswap and zram from Settings

## Status

Design agreed in chat on 2026-11-06, nothing implemented. The plan below is
the whole scope; the open decisions are in section 7. When it ships, fold the
behaviour into a new memory document under docs and delete this file.

| Phase | Scope | State |
|---|---|---|
| 1 | Read-only status route and the Settings tab with the banner, the three rows and Advanced | not started |
| 2 | Mutations: choose a mode, set eagerness, bring parked data back; Debian/Ubuntu only | not started |
| 3 | "Overflow nearly full" alert, Fedora/RHEL zram path, dashboard link | not started |

Contents: 1. Why · 2. What the user sees · 3. Recommendation rules ·
4. Backend · 5. Safety · 6. Tests and docs · 7. Open decisions · 8. Tasks

## 1. Why

LinuxIO shows swap use on the dashboard and nothing else. The three knobs an
admin reaches for on a home server are not visible and not persisted:

- how eagerly the kernel moves data out of RAM (`vm.swappiness`);
- whether that data is compressed in RAM first (zswap in front of a swap
  file, or zram with no file at all);
- bringing parked data back after a job that pushed it out.

Two facts make it more than a nicety. A swap file on btrfs blocks snapshots of
the subvolume that holds it and cannot be balanced; Ubuntu puts `/swap.img` in
the root subvolume, so on a btrfs root the system drive cannot be snapshotted.
And no mainstream panel exposes any of this: Cockpit formats swap partitions
through UDisks, Proxmox and Webmin only show usage.

The page must read as one decision to someone who has never heard the words
zswap or zram. The storage page is the model: a health line first with one
action, each option as a row with a plain name and one or two sentences, the
recommendation computed for the host and preselected, a missing package shown
as a link to Capabilities.

## 2. What the user sees

A new privileged Settings tab, **Memory**, beside Power.

### Banner (worst condition first, one action)

| Condition | Text | Action |
|---|---|---|
| No swap device and no zram | "If memory runs out, Linux will close programs." | Turn on compressed memory |
| Swap used on a disk device, and `MemAvailable` leaves room | "X of memory is parked on the system drive. Programs that need it are slower until it comes back." | Bring back to memory |
| Same, no room | Same text; button disabled with "Not enough free memory right now" | none |
| Active swap file on a btrfs filesystem | "The swap file blocks snapshots of the system drive." | Switch to compressed memory |
| Otherwise | "Memory is fine. Nothing is parked." | none |

### "When memory runs out" (one radio group, Apply button)

Rows in this order. The technical name appears once, as a secondary caption.

1. **Compressed memory with the disk as backup** (zswap). "Squeezes rarely
   used data to about a third of its size and keeps it in memory. When the
   compressed area is full the oldest data spills to the swap file." Disabled
   with "Needs a swap file on the system drive" when there is none.
2. **Compressed memory only** (zram). "The same, with no swap file. Writes
   nothing to disk and snapshots of the system drive work. Holds about X using
   about Y of memory." Caution in the warning colour: "No backup. If it fills
   up, Linux closes the hungriest program, and a VM would stop like a power
   cut." When `zram-tools` is missing the row shows the Capabilities link
   instead of a radio, as Scheduled maintenance does for btrfsmaintenance.
3. **Disk only** (swap file). "Rarely used data goes straight to the swap file
   on the system drive. Slowest." Caution: "Blocks snapshots of the system
   drive while in use." Disabled when there is no swap file.

Chips: **Recommended** on the row section 3 picks, **Current** on the row that
matches the host now. Choosing a row that stops the swap file opens the
confirm dialog with the steps in plain words.

### Advanced (collapsed)

One slider, "Eagerness to move data out of memory", 0 to 200, with the
caption "Recommended for <mode>: N". When TuneD is active the slider is
read-only with "Managed by the TuneD profile <name>", because TuneD reapplies
its own value on every profile switch and at boot.

### Dashboard

The Swap row on the Memory card links to `/settings?tab=memory`. Nothing else
changes outside Settings; dashboard and hardware cards stay read-only.

## 3. Recommendation rules

Computed by the backend and returned in the status, like the pool dialog's
metadata profile. First match wins.

| Host | Recommended |
|---|---|
| No swap file | zram |
| Swap file on SD or eMMC (`mmcblk*`, or removable and non-rotational) | zram |
| Swap file on btrfs | zram, caution shown |
| Swap file anywhere else | zswap |

Mode presets, applied with the mode and written to the sysctl drop-in:

| Mode | swappiness | page-cluster | watermark_boost_factor | watermark_scale_factor |
|---|---|---|---|---|
| zram | 180 | 0 | 0 | 125 |
| zswap | 60 | kernel default | kernel default | kernel default |
| Disk only | 60 | kernel default | kernel default | kernel default |

The zram row follows the Pop!_OS and Arch values. zram size is half of RAM
capped at 8 GiB, Fedora's default; "holds about X using about Y" assumes 3:1.

## 4. Backend

New handler package `memory` beside `power`, registered in `register.go`,
shaped like `handlers/power`.

| Route | Kind | Privileged | Does |
|---|---|---|---|
| `memory.get_swap_status` | call, retry-safe | no | Reads everything below; no secrets |
| `memory.set_swap_mode` | mutation with progress | yes | Applies one row end to end (section 5 order) |
| `memory.set_swappiness` | mutation | yes | Writes `/proc/sys/vm/swappiness` live and the drop-in |
| `memory.drain_swap` | mutation with progress | yes | Stops and starts the active swap units |

`SwapStatus` (apischema) carries: devices from `/proc/swaps` with type, size,
used, priority and for a file its filesystem type and disk hint; `MemAvailable`
and swap totals from `/proc/meminfo`; swappiness and the three other sysctls;
zswap `enabled`, `compressor`, `max_pool_percent`; zram devices with disksize
and compressed size from `/sys/block/zram*/mm_stat`; `zram_available` from the
capability; `tuned_active` and the profile name via `power.GetStatus`; the
derived `mode` (`disk`, `zswap`, `zram`, `none`, `mixed`), `recommended_mode`
and `recommended_swappiness`; and `can_drain` with its reason.

Reads use `os.ReadFile` on `/proc` and `/sys`; no subprocesses. Writes:

- sysctls: write the `/proc/sys/vm/*` files live, persist to
  `/etc/sysctl.d/90-linuxio-memory.conf` with `atomicfile.Write`;
- zswap: write `/sys/module/zswap/parameters/{compressor,enabled}` and read
  them back (the kernel rejects a missing compressor with EINVAL); persist as
  `w` lines in `/etc/tmpfiles.d/linuxio-zswap.conf`, never the kernel command
  line;
- zram (Debian/Ubuntu): `/etc/default/zramswap` with `ALGO=zstd`, `SIZE=<MiB>`,
  `PRIORITY=100`; `zramswap.service` enabled and started through
  `common/systemd`; verify `zram0` appears in `/proc/swaps`;
- swap file: stop and mask, or unmask and start, the unit systemd generated
  from fstab (`/swap.img` is `swap.img.swap`; escape the path the way systemd
  does; `systemdEscapePath` in `backend/common/btrfs/degraded_integration_test.go` already does it and should move to `common/systemd`). Masking wins over the fstab generator, so the file never comes back
  at boot until unmasked. The file itself is never deleted or created.

Capability entry: `Name: "zram"`, `LogName: "zram-tools"`, detect by the
`zramswap` command, `Install: PackageDebian "zram-tools"`. The RHEL/Fedora
package is `zram-generator` with a different config file; phase 3.

Client errors wrap the sentinels: unknown mode or swappiness out of range
`ErrInvalidArgs`; a row the host cannot take (no swap file, package missing)
`ErrConflict`; drain without room `ErrConflict`. Every mutation logs one Info
line past tense with the mode as an attribute.

## 5. Safety

Order for `set_swap_mode` to zram, each step verified before the next, so a
failure leaves the host no less safe than before:

1. Write the zram config and the sysctl drop-in.
2. Enable and start `zramswap.service`; verify `zram0` in `/proc/swaps`.
3. Apply the sysctls live.
4. Disable zswap in sysfs and remove its tmpfiles entry.
5. Only now stop and mask the swap file unit, and only when its used bytes
   are below `MemAvailable` minus a 512 MiB margin. On refusal the status
   reports `mixed` and the UI says the file is still active with a retry.

zswap and disk modes reverse the order: unmask and start the file first,
verify it in `/proc/swaps`, then stop and disable `zramswap.service`, then
sysctls, then zswap. Draining uses the same room check; it refuses rather than
inviting the OOM killer.

Nothing in this feature creates, grows or deletes a swap file, installs zram on
Fedora, exposes zram writeback, or touches swap partitions on the storage page.

## 6. Tests and docs

- Go unit tests with a fake `/proc` and `/sys` root: the parsers, the
  recommendation table, the mode derivation including `mixed`, the planner
  that emits the ordered steps for each transition and the refusal cases.
- A root integration test behind the existing root-test pattern that runs the
  zram transition and back in the test VM; the user runs it.
- Frontend: a `MemorySettingsSection.test.tsx` like the Docker daemon one
  covering each banner state, the disabled rows, the Capabilities link, the
  TuneD read-only slider and the confirm dialog.
- Docs: a new memory document under docs for the behaviour, the capability in
  `docs/capabilities.md`, the routes in `docs/privilege_pattern.md`'s table,
  and the mutations in the [mutation map](./mutation-map.md).
- Finish with `make generate` and `make test-quiet`; the section test also
  needs `make test-frontend-browser-quiet` for the confirm dialog focus.

## 7. Open decisions

- Who raises the "overflow more than 80 percent full" alert: the monitoring
  daemon already samples swap every few seconds, but alerts are raised today
  from the btrfs timer. Decide before phase 3.
- Whether the SD or eMMC hint should also cover USB sticks; a USB SSD would be
  misclassified, so start with `mmcblk*` only.
- Fedora: `zram-generator` has no enable step (its units are generated), so
  the Debian `EnableService` path does not apply as is.

## 8. Tasks

Phase 1

- [ ] `apischema.SwapStatus` and `memory.get_swap_status` with the parsers and
  the recommendation table, unit-tested against fixture `/proc` and `/sys`
- [ ] `zram` capability entry, Debian package only
- [ ] `memory` tab in `settingsTabs.ts` (privileged) and
  `MemorySettingsSection.tsx`: banner, rows, chips, Advanced slider read-only
- [ ] `make generate`, `make test-quiet`

Phase 2

- [ ] Persistence writers: sysctl drop-in, zswap tmpfiles, `/etc/default/zramswap`
- [ ] Swap-unit name escaping and mask/unmask/stop/start through `common/systemd`
- [ ] `memory.set_swap_mode` planner with the section 5 order and rollback,
  progress per step
- [ ] `memory.set_swappiness` with the TuneD guard
- [ ] `memory.drain_swap` with the room check
- [ ] Confirm dialog, Apply flow with toasts, slider editable
- [ ] Root integration test in the VM; the memory document; mutation map rows

Phase 3

- [ ] Overflow alert (decision above)
- [ ] Fedora/RHEL zram through `zram-generator`
- [ ] Dashboard Memory card Swap row links to the tab
