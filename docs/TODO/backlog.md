# Backlog

Every open item about the backend, the API, jobs, scheduled scripts and
consistency is here, once. It replaces the reliability roadmap, the jobs
follow-ups, the backend audit and the consistency backlog; their history is in
git. What is built is described in [the docs](../README.md), not here.

How to use it: take an item, fix it with a test, run the Make target
`AGENTS.md` asks for, and delete the item. Add a new item in the section it
belongs to, in the same shape.

Each item says how sure we are:

- **checked**: the code was read and the problem follows from it.
- **reported**: a scan or a review said so and nobody has re-read the code.
- **not verified**: seen once in a review or a test run and not explained.
- **needs a host**: only a real machine can confirm it.

The sections are in the order to work them. A number in brackets, such as
(audit 134), is the item's number in the old backend audit, for looking it up
in git.

## 1. File browser: root follows links a user planted

When an admin uses the file browser, the work runs as root. A program running
as an ordinary user can swap one of that user's folders for a symlink, and root
then acts on the folder the link points to. Delete is already safe: it opens
the folder with `openDirNoUserLinks` and removes through `removeAllAt`
(`backend/bridge/handlers/filebrowser/services/handover.go`). The items below
need the same treatment. All were re-read on 2026-11-13.

- [ ] **Copy, move, rename, chmod and chown follow a swapped link** (audit 134,
  high). `services/file_operations.go` passes the full path of every entry to
  `os.Root`, which follows relative symlinks in the folders on the way. With a
  subfolder swapped for a link to `../../../../etc` mid-operation: a recursive
  chown hands the matching names there to the uid the admin picked, a recursive
  chmod sets their mode, a move across filesystems deletes them, and a copy
  creates files there with the source's owner and mode, setuid included. These
  run in the root job host. Checked.
  - Fix: open the source and destination parents with `openDirNoUserLinks`,
    then work through handles: `openat` with `O_NOFOLLOW` per level,
    `fchownat`/`fchmod` on the entry, `renameat2`/`linkat` between folder
    handles, `removeAllAt` for the source of a move and for the aside.
  - After the fix: a normal operation is unchanged; one through a link the user
    could have planted is refused with 403.
- [ ] **Compress and extract have the same problem.** Compress creates its
  output by path and then sets its mode; extract makes and opens the
  destination by path, then keeps the entries inside it
  (`services/archive_operations.go`). Reported 2026-11-13.
- [ ] **A save with an owner follows links in the folder path** (audit 131).
  An upload that sets `owner` (the Compose save) refuses a symlink at the final
  name, but `openUploadDir` (`upload_owner.go`) opens the deepest existing
  folder by path through `services.OpenNearestDir`, which follows links. A
  user's process can replace `~/docker/web` with a link to another folder root
  can write and redirect an admin's save there. The later same-file check
  catches a swap made after the upload was prepared, not one made before.
  Callers: `upload_operation.go` (`prepare`) and `upload_batch_operation.go`
  (`ensurePrepared` and folder creation). These run in the root bridge.
  Checked.
  - Fix: for a save with an owner, refuse a symlink in any path component
    whose parent folder the session user can write. Links in folders the user
    cannot write (`/home -> /var/home`) keep working.
- [ ] **A trash restore goes where the user-editable `.trashinfo` says**
  (audit 132). `RestoreFromTrash` (`services/trash.go`) reads the destination
  from `Path=`, which the user can edit. So an item an admin trashed from
  `/etc` can be sent to a different system folder. The destination folder is
  also opened by path, so a planted link is followed there too. Runs in the
  root job host. Checked.
  - Fix: record the original location root-side when the item is trashed and
    restore only to it, or ask before a restore into a system folder.
- [ ] **A Compose stack saved by a sudo user got a folder owned by root.** The
  stack folder was root's, mode `0700`, inside that user's home
  (`backend/bridge/handlers/docker/background_operations.go`). Not verified.
- [ ] **Nothing works below a folder link with an absolute target**
  (audit 36). A link such as `/home/u/data -> /mnt/data` is refused by Go's
  `os.Root` for any path that goes through it, and `iteminfo.ResolveSymlinksAt`
  resolves only the last name. Checked in the Go 1.27.4 source; needs a host
  to reproduce first.
  - Fix: resolve the parent folders at the points where a path enters, and run
    `CheckNotProtected` on the result. Large: 37 `fsroot.Open` call sites, and
    a mistake here lets a path escape.

## 2. Admin-only routes

Decided 2026-11-13: changing services, network, hostname, time, reboot and
power-off is admin-only. A route that changes the host is `Privileged()` unless
it is on a short list of named exceptions. Today nothing checks that, and
[Privilege Pattern](../privilege_pattern.md) still describes these routes as
undecided until this lands.

- [ ] **Declare these 25 routes `Privileged()`.** Listed by reading every
  binding on 2026-11-13:
  - `systemd.start_service`, `stop_service`, `restart_service`,
    `reload_service`, `enable_service`, `disable_service`, `mask_service`,
    `unmask_service`, `reset_failed_service`
  - `network.enable_connection`, `disable_connection`, `set_ipv4`,
    `set_ipv4_manual`, `set_ipv6`, `set_mtu`, `set_optional`
  - `hostname.set_hostname`
  - `datetime.set_ntp`, `set_ntp_servers`, `set_server_time`, `set_timezone`
  - `control.reboot`, `control.power_off`
  - `container.open`, `terminal.list_shells` (they start processes in a
    container through the Docker socket)

  After the change a user without sudo gets a 403 before anything runs, and
  loses whatever the host's own rules (polkit) let them do today.
- [ ] **Hide or disable those controls for a user without sudo.** The page
  must not offer what the server will refuse. The same gap shows today: the
  dashboard's CPU graph calls `monitoring.get_cpu_history` with no check, and
  a non-sudo bridge logged `forbidden: privileged route` for it, for the
  network and disk histories, and for `filebrowser.subfolders`
  (`frontend/src/routes/_authenticated/-dashboard/ProcessorGraph.tsx`). Not
  verified.
- [ ] **Write down the exceptions.** These change something and stay open to
  every user, each with its reason: the `filebrowser.*` writes (the user's own
  files), the `assistant.*` routes (the user's own `claude`), `terminal.open`
  (the user's own shell), `control.logoff`, and `tasks.cancel`, `tasks.watch`,
  `tasks.data` and `tasks.events` (the user's own jobs).
- [ ] **Decide these, then flag them or list them as exceptions.** Not
  re-read: `config.set`, `config.set_ui`, `accounts.terminate_session`,
  `system.dismiss_failed_login_alert`, `system.dismiss_unclean_shutdown`,
  `docker.check_updates`, `docker.check_container_update`, `docker.get_icon`,
  `docker.get_icon_uri`, `network.get_network_info`,
  `system.get_health_summary`.
- [ ] **Add the test that keeps it true.** Every `Mutation`, `MutationVoid`,
  `MutationTask`, `DuplexRoute` and every `Call` without `RetrySafe()` is
  `Privileged()` or is in the exceptions list with a reason. It replaces the
  two tests that check routes by name in
  `backend/bridge/apischema/schema_test.go`. The same walk over every binding
  can check the inventory of `Resumable` and `BrowserBound` flags that the jobs
  design asked for and nobody wrote.
- [ ] **A caller can cut their own connection.** `systemd.stop_service` and
  `systemd.restart_service` accept LinuxIO's own units, and the network routes
  accept the interface that carries the session. No check was found in
  `backend/bridge/handlers/systemd` on 2026-11-13. Reported.

## 3. Storage safety

Each item here is data-safety work: it starts from what the kernel and
btrfs-progs do, gets an independent review, and adds a root test on loop
devices (`make test-btrfs-integration`, and `make vm-test-btrfs-integration`
after a change that issues ioctls).

- [ ] **"Is this disk in use?" is decided three ways.** `deviceInUse`
  (`backend/bridge/handlers/storage/partitions.go`) checks busy devices,
  mounted and unmounted btrfs pools and `/etc/fstab`. `checkBtrfsDeviceFree`
  (`btrfs_device_operations.go`) and `checkMDDeviceFree` (`mdraid.go`) check
  mountpoints, holders and signatures. With `force`, the btrfs check skips the
  signature and does not look at unmounted pools, so a disk that belongs to an
  unmounted pool can be taken. Checked.
  - Decided 2026-11-13: `force` only means "ignore old data left on this
    disk". It never takes a disk from an existing pool, mounted or not. Use
    one function for all three, and say in the dialog what `force` does.
  - Still to settle in the design: how to tell leftover data from a member of
    a pool that is only unmounted. Start from what `deviceInUse` does.
- [ ] **Replication blocks snapshots** (audit 33). Both run in the hourly
  snapshot service (`replicateJobs` in `backend/btrfs-timer/snapshot.go`), so
  during a first send that takes hours the timer's runs are skipped. Reported.
  - Decided 2026-11-13: replication gets its own service, started when the
    snapshot pass finishes. No second timer. systemd already skips a start
    while the last send still runs.
  - Comes with it: the units are rendered at run time (`RenderUnit`,
    `RenderTimer` in `backend/common/btrfs/unit.go`, written by
    `syncBtrfsTimer`), so an existing install gets the new unit on the next
    rule or job save; the new unit needs its own run record
    ([Jobs](../jobs.md#scheduled-runs)) and its place in the installer's
    drop-in list.
  - In the same code: the snapshot pass's summary can read
    `replicated=0 sent=1` when recording a job's outcome fails after its send.
- [ ] **A scrub starts over after a power cut.** It is checkpointed only by
  its `Suspend` hook at an orderly stop
  (`backend/bridge/handlers/storage/btrfs_device_operations.go`). Checked.
- [ ] **No cancel for a device removal or a replace on the page.** The Devices
  card cancels a running balance only
  (`frontend/src/routes/_authenticated/storage/-components/btrfs/BtrfsDevicesCard.tsx`).
  The backend can cancel a removal in the kernel. Whether the tray lists these
  jobs with a cancel: not verified.
- [ ] **Listing pools changes things.** `storage.btrfs_list` raises alerts and
  turns on the health timer (`ListBtrfsFilesystems` in
  `backend/bridge/handlers/storage/btrfs_subvolumes.go`). Checked. Reported and
  not re-read: the subvolume, snapshot and settings reads mount a pool's top
  level under `/run/linuxio/btrfs/`, after which `pool_destroy` refuses the
  pool.
- [ ] **A scrub might end by itself.** The kernel stops a scrub on any signal
  to its thread, and nothing in `backend/common/btrfs` retries `EINTR`. An
  inference; never observed. Needs a test.

## 4. Other likely bugs

- [ ] **Live metrics redaction names each section by hand.**
  `redactLiveSections` in `backend/monitoring/internal/httpapi/server.go`. A
  section added to `backend/monitoring/api/live.go` and missed there is sent
  to a listener that is not allowed it. Function checked; that the test covers
  six of nine sections is reported.
  - Add the test with the fix: for every member of `monitoringapi.Live`, a
    listener that is not permitted its plugin receives it empty. Walk the
    struct by reflection so a new member cannot be missed.
- [ ] **Upload chunk size above 16 MiB.** The setting allows 32
  (`ValidChunkSizeMB` in `backend/bridge/internal/userconfig/rules.go`), but a
  relay frame carries at most 16 MiB (`backend/common/ipc/relay/protocol.go`)
  and the frontend sends a chunk as one frame. A setting of 17 to 32 should
  fail. Constants checked; not reproduced.
- [ ] **Bad settings answer 500, not 400.** The `apply_*.go` files in
  `backend/bridge/handlers/config` return plain errors for bad values.
  Reported.
- [ ] **Icon cache folders lose their mode.**
  `packaging/systemd/linuxio-tmpfiles.conf` creates them `1777`;
  `backend/bridge/handlers/docker/icons.go` recreates them `0755` after a
  cache clear, after which a non-root bridge probably cannot cache icons.
  Reported; the effect is inferred.

## 5. Rules written twice that disagree

- [ ] **VM names.** The server requires a leading letter or digit, at most 63
  characters and no `..` (`backend/bridge/handlers/virt/validation.go`); the
  create and edit dialogs under
  `frontend/src/routes/_authenticated/vm/-components` accept any run of the
  allowed characters. Server side checked.
- [ ] **Docker address pools.** The server accepts any CIDR
  (`backend/bridge/handlers/docker/daemon_config.go`); the settings form
  accepts IPv4 only. Checked.
- [ ] **`linuxio logs` unit list.** `backend/cli/journal.go` names the units by
  hand and misses the jobs and btrfs units in `packaging/systemd`. Checked.
  Reported: the same five programs are also listed in `backend/cli/version.go`
  and `backend/webserver/appupdate/version.go`, which leaves out monitoring.
- [ ] **Login error codes.** `backend/webserver/auth/auth.go` sends
  `too_many_requests`; `frontend/src/api/webserver.ts` does not list it.
  Checked.
- [ ] **Container state and health colours.** Decided in five frontend files
  (`constants/statusColors.ts`, `utils/dockerContainer.ts`, the dashboard's
  `Docker.tsx` and two under `docker/-components`). Unhealthy is an error in
  one and a warning in another; dead is neutral or an error. Checked.
- [ ] **Indexer status words.** Plain strings in
  `backend/indexer/internal/daemon` and `backend/bridge/handlers/indexer`; the
  bridge tests for `indexing` and `failed`, which the daemon was not found
  sending. Checked.
- [ ] **Theme colour defaults.** `frontend/src/theme/variants.ts` repeats the
  table in `backend/bridge/internal/userconfig/themecolors.go` and differs on
  the code background. Checked.
- [ ] **Add the test that keeps allowed values true.** Every
  `apischema.StringEnums` entry is built from the Go definition it describes,
  or a test compares the two. Today only `CapabilityName`, `WireGuardHookKind`
  and `ThemeColorKey` are. The others retype Go constants: dock tile colours,
  navigation mode, theme, view mode, task state, storage mode, container
  metrics status, network bridge handoff state, auto-update backend and
  notify, VM image presets and firmware.

## 6. Small jobs fixes

- [ ] The scrub banner prints the percentage unrounded, such as
  `18.986434994665448%` (`BtrfsHealthBanner.tsx`).
- [ ] The job host's recorder writes progress files while it holds its mutex
  (`backend/bridge/internal/jobshost/recorder.go`, `writeProgressLocked`).
- [ ] `Store.Finish` on a record that is already finished rewrites the file
  unchanged (`backend/common/jobs/store.go`, `mutate`).
- [ ] Store reads check a file with `Lstat` and then open it, in two steps
  (`backend/common/jobs/store.go`, `openRegularFile`). The folders belong to
  root or to the uid itself.
- [ ] A user's host records the owner's username from the bridge's hello
  rather than its own account (`jobshost/host.go`, `recordOwner`).
- [ ] A resend is compared byte for byte, so a client that reorders a
  request's keys gets a 409; the browser resends identical bytes
  (`jobshost/host.go`, `sameJSON`).
- [ ] A schedule deleted while `notifications.json` does not parse logs "could
  not clear the notification choice" even when it had none
  (`backend/bridge/handlers/schedules/manager.go`, `forgetNotifyChoice`).
- [ ] Saving only a schedule's notification choice skips the re-apply that
  repairs an incomplete schedule; a second save repairs it. `changeLocked`
  takes six positional parameters (same file).
- [ ] "Mark all seen" lists the alerts and marks them under separate locks,
  and `alerts.mark_seen` with explicit IDs is not filtered by the levels grid
  (`backend/bridge/handlers/alerts/operations.go`, `markSeenForUID`).
- [ ] Docker update alerts share one source, so a container named
  `updated-<x>` shares its failure key with the "updated" notice of a
  container named `<x>` (`backend/common/dockerupdate/alerts.go`).
- [ ] A bare `\r` in an alert message is not normalised in the email body
  (`backend/common/alerts/deliver.go`).
- [ ] On the notification settings page the *Scheduled scripts* help line is
  not tied to its select for assistive technology, and the page's warnings use
  `role="alert"`, which `AppAlert` always sets.
- [ ] A user without sudo who signs in while an update waits loses that
  sign-in's notice: the history answers empty and the notice takes the sign-in
  mark anyway (`useAwayNotice.ts`).
- [ ] **Release notes.** The release that ships jobs should state three
  behaviour changes: a stored Docker `never` no longer silences failures; an
  interruption by a shutdown is a warning, so "Errors only" no longer emails it
  unless *Warnings* is ticked for *Email*; and a save from the settings page
  over an unreadable `notifications.json` drops every schedule's choice and the
  SMTP password and keeps the old file as `notifications.json.unreadable`
  ([Alerts](../alerts.md#notification-settings)). Release notes are not kept
  in the repository, so this cannot be checked here.

## 7. Tests that are missing

- [ ] A relay speed test: a no-op mutation through the relay many times, with
  a bound on the median. The jobs design asked for it. No baseline numbers
  exist either for call latency, job start, alert listing or schedule status.
- [ ] The relay tests in `backend/bridge/cmd/jobs_relay_test.go` skip when run
  as root, and a privileged login skipping the host start has no hermetic test
  (`backend/auth/linuxio-auth-pam_test.c`).
- [ ] Not run in the test VM (`frontend/src/test/vm/jobs-phase5a.spec.ts`): a
  real update from a release that has job hosts, more than one waiting job, a
  user's host as the older host, `tasks.watch`, `tasks.cancel` and
  `tasks.output` during the wait, and uploads during it.

## 8. Needs a real machine or a measurement

- [ ] **Scheduled scripts.** A run that passes its timeout, catching up a
  missed run (`Persistent=`), journal retention over several days, and a
  bridge restart during a run. See
  [Scheduled Scripts](../scheduled-scripts.md#testing).
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
- [ ] **Seen once and not explained:**
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
- [ ] **What happens after a route cuts its own connection is not written
  down.** The old roadmap's rule was: send it once; after reconnect, reboot
  compares the boot identity, a network change reads back the interface, and
  power-off reports "acknowledged" or "unknown", never success just because the
  host went quiet. Whether the frontend does this was not checked. Check it,
  then describe it in [API Contract](../api-contract.md).

## 9. One thing named in many places

Nothing fails today. Fix one when a miss would be silent or the set keeps
growing. A fixed set of three or four that the build already guards is not
worth a table. Reported unless it says checked.

Defaults that cannot be unset:

- [ ] **Every UI default is written to `ui.yaml` on the first save.**
  `config.set_ui` starts from `DefaultUIPreferences()` and stores the whole
  value, so a later change to a backend default never reaches that user. Only
  view modes are stored as differences (`normalizeViewModesForDefault` in
  `backend/bridge/internal/userconfig/validator.go`). Do the same for the
  rest.
- [ ] **Upload chunk size.** Documented as "0 means default", but
  `userconfig/settings.go` writes 1 to a new file, so the frontend fallback in
  `frontend/src/hooks/useUploadChunkSize.ts` runs only after an explicit 0.
  Backend checked.
- [ ] **Fallbacks that cannot run.** Remove them: the primary and secondary
  fallbacks in `frontend/src/theme/appTheme.ts` and `theme/colors.ts`, view
  mode and storage mode in `hooks/useConfig.ts`, the dock gradient range in
  `dockPalette.ts`, and the alternative field names probed in
  `NetworkInterfaceEditor.tsx`, none of which the backend sends. Network
  editor checked.
- [ ] **Summary falls back from stats to a mirror of the same stats.**
  `backend/monitoring/internal/domain/system/summary.go`.

Backend sets:

- [ ] **System metrics.** About forty, each named in roughly ten places under
  `backend/monitoring`. A metric left out of the averaging in
  `internal/plugins/aggregate.go` is silent.
- [ ] **Monitoring plugins.** Sixteen, with attributes spread over four files.
  A plugin missing from the collection groups is dropped silently.
- [ ] **btrfs profiles.** Five tables in `backend/common/btrfs` and three in
  `btrfsShared.ts`. A profile missing from `MinDevices` returns 0, so any
  device count passes validation. Checked to agree today.
- [ ] **Job settings.** Five fields in eight places across
  `backend/bridge/internal/userconfig`, `backend/bridge/apischema/config.go`
  and `backend/bridge/handlers/config`.
- [ ] **Storage and packages register routes from a second list.**
  `storage/handlers.go` declares the route sets and
  `storage/smart_test_operation.go` registers them; a set in one and not the
  other is a route that answers "not found". Checked for storage.
- [ ] **Docker daemon keys.** Seven keys in read, write and two change lists
  in `docker/daemon_config.go`; a key missing from the change lists is saved
  only when another key changes.
- [ ] **Smaller sets.** Indexer status fields, alert notification settings,
  journal filters, archive formats, compose file names, UnitInfo properties,
  GPU collector sources.

Frontend sets:

- [ ] **The UI settings save payload.** `buildUIPayload` in
  `frontend/src/contexts/ConfigProvider.tsx` names each setting, the payload
  type is all-optional, and the save replaces everything, so a setting left
  out is reset on every save. Checked.
- [ ] **Monitoring settings form.** Four durations in eight or more places in
  `MonitoringSettingsSection.tsx`.
- [ ] **Smaller sets.** Transfer kinds, palette roles, editor languages,
  syslog priorities, Docker prune kinds, the accounts toast constant defined
  in eight dialogs (checked), and view-mode surface ids typed in both a page
  and its list.

The same rule on both sides, in step today. Most become generated once the
value is a named type or a response field:

- [ ] Notification kinds, levels and report choices
  (`backend/common/alerts/settings.go`).
- [ ] btrfs compression algorithms (`backend/common/btrfs/settings.go`).
- [ ] Filesystem label limits and minimum sizes
  (`backend/common/btrfs/format.go` and the partition dialogs).
- [ ] VM image presets, their disk minimums and the managed storage paths.
- [ ] Scheduled-run kinds (`backend/common/scheduledrun/kind.go`,
  `frontend/src/tasks/scheduledRuns.ts`).
- [ ] Docker auto-update defaults.
- [ ] The stream flags and opcodes of the binary protocol
  (`backend/webserver/ws/ws.go`, `backend/common/ipc/relay/protocol.go`,
  `frontend/src/api/StreamMultiplexer.ts`).
- [ ] The Claude relay limits and environment names.
- [ ] Installer and Go: the run-record drop-in units and the socket paths in
  `packaging/systemd`.

The frontend was sampled in the scan that found these, 20 to 25 percent of its
files, so more of the same is likely there. Theme colours, host capabilities,
UI section toggles, Docker scheduled-update options and WireGuard hook kinds
already have one definition.

## 10. Later, no plan yet

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

Listed so they are not raised again.

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
