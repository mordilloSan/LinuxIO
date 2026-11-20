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

## Network handoff

The VM bridge handoff ([VM bridge networking](../vm-bridge-networking.md),
Phase 2b) moves the host's addresses from a network card to a new bridge, so
anything that names the card stops matching. A hand-written drop-in that waited
on `enp2s0:routable` cost 30 s at every boot after the handoff put the address
on `br-enp2s0`. This setting has to survive the handoff with no rewrite:

- **The drop-in names addresses only**, never a card or a bridge, so the
  handoff does not touch it and there is nothing to rewrite on confirm or
  revert.
- **IPv4 always follows the handoff.** The handoff is confirmed only when every
  original IPv4 address is on the bridge (`handoffVerifiedAddresses`), and
  rolls back otherwise. Netplan handoffs hold every non-link-local IPv6
  address to the same check.
- **A NetworkManager handoff may change the host's dynamic global IPv6 address
  once.** A listed IPv6 address is then never assigned again: Docker waits the
  full 30 s at every boot and the container that publishes on it cannot bind
  either. LinuxIO does not rewrite the list, because it cannot tell which new
  address replaces the old one and the container's own port binding needs the
  same edit. The settings block shows it instead: every listed or detected IP
  that no host interface holds now is marked, and is never pre-filled.
- **The handoff bridge does not slow the wait.** NetworkManager handoff bridges
  are created with STP off, so the address is not held back by a forwarding
  delay; the wait needs the address assigned, not the port forwarding.

## Decisions

1. **IP addresses only, and no network card field later.** Waiting for a
   network card was considered: it covers a later IP on the same card without
   editing the list, but the card is picked by name, a card name is a second,
   free-text value in the shell line, and the bridge handoff moves the address
   off the card (see "Network handoff"). A card field would need the handoff
   to rewrite the drop-in inside the network manager's rollback window.
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
9. **An unassigned IP is marked, not refused.** Save accepts an IP that no
   interface holds now, because an address can be legitimately absent (a card
   that is down, a VPN that is not up yet). The mark is the only signal, and it
   is what catches an address a handoff left behind.

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
- [ ] `docker.go`: `DockerHostIPs` with `Published []string` (host IPs that
  containers publish ports on) and `Assigned []string` (addresses the host's
  interfaces hold now), both always non-nil arrays in the canonical form the
  config stores.

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
- [ ] `ListHostIPs(ctx)` and a pure
  `publishedHostIPs([]container.InspectResponse) []string`: skip invalid,
  unspecified, loopback and zoned addresses, unmap, de-duplicate, sort.
- [ ] A pure `assignedHostIPs([]net.Addr) []string` fed from
  `net.InterfaceAddrs()`, with the same filter plus link-local, so the two
  lists compare as plain strings. It reads the addresses and not the
  interfaces, so it is the same before and after a bridge handoff.
- [ ] `handlers.go`: `docker.list_host_ips`, a `Call` from `NoRequest` to
  `DockerHostIPs` with `RetrySafe()`, unprivileged like
  `docker.list_containers` and `network.get_network_info`, which already show
  the same addresses.
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
  2001:db8::66                              x
    Not assigned on this host now. Docker waits 30 s for it at every boot.
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
  - Suggestions come from the generated `linuxio.docker.list_host_ips` query,
    fetched only while the draft toggle is on. With an empty stored list the
    draft is pre-filled from the published IPs that are also assigned, derived
    and not copied into state by an Effect. Detected IPs missing from the list
    are offered as a one-click Add. Nothing is stored until Save.
  - A row whose valid IP is not in `assigned` shows *Not assigned on this host
    now. Docker waits 30 s for it at every boot.* as a warning under the row; it
    does not block Save. A detected IP that is not assigned carries the same
    note beside its Add. This is what shows an address that a bridge handoff
    replaced.
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
  empty list errors; `publishedHostIPs` filtering from fake inspect results;
  `assignedHostIPs` drops loopback and link-local, unmaps, and returns the
  same strings for an address whether a card or a bridge holds it; the
  rendered drop-in holds no interface name.
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
  fields; a detected IP not in the list can be added; a published IP that is
  not assigned is not pre-filled; a listed IP that is not assigned shows the
  warning and still saves; an invalid row shows its
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
- [ ] [VM bridge networking](../vm-bridge-networking.md): Docker's startup
  wait names addresses, so a handoff leaves it alone; after a NetworkManager
  handoff that changed the global IPv6 address, the Docker settings mark the
  old one. Add the boot check below to its pending Netplan-with-Docker run.
- [ ] Delete this file and its line in [TODO](./README.md).

## Backlog items to add when it lands

- IPs wait one after another, so N missing IPs delay Docker by up to
  N × 30 s.
- Under Rejected: waiting for a network card instead of an IP, because the
  bridge handoff moves the address off the card.
- An IPv6 address that a NetworkManager handoff replaced is marked in the
  settings, not rewritten; the list and the container's port binding are
  edited by hand.
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
- On a real host with the address already on a handoff bridge (the owner's
  server, `192.168.1.66` on `br-enp2s0`): reboot and confirm with
  `systemd-analyze blame` that `docker.service` no longer waits 30 s and the
  container that publishes on the address is up.
- In the VM, with the setting on and a container publishing on the host IP:
  run the bridge handoff and confirm it. `systemctl cat docker.service` is
  unchanged, the port answers without restarting Docker, and both still hold
  after a reboot. Then revert a second handoff and check the same.
