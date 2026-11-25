# Storage redesign

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Never commit: the owner commits (`AGENTS.md`).

**Goal:** Storage that reads like a NAS: right numbers in plain words, a pool page that leads with status, Storage organised by what an administrator manages, and shared folders people can sign in to from Windows and a Mac.

**Architecture:** Five phases, each shippable alone. Phase 1 changes numbers and words on today's cards. Phases 2 and 3 reorganise the frontend around pools and shared folders, reusing the existing routes. Phases 4 and 5 add the shared-folder backend (one create operation, access per user and group, SMB sign-in with the LinuxIO account) and the network extras (earlier versions, recycle bin, Time Machine, discovery).

**Tech Stack:** Go (`backend/common/btrfs`, `backend/bridge/handlers/storage`, `backend/bridge/handlers/shares`, `backend/bridge/handlers/accounts`), React with TanStack Router and Query, the generated API contract.

**Spec:** the approved mockups, canvas [LinuxIO storage redesign](https://claude.ai/artifact/DCRpyySgVH3P1GW2rnBGWS): A1, A2 for phase 1; B1, B2 for phase 2; C1, C2 for phases 3 and 4.

## Global Constraints

- Free space is the kernel's own figure, the one `df` shows (statfs `f_bavail`, `poolFreeBytes`); stored is the used bytes of every allocation row, counted once. Phase 1's first form, the `btrfs filesystem usage` estimate, overstated a mirror of uneven disks.
- Words: no "fstab", "btrfsmaintenance", "systemd", "chunk" or "(s)" in visible text; "stored" and "about N free" for space; "allocated" for a disk's share.
- Build with the existing components; the mockups set layout and content, the app's theme sets the look. Cards are `FrostedCard` with `CardIconHeader`; bars `AppLinearProgress`; folds `AppCollapse`; row menus `AppActionIconButton` with `AppMenu`; kind dots and health `StatusDot` and `AppChip`; controls `AppSwitch`, `AppSelect`, `AppButton`, `AppRadio`, `AppFormControlLabel`; disk tiles `DriveCard`; the pool page `ResourceDetailsLayout` with routed tabs, as the VM and Docker details are. A piece with no component yet (the snapshot timeline row, the capacity legend, the access-matrix row) is composed from these inside the storage route and moves into `components/ui` only when a second page needs it.
- Styling only through `components/ui` and the `--app-*` variables; no inline hex or `fontSize`.
- No new `useMemo`, `useCallback` or `memo` outside `*Virtual*` files.
- Tests and checks through Make: `make test-go-quiet GO_TEST_PKGS=...`, `make test-frontend-quiet VITEST_FILE=...`, `make generate`, and `make test-quiet` at the end of a phase that changes the contract.

## Review Focus

- A pool whose space could not be read (no allocation rows) shows no space figures rather than "0 B stored". Owned by Task 1.2.
- A pool missing a disk does not count that disk's unallocated space as free. Owned by Task 1.1.
- A pool in the middle of a profile change (two data profiles) weighs the copies of each. Owned by Task 1.1.
- A snapshot taken at 00:08 local time groups under Today, not Yesterday. Owned by Task 1.4.
- With two or more sources, each snapshot row names its subvolume and the source filter still applies; menu actions act on that row's snapshot. Owned by Task 1.4.

---

## Phase 1: right numbers, plain words (A)

### Task 1.1: Stored and free space from the backend

**Files:**
- Modify: `backend/common/btrfs/inventory.go` (`BtrfsFilesystem`, `fillFilesystem`)
- Test: `backend/common/btrfs/inventory_test.go`
- Generated: `frontend/src/api/generated/linuxio-types.ts` through `make generate`

**Interfaces:**
- Produces: `BtrfsFilesystem.StoredBytes uint64` (`json:"storedBytes"`) and `BtrfsFilesystem.FreeBytes uint64` (`json:"freeBytes"`); TypeScript `storedBytes: number`, `freeBytes: number`.
- Produces: `func spaceSummary(alloc []BtrfsAllocation, unallocated uint64, present int) (stored, free uint64)`.

- [x] **Step 1: Write the failing tests.** `TestSpaceSummary` in `inventory_test.go`, table-driven:
  - single data (total 372.01 GB, used 327.26 GB) with dup metadata (4 GB, 1.66 GB) and 95.82 GB unallocated: stored = data used + metadata used + system used; free = 44.75 GB + 95.82 GB.
  - raid1 data 50 GiB total, 40 GiB used, 98 GiB unallocated, 2 present: free = 10 GiB + 49 GiB.
  - data in raid1 (10 GiB, 4 used) and single (10 GiB, 6 used) during a convert, 30 GiB unallocated, 2 present: free = 10 GiB + 30 GiB / 1.5.
  - no allocation rows: stored 0, free 0.
  In `TestListFilesystemsReadsKernel` assert `fs.StoredBytes == 40<<30+512<<20+16<<10` and `fs.FreeBytes == 59<<30`. In `TestListFilesystemsMissingDevice` assert the missing disk's unallocated space is not in `FreeBytes`.
- [x] **Step 2: Run** `make test-go-quiet GO_TEST_PKGS=./common/btrfs/... GO_TEST_FLAGS='-run "TestSpaceSummary|TestListFilesystems"'`. Expected: FAIL, `spaceSummary` undefined.
- [x] **Step 3: Implement.** `spaceSummary`: stored is the sum of every row's logical `UsedBytes`; the data ratio is the raw size of the data rows over their logical size (`profileRatio(profile, present)` per row, 1 when there is no data row); free is the sum of `TotalBytes-UsedBytes` over data rows plus `unallocated / ratio`. A row is data when its `Kind` contains `data`. In `fillFilesystem`, after the allocation loop, call it with the unallocated space of the devices that are not `Missing` and set both fields; leave them 0 when space info could not be read.
- [x] **Step 4: Run** the same command. Expected: PASS.
- [x] **Step 5: Run** `make generate`. Expected: `storedBytes` and `freeBytes` appear on `BtrfsFilesystem` in `linuxio-types.ts`.

### Task 1.2: One space figure in the overview, the Space card and the disk rows

**Files:**
- Modify: `frontend/src/routes/_authenticated/storage/-components/disk-overview/storageGraph.ts` (`describePool`, `PoolSummary`)
- Modify: `frontend/src/routes/_authenticated/storage/-components/disk-overview/StoragePools.tsx:257`
- Modify: `frontend/src/routes/_authenticated/storage/-components/btrfs/BtrfsSpaceCard.tsx`
- Modify: `frontend/src/routes/_authenticated/storage/-components/btrfs/BtrfsDevicesCard.tsx` (usage column, subtitle)
- Test: `StoragePools.test.tsx`, `storageGraph.test.ts`, `BtrfsCards.test.tsx`, `BtrfsDevicesCard.test.tsx`

**Interfaces:**
- Consumes: `storedBytes`, `freeBytes` (Task 1.1).
- Produces: `PoolSummary.storedBytes`, `PoolSummary.freeBytes` (replacing `allocatedBytes` and the raw `freeBytes`).

- [x] **Step 1: Write the failing tests.**
  - `StoragePools.test.tsx`: the pool node reads `"<stored> stored · about <free> free"` from the fixture's `storedBytes` and `freeBytes`.
  - `BtrfsCards.test.tsx`: the Space card shows the stored figure, `"About <free> free of <stored+free>"`, a progress bar with value stored/(stored+free), and the allocation rows only after **Details** is opened; a filesystem with no allocation rows shows none of these and no "0 B".
  - Devices card: the usage cell reads `"<used> allocated"` with no "free"; the subtitle reads `"1 disk"` for one device and `"2 disks"` for two.
- [x] **Step 2: Run** `make test-frontend-quiet VITEST_FILE=src/routes/_authenticated/storage/-components/btrfs/BtrfsCards.test.tsx` and the same for `StoragePools.test.tsx`. Expected: FAIL.
- [x] **Step 3: Implement.** Space card: `CardIconHeader` with title "Space"; the stored figure, the free line, one `AppLinearProgress`, then an `AppCollapse` behind a "Details: how the disk is laid out" toggle holding the rows `Data · <profile>`, `Metadata · <profile>`, `System · <profile>` as "`<used>` of `<total>` set aside", "Not set aside yet" (unallocated) and the UUID. Devices card column header "Allocated".
- [x] **Step 4: Run** the same commands. Expected: PASS.

### Task 1.3: Plain words in settings, maintenance and the hot spare

**Files:**
- Modify: `frontend/src/routes/_authenticated/storage/-components/btrfs/BtrfsSettingsCard.tsx`
- Modify: `frontend/src/routes/_authenticated/storage/-components/btrfs/BtrfsMaintenanceControls.tsx`
- Modify: `frontend/src/routes/_authenticated/storage/-components/btrfs/BtrfsHotSpare.tsx`
- Test: `BtrfsSettingsCard.test.tsx`, `BtrfsMaintenanceControls.test.tsx`, `BtrfsDevicesCard.test.tsx`

- [x] **Step 1: Write the failing tests.**
  - Settings: the compression chip reads `"Kept after a restart"` when `fstabPersisted`, `"Until the next restart"` otherwise; the quotas switch's accessible name is "Quotas" and its description is in the same row group.
  - Maintenance: the subtitle reads `"Runs in the background on a schedule."`; `"Suggested: Monthly"` shows for scrub only when its period is not monthly.
  - Hot spare: a one-disk pool without a spare renders no "Hot spare" row; a two-disk single pool reads `"A hot spare needs a pool that keeps two or more copies; this one keeps one (single)."`
- [x] **Step 2: Run** each file with `make test-frontend-quiet VITEST_FILE=...`. Expected: FAIL.
- [x] **Step 3: Implement.** Settings: replace the quotas `InfoRow` with a row whose `AppFormControlLabel` label is "Quotas" next to the switch, the description as a caption below; the force switch's label "Also compress data that looks incompressible". Maintenance task texts as in mockup A1: scrub "Reads every block and checks it against its checksum, to catch silent corruption. Where a second copy exists, a bad copy is repaired from it."; balance "Repacks nearly empty areas so their space can be reused, which prevents "no space left" errors while free space still shows."; trim "Tells an SSD which blocks are free so it keeps its write speed. On kernel 6.2 or later btrfs already does this as files are deleted."; defrag caution "Undoes the sharing between files and their snapshots, which can take a lot of extra space."; the capability hint keeps naming the package to install. Hot spare: return `null` when there is no spare and the pool has fewer than two devices.
- [x] **Step 4: Run** the same commands. Expected: PASS.

### Task 1.4: Snapshots grouped by day with a menu

**Files:**
- Create: `frontend/src/routes/_authenticated/storage/-components/btrfs/snapshotDays.ts`
- Test: `frontend/src/routes/_authenticated/storage/-components/btrfs/snapshotDays.test.ts`
- Modify: `frontend/src/routes/_authenticated/storage/-components/btrfs/BtrfsSnapshotsCard.tsx`
- Test: `BtrfsSnapshotsCard.test.tsx`

**Interfaces:**
- Produces: `groupSnapshotsByDay(snapshots: BtrfsSnapshot[], now: Date): { key: string; label: string; recent: boolean; items: BtrfsSnapshot[] }[]`, newest day first and newest snapshot first; `label` is "Today", "Yesterday", or the long local date ("Tuesday 20 October 2026"); `recent` is true for Today and Yesterday.

- [x] **Step 1: Write the failing tests.** `snapshotDays.test.ts`: with `now` = 2026-11-25 05:00 local, snapshots at 2026-11-25 00:08, 2026-11-24 23:11 and 2026-10-20 07:41 land under "Today", "Yesterday" and "Tuesday 20 October 2026" in that order; two snapshots on one day keep newest first. `BtrfsSnapshotsCard.test.tsx`: day headings render; each row has a "Browse" button and a "More actions" button whose menu holds "Roll back to this snapshot…" and "Delete…", which open the existing rollback and delete dialogs for that row; the source path shows only when there are two or more sources; a legend names the kinds present.
- [x] **Step 2: Run** both files. Expected: FAIL.
- [x] **Step 3: Implement.** Kind labels "Scheduled", "Before an update", "Taken by hand", "Before a rollback". Replace the table with day sections (a heading per day, rows of dot, time `HH:MM` with " · <relative age>" when `recent`, kind label, the path when there are several sources, the size when quotas give one, then Browse and the menu through `AppMenu`). The list's width is capped near 880 px inside the card and scrolls past 520 px.
- [x] **Step 4: Run** both files. Expected: PASS.

### Task 1.5: Docs and the phase check

**Files:**
- Modify: `docs/btrfs.md` (pool details: how space is counted; the snapshots list), `docs/storage-disks.md` (the pool node's line)

- [x] **Step 1:** Describe stored and free (the estimate and why a mirrored pool's free is half its raw space), the Details fold, the day groups and menu.
- [x] **Step 2: Run** `make check-docs-quiet`, then `make test-quiet`. Expected: both pass.
- [x] **Step 3:** Review the phase diff (code-review skill), fix findings, run `make test-quiet` again.

---

## Phase 2: the pool page (B)

The pool details leave the Disks overview for a **Pools** tab built as the VM page is: `/storage/pools` lists the pools as cards, and `/storage/pools/$uuid` folds the list into the selected pool's card beside a details panel (`ResourceDetailsLayout`) whose tabs (`?tab=overview|snapshots|disks|settings`) hold the rest. `/storage?pool=<uuid>`, which the alerts link to, redirects there; the alert links stay as they are. Mounted pools only; a pool found on disk but not mounted stays on the Disks overview until phase 3.

### Task 2.1: Why each snapshot is kept

**Files:** Modify `backend/common/btrfs/retention.go`, `backend/common/btrfs/snapshots.go` (`BtrfsSnapshot.KeptBy`), the bridge handler serving `storage.btrfs_snapshots`; Test `backend/common/btrfs/retention_test.go`; Generated through `make generate`.

**Interfaces:** Produces `type KeptReason string` with `KeptManual`, `KeptUnscheduled` (no rule for its source), `KeptScheduleOff` (a disabled rule leaves its timeline), `KeptRecent` (one of the newest 3 of its kind), `KeptHourly`, `KeptDaily`, `KeptWeekly`, `KeptMonthly`, `KeptReplication` (the replication parent), `KeptUntilNextPass` (the next pass deletes it); `func KeptBy(snaps []BtrfsSnapshot, rule *BtrfsSnapshotRule, protectsParent bool) map[string]KeptReason` keyed by path; `BtrfsSnapshot.KeptBy KeptReason` (`json:"keptBy,omitempty"`); TypeScript `keptBy?: KeptReason`.

- [x] Failing tests: `TestKeptBy` (one case per reason, the smallest bucket that keeps a timeline snapshot named, a disabled rule keeping its timeline but not its fourth pre-update snapshot); `TestKeptByAgreesWithPrune` (over generated snapshot sets with an enabled rule and no replication, a snapshot is `KeptUntilNextPass` exactly when `Prune` returns it). `Prune` is not changed.
- [x] Implement `KeptBy` from `keep.buckets()` and `keepNewestPerKind`; the list route loads the rules and replication jobs once and sets `KeptBy` per source, leaving it empty when they cannot be read.
- [x] `make test-go-quiet GO_TEST_PKGS='./common/btrfs/... ./bridge/handlers/storage/...'`, then `make generate`.

### Task 2.2: One health verdict for a pool

**Files:** Modify `btrfs/BtrfsHealthBanner.tsx` (export `poolHealth`, today's `healthOf`), Test `btrfs/BtrfsCards.test.tsx`.

**Interfaces:** Produces `poolHealth(filesystem: BtrfsFilesystem, name: string): { title: string; color: string; icon: string; text: string; action?: … }`, and `keptCopies(profile: string): string` in `btrfs/btrfsShared.ts`: "data stored once" (single), "two copies on one disk" (dup), "two copies on different disks" (raid1), "three copies" (raid1c3), "four copies" (raid1c4), "two copies, striped" (raid10), "striped, no second copy" (raid0).

- [x] Failing tests: `poolHealth` gives "Healthy", "Degraded", "Errors", "Data errors", "Low space" as the banner does; `keptCopies` per profile.
- [x] Implement by moving, not rewriting, the banner's logic.

### Task 2.3: The Pools tab and its routes

**Files:** Create `storage/pools/route.tsx` (layout: list or details), `storage/pools/index.tsx`, `storage/pools/$uuid.tsx` (`validateSearch` for `tab`), `storage/-components/pools/PoolCard.tsx`, `storage/-components/pools/PoolsLayout.tsx`; Modify `storage/-components/storageTabs.ts` (Pools after Disks, `matchChildren`), `storage/index.tsx` (`pool` redirects to `/storage/pools/$uuid`), `disk-overview/StoragePools.tsx` (a pool card navigates to its page; the details, rename and destroy leave this file); Tests beside each, `storageTabs.test.tsx`, `StoragePools.test.tsx`.

**Interfaces:** Consumes `poolHealth`, `keptCopies`, `PoolSpace` (`storageGraph.ts`). Produces `PoolCard({ fs, selected, onSelect })`: name, health chip, "N disks · <keptCopies>", capacity bar with "<stored> stored · about <free> free" or "Space unknown", a chip per disk.

- [x] Failing tests: the Pools tab lists one card per mounted pool; selecting one goes to `/storage/pools/<uuid>` and shows its card beside the panel; closing returns to `/storage/pools`; an unknown `tab` falls back to Overview; `/storage?pool=<uuid>` lands on the pool page; a pool card on the Disks overview links to the pool page.
- [x] Implement as `vm/machines` does (`VMMachinesLayout`), Escape clearing the selection.

### Task 2.4: The pool page panel and its Overview tab

**Files:** Create `storage/-components/pools/PoolDetails.tsx` (health banner, tab selector, rename and destroy under a ⋯ menu, moved from `StoragePools.tsx` with their dialogs), `storage/-components/pools/PoolOverview.tsx` (Protection, disks, maintenance); Tests beside them.

- [x] Failing tests: Protection says "A disk failure loses this pool" for single, raid0 and dup and "Survives one disk failing" for raid1 and raid10 ("two" for raid1c3, "three" for raid1c4); the last scrub's date and result or "Never checked"; the newest snapshot's age or "No snapshots"; "No copy anywhere else" without an enabled replication job of this pool, otherwise the job's target and last run; disks as tiles from the pool's own device list (not `DriveCard`, which needs the drive inventory only the overview graph maps) with an Add-a-disk tile that opens the Disks tab; maintenance periods with a link to the Settings tab.
- [x] Implement with `FrostedCard`, `CardIconHeader`, `KeyValues`, `AppLinearProgress`, `AppButton`.

### Task 2.5: Snapshots as a timeline with a detail panel

**Files:** Modify `btrfs/BtrfsSnapshotsCard.tsx` (used by the pool page's Snapshots tab and the Subvolumes tab), `btrfs/BtrfsSnapshotsCard.test.tsx`.

- [x] Failing tests: kind filter chips (All, Scheduled, Before updates, By hand) narrow the list; a row is a button that selects its snapshot; the detail panel shows kind, full date, source, the Kept as reason in words and the size when quotas give one, with Browse files, Roll back (saying a snapshot of the current state is taken first) and Delete, acting on the selected snapshot; the ⋯ menu is gone.
- [x] Implement; the panel sits beside the list from the `md` breakpoint and below it under.

### Task 2.6: Disks and Settings tabs, and the words left over

**Files:** Modify `pools/PoolDetails.tsx` (Disks: `BtrfsDevicesCard` and `BtrfsSpaceCard`; Settings: `BtrfsSettingsCard`; the snapshot schedule sits on the Snapshots tab beside the list, as mockup B2 has it), and the visible text of `disk-overview/StoragePools.tsx` (offline pool card, destroy dialog), `btrfs/BtrfsCreatePoolDialog.tsx`, `btrfs/BtrfsBalanceDialog.tsx`, `btrfs/BtrfsHealthBanner.tsx`; their tests.

- [x] Failing tests where a string is pinned; no "fstab" or "chunk" in those texts.
- [x] Implement.

### Task 2.7: Docs and the phase check

- [x] `docs/btrfs.md` (the pool page, Kept as), `docs/storage-disks.md` (pool cards link to the pool page), `docs/tanstack-router.md` if the Pools tab adds a pattern; `make check-docs-quiet`, `make test-quiet`, a fresh review, fixes, `make test-quiet` again.

## Phase 3: Storage by what you manage (C, frontend)

Storage tabs become **Overview** (`/storage`), **Pools**, **Shared folders** (today's `/storage/shares`), **Snapshots** (`/storage/snapshots`, today's Subvolumes page opening on its Snapshots view) and **Advanced ▾**, a menu of **Disks** (`/storage/disks`, today's overview graph), **LVM**, **Mounts** and **rsync**. Old links keep working: `/storage?drive=|fs=|raid=` redirect to `/storage/disks`, `/storage?pool=` to the pool page, and `/storage/subvolumes` to `/storage/snapshots` with its `view` and `fs`. The Shared folders panel of mockup C2 comes with phase 4, which builds what it shows, so it is not built twice.

### Task 3.1: A menu tab in the shared tab bar

**Files:** Modify `frontend/src/components/tabbar/RoutedTabContainer.tsx` (and `index.ts`), `tabLayoutFactory.ts` if it types tabs; Test `RoutedTabContainer.test.tsx`; Docs `docs/tanstack-router.md` (Page Tabs).

**Interfaces:** Produces `interface RoutedTabGroup { label: string; items: readonly RoutedTab[] }` and `type RoutedTabEntry = RoutedTab | RoutedTabGroup`; `RoutedTabLayout`, `makeTabLayout` and the strip take `readonly RoutedTabEntry[]`.

- [x] Failing tests: a group renders one pill named by its label with `aria-haspopup="menu"`; opening it lists its items as menu items that navigate; while one of its routes is open the pill is selected and reads "Advanced: Disks".
- [x] Implement with `AppMenu` and `AppMenuItem`, no new memoization.

### Task 3.2: The routes move

**Files:** Create `storage/disks.tsx` (the overview graph, its search and loader, from `index.tsx`), `storage/snapshots.tsx` (from `subvolumes.tsx`, default view Snapshots, views ordered Snapshots, Replication, Subvolumes); Modify `storage/index.tsx` (Overview; redirects), `storage/subvolumes.tsx` (redirect), `storageTabs.ts`, `storage/route.tsx` (gating per tab and group item), `disk-overview/DiskOverview.tsx` (its route API and navigations), `btrfs/btrfsViews.ts`, `tasks/scheduledRuns.ts`, `pools/PoolsLayout.tsx` (its Disks link), the browser fixture and specs (`test/browser/fixture/main.tsx`, `storage-disks.spec.ts`, `topology-flow.spec.ts`), the VM spec opening Subvolumes (`jobs-phase4.spec.ts`); Tests `storageTabs.test.tsx`, redirect tests beside `pools/poolRedirect.test.ts`, `router.test.tsx`.

- [x] Failing tests: the tab strip reads Overview, Pools, Shared folders, Snapshots, Advanced (Pools and Snapshots only with btrfs, privilege and a mounted pool); each old link lands where it should, search kept; the Disks overview's own navigations stay on `/storage/disks`.
- [x] Implement.

### Task 3.3: The Overview

**Files:** Create `storage/-components/overview/StorageOverview.tsx`, `storage/-components/pools/CreatePoolButton.tsx` (the create dialog and its task, lifted from `StoragePools.tsx`, which uses it too); Tests beside them.

- [x] Failing tests: one health line ("All disks and pools are healthy", or how many need a look); *Worth a look* lists a pool that is degraded or not healthy (its banner title), a pool found on disk but not mounted, a pool of one disk ("a disk failure loses it"), a pool no enabled replication job copies, an md array running degraded; each links to where it is fixed; pool cards link to their pages; shared folders (SMB and NFS, from today's lists) with a link to the tab; **Create pool** opens the create dialog; without btrfs or privilege the pool parts are left out and the page says what it can.
- [x] Implement with `PoolSummaryCard`, `FrostedCard`, `CardIconHeader`, `KeyValues`.

### Task 3.4: Docs and the phase check

- [x] `docs/storage-disks.md` (its paths), `docs/btrfs.md` (tab names), `docs/tanstack-router.md` (route tables, guards, the menu tab), `docs/README.md` rows; `make check-docs-quiet`, `make format-frontend`, `make test-quiet`, the browser fixture specs (`make test-frontend-browser-quiet`), a fresh review, fixes.

## Phase 4: shared folders backend

Decisions (the owner may overturn any):

- A shared folder is a subvolume on a pool LinuxIO manages, recorded in `/etc/linuxio/shared-folders.json` (`root:root 0600`); folders outside a pool can still be shared, without the access matrix.
- **Create shared folder** is one privileged job: subvolume, quota limit, snapshot rule, access, SMB and NFS sections; a failure undoes the steps already done, newest first.
- Access per user and group (read and write, read only, no access) is written twice, to the same effect: `valid users`, `read list` and `write list` in the share's `smb.conf` section, and POSIX ACLs on the folder (with default ACLs, so new files follow), so local and NFS access match.
- Signing in from Windows or a Mac uses the LinuxIO account's password: setting or changing a password with **Can open shared folders** on also writes it to Samba's own user list (`pdbedit` over stdin, through the existing command path); an existing user sets their password once more to turn it on. Samba cannot use PAM for this sign-in.
- New routes declare `Privileged()`; a new doc in `docs/` for shared folders covers it and takes over SMB and NFS from `rsync-backups.md`'s row.

## Phase 5: network extras

Per shared folder: earlier versions in Windows and Finder from its snapshots (`vfs_shadow_copy2`, with the snapshot name format checked against what it parses, in the test VM); a recycle bin for files deleted over SMB (`vfs_recycle`) emptied after 30 days by a `linuxio-worker` word on a timer, listed for package removal; Time Machine (`vfs_fruit`, a size limit); discovery by Windows (WS-Discovery, as a capability) and by Macs (Avahi `_smb._tcp`). SMB service settings (minimum protocol, encryption) on the Shared folders tab.
