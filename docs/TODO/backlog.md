# Backlog

Every open item about the backend, the API, jobs, scheduled scripts and
consistency is here, once. What is built is described in
[the docs](../README.md), not here.

An item belongs here only if someone could be harmed by it or would notice it.
LinuxIO is server administration software that other people install, so a
second local user and a session without sudo count. An observation with no
consequence does not get an item; if it is likely to be raised again it gets
one line under [Rejected](#rejected) with its reason.

How to use it: take an item, fix it with a test, run the Make target
`AGENTS.md` asks for, and delete the item. Add a new item in the section it
belongs to, in the same shape.

Each item says how sure we are:

- **checked**: the code was read and the problem follows from it.
- **reported**: a scan or a review said so and nobody has re-read the code.
- **not verified**: seen once in a review or a test run and not explained.
- **needs a host**: only a real machine can confirm it.

The sections are in the order to work them. A number in brackets, such as
(audit 132), is the item's number in the old backend audit, for looking it up
in git.

## File browser: root follows links a user planted

When an admin uses the file browser, the work runs as root. A program running
as that user can swap one of the user's folders for a symlink, and root would
then act on the folder the link points to. Create, delete, copy, move, rename,
chmod, chown, compress, extract, a save with an owner and the destination
folder of a trash restore are closed
([Privilege Pattern](../privilege_pattern.md#root-acting-in-the-session-users-folders)).
What is left:

- [ ] **Decide: an admin's own folder links are refused.** The rule cannot
  tell a link the admin made on purpose (`~/Documents` pointing at another
  disk) from one a hostile program planted, so the file browser answers 403
  for a change through such a path and the admin must use the real path.
  - Decided 2026-11-13: the error names the folder the link points to, so the
    admin knows where to go. Running the change as the user when the path is
    in folders the user owns stays a later option; it would make links work.
- [ ] **A trash restore goes where the user-editable `.trashinfo` says**
  (audit 132). `RestoreFromTrash`
  (`backend/bridge/handlers/filebrowser/services/trash.go`) reads the
  destination from `Path=`, which the user can edit, so an item an admin
  trashed from `/etc` can be sent to a different system folder. Checked.
  - Decided 2026-11-13: record the original location root-side when the item
    is trashed and restore only to it. An item with no record, trashed by
    another program such as a desktop file manager, asks before a restore into
    a system folder.
- [ ] **Compress reads through links.** An admin compressing a user's folder
  that holds a planted link pulls the link's target into the archive, which is
  created readable (`statWithSymlinkResolution` in
  `services/archive_operations.go`). Reported.
- [ ] **A root upload without an owner follows links** (`upload_owner.go`,
  `openUploadDir`). Reported.
- [ ] **Can root sign in?** Not checked. The link rule is switched off for a
  root session, but the trash list and purge paths (`services/trash.go`,
  `openTrash` callers) still build their owner from the process uid alone.

## Storage safety

Each item here is data-safety work: it starts from what the kernel and
btrfs-progs do, gets an independent review, and adds a root test on loop
devices (`make test-btrfs-integration`, and `make vm-test-btrfs-integration`
after a change that issues ioctls).

- [ ] **Format and Delete can still destroy a member partition without
  naming it.** On the Disks page, Format with **Overwrite existing data**
  erases a partition that belongs to a stopped md array, an inactive LVM
  volume group or another set, and Delete drops it from the table; neither
  says what it belongs to (`FormatPartition`, `DeletePartition` in
  `backend/bridge/handlers/storage/partitions.go`). Initialize disk names and
  confirms such members; these two should do the same. Checked by the review
  of 2026-11-13.
- [ ] **The disk check runs before the disk lock.** Every operation checks
  one inventory read and then writes, and only Format checks "not busy" again
  under the lock. `wipeDeviceEdges` opens the device without `O_EXCL` before
  the add ioctl (`backend/common/btrfs/devices.go`), so a device claimed in
  that window gets its edges zeroed. md and LVM membership is read from the
  udev database, so a signature udev has not probed is not seen. Reported;
  the window is short and was there before.
- [ ] **Reading a pool can stop it from being destroyed.** The subvolume,
  snapshot and settings reads are reported to mount a pool's top level under
  `/run/linuxio/btrfs/`, after which `pool_destroy` refuses the pool. Not
  re-read. Checked beside it: `storage.btrfs_list` raises alerts and turns on
  the health timer (`ListBtrfsFilesystems` in
  `backend/bridge/handlers/storage/btrfs_subvolumes.go`).
- [ ] **A scrub starts over after a power cut.** It is checkpointed only by
  its `Suspend` hook at an orderly stop
  (`backend/bridge/handlers/storage/btrfs_device_operations.go`). Checked.
- [ ] **No cancel for a device removal or a replace on the page.** The Devices
  card cancels a running balance only
  (`frontend/src/routes/_authenticated/storage/-components/btrfs/BtrfsDevicesCard.tsx`).
  The backend can cancel a removal in the kernel. Whether the tray lists these
  jobs with a cancel: not verified.
- [ ] **A scrub might end by itself.** The kernel stops a scrub on any signal
  to its thread, and nothing in `backend/common/btrfs` retries `EINTR`. An
  inference; never observed. Needs a test.

## What a user without sudo sees

A route that changes the host is `Privileged()` or is a named exception, and a
test enforces it ([Privilege Pattern](../privilege_pattern.md)). What is left
is on the pages: the server refuses, but some pages still offer the control.

- [ ] **The Accounts page shows admin-only controls.** Creating, changing and
  deleting users and groups, and ending a session, are all privileged, and the
  page is open to everyone with no check
  (`frontend/src/routes/_authenticated/accounts`). Checked.
- [ ] **The Docker pages show admin-only controls.** They need only the Docker
  capability (`docker/route.tsx`), so a user without sudo who can reach Docker
  sees start, stop, the container terminal and the rest, and gets a 403.
  Whether such a user ever has the capability was not checked.
- [ ] **The network editor is hidden, not read-only.** A user without sudo no
  longer sees an interface's IP mode, DNS, MTU and autoconnect at all
  (`NetworkInterfaceList.tsx`). A read-only view would be better. That gate
  has no unit test.
- [ ] **The dashboard calls privileged reads without a check.** A non-sudo
  bridge logged `forbidden: privileged route` for
  `monitoring.get_cpu_history`, the network and disk histories, and
  `filebrowser.subfolders`
  (`frontend/src/routes/_authenticated/-dashboard/ProcessorGraph.tsx`). Not
  verified.
- [ ] **A user without sudo loses the sign-in notice during an update wait.**
  The history answers empty and the notice takes the sign-in mark anyway
  (`frontend/src/routes/_authenticated/-components/useAwayNotice.ts`).
- [ ] **An admin can cut their own connection.** `systemd.stop_service` and
  `systemd.restart_service` accept LinuxIO's own units, and the network routes
  accept the interface that carries the session. No check was found in
  `backend/bridge/handlers/systemd` on 2026-11-13. Reported.
- [ ] **`control.logoff` has no caller in the frontend.** It ends any login
  session by ID, the same as `accounts.terminate_session`. Check the history
  and remove one of the two.

## Visible bugs

- [ ] **A Compose stack saved by a sudo user got a folder owned by root.** The
  stack folder was root's, mode `0700`, inside that user's home. Not
  reproduced. Read 2026-11-13: the save is a file browser upload with
  `owner: "session"`, which hands a new folder to the user only when the
  folder it is made in already belongs to them (`mkdirAt` in
  `backend/bridge/handlers/filebrowser/services/handover.go`). A parent that
  is root's, such as a `~/docker` made earlier without the hand-over, would
  explain it. If it happens again, check who owns the nearest existing parent.
- [ ] **Container state and health colours.** Decided in five frontend files
  (`constants/statusColors.ts`, `utils/dockerContainer.ts`, the dashboard's
  `Docker.tsx` and two under `docker/-components`). Unhealthy is an error in
  one and a warning in another; dead is neutral or an error. Checked.
- [ ] **The tray still knows an indexer word the daemon never sends.**
  `frontend/src/tasks/indexerProgress.ts` treats a status of `indexing` as
  running; the daemon sends `uninitialized`, `running` and `idle`
  (`backend/indexer/api`). Whether that file is reached by the daemon's status
  was not checked. Reported.
- [ ] **Release notes.** The release that ships jobs should state three
  behaviour changes: a stored Docker `never` no longer silences failures; an
  interruption by a shutdown is a warning, so "Errors only" no longer emails it
  unless *Warnings* is ticked for *Email*; and a save from the settings page
  over an unreadable `notifications.json` drops every schedule's choice and the
  SMTP password and keeps the old file as `notifications.json.unreadable`
  ([Alerts](../alerts.md#notification-settings)). Release notes are not kept
  in the repository, so this cannot be checked here.

## Silent misses worth a guard

Nothing fails today. Each of these is one kind of item named in several
places, where leaving one out fails without an error. The rules are in
[API Contract](../api-contract.md) (One Definition, Checked Rules). Reported
unless it says checked. The scan that found them read the Go backend largely
in full and 20 to 25 percent of the frontend, so more of the same is likely
there.

- [ ] **Live metrics redaction names each section by hand.**
  `redactLiveSections` in `backend/monitoring/internal/httpapi/server.go`. A
  section added to `backend/monitoring/api/live.go` and missed there is sent
  to a listener that is not allowed it. Checked 2026-11-13: all nine sections
  are redacted today, so nothing leaks; only the capture time and the uptime
  go to every listener. That the test covers six of the nine is reported.
  - Add the test with the fix: for every member of `monitoringapi.Live`, a
    listener that is not permitted its plugin receives it empty. Walk the
    struct by reflection so a new member cannot be missed.
- [ ] **The UI settings save payload.** `buildUIPayload` in
  `frontend/src/contexts/ConfigProvider.tsx` names each setting, the payload
  type is all-optional, and the save replaces everything, so a setting left
  out is reset on every save. Checked.
- [ ] **Every UI default is written to `ui.yaml` on the first save.**
  `config.set_ui` starts from `DefaultUIPreferences()` and stores the whole
  value, so a later change to a backend default never reaches that user. Only
  view modes are stored as differences (`normalizeViewModesForDefault` in
  `backend/bridge/internal/userconfig/validator.go`). Do the same for the
  rest.
- [ ] **btrfs profiles.** Five tables in `backend/common/btrfs` and three in
  `btrfsShared.ts`. A profile missing from `MinDevices` returns 0, so any
  device count passes validation. Checked to agree today.
- [ ] **Storage and packages register routes from a second list.**
  `storage/handlers.go` declares the route sets and
  `storage/smart_test_operation.go` registers them; a set in one and not the
  other is a route that answers "not found". Checked for storage.
- [ ] **Docker daemon keys.** Seven keys in read, write and two change lists
  in `docker/daemon_config.go`; a key missing from the change lists is saved
  only when another key changes.
- [ ] **System metrics.** About forty, each named in roughly ten places under
  `backend/monitoring`. A metric left out of the averaging in
  `internal/plugins/aggregate.go` is silent.
- [ ] **Monitoring plugins.** Sixteen, with attributes spread over four files.
  A plugin missing from the collection groups is dropped silently.
- [ ] **Allowed values.** Every `apischema.StringEnums` entry is built from
  the Go definition it describes, or a test compares the two. Today only
  `CapabilityName`, `WireGuardHookKind` and `ThemeColorKey` are. The others
  retype Go constants: dock tile colours, navigation mode, theme, view mode,
  task state, storage mode, container metrics status, network bridge handoff
  state, auto-update backend and notify, VM image presets and firmware.

## Tests that are missing

- [ ] The relay tests in `backend/bridge/cmd/jobs_relay_test.go` skip when run
  as root, and a privileged login skipping the host start has no hermetic test
  (`backend/auth/linuxio-auth-pam_test.c`).
- [ ] Not run in the test VM (`frontend/src/test/vm/jobs-phase5a.spec.ts`): a
  real update from a release that has job hosts, more than one waiting job, a
  user's host as the older host, `tasks.watch`, `tasks.cancel` and
  `tasks.output` during the wait, and uploads during it.
- [ ] A relay speed test: a no-op mutation through the relay many times, with
  a bound on the median. No baseline numbers exist either for call latency,
  job start, alert listing or schedule status.

## Needs a real machine or a measurement

- [ ] **Seen once and not explained.** Try each in the test VM once, then fix
  it or delete the line:
  - A progress frame published between a stream's subscribe and its first
    snapshot may be skipped (`backend/bridge/internal/bridgeipc/tasks.go`,
    `subscribeWithReplayStatus`).
  - An abort while the client waits for the stream mux after its job started
    may not reach the server (`frontend/src/api/linuxio-core.ts`).
  - After a failed first history load, a non-admin's notice observer refetches
    on focus and ignores the result (`useAwayNotice.ts`).
  - A session bridge logged `route failed: write frame: i/o deadline reached`
    while a scheduled script ran.
  - The journal field `LINUXIO_REASON` held the reason JSON-quoted
    (`"reboot"`).
- [ ] **Nothing works below a folder link with an absolute target**
  (audit 36). A link such as `/home/u/data -> /mnt/data` is refused by Go's
  `os.Root` for any path that goes through it, and `iteminfo.ResolveSymlinksAt`
  resolves only the last name. Checked in the Go 1.27.4 source; needs a host
  to reproduce first.
  - Fix: resolve the parent folders at the points where a path enters, and run
    `CheckNotProtected` on the result. Large: 37 `fsroot.Open` call sites, and
    a mistake here lets a path escape.
- [ ] **Scheduled scripts.** A run that passes its timeout, catching up a
  missed run (`Persistent=`), journal retention over several days, and a
  bridge restart during a run. See
  [Scheduled Scripts](../scheduled-scripts.md#testing).
- [ ] **Agents view against a live Claude chat.** Seen live on 2026-11-13:
  a background subagent's calls and its `SubagentHandback` arrive tagged
  with its call. Still read only from the adapter's source
  (claude-agent-acp 0.102.0) and unit tests: a foreground subagent's tokens
  (`toolResponse.totalTokens`) when it ends, its text kept out of the chat
  and under *Messages*, and a background subagent running until its turn
  ends. Check them in a chat that sends off one subagent in the foreground
  and one in the background. See [AI Assistant](../AI-assistant.md)
  (Agents).
- [ ] **What happens after a route cuts its own connection is not written
  down.** The old roadmap's rule was: send it once; after reconnect, reboot
  compares the boot identity, a network change reads back the interface, and
  power-off reports "acknowledged" or "unknown", never success just because the
  host went quiet. Whether the frontend does this was not checked. Check it,
  then describe it in [API Contract](../api-contract.md).
- [ ] **Hardlink reconcile runs one query per file** in one transaction
  (audit 57): `reconcileHardlinksAfterReindex` in
  `backend/indexer/internal/storage/hardlinks.go`. Measure first, then add up
  the changes per folder. The risk of a wrong fix is silent wrong folder
  sizes.
- [ ] **NVIDIA bus id** (audit 124). The unversioned `nvmlDeviceGetPciInfo`
  may be an older ABI than the struct it fills
  (`backend/monitoring/internal/collector/gpu/nvml.go`). The struct has the
  largest layout, so nothing is written out of bounds; what is left is to
  check on an NVIDIA machine that the bus id reads correctly, or to register
  `nvmlDeviceGetPciInfo_v3` by name.

## Later, no plan yet

Do one of these only when something shows it is needed.

- Pick up a package update again after the job host dies, by asking
  PackageKit.
- Pick up a SMART test again from the drive's own state.
- More alert sources and other ways to deliver them: see "Not in this slice"
  in [Alerts](../alerts.md).
- What jobs do not do: see "Not built" in [Jobs](../jobs.md#not-built).
- Bring the [mutation map](./mutation-map.md) up to date. Its route tables are
  from before jobs.

## Rejected

Listed once, with the reason, so they are not raised again.

File browser, after the link fix:

- A real folder (not a link) moved into the tree while root works on it, or
  put at a name between root's `mkdirat` and `openat`: no link is followed and
  root still acts at the place that was asked for.
- Filesystems without `RENAME_NOREPLACE` (NFS, FUSE) fall back to a check and
  a plain rename: a swap there affects only the user's own files.
- The checks before an operation go by path: they decide names and sizes
  only, and the change itself walks again through handles.

Storage and jobs:

- A D-Bus error can lose one replication run's record when the last job is
  disabled during a send: it needs both at once, and it loses a record, not
  data.
- The job recorder writes progress files while it holds its mutex,
  `Store.Finish` rewrites an already finished record unchanged, and store
  reads `Lstat` and then open in two steps: no effect was seen, and the store
  folders belong to root or to the uid itself.
- A user's job host records the owner's username from the bridge's hello, and
  a resend is compared byte for byte: both are the same user's own processes,
  and the browser resends identical bytes.
- `linuxio logs` lists template units by a name no instance has: the lines
  are found through the program's log identifier.

One thing named in many places, with no silent consequence. Convert one when
touching its file:

- Sets: job settings, the monitoring settings form, indexer status fields,
  alert notification settings, journal filters, archive formats, compose file
  names, UnitInfo properties, GPU collector sources, transfer kinds, palette
  roles, editor languages, syslog priorities, Docker prune kinds, the accounts
  toast constant, view-mode surface ids.
- The same rule on both sides, in step today: notification kinds and levels,
  btrfs compression algorithms, filesystem label limits and minimum sizes, VM
  image presets, scheduled-run kinds, Docker auto-update defaults, the stream
  flags and opcodes of the binary protocol, the Claude relay limits, the
  run-record drop-in units and socket paths.
- Fallbacks that cannot run (theme primary and secondary, view and storage
  mode in `useConfig.ts`, the dock gradient range, the alternative field names
  in `NetworkInterfaceEditor.tsx`) and the monitoring summary's fallback to a
  mirror of the same stats: dead code, removed when the file is touched.

From the backend audit of October 2026:

- SHA-256 of the bridge binary on every login (audit 48): about 50 ms is not
  worth a cache keyed on inode metadata.
- Lazy marshal of the collector hand-off (audit 55): the eager marshal is what
  detaches the sample from later changes to `data`.
- Context as the first parameter across `filebrowser/services` (audit 96):
  about 57 call and test sites for no user value.
- `hostcmd.LookPath` everywhere (audit 97): the sbin fallback would make
  `smartctl` and `dmidecode` "found" on a non-root Debian bridge, turning
  "feature unavailable" into permission errors.
- Logging `component`/`subsystem` attributes (audit 102) and `sort.Slice` to
  `slices.Sort*` (audit 108): convert when touching a file.
