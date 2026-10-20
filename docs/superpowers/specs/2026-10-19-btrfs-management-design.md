# Btrfs management — design

Status: draft for review. Approved 2026-10-19 decisions: LinuxIO-native automatic
snapshots (no snapper), full root rollback with reboot, replication to local and
SSH targets.

## Goal

Make btrfs on a LinuxIO host as manageable as a ZFS pool from the browser:
see each filesystem's real health and space, manage subvolumes, take and roll
back snapshots, schedule snapshots with retention, change the device layout,
replicate snapshots elsewhere, and set quotas and compression. Everything uses
`btrfs-progs` and systemd, which are already on any host that boots or mounts
btrfs. No new packages or daemons.

Out of scope: RAID5/6 profiles (refused, unsafe upstream), nested-subvolume
recursion in snapshots (btrfs does not do it; the UI says so), booting into a
snapshot from GRUB, distro package-manager hooks (apt/dnf) for pre-update
snapshots, Docker's btrfs storage driver subvolumes (hidden from the list).

## Shared foundation

### Backend

- Files live in `backend/bridge/handlers/storage/btrfs_*.go`, next to
  `btrfs_maintenance.go`. `CreateBtrfsSubvolume` moves from `filesystems.go`
  to `btrfs_subvolumes.go` unchanged.
- Subprocesses use `exec.CommandContext` with separate args, as `lvm.go` does.
  Byte-exact output flags (`-b`, `--raw`) are used; output is parsed from text
  so hosts with btrfs-progs 5.x (Ubuntu 22.04) work. Parsers are pure functions
  with fixture tests. No `--format json` dual path.
- Filesystems are identified by **filesystem UUID**. Subvolumes are identified
  by their **path relative to the top-level subvolume (ID 5)**, e.g. `@`,
  `@home`, `data/photos`. Requests carry `{uuid, path}`.
- Top-level access: Ubuntu mounts `/` with `subvol=@`, so the top level is not
  mounted anywhere. LinuxIO mounts it privately on first use at
  `/run/linuxio/btrfs/<uuid>` with `subvolid=5,noatime` and leaves it mounted
  (`/run` is cleared at boot). All subvolume, snapshot and rollback operations
  work through this path. `mountpointForSubvolume` resolves a subvolume to a
  real path: the user mount when one exists (from `/proc/self/mountinfo`
  `subvol=`), otherwise the top-level mount.
- Capability `btrfs` (binary `btrfs`, installable package `btrfs-progs` on
  Debian and RHEL) gates the feature in both manifests. Replication to SSH
  additionally needs `ssh` and `ssh-keygen` on PATH; absence is reported by the
  route, not a capability.
- Routes are `storage.btrfs_*` in the storage `Bindings`. Reads are
  `RetrySafe()`; every mutation is `Privileged()`. Long operations
  (balance, device remove/replace, replication run, rollback) are
  `TaskRunner` with typed progress, following `smart_test_operation.go`.
- Config files are root-owned `0600` JSON under `/etc/linuxio/`, written with
  the existing atomic writer: `btrfs-snapshots.json` (timeline rules) and
  `btrfs-replication.json` (jobs).

### Snapshot layout

Snapshots are read-only subvolumes created at the top level under a plain
directory tree, never nested inside the source subvolume:

```
<top-level>/linuxio-snapshots/<source path with '/' → '%2F'>/<YYYYMMDDTHHMMSSZ>-<kind>
```

`kind` ∈ `manual`, `timeline`, `pre-update`, `pre-rollback`. Keeping every
snapshot's parent at ID 5 is what makes rename-based rollback legal (btrfs
refuses to rename a subvolume across subvolume boundaries) and keeps snapshots
out of their own source. The list route derives source, time and kind from the
path; nothing else is stored. Foreign read-only subvolumes elsewhere on the
filesystem show in the subvolume list, not the snapshot list.

### Frontend

- New **Btrfs** tab under Storage (`/storage/btrfs`), fourth entry in
  `storageTabs.ts`, loader-prefetched like `lvm.tsx`.
- Selected filesystem is URL state: validated search param `fs=<uuid>`,
  defaulting to the first filesystem.
- Btrfs tab layout (snapshots only): a status strip (health dot, space bar,
  "Check for errors"), **Snapshots** (flat rows: kind dot, age, what, size
  when quotas are on, Browse/Restore/Delete), **Automatic snapshots** (one
  switch per folder, schedule summary, edit) and a collapsed **Advanced**
  section with **Subvolumes**, **Replication** and **Settings**. The
  filesystem picker only appears with more than one filesystem, and
  distro/container-runtime subvolumes are hidden by default.
- Disks tab (`/storage`) gains a **Storage space** flow above Filesystems:
  disks -> pool -> volumes for each mounted btrfs filesystem (pool = the
  filesystem with its data profile, allocated and free space; volumes = its
  mount points), plus **Pool details** for the selected pool: the **Disks**
  card (add, replace, remove, change RAID profile) and the **Space**
  allocation breakdown. Shown only with the `btrfs` capability and privilege.
  All built from `components/ui` and `--app-*` variables.
- The existing "Create subvolume" field and `BtrfsMaintenanceControls` move
  from `FilesystemCard` into the Btrfs tab's Subvolumes and Settings cards;
  the filesystem card keeps a single "Manage btrfs" link. No half-migrated
  state.
- Server state is TanStack Query via generated `linuxio.storage.btrfs_*`
  options; mutations register in `operation-query-invalidations.ts`.
- The route is gated on the `btrfs` capability with the standard install
  prompt when absent.

## Phases

Each phase is independently shippable and gets its own plan section.

### 1. Inventory and health

`storage.btrfs_list` → `[]BtrfsFilesystem`:

- `uuid`, `label`, `mountpoints[]` (every mount of this fs with its `subvol`),
  `totalBytes`, `unallocatedBytes`, allocation per profile for data/metadata/
  system (`profile`, `totalBytes`, `usedBytes`) from `btrfs filesystem usage -b`,
  `devices[]` (`id`, `path`, `sizeBytes`, `usedBytes`, `missing`) from
  `btrfs filesystem show --raw`, per-device error counters from
  `btrfs device stats`, `scrub` (`status`, `startedAt`, `finishedAt`,
  `errorCount`) from `btrfs scrub status`, `generation`.
- Degraded or missing devices and non-zero error counters raise an alert
  through the existing `alerts` package on list, resolved when clear.

### 2. Subvolumes

- `storage.btrfs_subvolumes {uuid}` → `[]BtrfsSubvolume`: `id`, `path`,
  `parentId`, `uuid`, `parentUuid`, `receivedUuid`, `readOnly`, `createdAt`,
  `mountpoint?`, `isDefault`, `isSnapshot` (lives under `linuxio-snapshots`),
  `referencedBytes?`/`exclusiveBytes?` when quotas are on. Built from
  `btrfs subvolume list -a -p -u -q -R -o?` + `-r` (read-only set) + `-s`
  (otime) + `get-default`. Docker's `/var/lib/docker/btrfs/subvolumes/*`
  are filtered out.
- `storage.btrfs_subvolume_create {uuid, path}` (moved existing),
  `storage.btrfs_subvolume_delete {uuid, path}`,
  `storage.btrfs_subvolume_set_readonly {uuid, path, readOnly}`
  (`btrfs property set -ts`), `storage.btrfs_subvolume_set_default {uuid, path}`.
- Delete refuses: the default subvolume, any mounted subvolume, anything under
  `linuxio-snapshots` (use the snapshot route), and paths that fail the
  existing subvolume name validation.

### 3. Snapshots

- `storage.btrfs_snapshots {uuid, path?}` → `[]BtrfsSnapshot` (`source`,
  `path`, `kind`, `createdAt`, `readOnly`, size fields when quotas are on).
- `storage.btrfs_snapshot_create {uuid, path, kind: manual}` →
  `btrfs subvolume snapshot -r`.
- `storage.btrfs_snapshot_delete {uuid, snapshotPath}`.
- **Browse**: the UI links to the file browser at
  `/run/linuxio/btrfs/<uuid>/linuxio-snapshots/...`. Restoring a single file
  is the file browser's existing copy; no dedicated restore route.
- **Rollback** `storage.btrfs_snapshot_rollback {uuid, snapshotPath}`
  (TaskRunner). Plan is a pure function of (source path, mountinfo, default
  subvolume id) and is unit-tested:
  1. `btrfs subvolume snapshot -r <source> linuxio-snapshots/<src>/<ts>-pre-rollback`
  2. `btrfs subvolume snapshot <snapshot> <source>.linuxio-restore-<ts>`
     (read-write copy into a temporary sibling, before anything is renamed;
     on failure the copy is deleted best effort)
  3. rename `<source>` → `<source>.linuxio-rollback-<ts>` (same parent, so it
     is a legal rename even while mounted)
  4. rename `<source>.linuxio-restore-<ts>` → `<source>`; if this fails the
     old subvolume is renamed back to `<source>` and the error says whether
     that worked
  5. if the old subvolume was the default, `set-default` the new one (by its
     new id)
  6. delete the renamed subvolume **only if it is not mounted**; otherwise it
     is kept as `<source>.linuxio-rollback-<ts>` and shown as a leftover;
     delete it from the Subvolumes card
  7. if the source is mounted and is not `/`: remount each mount explicitly
     (`umount <mp>`, then `mount -t btrfs -o <its options without
     subvol=/subvolid=>,subvol=/<source> UUID=<uuid> <mp>`, so a `subvolid=`
     fstab entry cannot bring the old subvolume back) and confirm with
     findmnt that `<mp>` now shows `<source>`; when busy (`target is busy`) or
     unconfirmed, report "takes effect after reboot"
  8. if the source is `/` (or holds `/usr`, `/var`, `/etc`): result carries
     `rebootRequired: true`; the UI offers the existing `control.reboot`.
  Covers Ubuntu/Fedora (`subvol=` by path) and openSUSE-style (boot by default
  id) in one path. The UI confirmation names the subvolume, the snapshot
  date, and that nested subvolumes are not restored.

### 4. Automatic snapshots

- Config `/etc/linuxio/btrfs-snapshots.json`:
  `{ "rules": [{ "uuid", "path", "enabled", "every": "hourly|daily|weekly",
  "hour": 0-23, "weekday": 0-6, "keep": { "hourly", "daily", "weekly",
  "monthly" }, "beforeUpdates": bool }] }`. `hour` (local time) is when daily
  and weekly rules run and `weekday` (0 = Sunday) picks the weekly day.
- Routes: `storage.btrfs_snapshot_rules` (get), `storage.btrfs_snapshot_rule_set`,
  `storage.btrfs_snapshot_rule_delete`, `storage.btrfs_snapshot_run_now`.
- Execution: `linuxio btrfs-snapshot run` subcommand in `backend/cli` (same
  shape as `linuxio alert`). `linuxio-btrfs-snapshot.timer` (`OnCalendar=hourly`,
  `Persistent=true`) + oneshot service, written through the existing unit
  writer and systemd D-Bus ops used by the Docker auto-update timer. The timer
  is enabled while at least one rule is enabled and disabled when none are.
  Each run: for every enabled rule, take a `timeline` snapshot when due:
  hourly rules an hour after the newest timeline snapshot, daily and weekly
  ones once their `hour`/`weekday` slot has passed with no snapshot since
  (a 5-minute tolerance lets an early timer count); then prune by the keep counts
  (newest N per bucket, snapper's "timeline" algorithm). `manual` snapshots
  are never pruned; `pre-update` and `pre-rollback` keep the last 3.
- Before updates: `updates.apply_offline_updates` and the package auto-update
  runner call the same Go function to take a `pre-update` snapshot for every
  rule with `beforeUpdates` before applying. Distro-driven updates (apt run by
  hand, unattended-upgrades) are not hooked.
- Failures are recorded as alerts (`btrfs/snapshot/<uuid>/<path>`), resolved on
  the next success. journald holds the run log.

### 5. Multi-device

- `storage.btrfs_device_add {uuid, device, force}` → `btrfs device add [-f]`.
  Rejects devices that are mounted, in LVM, or in another btrfs (via the
  existing block-device inventory).
- `storage.btrfs_device_remove {uuid, deviceId}` (TaskRunner; progress from
  `btrfs filesystem usage` polling). Refuses when the result would leave fewer
  devices than the profile needs.
- `storage.btrfs_device_replace {uuid, deviceId, newDevice}` (TaskRunner;
  `btrfs replace start -f`, progress from `btrfs replace status`).
- `storage.btrfs_balance {uuid, dataProfile?, metadataProfile?}` (TaskRunner;
  `btrfs balance start -dconvert= -mconvert=`, progress from
  `btrfs balance status`; cancel → `btrfs balance cancel`). Profiles offered:
  `single`, `dup`, `raid0`, `raid1`, `raid1c3`, `raid1c4`, `raid10`, filtered
  by device count. `raid5`/`raid6` are refused server-side.
- `storage.btrfs_scrub_start / scrub_cancel` as a small bonus beside the
  maintenance timers: on-demand scrub with progress.

### 6. Replication (send/receive)

- Config `/etc/linuxio/btrfs-replication.json`: jobs
  `{ "id", "uuid", "path", "target": { "kind": "local", "path" } |
  { "kind": "ssh", "host", "port", "user", "path" }, "keepOnTarget", "enabled",
  "after": "timeline|manual" }`.
- Routes: list/set/delete job, `storage.btrfs_replication_run {id}`
  (TaskRunner), `storage.btrfs_replication_ssh_key` (returns the public key,
  generating `/etc/linuxio/btrfs-replication/id_ed25519` on first call),
  `storage.btrfs_replication_ssh_trust {host, port}` (runs `ssh-keyscan`,
  returns fingerprints; a second call with `confirm: true` writes
  `/etc/linuxio/btrfs-replication/known_hosts`).
- Run: find the newest snapshot of the source not yet on the target; choose a
  parent as the newest snapshot present on both sides (matched by
  `uuid`/`received_uuid` from `btrfs subvolume list -a -u -R` on each side);
  `btrfs send [-p parent] <snap>` piped in Go through a counting writer into
  `btrfs receive <target>` locally or
  `ssh -i key -o UserKnownHostsFile=... -p port user@host btrfs receive <path>`.
  Progress is bytes sent. The remote account must be able to run
  `btrfs receive` (root or a user with that privilege); the UI says so.
  Afterwards prune target snapshots beyond `keepOnTarget` (local delete or
  `ssh ... btrfs subvolume delete`).
- Scheduling: the same `linuxio btrfs-snapshot run` invocation runs enabled
  jobs after taking snapshots, so one timer covers both. Failures are alerts.
- Both target kinds ship in the first version, as decided.

### 7. Quotas and compression

- `storage.btrfs_settings {uuid}` → `{ quotasEnabled, compression: {mountOption,
  fstabPersisted} }`. `storage.btrfs_set_quotas {uuid, enabled}` →
  `btrfs quota enable|disable`; with quotas on, phases 2–3 show referenced and
  exclusive bytes from `btrfs qgroup show --raw`.
- `storage.btrfs_qgroup_limit {uuid, path, maxReferencedBytes|null}` →
  `btrfs qgroup limit`.
- `storage.btrfs_set_compression {uuid, mountpoint, algorithm: none|zstd|lzo|zlib,
  level?}`: `mount -o remount,compress=<...>` and update the fstab options via
  the existing fstab helpers. Per-subvolume override:
  `storage.btrfs_subvolume_set_compression {uuid, path, algorithm}` →
  `btrfs property set compression`. Re-compressing existing data
  (`filesystem defrag -c`) is not offered.

## Data flow

Browser → webserver → bridge `storage.btrfs_*` (root). Reads shell out to
`btrfs` and parse. Mutations validate the request against the current
inventory (UUID exists, path is a subvolume, device is free, profile is
allowed), run the command, and return the refreshed object. Timers run
`linuxio btrfs-snapshot run` as root under systemd with no bridge involved.

## Error handling

- Every subprocess error returns trimmed stderr wrapped with the operation,
  as `lvm.go` does. No panics for host state.
- Destructive routes are idempotent where the command is (`subvolume delete`
  of a missing path is "already gone").
- Rollback is the only multi-step mutation. Each step is logged; a failure
  after step 3 leaves the renamed subvolume in place and the result names it
  so the admin can finish by hand. The pair of renames in steps 3–4 is the
  point of no return and the UI confirmation says so.
- Background runs never block on a lock held by the bridge: the config files
  use the existing `filelock`.

## Testing

- Unit (default `make check-backend-quiet`): output parsers against fixtures
  from btrfs-progs 5.16 and 6.6, snapshot path encode/decode, retention
  pruning, rollback plan, replication parent selection, config round-trip,
  unit-file rendering, CLI argument handling. Fixture-driven, no host access,
  like `btrfs_maintenance_test.go`.
- Host tests (skipped unless root and a loop-device btrfs can be created, same
  gating convention as the other root-requiring tests): create → snapshot →
  rollback → replicate to a second loop filesystem → prune. Not part of the
  quiet targets.
- Frontend: component tests for each card with mocked query data, the
  rollback confirmation, and the search-param validation; `make
  check-frontend-quiet`. Spanning change overall: `make test-quiet` and
  `make generate` after contract changes.

## Documentation

`docs/btrfs.md` (canonical: layout, rollback semantics, replication SSH
setup), a `btrfs` entry in `docs/capabilities.md`, and the alert keys in
`docs/alerts.md`.
