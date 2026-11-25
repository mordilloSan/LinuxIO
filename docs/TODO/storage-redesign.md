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

A route `/storage/pools/$uuid`, laid out with `ResourceDetailsLayout` like the VM and Docker details, with tabs Overview, Snapshots, Disks and Settings (`?tab=`), replacing the pool details on the Disks overview (`/storage?pool=`, which redirects). The header carries the name, a health chip from the health banner's own condition, and one line (disks, how data is kept, stored and free). Overview: the capacity card, disks as tiles with an Add-a-disk tile, a Protection card that says plainly what a disk failure does for this profile, whether a scrub found errors and when, the newest snapshot, and whether anything replicates. The Snapshots tab is the timeline of mockup B2 with a detail panel; "Kept as" needs the backend to say which retention bucket keeps each snapshot (`linuxio-worker` prune logic shared with the list), added here. Disks and Settings reuse today's cards. Detailed tasks are written when phase 1 lands.

## Phase 3: Storage by what you manage (C, frontend)

Storage tabs become Overview, Pools, Shared folders, Snapshots and Advanced (All disks, md arrays, LVM, Mounts, rsync), with today's routes kept under Advanced and old paths redirected. Overview: one health line, "Worth a look" items derived from pool state (one disk, nothing replicated, a degraded pool), pool cards, and shared folders. Shared folders shows today's shares as one list, with the panel of mockup C2 limited to what the backend supports until phase 4. Detailed tasks are written when phase 2 lands.

## Phase 4: shared folders backend

Decisions (the owner may overturn any):

- A shared folder is a subvolume on a pool LinuxIO manages, recorded in `/etc/linuxio/shared-folders.json` (`root:root 0600`); folders outside a pool can still be shared, without the access matrix.
- **Create shared folder** is one privileged job: subvolume, quota limit, snapshot rule, access, SMB and NFS sections; a failure undoes the steps already done, newest first.
- Access per user and group (read and write, read only, no access) is written twice, to the same effect: `valid users`, `read list` and `write list` in the share's `smb.conf` section, and POSIX ACLs on the folder (with default ACLs, so new files follow), so local and NFS access match.
- Signing in from Windows or a Mac uses the LinuxIO account's password: setting or changing a password with **Can open shared folders** on also writes it to Samba's own user list (`pdbedit` over stdin, through the existing command path); an existing user sets their password once more to turn it on. Samba cannot use PAM for this sign-in.
- New routes declare `Privileged()`; a new doc in `docs/` for shared folders covers it and takes over SMB and NFS from `rsync-backups.md`'s row.

## Phase 5: network extras

Per shared folder: earlier versions in Windows and Finder from its snapshots (`vfs_shadow_copy2`, with the snapshot name format checked against what it parses, in the test VM); a recycle bin for files deleted over SMB (`vfs_recycle`) emptied after 30 days by a `linuxio-worker` word on a timer, listed for package removal; Time Machine (`vfs_fruit`, a size limit); discovery by Windows (WS-Discovery, as a capability) and by Macs (Avahi `_smb._tcp`). SMB service settings (minimum protocol, encryption) on the Shared folders tab.
