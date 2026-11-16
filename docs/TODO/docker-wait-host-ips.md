# Docker: wait for published host IPs

## Status

Not built. This is the agreed plan for a second opt-in Docker startup setting,
next to "Wait for folder mounts". Nothing below exists in the code yet.

## Problem

A container that publishes a port on a specific host IP (for example
`192.168.1.66:53:53/udp`) fails at boot with "cannot assign requested address"
when Docker starts before that IP is assigned, and the container stays down.
`network-online.target` does not prevent it: on netplan hosts the generator can
run before the NIC exists, so wait-online never waits for that interface. The
"Wait for folder mounts" setting adds `After=network-online.target`, which does
not help either.

## What it does

When the setting is on, LinuxIO writes
`/etc/systemd/system/docker.service.d/linuxio-wait-host-ips.conf` and runs
daemon-reload. The file makes `docker.service` wait, with a time limit, until
each configured host IP is assigned:

```ini
# Managed by LinuxIO. Do not edit by hand.
[Service]
ExecStartPre=-/usr/bin/timeout 30 /bin/sh -c 'until /usr/sbin/ip -o addr show to 192.168.1.66 -tentative | grep -q .; do sleep 0.5; done'
```

There is one `ExecStartPre` line per IP. When the setting is off the file is
removed; a missing file is not an error. A user who never turns the setting on
sees no change.

Why the line is built this way:

- **It waits for the address, not for `systemd-networkd-wait-online`.** That
  works with networkd and NetworkManager, and keeps working if an interface is
  renamed.
- **`timeout 30` plus the `-` prefix.** Docker must still start if an IP never
  appears. A timeout inside `ExecStartPre` counts against `TimeoutStartSec`,
  and `-` does not ignore timeouts, so the limit has to be inside the command.
- **`-tentative`.** An IPv6 address still in duplicate-address detection does
  not count, because binding to it fails.
- **Only the validated IP is interpolated.** It cannot contain `$`, `%`,
  quotes, spaces or newlines; `net/netip` validation guarantees that and a test
  proves it.

## Decisions

1. **IP addresses only.** Waiting for a network card instead was considered:
   it covers a later IP on the same card without editing the list, but the
   card has to be picked by name, a rename breaks it, and a card name is a
   second, free-text value in the shell line. It can be added later as a field
   beside the IP list.
2. **Toggle and list in their own block, one Save.** The block sits below the
   Docker Folders block. The toggle and the list are a draft and Save sends
   both fields together. Switching off and saving keeps the stored list.
3. **The shell line above**, not a `linuxio` verb. It needs `timeout`, `sh`,
   `ip`, `grep` and `sleep` on the host and allows 30 s per IP, but it does not
   depend on LinuxIO staying installed.
4. **Both fields always need a privileged session.** The folders rule is
   looser (folders need it only while the mount setting is on); the IP list
   has no use except the root-written drop-in.
5. **On with an empty list is refused before the config is saved** (400), not
   only in the sync. The sync runs after the config is on disk, so checking
   only there would leave "on, no IPs" stored.
6. **IPv4-mapped IPv6 is unmapped first.** `::ffff:1.2.3.4` is stored as
   `1.2.3.4`, so `::ffff:127.0.0.1` is caught as loopback and
   `ip addr show to` gets a form it can match.
7. **Tool paths come from `hostcmd.LookPath`**
   (`backend/common/hostcmd/lookpath.go`) for `timeout`, `sh` and `ip` when the
   drop-in is rendered, through a package variable tests replace. `grep` and
   `sleep` stay bare (systemd's default PATH). A missing tool fails the sync
   with an error naming it.
8. **Suggestions read `HostConfig.PortBindings` from an inspect of every
   container**, running or not. The container list's `Ports` is empty for a
   container that failed to start, which is the case this feature is for.

## Backend

Model everything on `RequireMountsForFolders`
(`backend/bridge/handlers/docker/daemon_mount_ordering.go`).

`backend/bridge/internal/userconfig`:

- [ ] `types.go`: add `WaitForHostIPs bool` (`waitForHostIPs`) and
  `HostIPs []string` (`hostIPs`) to `Docker`. Both default to off and empty;
  a config without them loads unchanged.
- [ ] `rules.go`: `ParseDockerHostIPs(values []string) ([]string, error)`
  beside `ParseDockerFolders`: trim, `netip.ParseAddr`, reject empty, zone,
  unspecified, loopback and duplicates (compared after `Unmap()` and the
  canonical `String()`). An empty input list is valid.
- [ ] `validator.go` `validateSettings`: validate `HostIPs` and reject
  on-with-empty.
- [ ] `store_clone.go` `cloneDocker`: clone `HostIPs`.

`backend/bridge/apischema`:

- [ ] `config.go` `ConfigDockerPayload`: `WaitForHostIPs *bool` (`omitzero`)
  and `HostIPs []string` (`omitempty`; a non-nil empty list clears it).
- [ ] `docker.go` `DockerSettings`: `WaitForHostIPs bool` and
  `HostIPs []string` (always a non-nil array).

`backend/bridge/handlers/config`:

- [ ] `apply_docker.go`: parse `HostIPs` (wrap `ErrInvalidArgs`), apply the
  bool, then reject on-with-empty, with no partial apply on error.
- [ ] `contracts.go` `appConfigToAPI`: map both fields.
- [ ] `config_operations.go`: extend `requireDockerMountOrderingPrivilege` so
  either new field needs a privileged session (403). Add
  `shouldSyncDockerHostIPWait`: never in memory mode; true when
  `WaitForHostIPs` is in the payload, or `HostIPs` is in the payload while the
  setting is on. Call `docker.SyncDaemonHostIPWait(ctx, updated.Docker)` after
  the mount-ordering sync, with the error wrapped as
  `sync docker service host IP wait: %w`.

`backend/bridge/handlers/docker`:

- [ ] New `daemon_host_ip_wait.go`, reusing `ensureSystemdDropInDir`,
  `atomicfile.Write`, `systemdDaemonReload` and the missing-file-is-fine
  remove. `SyncDaemonHostIPWait(ctx, docker)` calls
  `syncDaemonHostIPWait(ctx, path, docker)` with the path injected, as
  `applyDaemonStorage` does. `renderDockerHostIPWaitDropIn(ips)` re-parses
  with `ParseDockerHostIPs` and errors on an empty list. No log line of its
  own, like `SyncDaemonMountOrdering`.
- [ ] `ListPublishedHostIPs(ctx)` and a pure
  `publishedHostIPs([]container.InspectResponse) []string`: skip invalid,
  unspecified, loopback and zoned addresses, unmap, de-duplicate, sort.
- [ ] `handlers.go`: `docker.list_published_host_ips`, a `Call` from
  `NoRequest` to `[]string` with `RetrySafe()`, unprivileged like
  `docker.list_containers`, which already shows the same host IPs.
- [ ] `backend/bridge/handlers/import_boundary_test.go`: reword the
  `config → docker` edge reason to cover both startup drop-ins.

Then `make generate`. `frontend/src/api/generated/*` is never edited by hand.

## Frontend

Page layout after the change:

```text
Docker Folders
  /srv/docker
  + Add Docker folder
  [Wait for folder mounts]            toggle (saves at once, as today)
  [Reset] [Save]

[Wait for published host IPs]         toggle (draft)
  192.168.1.66                              x
  + Add IP address
  Detected, not in the list: 192.168.1.67  [Add]
  [Reset] [Save]
```

- [ ] New `hostIp.ts` in the frontend `utils` folder: `validateHostIPs(drafts)` returns
  the canonical values and per-row errors with the backend's rules (strict
  dotted-quad IPv4; IPv6 through the URL parser's `[addr]` form, which also
  canonicalises; no zone, not unspecified, not loopback, no duplicates). The
  backend stays authoritative and its message is shown for anything this lets
  through.
- [ ] New `DockerHostIPWaitSetting.tsx` beside `DockerSettingsSection.tsx`
  (`frontend/src/routes/_authenticated/settings/-components/docker/`):
  - Toggle card styled like the mounts toggle (`FrostedCard`,
    `cardBodyToggleProps`, `AppSwitch`), disabled and dimmed when the session
    is not privileged. Title **Wait for published host IPs**. Description:
    *Wait until the IP addresses that containers publish ports on are assigned
    before Docker starts.*
  - While the draft toggle is on: one row per IP with a text field and per-row
    error text, a remove button and an "Add IP address" card, matching the
    folder rows.
  - One Reset/Save row for the toggle and the list, through `useSettingsDraft`
    with the draft `{ enabled, hostIPs }`. Save calls
    `updateConfig({ docker: { waitForHostIPs, hostIPs } })` with the same toast
    and error handling as the folder save. On with no valid IP shows a row
    error and does not save.
  - Suggestions come from the generated `linuxio.docker.list_published_host_ips`
    query, fetched only while the draft toggle is on. With an empty stored list
    the draft is pre-filled from them, derived and not copied into state by an
    Effect. Detected IPs missing from the list are offered as a one-click Add.
    Nothing is stored until Save.
- [ ] `DockerSettingsSection.tsx`: move the "Add Docker folder" card directly
  under the folder rows, and render the new component after the folders'
  Reset/Save row, before the daemon section.
- [ ] Fixtures: add the two fields in `frontend/src/test/render.tsx` and
  `frontend/src/contexts/ConfigProvider.test.tsx`, and assert them in
  `frontend/src/hooks/useConfig.test.tsx`.

## Tests

Backend, table-driven, following `daemon_mount_ordering_test.go`:

- [ ] `daemon_host_ip_wait_test.go`: exact file content for one IPv4, one IPv6
  and several IPs with stubbed tool paths; a failed tool lookup; sync off
  removes the file and is a no-op, with no daemon-reload, when it is missing;
  sync on writes mode 0644 in `t.TempDir()`, never real `/etc`; on with an
  empty list errors; `publishedHostIPs` filtering from fake inspect results.
- [ ] `userconfig/rules_test.go`: rejects `0.0.0.0`, `::`, `127.0.0.1`, `::1`,
  `::ffff:127.0.0.1`, `fe80::1%eth0`, `""`, duplicates (including
  `2001:DB8::1` against `2001:db8:0::1`) and `1.2.3.4; rm -rf /`. Every
  accepted canonical value matches `^[0-9a-f:.]+$`, which is the proof that
  nothing interpolated carries `$`, `%`, quotes, spaces or newlines.
- [ ] `userconfig/settings_test.go`: round-trip with the new fields; an old
  config without them loads unchanged with both off and empty; on-with-empty
  on disk is a semantic failure.
- [ ] `config/apply_settings_test.go`, `config_operations_test.go` and
  `contracts_test.go`: apply and normalisation; on-with-empty is
  `ErrInvalidArgs` with no partial apply; the privilege (403) and should-sync
  cases; the API mapping returns a non-nil empty array.
- [ ] A `docker` handler test for the new binding: unprivileged and
  retry-safe.

Frontend:

- [ ] `hostIp.test.ts`.
- [ ] `DockerHostIPWaitSetting.test.tsx`: off hides the list; switching on
  with an empty stored list pre-fills from the suggestions and Save sends both
  fields; a detected IP not in the list can be added; an invalid row shows its
  error and does not save; switching off and saving sends
  `waitForHostIPs: false` only; a session that is not privileged gets a
  disabled block.

## Docs to update when it lands

- [ ] [Config storage](../config-storage.md): the two fields, the
  on-with-empty rule, and a short part on the Docker startup drop-ins, with
  the reasons from "What it does" above.
- [ ] [Privilege pattern](../privilege_pattern.md): `config.set` refuses three
  root-only Docker fields in the handler, not one.
- [ ] [Bridge handler patterns](../bridge_handler_patterns.md): the wording of
  the `config → docker` edge.
- [ ] Delete this file and its line in [TODO](./README.md).

## Backlog items to add when it lands

- IPs wait one after another, so N missing IPs delay Docker by up to
  N × 30 s.
- Waiting for a network card instead of an IP is not built.
- The setting is per user while the drop-in is host-wide, so the last save
  wins. The mount setting has the same limit.
- Boot behaviour on a real netplan host is not covered by an automated test.

## Verification

- Iterate with `make test-go GO_TEST_PKGS=./bridge/handlers/docker/...` (and
  `./bridge/handlers/config/...`, `./bridge/internal/userconfig/...`) and the
  focused frontend target with `VITEST_FILE=…`. Run one Make at a time.
- `make generate`, then `make test-quiet` and `make check-docs`.
- A privilege-boundary review of the diff, because it changes
  `backend/bridge/handlers`.
- On a real host, as root: turn the setting on, check
  `systemctl cat docker.service` and `systemd-analyze verify docker.service`,
  then reboot with the IP assigned late and confirm the container starts. A
  hand-written drop-in that does the same job is not touched by LinuxIO and is
  removed by hand afterwards.
