# Btrfs Management Implementation Plan

> **Historical.** This document describes the first, `btrfs-progs`-parsing design. The implementation now uses the kernel's ioctls instead; [docs/btrfs.md](../../btrfs.md) is the current reference.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Full btrfs management from the Storage → Btrfs tab: health, subvolumes, snapshots with rollback, scheduled snapshots, device/RAID changes, send/receive replication, quotas and compression.

**Architecture:** A new `backend/common/btrfs` package wraps `btrfs-progs` (text parsing, pure planners, config files) and is shared by the bridge and the `linuxio` CLI. The bridge `storage` family exposes `storage.btrfs_*` routes as thin adapters; long operations are `TaskRunner`s. One hourly systemd timer runs `linuxio btrfs-snapshot run` for timeline snapshots, pruning and replication. The frontend gets a `/storage/btrfs` route with an `AppGrid` of small cards.

**Tech Stack:** Go (`os/exec`, systemd D-Bus via `handlers/systemd`), btrfs-progs ≥ 5.16, findmnt, ssh/ssh-keygen/ssh-keyscan, React + TanStack Router/Query, generated client via `make generate`.

**Spec:** `docs/superpowers/specs/2026-10-19-btrfs-management-design.md`

## Global Constraints

- Never commit. Stage own files with `git add <explicit paths>`; present a suggested commit message per task.
- Use Make targets only: `make test-go-quiet GO_TEST_PKGS=./common/btrfs/... GO_TEST_FLAGS='-run TestX'` (paths relative to `backend/`), `make generate` after any route/type change, `make test-frontend-only VITEST_FILE=<path>`, final `make test-quiet`.
- `handlers.go` files hold only `var api/Routes` and `handle*`/`RegisterHandlers` funcs (`handler_pattern_test.go`). All subprocesses use `exec.CommandContext(ctx, ...)` with separate args; no `context.Background()`.
- Every mutation route is `apischema.Privileged()`; every read is `apischema.RetrySafe()`.
- Config files: `/etc/linuxio/btrfs-snapshots.json`, `/etc/linuxio/btrfs-replication.json`, `/etc/linuxio/btrfs-replication/{id_ed25519,id_ed25519.pub,known_hosts}`; all written with `utils.WriteFileAtomic(path, data, 0o600)` under `filelock.RunExclusive(ctx, path+".lock", fn, filelock.WithTimeout(10*time.Second))`.
- Snapshot path: `<top>/linuxio-snapshots/<escaped source>/<YYYYMMDDTHHMMSSZ>-<kind>`, `kind ∈ manual|timeline|pre-update|pre-rollback`, source `/` escaped as `%2F`. Top-level mount: `/run/linuxio/btrfs/<uuid>` with `-o subvolid=5,noatime`.
- Allowed balance profiles: `single dup raid0 raid1 raid1c3 raid1c4 raid10`; `raid5`/`raid6` rejected with error text `raid5 and raid6 are not supported`.
- Alert sources: `btrfs/health/<uuid>`, `btrfs/snapshot/<uuid>/<escaped path>`, `btrfs/replication/<job id>`.
- Timer: `linuxio-btrfs-snapshot.timer` (`OnCalendar=hourly`, `Persistent=true`, `WantedBy=timers.target`) and `linuxio-btrfs-snapshot.service` (`Type=oneshot`, `ExecStart=/usr/local/bin/linuxio btrfs-snapshot run`).
- Frontend styling only via `components/ui` and `--app-*` variables; no `useMemo`/`useCallback` outside `*Virtual*` files; mutation invalidation goes in `operation-query-invalidations.ts`.
- Docs reconciled on landing: `docs/btrfs.md`, `docs/capabilities.md`, `docs/alerts.md`.

## Review Focus

1. A subvolume path containing `..`, a leading `/`, or `linuxio-snapshots` as a prefix must be rejected by every mutation route (Task 6 tests `TestValidateSubvolumePathRejects`).
2. Rollback of a subvolume whose mount is busy must leave the renamed subvolume in place and report it, not delete a mounted subvolume (Task 9 tests `TestRollbackPlanBusyMountKeepsOld`).
3. The hourly run with a rule for a filesystem that is not mounted (disk unplugged) must skip that rule and raise one alert, not fail the whole run (Task 12 tests `TestRunSkipsMissingFilesystem`).
4. Replication parent selection must never pass a parent that only exists locally; with no common parent it must do a full send (Task 17 tests `TestChooseParentNoCommon`).
5. Compression mount-option changes must preserve every other fstab option (`subvol=@`, `noatime`) (Task 20 tests `TestSetCompressionKeepsOtherOptions`).

---

## Foundation

### Task 1: `common/btrfs` command runner and mounts

**Files:**
- Create: `backend/common/btrfs/cmd.go`, `backend/common/btrfs/mounts.go`, `backend/common/btrfs/mounts_test.go`, `backend/common/btrfs/testdata/findmnt.json`

**Interfaces:**
- Produces: `func run(ctx context.Context, args ...string) (string, error)` — runs `btrfs` (LookPath with `/usr/sbin:/sbin` fallback like `shares.findServerCommand`), returns trimmed stdout, error wraps trimmed stderr as `btrfs <args[0]> <args[1]>: <stderr>`.
- Produces: `var execCommand = exec.CommandContext` (package var so tests can swap).
- Produces: `type Mount struct { Mountpoint, Device, UUID, Subvol string; SubvolID int; Options []string }`; `func ListMounts(ctx) ([]Mount, error)` via `findmnt --json --bytes -t btrfs -o TARGET,SOURCE,UUID,OPTIONS`; `func parseFindmnt(data []byte) ([]Mount, error)`.
- Produces: `func TopLevel(ctx, uuid string) (string, error)` — returns an existing mount with `SubvolID == 5`, else mounts `UUID=<uuid>` at `/run/linuxio/btrfs/<uuid>` (`mount -t btrfs -o subvolid=5,noatime`) and returns that path; `var TopLevelDir = "/run/linuxio/btrfs"`.
- Produces: `func MountFor(ctx, uuid, subvolPath string) (dir string, err error)` — a user mount of exactly that subvol if present, else `filepath.Join(TopLevel(...), subvolPath)`.

- [ ] **Step 1: Write failing tests** in `mounts_test.go`: `TestParseFindmnt` (fixture has `/` with `subvol=/@,subvolid=256` and `/data` with `subvolid=5`; assert both parsed, `Subvol == "@"` with leading `/` stripped, `SubvolID` ints, UUID equal). `TestMountForPrefersUserMount` using a stubbed `listMounts` var.
- [ ] **Step 2: Run** `make test-go-quiet GO_TEST_PKGS=./common/btrfs/... GO_TEST_FLAGS='-run TestParseFindmnt'` → FAIL (package missing).
- [ ] **Step 3: Implement** `cmd.go` and `mounts.go` per Interfaces. `listMounts` is a package var defaulting to the findmnt path so `TopLevel`/`MountFor` are testable.
- [ ] **Step 4: Run** the same target → PASS.
- [ ] **Step 5: Stage** `git add backend/common/btrfs`. Suggested commit: `feat(btrfs): add btrfs command runner and mount discovery`.

### Task 2: `btrfs` capability

**Files:**
- Modify: `backend/bridge/handlers/system/capabilities.go` (registry + `setCapabilityField`), `backend/common/session/session.go` (`BtrfsAvailable bool json:"btrfs_available"`, `BtrfsError *string json:"btrfs_error,omitempty"`), `frontend/src/api/capabilities.ts`, `docs/capabilities.md`
- Test: `backend/bridge/handlers/system/capabilities_test.go` (existing anti-drift tests cover it)

- [ ] **Step 1: Add registry entry** `{Name: "btrfs", LogName: "btrfs-progs", Detect: checkedCapability(checkDependencyCommand("btrfs", "btrfs-progs")), Install: &InstallSpec{PackageDebian: "btrfs-progs", PackageRHEL: "btrfs-progs"}}` and the `case "btrfs":` in `setCapabilityField`; add session fields.
- [ ] **Step 2: Run** `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/system/...` → PASS (`TestCapabilityRegistryCoversWireFields`).
- [ ] **Step 3: Run** `make generate`.
- [ ] **Step 4: Add `CapabilityDef`** `{wire: "btrfs", state: "btrfsAvailable", label: "Btrfs tools", description: "Subvolumes, snapshots, devices and replication for btrfs filesystems", readyText: "btrfs-progs is installed.", dependency: "btrfs-progs", icon: "mdi:file-tree-outline", reasonUnknown: "btrfs-progs availability is still being checked.", reasonUnavailable: "btrfs-progs is not installed.", installable: {requiresPackageKit: true}, route: {label: "Open Btrfs", to: "/storage/btrfs"}}`. Add a "Btrfs tools" section to `docs/capabilities.md` beside "Btrfs maintenance".
- [ ] **Step 5: Run** `make tsc-only` → PASS. Stage the five files. Suggested commit: `feat(capabilities): add btrfs-progs capability`.

## Phase 1 — Inventory and health

### Task 3: Filesystem inventory

**Files:**
- Create: `backend/common/btrfs/inventory.go`, `backend/common/btrfs/inventory_test.go`, `backend/common/btrfs/testdata/{fi_usage_5.16.txt,fi_usage_6.6.txt,fi_show.txt,device_stats.txt,scrub_status.txt}`
- Modify: `backend/bridge/handlers/storage/handlers.go` (binding + `handleBtrfsList`), `backend/bridge/apischema/contracts.go` (`UUIDRequest{UUID string json:"uuid"}`)
- Create: `backend/bridge/handlers/storage/btrfs_adapters.go` (alert raising)

**Interfaces:**
- Produces types (all `json` camelCase):
  `BtrfsFilesystem{UUID, Label string; Mountpoints []Mount; TotalBytes, UnallocatedBytes, UsedBytes uint64; Allocation []BtrfsAllocation; Devices []BtrfsDevice; Scrub BtrfsScrubStatus; Generation uint64; Degraded bool}`
  `BtrfsAllocation{Kind string /*data|metadata|system*/; Profile string; TotalBytes, UsedBytes uint64}`
  `BtrfsDevice{ID int; Path string; SizeBytes, UsedBytes uint64; Missing bool; Errors BtrfsDeviceErrors}`
  `BtrfsDeviceErrors{Write, Read, Flush, Corruption, Generation uint64}`
  `BtrfsScrubStatus{Status string /*none|running|finished|aborted|canceled*/; StartedAt, FinishedAt *time.Time; ErrorCount uint64; BytesScrubbed uint64; Percent float64}`
- Produces: `func ListFilesystems(ctx) ([]BtrfsFilesystem, error)` (one `findmnt` + per-uuid `filesystem usage -b`, `filesystem show --raw`, `device stats`, `scrub status`, all run against the first mountpoint).
- Produces pure parsers: `parseFilesystemUsage(string) (total, unalloc, used uint64, alloc []BtrfsAllocation, err)`, `parseFilesystemShow(string) (label string, devices []BtrfsDevice, missing bool, err)`, `parseDeviceStats(string) (map[string]BtrfsDeviceErrors, error)` keyed by device path, `parseScrubStatus(string) (BtrfsScrubStatus, error)`.
- Route: `storage.btrfs_list` → `[]btrfs.BtrfsFilesystem`, `RetrySafe()`.
- Adapter: `raiseBtrfsHealthAlerts(ctx, fs)` raises `btrfs/health/<uuid>` (severity error on missing device or any error counter > 0, title `Btrfs filesystem <label> needs attention`, link `/storage/btrfs?fs=<uuid>`, OccurrenceID = joined counters) and resolves otherwise.

- [ ] **Step 1: Write failing tests**: one per parser against both btrfs-progs fixtures (`TestParseFilesystemUsage516`, `...66`, `TestParseFilesystemShowMissingDevice`, `TestParseDeviceStats`, `TestParseScrubStatusFinished`/`Running`). Assert exact byte values from the fixtures and profile names lower-cased (`raid1`, `single`, `dup`).
- [ ] **Step 2: Run** `make test-go-quiet GO_TEST_PKGS=./common/btrfs/...` → FAIL.
- [ ] **Step 3: Implement** parsers and `ListFilesystems`; add the route, handler and alert adapter.
- [ ] **Step 4: Run** the package tests and `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/...` → PASS. `make generate`.
- [ ] **Step 5: Stage** files. Suggested commit: `feat(btrfs): list filesystems with allocation, devices, errors and scrub state`.

### Task 4: Btrfs tab skeleton

**Files:**
- Create: `frontend/src/routes/_authenticated/storage/btrfs.tsx`, `frontend/src/routes/_authenticated/storage/-components/btrfs/BtrfsPage.tsx`, `frontend/src/routes/_authenticated/storage/-components/btrfs/BtrfsPage.test.tsx`
- Modify: `frontend/src/routes/_authenticated/storage/-components/storageTabs.ts` (add `{ label: "Btrfs", to: "/storage/btrfs" }` after LVM)

**Interfaces:**
- Produces: `BtrfsPage({ selectedUuid?: string, onSelect(uuid: string): void })` renders the capability gate (same shape as `VMPage.tsx:119-131`), an `AppSelect` of filesystems from `linuxio.storage.btrfs_list` (label `<label or uuid> · <first mountpoint>`), and an `AppGrid container spacing={2}` into which later tasks drop cards. Uses the generated `linuxio.storage.btrfs_list` from Task 3.
- Route: `validateSearch: (search) => optionalString(search, "fs")`, `beforeLoad: requireAccess({requiredCapabilities: ["btrfsAvailable"]}, context)`, loader prefetches `linuxio.storage.btrfs_list`, `onSelect` navigates with `search: { fs }`.

- [ ] **Step 1: Write failing test** `BtrfsPage.test.tsx`: mock `@/api` like `VMDisksPage.test.tsx`, `btrfs_list` resolving two filesystems; assert the select shows both and that selecting the second calls `onSelect` with its uuid; with `capabilities: { btrfsAvailable: false }` assert the alert text "btrfs-progs is not installed." renders.
- [ ] **Step 2: Run** `make test-frontend-only VITEST_FILE=src/routes/_authenticated/storage/-components/btrfs/BtrfsPage.test.tsx` → FAIL.
- [ ] **Step 3: Implement** route, page and tab entry.
- [ ] **Step 4: Run** the test → PASS; `make tsc-only` → PASS.
- [ ] **Step 5: Stage** the four files. Suggested commit: `feat(storage): add Btrfs tab skeleton`.

### Task 5: Health, Devices and Space cards

**Files:**
- Create: `.../storage/-components/btrfs/BtrfsHealthCard.tsx`, `BtrfsDevicesCard.tsx`, `BtrfsSpaceCard.tsx`, `BtrfsCards.test.tsx`
- Modify: `BtrfsPage.tsx` (render the three cards for the selected filesystem)

**Interfaces:**
- Each card takes `{ filesystem: BtrfsFilesystem }`. Health: `AppChip` status (Healthy / Degraded / Errors), scrub line (`Last scrub: <date>, <n> errors` or `Never scrubbed`), generation. Devices: `AppVirtualTable` (`variant="embedded"`, `density="compact"`, columns Path, Size, Used, Errors; row id = device path; missing → chip "Missing"). Space: one `AppLinearProgress` per allocation kind with `<profile> · used/total`, plus an "Unallocated" row.

- [ ] **Step 1: Write failing tests**: render each card with a fixture filesystem; assert "Degraded" chip when `Degraded`, error count text, unallocated bytes via `formatFileSize`.
- [ ] **Step 2: Run** `make test-frontend-only VITEST_FILE=...BtrfsCards.test.tsx` → FAIL.
- [ ] **Step 3: Implement** cards and mount them in `BtrfsPage`.
- [ ] **Step 4: Run** the test and `make tsc-only` → PASS.
- [ ] **Step 5: Stage**. Suggested commit: `feat(storage): show btrfs health, devices and space`.

## Phase 2 — Subvolumes

### Task 6: Subvolume operations

**Files:**
- Create: `backend/common/btrfs/subvolumes.go`, `subvolumes_test.go`, `testdata/subvol_list.txt`, `testdata/subvol_list_ro.txt`, `testdata/subvol_list_snap.txt`
- Modify: `backend/bridge/handlers/storage/handlers.go`, `backend/bridge/apischema/contracts.go` (`BtrfsSubvolumeRequest{UUID, Path string}`, `BtrfsSubvolumeReadOnlyRequest{UUID, Path string; ReadOnly bool}`)
- Modify: `backend/bridge/handlers/storage/filesystems.go` (delete `CreateBtrfsSubvolume`), `handlers.go` (delete `storage.create_btrfs_subvolume` binding and `handleCreateBtrfsSubvolume`)

**Interfaces:**
- `BtrfsSubvolume{ID, ParentID int; Path, UUID, ParentUUID, ReceivedUUID string; ReadOnly, IsDefault, IsSnapshot bool; CreatedAt *time.Time; Mountpoint *string; ReferencedBytes, ExclusiveBytes *uint64}`
- `func ValidateSubvolumePath(p string) error` — rejects empty, absolute, `.`/`..` segments, segments containing NUL, and any path whose first segment is `linuxio-snapshots` (error `path is reserved for snapshots`); allows `/` inside.
- `func ListSubvolumes(ctx, uuid string) ([]BtrfsSubvolume, error)` — `subvolume list -a -p -u -q -R <top>` + `-a -r` + `-a -s` + `subvolume get-default <top>`; filters paths starting with `var/lib/docker/btrfs/subvolumes/` or containing `/var/lib/docker/btrfs/subvolumes/`; `parseSubvolumeList(string) ([]subvolumeRow, error)` strips the `<FS_TREE>/` prefix.
- `CreateSubvolume(ctx, uuid, path) (BtrfsSubvolume, error)`, `DeleteSubvolume(ctx, uuid, path) error` (refuses default, mounted, snapshot tree), `SetSubvolumeReadOnly(ctx, uuid, path string, ro bool) (BtrfsSubvolume, error)` via `property set -ts <dir> ro true|false`, `SetDefaultSubvolume(ctx, uuid, path) error`.
- Routes: `storage.btrfs_subvolumes` (UUIDRequest → `[]BtrfsSubvolume`), `storage.btrfs_subvolume_create` (→ `BtrfsSubvolume`), `storage.btrfs_subvolume_delete` (→ `SuccessResponse`), `storage.btrfs_subvolume_set_readonly`, `storage.btrfs_subvolume_set_default`.

- [ ] **Step 1: Write failing tests**: `TestParseSubvolumeList` (IDs, parent, uuids, `<FS_TREE>` stripped), `TestListSubvolumesMarksReadOnlyAndSnapshots` (stub `run` via `execCommand` returning fixtures per args), `TestValidateSubvolumePathRejects` (table: `""`, `/@`, `a/../b`, `linuxio-snapshots/x`, `.`), `TestDeleteSubvolumeRefusesDefault`.
- [ ] **Step 2: Run** `make test-go-quiet GO_TEST_PKGS=./common/btrfs/...` → FAIL.
- [ ] **Step 3: Implement**; remove the old create route and `CreateBtrfsSubvolume`; add routes.
- [ ] **Step 4: Run** package + handler tests → PASS; `make generate`.
- [ ] **Step 5: Stage**. Suggested commit: `feat(btrfs): list, create, delete and flag subvolumes`.

### Task 7: Subvolumes card and Disks-tab cleanup

**Files:**
- Create: `.../btrfs/BtrfsSubvolumesCard.tsx`, `BtrfsSubvolumesCard.test.tsx`
- Modify: `BtrfsPage.tsx`, `frontend/src/components/cards/FilesystemCard.tsx` (remove subvolume props/block lines 230-260 and 24-25, 35-43, 276-286; remove `BtrfsMaintenanceControls` import and block 262-267; add `<Link to="/storage/btrfs" search={{fs: ...}}>Manage btrfs</Link>` — needs `uuid`; `FilesystemInfo` has none, so link to `/storage/btrfs` without `fs` and let the page default), `.../DiskOverview/index.tsx` (remove lines 319-329, 349-360, 376-395, 490-510, props 652-661/667-682 and the `creatingSubvolumeMountpoint` uses at 398/414/420/458/485), `frontend/src/api/operation-query-invalidations.ts` (remove `storage.create_btrfs_subvolume`; add `storage.btrfs_subvolume_create/delete/set_readonly/set_default: [endpointQueryPrefix("storage.btrfs_subvolumes"), endpointQueryPrefix("storage.get_topology")]`), `operation-query-invalidations.test.ts` (swap `create_btrfs_subvolume` for `btrfs_subvolume_create` in the topology list), `components/cards/BtrfsMaintenanceControls.tsx` moves unchanged into the Settings card in Task 21 — for now render it inside `BtrfsPage` below the grid with the `useCapability("btrfsMaintenanceAvailable")` + `get_btrfs_maintenance` wiring moved from DiskOverview.

**Interfaces:**
- `BtrfsSubvolumesCard({ uuid: string })`: `useSuspenseQuery(linuxio.storage.btrfs_subvolumes({uuid}))`, table columns Path, ID, Mounted at, Read-only (switch), Default (chip), Size (when quota fields present); row actions: Snapshot (calls `linuxio.storage.btrfs_snapshot_create` — wired in Task 10; stub prop `onSnapshot?` until then), Set default, Delete (confirm `GeneralDialog`). Header action "Create subvolume" opens a dialog with `AppTextField` label "Subvolume path".

- [ ] **Step 1: Write failing test**: mocks `btrfs_subvolumes` → two rows; assert delete button disabled on the default row; create dialog submits `{uuid, path: "data/photos"}`.
- [ ] **Step 2: Run** the test → FAIL.
- [ ] **Step 3: Implement** the card, remove the Disks-tab plumbing, update invalidations.
- [ ] **Step 4: Run** `make test-frontend-only VITEST_FILE=src/routes/_authenticated/storage` and `VITEST_FILE=src/components/cards` and `VITEST_FILE=src/api/operation-query-invalidations.test.ts` → PASS; `make tsc-only` → PASS.
- [ ] **Step 5: Stage**. Suggested commit: `feat(storage): manage btrfs subvolumes from the Btrfs tab`.

## Phase 3 — Snapshots

### Task 8: Snapshot layout and CRUD

**Files:**
- Create: `backend/common/btrfs/snapshots.go`, `snapshots_test.go`
- Modify: `handlers.go`, `contracts.go` (`BtrfsSnapshotsRequest{UUID string; Path *string}`, `BtrfsSnapshotRequest{UUID, SnapshotPath string}`)

**Interfaces:**
- `const SnapshotRoot = "linuxio-snapshots"`; `type SnapshotKind string` with `KindManual = "manual"`, `KindTimeline = "timeline"`, `KindPreUpdate = "pre-update"`, `KindPreRollback = "pre-rollback"`.
- `BtrfsSnapshot{Source, Path string; Kind SnapshotKind; CreatedAt time.Time; ReadOnly bool; ID int; UUID string; ReferencedBytes, ExclusiveBytes *uint64}`
- Pure: `EscapeSource(path string) string` (`/`→`%2F`), `UnescapeSource`, `SnapshotPath(source string, at time.Time, kind SnapshotKind) string`, `ParseSnapshotPath(p string) (source string, at time.Time, kind SnapshotKind, ok bool)`.
- `CreateSnapshot(ctx, uuid, source string, kind SnapshotKind, now time.Time) (BtrfsSnapshot, error)` — `MkdirAll(<top>/linuxio-snapshots/<escaped>)`, `subvolume snapshot -r <MountFor(source)> <dest>`.
- `ListSnapshots(ctx, uuid string, source *string) ([]BtrfsSnapshot, error)` — from `ListSubvolumes` rows under `SnapshotRoot`, sorted newest first.
- `DeleteSnapshot(ctx, uuid, snapshotPath string) error` — path must parse with `ParseSnapshotPath`; `subvolume delete`; missing path returns nil.
- Routes: `storage.btrfs_snapshots`, `storage.btrfs_snapshot_create` (`BtrfsSubvolumeRequest` → `BtrfsSnapshot`, kind manual), `storage.btrfs_snapshot_delete` (→ `SuccessResponse`).

- [ ] **Step 1: Write failing tests**: `TestSnapshotPathRoundTrip` (`"@"`, `"data/photos"`, `"@"` with all kinds; timestamp `20261019T101500Z`), `TestParseSnapshotPathRejectsForeign` (`"foo/bar"`, wrong timestamp), `TestDeleteSnapshotRefusesNonSnapshotPath`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** package + handlers → PASS; `make generate`.
- [ ] **Step 5: Stage**. Suggested commit: `feat(btrfs): create, list and delete read-only snapshots`.

### Task 9: Rollback planner and task

**Files:**
- Create: `backend/common/btrfs/rollback.go`, `rollback_test.go`, `backend/bridge/handlers/storage/btrfs_rollback_operation.go`
- Modify: `handlers.go` (task binding), `backend/bridge/handlers/storage/handlers.go` `Routes` via `CombineRoutes`

**Interfaces:**
- `type RollbackStep struct { Op string /*snapshot-ro|rename|snapshot-rw|set-default|delete|remount*/; From, To string }`
- `type RollbackPlan struct { Steps []RollbackStep; RebootRequired bool; KeepOld bool; OldPath string }`
- Pure: `PlanRollback(source, snapshotPath string, mounts []Mount, defaultID, sourceID int, now time.Time) (RollbackPlan, error)` — rules from spec §3: always snapshot-ro + rename to `<source>.linuxio-rollback-<ts>` + snapshot-rw; set-default when `defaultID == sourceID`; `RebootRequired` when any mount of `source` is `/`, `/usr`, `/var`, `/etc`; `KeepOld` when source is mounted anywhere (delete only otherwise); remount when mounted and not reboot-required.
- `ExecuteRollback(ctx, uuid string, plan RollbackPlan, report func(step int, msg string)) (BtrfsRollbackResult, error)`; `BtrfsRollbackResult{RebootRequired bool; LeftoverPath *string; Remounted bool}`; remount = `umount <mp>` then `mount <mp>`; on `EBUSY` set `Remounted=false` and keep going.
- `BtrfsRollbackProgress{Step int; Total int; Message string}` with `ProgressEnvelope()` (Percentage = Step/Total*100, Phase = "rollback").
- Route: `storage.btrfs_snapshot_rollback` `TaskRunner[apischema.BtrfsSnapshotRequest, btrfs.BtrfsRollbackResult]`, `Privileged()`, `SessionTask()`, `WithTaskProgress[BtrfsRollbackProgress]`, `WithTaskMetadata(Identity: [uuid, snapshotPath], Label: "Rolling back btrfs subvolume")`.

- [ ] **Step 1: Write failing tests**: `TestRollbackPlanRootRequiresReboot` (`@` mounted at `/`, default 256 == source → steps include set-default, `RebootRequired`, `KeepOld`), `TestRollbackPlanDataRemounts` (`data` at `/srv/data`, default 5 → no set-default, remount step), `TestRollbackPlanUnmountedDeletesOld`, `TestRollbackPlanBusyMountKeepsOld` (execute with a stubbed `umount` failing EBUSY → `LeftoverPath` set, no delete), `TestRollbackPlanRejectsForeignSnapshot`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS; `make generate`.
- [ ] **Step 5: Stage**. Suggested commit: `feat(btrfs): roll back subvolumes to snapshots`.

### Task 10: Snapshots card and rollback dialog

**Files:**
- Create: `.../btrfs/BtrfsSnapshotsCard.tsx`, `BtrfsRollbackDialog.tsx`, `BtrfsSnapshotsCard.test.tsx`
- Modify: `BtrfsPage.tsx`, `BtrfsSubvolumesCard.tsx` (Snapshot action → `btrfs_snapshot_create`), `operation-query-invalidations.ts` (+ `storage.btrfs_snapshot_create/delete/rollback` → `btrfs_snapshots`, `btrfs_subvolumes`, `get_topology`), `frontend/src/constants/backgroundTaskTypes.ts` (`TASK_TYPE_STORAGE_BTRFS_ROLLBACK = "storage.btrfs_snapshot_rollback"`)

**Interfaces:**
- `BtrfsSnapshotsCard({ uuid, filesystem })`: `AppSelect` source filter (All / each subvolume), table columns Source, Taken, Kind (chip), Size?; row actions Browse (navigate `/filebrowser/$` with `_splat = "run/linuxio/btrfs/<uuid>/linuxio-snapshots/<escaped>/<name>"`), Roll back, Delete.
- `BtrfsRollbackDialog({ snapshot, onConfirm })`: copy — title "Roll back <source>?", body "Replaces the current contents of <source> with the snapshot taken <date>. Nested subvolumes are not restored. A copy of the current state is kept as a pre-rollback snapshot." Uses `linuxio.storage.btrfs_snapshot_rollback.useTaskStreamAction` with `onProgress` → `AppLinearProgress`; on result `rebootRequired` show `AppAlert` with a "Reboot now" `AppButton` calling `useCallMutation(linuxio.control.reboot)` + `usePowerAction().triggerReboot()`; on `leftoverPath` show it in a warning alert.

- [ ] **Step 1: Write failing test**: list renders kinds; rollback dialog submits `{uuid, snapshotPath}` via `createTaskStreamActionMock`; a result with `rebootRequired: true` shows "Reboot now".
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** test, invalidations test, `make tsc-only` → PASS.
- [ ] **Step 5: Stage**. Suggested commit: `feat(storage): btrfs snapshots card with browse, delete and rollback`.

## Phase 4 — Automatic snapshots

### Task 11: Rules config and retention

**Files:**
- Create: `backend/common/btrfs/rules.go`, `retention.go`, `rules_test.go`, `retention_test.go`

**Interfaces:**
- `BtrfsSnapshotRule{UUID, Path string; Enabled bool; Every string /*hourly|daily|weekly*/; Keep BtrfsKeep; BeforeUpdates bool}`; `BtrfsKeep{Hourly, Daily, Weekly, Monthly int}`; `BtrfsSnapshotRules{Rules []BtrfsSnapshotRule; TimerEnabled bool}`.
- `var RulesPath = "/etc/linuxio/btrfs-snapshots.json"`; `LoadRules() ([]BtrfsSnapshotRule, error)` (missing file → empty), `SaveRules(ctx, []BtrfsSnapshotRule) error`, `ValidateRule(r) error` (every ∈ set, keep ≥ 0, path via `ValidateSubvolumePath`), `UpsertRule`, `DeleteRule(rules, uuid, path)`.
- Pure: `Due(rule, newestTimeline *time.Time, now time.Time) bool` (hourly = 1h, daily = 24h, weekly = 7×24h, with a 5-minute tolerance).
- Pure: `Prune(snaps []BtrfsSnapshot, keep BtrfsKeep, now time.Time) (del []BtrfsSnapshot)` — timeline snapshots only; keep newest N per hour/day/week/month bucket (bucket key = truncated timestamp); a snapshot survives if any bucket keeps it; `pre-update`/`pre-rollback` keep newest 3; `manual` never pruned.

- [ ] **Step 1: Write failing tests**: `TestRulesRoundTrip` (temp `RulesPath`), `TestValidateRuleRejectsBadEvery`, `TestDueHourlyWithTolerance`, `TestPruneKeepsBuckets` (30 hourly snapshots over 3 days, keep {Hourly:24, Daily:7} → exactly the right survivors), `TestPruneNeverDeletesManual`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS.
- [ ] **Step 5: Stage**. Suggested commit: `feat(btrfs): snapshot rules and retention pruning`.

### Task 12: Runner, CLI subcommand and timer

**Files:**
- Create: `backend/common/btrfs/runner.go`, `runner_test.go`, `backend/common/btrfs/unit.go`, `unit_test.go`, `backend/cli/btrfs_snapshot.go`, `backend/cli/btrfs_snapshot_test.go`, `backend/bridge/handlers/storage/btrfs_timer.go`, `btrfs_timer_test.go`
- Modify: `backend/cli/main.go` (`case "btrfs-snapshot": return runBtrfsSnapshot(ctx, cmdArgs)` + usage line), `handlers.go`, `contracts.go` (`BtrfsSnapshotRuleRequest{Rule btrfs.BtrfsSnapshotRule}`)

**Interfaces:**
- `type RunReport struct { Taken []BtrfsSnapshot; Pruned []BtrfsSnapshot; Skipped []string; Errors []string }`
- `func Run(ctx, rules []BtrfsSnapshotRule, now time.Time, kind SnapshotKind) (RunReport, error)` — for each enabled rule: filesystem absent from `ListMounts` → append to `Skipped`, `alerts.Raise(btrfs/snapshot/<uuid>/<escaped>, warning, "Snapshot skipped: filesystem not mounted")`; else create when `Due` (or always when `kind == pre-update`), prune, `alerts.Resolve` on success; per-rule errors are collected, not fatal.
- `func RenderUnit() []byte`, `func RenderTimer() []byte` (exact unit text from Global Constraints).
- Bridge: `btrfsTimerOps` struct of funcs mirroring `containerUpdateSystemdOps`; `syncBtrfsTimer(ctx, ops, rules) error` writes both unit files to `/etc/systemd/system/`, daemon-reload, enable+start when any rule enabled else stop+disable; `btrfsTimerEnabled(ctx, ops) (bool, error)`.
- Routes: `storage.btrfs_snapshot_rules` (→ `BtrfsSnapshotRules`), `storage.btrfs_snapshot_rule_set` (→ `BtrfsSnapshotRules`), `storage.btrfs_snapshot_rule_delete` (`BtrfsSubvolumeRequest` → `BtrfsSnapshotRules`), `storage.btrfs_snapshot_run_now` (`NoRequest` → `RunReport`).
- CLI: `linuxio btrfs-snapshot run [--now RFC3339]` → `btrfs.Run(ctx, rules, now, timeline)` then `btrfs.RunReplication(...)` (Task 18; stub call until then), exit 1 on any `Errors`; `linuxio btrfs-snapshot pre-update` → `Run(..., pre-update)` for rules with `BeforeUpdates`.

- [ ] **Step 1: Write failing tests**: `TestRunSkipsMissingFilesystem`, `TestRunTakesDueOnly` (stubbed `execCommand` recording args), `TestRenderTimerHourlyPersistent`, `TestSyncBtrfsTimerEnablesWhenRuleEnabled`/`DisablesWhenNone` (recording ops like `recordingContainerUpdateOps`), `TestRunBtrfsSnapshotUsage` (unknown subcommand exits 2).
- [ ] **Step 2: Run** `make test-go-quiet GO_TEST_PKGS='./common/btrfs/... ./cli/... ./bridge/handlers/storage/...'` → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS; `make generate`.
- [ ] **Step 5: Stage**. Suggested commit: `feat(btrfs): hourly timeline snapshots via linuxio btrfs-snapshot timer`.

### Task 13: Pre-update snapshots

**Files:**
- Modify: `backend/bridge/handlers/packages/updates.go` (`applyOfflineUpdates`), `backend/bridge/handlers/packages/package_update_operation.go` (`runPackageUpdateTask` before `updatePackagesWithProgress`), `backend/bridge/handlers/packages/internal/autoupdate/notify.go` (new drop-in `ExecStartPre=/usr/local/bin/linuxio btrfs-snapshot pre-update` written by `writeNotifyDropIn` into the same `linuxio-alert.conf` body)
- Test: `backend/bridge/handlers/packages/internal/autoupdate/notify_test.go` (extend), `backend/bridge/handlers/packages/pre_update_snapshot_test.go`

**Interfaces:**
- `func preUpdateSnapshot(ctx) error` in packages: loads rules, calls `btrfs.Run(ctx, rules, time.Now(), btrfs.KindPreUpdate)`; returns nil when no rule has `BeforeUpdates`; errors are logged and reported as task progress message, never block the update.

- [ ] **Step 1: Write failing tests**: drop-in body contains the `ExecStartPre` line before `ExecStopPost`; `preUpdateSnapshot` with no rules returns nil without running `btrfs`.
- [ ] **Step 2: Run** `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/packages/...` → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS.
- [ ] **Step 5: Stage**. Suggested commit: `feat(updates): take btrfs pre-update snapshots`.

### Task 14: Automatic snapshots card

**Files:**
- Create: `.../btrfs/BtrfsSnapshotRulesCard.tsx`, `BtrfsSnapshotRulesCard.test.tsx`
- Modify: `BtrfsPage.tsx`, `operation-query-invalidations.ts` (`btrfs_snapshot_rule_set/delete/run_now` → `btrfs_snapshot_rules`, `btrfs_snapshots`)

**Interfaces:**
- `BtrfsSnapshotRulesCard({ uuid, subvolumes })`: one row per subvolume of this filesystem showing its rule (or "Off"); edit dialog with `AppSelect` Every (Hourly/Daily/Weekly), four `AppTextField type="number"` Keep fields (defaults 24/7/4/6), `AppSwitch` "Snapshot before updates"; header shows "Timer enabled/disabled" chip and "Run now" button.

- [ ] **Step 1: Write failing test**: dialog submits `{rule: {uuid, path: "@", enabled: true, every: "daily", keep: {hourly: 24, daily: 7, weekly: 4, monthly: 6}, beforeUpdates: true}}`.
- [ ] **Step 2–4:** FAIL → implement → PASS + `make tsc-only`.
- [ ] **Step 5: Stage**. Suggested commit: `feat(storage): schedule btrfs timeline snapshots`.

## Phase 5 — Multi-device

### Task 15: Device, balance and scrub operations

**Files:**
- Create: `backend/common/btrfs/devices.go`, `devices_test.go`, `testdata/{balance_status.txt,replace_status.txt}`, `backend/bridge/handlers/storage/btrfs_device_operations.go`
- Modify: `handlers.go`, `contracts.go` (`BtrfsDeviceAddRequest{UUID, Device string; Force bool}`, `BtrfsDeviceRequest{UUID string; DeviceID int}`, `BtrfsDeviceReplaceRequest{UUID string; DeviceID int; NewDevice string}`, `BtrfsBalanceRequest{UUID string; DataProfile, MetadataProfile *string}`)

**Interfaces:**
- `var AllowedProfiles = []string{"single","dup","raid0","raid1","raid1c3","raid1c4","raid10"}`; `MinDevices(profile) int` (single/dup 1, raid0/raid1 2, raid1c3 3, raid1c4/raid10 4); `ValidateProfiles(data, meta *string, deviceCount int) error` (raid5/6 → `raid5 and raid6 are not supported`).
- `AddDevice(ctx, uuid, device string, force bool) error` (device must appear in `storage.ListBlockDevices` with empty `Mountpoints` and `FSType` empty unless `force`; this check lives in the bridge operation file since `ListBlockDevices` is bridge-side), `RemoveDevice(ctx, uuid string, id int) error` (refuse when `len(devices)-1 < MinDevices(current data profile)`), `ReplaceStart(ctx, uuid string, id int, newDevice string) error`, `BalanceStart(ctx, uuid string, data, meta *string) error`, `BalanceCancel`, `ScrubStart`, `ScrubCancel`; parsers `parseBalanceStatus(string) (running bool, percent float64, err)`, `parseReplaceStatus(string) (running bool, percent float64, err)`.
- Tasks (all `Privileged()`, `SessionTask()`, progress `BtrfsOpProgress{Op string; Percent float64; Message string}` polled every 2 s): `storage.btrfs_device_remove`, `storage.btrfs_device_replace`, `storage.btrfs_balance`, `storage.btrfs_scrub_start`; results `BtrfsFilesystem` (refreshed). Calls: `storage.btrfs_device_add` (→ `BtrfsFilesystem`), `storage.btrfs_balance_cancel`, `storage.btrfs_scrub_cancel` (→ `SuccessResponse`).

- [ ] **Step 1: Write failing tests**: `TestValidateProfilesRejectsRaid56`, `TestValidateProfilesNeedsDevices` (raid10 with 2 devices), `TestRemoveDeviceRefusesBelowMinimum`, `TestParseBalanceStatusRunning`, `TestParseReplaceStatus`.
- [ ] **Step 2–4:** FAIL → implement → PASS; `make generate`.
- [ ] **Step 5: Stage**. Suggested commit: `feat(btrfs): add/remove/replace devices, balance and scrub tasks`.

### Task 16: Devices card actions and balance dialog

**Files:**
- Modify: `BtrfsDevicesCard.tsx`, `BtrfsCards.test.tsx`; Create: `BtrfsBalanceDialog.tsx`, `BtrfsAddDeviceDialog.tsx`; Modify `operation-query-invalidations.ts` (device/balance/scrub routes → `btrfs_list`, `get_topology`, `get_drive_info`)

**Interfaces:**
- Devices card header actions: Add device (dialog listing free devices from `linuxio.storage.get_topology` devices with no mountpoints/fstype; "Force" `AppSwitch`), Change profile (balance dialog: two `AppSelect`s filtered by `MinDevices` ≤ device count, raid5/6 absent), Scrub now; row actions Remove, Replace (device picker). Running task progress via `useTaskStreamAction` shown as `AppLinearProgress` with Cancel.

- [ ] **Step 1: Write failing test**: balance dialog offers `raid10` only when ≥ 4 devices; submit calls task mutate with `{uuid, dataProfile: "raid1", metadataProfile: "raid1"}`.
- [ ] **Step 2–4:** FAIL → implement → PASS + `make tsc-only`.
- [ ] **Step 5: Stage**. Suggested commit: `feat(storage): btrfs device, profile and scrub actions`.

## Phase 6 — Replication

### Task 17: Replication config, parent selection, SSH key and trust

**Files:**
- Create: `backend/common/btrfs/replication.go`, `replication_test.go`, `backend/common/btrfs/ssh.go`, `ssh_test.go`
- Modify: `handlers.go`, `contracts.go` (`BtrfsReplicationJobRequest{Job btrfs.BtrfsReplicationJob}`, `IDRequest` reuse if present else `BtrfsJobIDRequest{ID string}`, `BtrfsSSHTrustRequest{Host string; Port int; Confirm bool}`)

**Interfaces:**
- `BtrfsReplicationTarget{Kind string /*local|ssh*/; Path, Host, User string; Port int}`; `BtrfsReplicationJob{ID, UUID, Path string; Target BtrfsReplicationTarget; KeepOnTarget int; Enabled bool; After string /*timeline|manual*/; LastRun *time.Time; LastError *string}`
- `var ReplicationPath = "/etc/linuxio/btrfs-replication.json"`, `var ReplicationDir = "/etc/linuxio/btrfs-replication"`; `LoadJobs`, `SaveJobs`, `ValidateJob` (ID = UUIDv4 generated when empty; local path must be on a btrfs mount per `ListMounts`; ssh needs host, user, port 1–65535, absolute path).
- Pure: `ChooseParent(local, remote []BtrfsSnapshot) *BtrfsSnapshot` — newest local snapshot whose `UUID` equals a remote `ReceivedUUID`; nil when none.
- Pure: `PruneTarget(remote []BtrfsSnapshot, keep int) []BtrfsSnapshot`.
- `EnsureSSHKey(ctx) (publicKey string, err error)` — `ssh-keygen -t ed25519 -N "" -C linuxio-btrfs-replication -f <dir>/id_ed25519` when missing; dir `0700`. `ScanHost(ctx, host string, port int) (fingerprints []string, keys []string, err)` via `ssh-keyscan -p <port> -t ed25519,rsa <host>` + `ssh-keygen -lf -`; `TrustHost(ctx, keys []string) error` appends to `known_hosts`.
- `SSHArgs(job) []string` = `ssh -i <key> -o UserKnownHostsFile=<known_hosts> -o StrictHostKeyChecking=yes -o BatchMode=yes -p <port> <user>@<host>`.
- Routes: `storage.btrfs_replication_jobs` (→ `[]BtrfsReplicationJob`), `storage.btrfs_replication_job_set`, `storage.btrfs_replication_job_delete`, `storage.btrfs_replication_ssh_key` (`NoRequest` → `BtrfsSSHKey{PublicKey string}`), `storage.btrfs_replication_ssh_trust` (→ `BtrfsSSHTrust{Fingerprints []string; Trusted bool}`).

- [ ] **Step 1: Write failing tests**: `TestChooseParentNoCommon` (nil), `TestChooseParentNewestCommon`, `TestPruneTargetKeepsNewest`, `TestValidateJobSSHRequiresAbsolutePath`, `TestSSHArgsStrict` (asserts `StrictHostKeyChecking=yes` and `BatchMode=yes`), `TestJobsRoundTrip`.
- [ ] **Step 2–4:** FAIL → implement → PASS; `make generate`.
- [ ] **Step 5: Stage**. Suggested commit: `feat(btrfs): replication jobs, SSH key and host trust`.

### Task 18: Replication run

**Files:**
- Create: `backend/common/btrfs/replication_run.go`, `replication_run_test.go`, `backend/bridge/handlers/storage/btrfs_replication_operation.go`
- Modify: `backend/cli/btrfs_snapshot.go` (call `RunReplication` after `Run`), `handlers.go`

**Interfaces:**
- `ListTargetSnapshots(ctx, job) ([]BtrfsSnapshot, error)` — local: `ListSubvolumes` on the target's filesystem filtered to `job.Target.Path`; ssh: `ssh ... btrfs subvolume list -a -u -R -o <path>` parsed with `parseSubvolumeList`.
- `Replicate(ctx, job, progress func(bytes int64)) (BtrfsReplicationResult, error)` — picks newest local snapshot of `job.Path` with kind `job.After` (manual jobs also accept timeline) not on target; `btrfs send [-p parent] <snap>` stdout → counting writer → stdin of `btrfs receive <dst>` or `ssh ... btrfs receive <dst>`; waits both; stderr of both included on error; prune target per `KeepOnTarget` (`btrfs subvolume delete` locally or over ssh); writes `LastRun`/`LastError` into the job.
- `BtrfsReplicationResult{Sent *BtrfsSnapshot; Parent *BtrfsSnapshot; Bytes int64; Pruned int; UpToDate bool}`.
- `RunReplication(ctx, jobs []BtrfsReplicationJob) []error` — enabled jobs only; alerts `btrfs/replication/<id>` raise on error / resolve on success.
- Task `storage.btrfs_replication_run` (`BtrfsJobIDRequest` → `BtrfsReplicationResult`, progress `BtrfsOpProgress{Op:"send", Message:"<bytes> sent"}`).

- [ ] **Step 1: Write failing tests**: `TestReplicateFullWhenNoParent` and `TestReplicateIncremental` with `execCommand` stubbed to `/bin/sh -c` fakes that echo recorded args (assert `-p <parent dir>` present/absent and that `receive` got the job path), `TestReplicateUpToDate` (nothing to send).
- [ ] **Step 2–4:** FAIL → implement → PASS; `make generate`.
- [ ] **Step 5: Stage**. Suggested commit: `feat(btrfs): incremental send/receive replication to local and SSH targets`.

### Task 19: Replication card

**Files:**
- Create: `.../btrfs/BtrfsReplicationCard.tsx`, `BtrfsReplicationJobDialog.tsx`, `BtrfsReplicationCard.test.tsx`; Modify `BtrfsPage.tsx`, `operation-query-invalidations.ts` (job routes → `btrfs_replication_jobs`; run → also `btrfs_snapshots`)

**Interfaces:**
- Card lists jobs for this filesystem: Source, Target (`local:/mnt/backup` or `user@host:/path`), Last run, status chip; actions Run now (task progress bytes via `formatFileSize`), Edit, Delete.
- Dialog: Source subvolume `AppSelect`; Target kind toggle; local path `AppTextField`; ssh fields host/port/user/path; "Keep on target" number; "Replicate" select (After timeline snapshots / manual only); SSH section shows the public key (from `btrfs_replication_ssh_key`) with copy button and a "Check host key" button → shows fingerprints with "Trust" confirm (`btrfs_replication_ssh_trust` with `confirm: true`).

- [ ] **Step 1: Write failing test**: ssh job submit payload; trust flow calls `btrfs_replication_ssh_trust` twice (confirm false then true).
- [ ] **Step 2–4:** FAIL → implement → PASS + `make tsc-only`.
- [ ] **Step 5: Stage**. Suggested commit: `feat(storage): btrfs replication jobs UI`.

## Phase 7 — Quotas and compression

### Task 20: Quotas and compression backend

**Files:**
- Create: `backend/common/btrfs/settings.go`, `settings_test.go`, `testdata/qgroup_show.txt`
- Modify: `handlers.go`, `contracts.go` (`BtrfsQuotaRequest{UUID string; Enabled bool}`, `BtrfsQgroupLimitRequest{UUID, Path string; MaxReferencedBytes *uint64}`, `BtrfsCompressionRequest{UUID, Mountpoint, Algorithm string; Level *int}`, `BtrfsSubvolumeCompressionRequest{UUID, Path, Algorithm string}`), `backend/bridge/handlers/storage/mounts_common.go` (export nothing; add `replaceFstabOption(options []string, prefix, replacement string) []string` pure helper)

**Interfaces:**
- `BtrfsSettings{UUID string; QuotasEnabled bool; Compression []BtrfsCompressionMount}`; `BtrfsCompressionMount{Mountpoint, Algorithm string; Level *int; FstabPersisted bool}`.
- `GetSettings(ctx, uuid) (BtrfsSettings, error)` — quotas from `qgroup show` exit status (`quotas not enabled` text → false); compression from mount options `compress=` / `compress-force=`.
- `SetQuotas(ctx, uuid string, enabled bool) error`; `QgroupShow(ctx, uuid) (map[string]quotaUsage, error)` with `parseQgroupShow`; `SetQgroupLimit(ctx, uuid, path string, max *uint64) error` (`qgroup limit none` when nil); `ListSubvolumes`/`ListSnapshots` populate size fields from `QgroupShow` when quotas are on.
- `SetCompression(ctx, uuid, mountpoint, algorithm string, level *int) error` — algorithm ∈ `none|zstd|lzo|zlib`; option `compress=zstd:3`; `mount -o remount,<opt> <mountpoint>` then fstab options rewrite via `replaceFstabOption` + `updateFstabEntry` (only when the mountpoint has an fstab entry).
- `SetSubvolumeCompression(ctx, uuid, path, algorithm) error` via `property set <dir> compression <alg|"">`.
- Routes: `storage.btrfs_settings` (UUIDRequest), `storage.btrfs_set_quotas`, `storage.btrfs_qgroup_limit` (→ `BtrfsSubvolume`), `storage.btrfs_set_compression`, `storage.btrfs_subvolume_set_compression` (→ `BtrfsSubvolume`).

- [ ] **Step 1: Write failing tests**: `TestParseQgroupShow`, `TestSetCompressionKeepsOtherOptions` (fstab line `UUID=x / btrfs subvol=@,noatime,compress=zstd:1 0 0` → `subvol=@,noatime,compress=zstd:3`; `none` removes the option), `TestSetCompressionRejectsAlgorithm`.
- [ ] **Step 2–4:** FAIL → implement → PASS; `make generate`.
- [ ] **Step 5: Stage**. Suggested commit: `feat(btrfs): quotas, qgroup limits and compression settings`.

### Task 21: Settings card

**Files:**
- Create: `.../btrfs/BtrfsSettingsCard.tsx`, `BtrfsSettingsCard.test.tsx`; Modify `BtrfsPage.tsx` (move `BtrfsMaintenanceControls` into this card), `BtrfsSubvolumesCard.tsx` (size columns + "Limit" action when quotas on; per-subvolume compression select), `operation-query-invalidations.ts` (settings routes → `btrfs_settings`, `btrfs_subvolumes`, `btrfs_snapshots`)

**Interfaces:**
- Settings card: `AppSwitch` "Quotas" (help text: "Enables per-subvolume usage and limits. Adds some write overhead."), per-mountpoint compression `AppSelect` (Off / zstd / lzo / zlib) + level field for zstd (1–15), persisted chip; `BtrfsMaintenanceControls` section with its capability reason.

- [ ] **Step 1: Write failing test**: toggling quotas calls `{uuid, enabled: true}`; choosing zstd level 3 calls `{uuid, mountpoint: "/", algorithm: "zstd", level: 3}`.
- [ ] **Step 2–4:** FAIL → implement → PASS + `make tsc-only`.
- [ ] **Step 5: Stage**. Suggested commit: `feat(storage): btrfs quotas and compression settings`.

## Wrap-up

### Task 22: Docs and full verification

**Files:**
- Create: `docs/btrfs.md` (layout, top-level mount, rollback semantics and the busy-mount leftover, timer and retention, pre-update hook, replication SSH setup incl. remote account needs `btrfs receive` rights, refused features)
- Modify: `docs/alerts.md` (three new sources), `docs/capabilities.md` (verify Task 2 section), `docs/README.md` index if it lists docs, `docs/TODO/completed.md` entry

- [ ] **Step 1:** Write docs.
- [ ] **Step 2: Run** `make generate` then `make test-quiet`; on failure read `.cache/test-logs/` before rerunning.
- [ ] **Step 3: Run** `make test-frontend-browser-quiet` (route navigation to `/storage/btrfs` is a real chunk boundary).
- [ ] **Step 4:** Stage docs. Suggested commit: `docs(btrfs): document btrfs management`.
- [ ] **Step 5:** Hand off with the exact targets run and results, and one combined suggested commit message for the branch.
