# Consistency backlog: one definition, checked rules

A read-only scan of the whole repository in November 2026 looked for three
things: one kind of item named in many places, a default that makes "unset"
unreachable, and the same rule or constant defined independently in two
places. This is what it found and what is still open. The rules it measures
against are in [API Contract](../api-contract.md) (One Definition, Checked
Rules).

Each item says how it was confirmed: **checked** means both sites were read
again after the scan; **reported** means the scanner read them and nobody
has re-read them since. The Go backend was read largely in full. The
frontend was sampled: each scanner opened 20 to 25 percent of its files, so
more of the same is likely there.

Work from the top. Sections 1 and 2 are defects; the rest is latent.

## Guards to add

Three tests would have prevented most of sections 1 and 2, and keep working
after this list is closed. Each fails on the items below when first added,
so each lands with its fixes.

- [ ] **Privilege.** Every binding that can change the host (`Mutation`,
  `MutationVoid`, `MutationTask`, `DuplexRoute`, and any `Call` without
  `RetrySafe()`) is `Privileged()` or is named in an exceptions list with a
  reason. Replaces the listed-routes check in
  `backend/bridge/apischema/schema_test.go`. Needs the polkit decision in
  [Privilege Pattern](../privilege_pattern.md) written down as exceptions.
- [ ] **Metrics redaction.** For every member of `monitoringapi.Live`, a
  listener that is not permitted the member's plugin receives it empty.
  Walks the struct by reflection in the test, so a new member cannot be
  missed.
- [ ] **Allowed values.** Every `apischema.StringEnums` entry is built from
  the Go definition it describes, or a test compares the two. Today only
  `CapabilityName`, `WireGuardHookKind` and `ThemeColorKey` are.

## 1. Safety and likely bugs

- [ ] **`container.open` and `terminal.list_shells` are not `Privileged()`.**
  `backend/bridge/handlers/terminal/handlers.go`. Both start processes in a
  container through the Docker socket; the handler has no check of its own.
  A non-sudo session most likely fails at the socket instead of getting a
  403. Checked; not tried on a running host.
- [ ] **`datetime.set_ntp_servers` is not `Privileged()`.**
  `backend/bridge/handlers/datetime/handlers.go` binds it;
  `backend/bridge/handlers/datetime/datetime.go` writes the timesyncd or
  chrony configuration under `/etc`. Checked.
- [ ] **Live metrics redaction names each section by hand.**
  `redactLiveSections` in `backend/monitoring/internal/httpapi/server.go`.
  A section added to `backend/monitoring/api/live.go` and missed there is
  sent to a listener restricted to other plugins. Function checked; that
  the existing test covers six of the nine sections is reported.
- [ ] **"Is this device in use?" is decided three ways.** `deviceInUse` in
  `backend/bridge/handlers/storage/partitions.go` checks busy devices,
  mounted and unmounted btrfs pools and `/etc/fstab`;
  `checkBtrfsDeviceFree` in
  `backend/bridge/handlers/storage/btrfs_device_operations.go` and
  `checkMDDeviceFree` in `backend/bridge/handlers/storage/mdraid.go` check
  mountpoints, holders and signatures. With `force`, the btrfs check skips
  the signature and does not look at unmounted pools, so a member of an
  unmounted pool can be taken. Decide whether `force` means that, then use
  one function. Checked. Data-safety work: independent review and a root
  test, as `AGENTS.md` requires.
- [ ] **Upload chunk size above 16 MiB.** `ValidChunkSizeMB` in
  `backend/bridge/internal/userconfig/rules.go` allows 32 and
  `backend/webserver/ws/ws.go` sizes its read limit for that, but a relay
  frame carries at most 16 MiB (`backend/common/ipc/relay/protocol.go`) and
  the frontend sends a chunk as one frame. A setting of 17 to 32 should
  fail. Constants checked; not reproduced.
- [ ] **Config apply errors are not client errors.** The `apply_*.go` files
  in `backend/bridge/handlers/config` return plain errors for bad values, so
  the caller gets a 500 where a 400 is right. Reported.
- [ ] **Icon cache folders lose their mode.**
  `packaging/systemd/linuxio-tmpfiles.conf` creates them 1777;
  `backend/bridge/handlers/docker/icons.go` recreates them 0755 after a
  cache clear, after which an unprivileged bridge probably cannot cache
  icons. Lines reported; effect inferred.

## 2. Copies that already disagree

- [ ] **VM names.** `backend/bridge/handlers/virt/validation.go` requires a
  leading letter or digit, at most 63 characters and no `..`; the create and
  edit dialogs under `frontend/src/routes/_authenticated/vm/-components`
  accept any run of the allowed characters. Server side checked.
- [ ] **Docker address pools.**
  `backend/bridge/handlers/docker/daemon_config.go` accepts any CIDR; the
  settings form under
  `frontend/src/routes/_authenticated/settings/-components/docker` accepts
  IPv4 only. Checked.
- [ ] **`linuxio logs` unit list.** `backend/cli/journal.go` names the units
  by hand and misses the jobs and btrfs units in `packaging/systemd`.
  Checked. Reported: the same five programs are also listed in
  `backend/cli/version.go` and
  `backend/webserver/appupdate/version.go`, which leaves out monitoring.
- [ ] **Login error codes.** `backend/webserver/auth/auth.go` sends
  `too_many_requests`; `frontend/src/api/webserver.ts` does not list it.
  The login endpoint is outside the generator. Checked.
- [ ] **Container state and health colours.** Decided in
  `frontend/src/constants/statusColors.ts`,
  `frontend/src/utils/dockerContainer.ts`,
  `frontend/src/routes/_authenticated/-dashboard/Docker.tsx` and two files
  under `frontend/src/routes/_authenticated/docker/-components`. Unhealthy
  is an error in one and a warning in another; dead is neutral or an error.
  Checked.
- [ ] **Indexer status words.** Plain strings in
  `backend/indexer/internal/daemon` and in
  `backend/bridge/handlers/indexer`; the bridge tests for `indexing` and
  `failed`, which the daemon was not found sending.
  `backend/indexer/api/api.go` has constants for events only. Checked.
- [ ] **Theme colour defaults.** `frontend/src/theme/variants.ts` repeats
  the table in `backend/bridge/internal/userconfig/themecolors.go` and
  differs on the code background. Checked.

## 3. Defaults that defeat "unset"

- [ ] **Every UI default is written to `ui.yaml` on the first save.**
  `config.set_ui` starts from `DefaultUIPreferences()` and stores the whole
  value, so a later change to a backend default never reaches that user.
  Only view modes are stored as deviations
  (`normalizeViewModesForDefault` in
  `backend/bridge/internal/userconfig/validator.go`). Do the same for the
  rest. Reported; consistent with the measured merge-over-defaults read.
- [ ] **Upload chunk size.** Documented as "0 means default", but
  `backend/bridge/internal/userconfig/settings.go` writes 1 to a new file,
  so the frontend fallback in `frontend/src/hooks/useUploadChunkSize.ts`
  runs only after an explicit 0. Backend checked; frontend reported.
- [ ] **Fallbacks that cannot run.** Remove them:
  `frontend/src/theme/appTheme.ts` and `frontend/src/theme/colors.ts`
  (primary and secondary fallbacks), `frontend/src/hooks/useConfig.ts`
  (view mode and storage mode), the dock gradient range in
  `frontend/src/routes/_authenticated/-components/dock/dockPalette.ts`, and
  the alternative field names probed in
  `frontend/src/routes/_authenticated/network/-components/NetworkInterfaceEditor.tsx`,
  none of which the backend sends. Network editor checked; the rest
  reported.
- [ ] **Summary falls back from stats to a mirror of the same stats.**
  `backend/monitoring/internal/domain/system/summary.go`. Reported.

## 4. One kind of item named in many places

Fix where a miss is silent or the set grows. A fixed set of three or four
that the build already guards is not worth a table.

Backend:

- [ ] **System metrics.** About forty, each named in roughly ten places
  across `backend/monitoring/internal/domain/system`,
  `backend/monitoring/internal/plugins`, `backend/monitoring/api` and
  `backend/monitoring/internal/live`. A metric left out of the averaging in
  `backend/monitoring/internal/plugins/aggregate.go` is silent. Reported.
- [ ] **Monitoring plugins.** Sixteen, with attributes spread over
  `backend/monitoring/internal/plugins/plugins.go`,
  `backend/monitoring/internal/app/current.go`,
  `backend/monitoring/internal/plugins/payload.go` and
  `backend/monitoring/internal/httpapi/plugin.go`. A plugin missing from the
  collection groups is dropped silently. Reported.
- [ ] **`StringEnums` literals that retype Go constants.** Dock tile
  colours, navigation mode, theme, view mode, task state, storage mode,
  container metrics status, network bridge handoff state, auto-update
  backend and notify, VM image presets and firmware. See "Guards to add".
  Dock tile colours checked; the rest reported.
- [ ] **btrfs profiles.** Five tables in `backend/common/btrfs` and three in
  `frontend/src/routes/_authenticated/storage/-components/btrfs/btrfsShared.ts`.
  A profile missing from `MinDevices` returns 0, so any device count passes
  validation. Go and frontend tables checked to agree today.
- [ ] **Job settings.** Five fields in eight places across
  `backend/bridge/internal/userconfig`, `backend/bridge/apischema/config.go`
  and `backend/bridge/handlers/config`. Reported.
- [ ] **Storage and packages register routes from a second list.**
  `backend/bridge/handlers/storage/handlers.go` declares the route sets and
  `backend/bridge/handlers/storage/smart_test_operation.go` registers them;
  a set in one and not the other is a contract route that answers "not
  found". Checked for storage.
- [ ] **Docker daemon keys.** Seven keys in read, write and two change
  lists in `backend/bridge/handlers/docker/daemon_config.go`; a key missing
  from the change lists is saved only when another key changes. Reported.
- [ ] **Smaller sets.** Indexer status fields
  (`backend/bridge/handlers/indexer`), alert notification settings
  (`backend/common/alerts/settings.go`,
  `backend/bridge/apischema/alerts.go`), journal filters
  (`backend/bridge/apischema/logs.go`), archive formats
  (`backend/bridge/handlers/filebrowser`), compose file names, UnitInfo
  properties (`backend/bridge/handlers/systemd/unitinfo.go`), GPU collector
  sources (`backend/monitoring/internal/collector/gpu/manager.go`).
  Reported.

Frontend:

- [ ] **The UI settings save payload.** `buildUIPayload` in
  `frontend/src/contexts/ConfigProvider.tsx` names each setting, the
  payload type is all-optional, and the save replaces everything, so a
  setting left out is reset on every save. Checked.
- [ ] **Monitoring settings form.** Four durations in eight or more places
  in
  `frontend/src/routes/_authenticated/settings/-components/MonitoringSettingsSection.tsx`.
  Reported.
- [ ] **Smaller sets.** Transfer kinds (`frontend/src/tasks` and the
  notifications bell), palette roles (`frontend/src/theme` and
  `frontend/src/components/ui`), editor languages
  (`frontend/src/components/editor`), syslog priorities in the logs page,
  Docker prune kinds, the accounts toast constant defined in eight dialogs,
  and view-mode surface ids typed in both a page and its list. Accounts
  toast constant checked; the rest reported.

## 5. The same rule on both sides, in step today

Nothing fails if one side changes. Most become generated once the value is
a named type or a response field.

- [ ] Notification kinds, levels and report choices
  (`backend/common/alerts/settings.go`).
- [ ] btrfs compression algorithms (`backend/common/btrfs/settings.go`).
- [ ] Filesystem label limits and minimum sizes
  (`backend/common/btrfs/format.go` and the partition dialogs). Checked to
  agree.
- [ ] VM image presets, their disk minimums and the managed storage paths
  (`backend/bridge/handlers/virt/appliance_images.go`,
  `backend/bridge/handlers/virt/managed_storage.go`,
  `frontend/src/routes/_authenticated/vm/-components/vmShared.ts`).
- [ ] Scheduled-run kinds (`backend/common/scheduledrun/kind.go`,
  `frontend/src/tasks/scheduledRuns.ts`).
- [ ] Docker auto-update defaults (`backend/common/dockerupdate/config.go`
  and the settings state under
  `frontend/src/routes/_authenticated/settings/-components/docker`).
- [ ] The stream flags and opcodes of the binary protocol
  (`backend/webserver/ws/ws.go`, `backend/common/ipc/relay/protocol.go`,
  `frontend/src/api/StreamMultiplexer.ts`).
- [ ] The Claude relay limits and environment names
  (`backend/claude-relay/relay.go`,
  `backend/bridge/handlers/assistant/panes.go`).
- [ ] Installer and Go: the run-record drop-in units
  (`backend/common/scheduledrun/kind.go` and the install scripts under
  `packaging`), and the socket paths in `packaging/systemd`.

## Already done

Theme colours, host capabilities, UI section toggles, Docker scheduled-update
options and WireGuard hook kinds each have one definition. btrfs retention
buckets, the shared systemd unit properties and the monitoring duration
settings are down to a guarded few places; the reasons they stop there are
in their commits.
