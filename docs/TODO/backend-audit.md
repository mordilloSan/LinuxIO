# Backend Audit Checklist

Read-only audit of `backend/**/*.go` (C auth daemon excluded), done 2026-10-23
on `dev/v0.48.0`, re-scanned 2026-10-24 on `dev/v0.49.0`, and worked through
on 2026-10-26 on `dev/v0.52.0`: every item whose fix was fully specified has
landed with its test and is removed (fixes are in git history). Numbers are
stable, so gaps are finished or rejected items. What is left needs a product
or policy decision, a host that this machine is not, or a measurement first.

How to use: take an item, decide, fix it with a test, run
`make check-backend-quiet` (or `make test-quiet` when the frontend contract
moves), and remove it. Remove this file once every item is done or rejected.

Verification legend:

- `[source]` – the full code path was read; the failing case follows from the code.
- `[host]` – also reproduced on this machine (systemd 259, qemu-img, apt).
- `[lib]` – behaviour of a library confirmed from its source/docs
  (Go 1.27.3 stdlib, SQLite docs).
- `[worker]` – found and line-checked by a scoped sweep; the cited lines were
  spot-checked but not every caller re-traced.
- `[unverified]` – needs a host check before acting; stated where.

"Behaviour change" says what a user or client will observe differently after the
fix, so each item can be accepted or rejected on that basis.

---

## A. Needs a product or policy decision

- [ ] **33. Btrfs: replication shares the hourly snapshot service, so a long send delays snapshots**
  `backend/btrfs-timer/snapshot.go:~74` (`replicateJobs` inside the oneshot), `backend/common/btrfs/unit.go`, `bridge/handlers/storage/btrfs_timer.go:77`
  - `[worker]`: while a multi-hour first send runs, the oneshot service is busy and timer activations are skipped. The units are rendered at runtime (`RenderUnit`/`RenderTimer`, written by `syncBtrfsTimer`), so nothing is packaged.
  - Fix: render a second `linuxio-btrfs-replicate.service`/timer pair with a `replicate run` subcommand, or start it `--no-block` at the end of the snapshot pass. Ordering and cadence are a product decision; existing installs get the new units on the next rule/job save.
  - Behaviour change: snapshots keep their cadence during sends. Not done 2026-10-26: the cadence/ordering of the second unit is the user's call, and the units can only be verified by the root integration test.

- [ ] **99. `apischema.Privileged()` on the remaining mutation routes**
  ca4e5d31 flagged the 13 routes that fail without root (LVM, NFS/CIFS mounts, unmount, btrfs maintenance, SMART tests, Docker auto-update). Still unflagged: `accounts.*` (useradd/usermod via subprocess), NFS/Samba `shares.*` (write `/etc/exports`, `smb.conf`), `wireguard.*` (`/etc/wireguard`), `network.set_*`/`enable_connection`/`disable_connection`, `systemd.*`, `docker.*`, `hostname.set_hostname`, `datetime.*`, `control.app_update`, `control.reboot`/`power_off`, `filebrowser.resource_post`, `config.set*`. Several of these go through polkit-gated D-Bus (systemd, NetworkManager, hostname, datetime, login1) and may work for a non-admin today.
  - Policy decision first: flagging removes anything polkit lets a non-admin do. See `docs/privilege_pattern.md` for the survey command.
  - Behaviour change: a non-sudo session gets 403 before anything runs instead of a host permission error part-way through.

- [ ] **128. LVM exit status 5 is lvm2's generic `ECMD_FAILED`, so lock/permission/metadata failures show as "no LVM"**
  `storage/lvm.go:33-34,53`. Verified `[host]`: as a non-root user `vgs nosuchvg` fails on `/dev/mapper/control` and `/run/lock/lvm` permissions and exits 5, the same code as root's "not found"; `pvs` as non-root fails on `/run/lock/lvm/P_global`. LinuxIO lists all objects (no names), which exits 0 with an empty report when nothing exists.
  - Fix: keep only `exec.ErrNotFound` as "no volumes".
  - Behaviour change: a non-root bridge's LVM listing errors instead of showing an empty list — decide together with 99 (the LVM mutations are already privileged; the listing is not).

- [ ] **68. Health summary hides a failed unit listing as "0 failed"**
  `backend/bridge/handlers/system/health.go:23` ignores the `FetchServices` error. The per-unit property reads are gone (2026-10-26: `systemd.ListServiceStates`); the honest card needs an error flag on `SystemHealthSummary` (`make generate`, `SystemHealth.tsx`), i.e. a contract and frontend change. Behaviour: the card says the count is unknown instead of 0.

- [ ] **107. Monitoring store transactions do not take the caller's ctx**
  `backend/monitoring/internal/store/store.go`, `maintenance.go`: `db.Begin()`/`tx.Exec` with no ctx (the deferred `rollbackTx` half landed 2026-10-26). Threading ctx needs `WriteSnapshot`, `WriteSmartDevices`, `RunMaintenance` and `OpenStore` to take it, with callers in `monitoring/internal/app/runtime.go` and the `httpapi` tests. Behaviour change: a cancelled ctx (shutdown) aborts an in-flight write instead of letting it finish — decide whether that is wanted.

## B. Needs a host or a measurement first

- [ ] **36. File operations fail below a directory symlink whose target is absolute**
  `backend/bridge/handlers/filebrowser/fsroot/fsroot.go`, `services/info_operations.go`, `download_stream.go`, `services/safe_open.go`
  - `[lib]`: Go 1.27.3 `os/root.go` `splitPathInRoot` returns `errPathEscapes` for any link target starting with `/`, even at root `/`; `iteminfo.ResolveSymlinksAt` resolves only the last component, so anything below `/home/u/data -> /mnt/data` fails at the intermediate hop. `[unverified]` on a host — reproduce first.
  - Fix: resolve the parent chain with `filepath.EvalSymlinks` at the path-normalisation entry points (parent only, so the link itself can still be moved/deleted) and run `CheckNotProtected` on the resolved path. Large: ~37 `fsroot.Open` sites, path-containment risk.
  - Behaviour change: files under symlinked mounts become usable.

- [ ] **57.** Hardlink reconcile runs N+1 queries in one transaction — `backend/indexer/internal/storage/hardlinks.go:425` (`reconcileHardlinksAfterReindex`), `mutations.go:80`. Fix: measure first; then aggregate ancestor deltas per directory. Risk: silent wrong directory sizes.

- [ ] **124.** `[unverified]` The unversioned `nvmlDeviceGetPciInfo` symbol may be an older ABI than the struct it fills — `monitoring/internal/collector/gpu/nvml.go`. 2026-10-26: the Go struct now has the v3 layout (68 bytes, the largest), so any ABI writes within bounds; what is left is a host check on an NVIDIA machine that the bus id still reads correctly, or registering `nvmlDeviceGetPciInfo_v3` explicitly.

## G. Not read / outside this audit

- `_test.go` files were grepped for coverage only, not read.
- The C auth daemon (`backend/auth`) except username validation and result-code mapping.
- Runtime behaviour of exportfs, wg-quick, NetworkManager, netplan, libvirt, qemu-img and dnf logs is from documentation/source reading, except the host checks noted (`journalctl -n`, `qemu-img` VMDK descriptor, apt term.log).

## Rejected 2026-10-26 (listed once so they are not re-raised)

- 48 (SHA-256 of the bridge binary per login): ~50 ms per login is not worth a cache keyed on inode metadata.
- 55 (lazy marshal of the collector hand-off): the eager marshal is what detaches the sample from later mutation of `data`; "Behaviour: none" is not worth that guarantee.
- 96 (ctx-first in `filebrowser/services`): ~57 call and test sites for no user value, as the item said.
- 97 (`hostcmd.LookPath` everywhere): the sbin fallback would make `smartctl`/`dmidecode` "found" on a non-root Debian bridge whose PATH lacks sbin, turning "feature unavailable" into permission errors.
- 102 (logging `component`/`subsystem` attributes) and 108 (`sort.Slice` to `slices.Sort*`): no guard rule, no user value; convert when touching a file.
