# Complete Indexer Integration

> **Status: Implementation complete; verification and cleanup remain.**
> LinuxIO now owns, builds, ships, configures, and activates the indexer. The
> unchecked work below covers the guarded smoke test and retiring the legacy
> migration path. The approved YAML, runtime-path, and warm-start changes are in
> [Configuration and storage layout](../configuration-storage-layout.md).

## Current integration

- The bridge exposes indexer configuration, daemon status, and timer settings.
- File-browser search, directory-size, subfolder, and status routes proxy the
  indexer through its Unix socket.
- File-browser mutations and Docker indexing notify the daemon about changed
  entries; directory changes can request a scoped reindex.
- Full and scoped indexing stream progress through Tasks and SSE, including
  reconnect recovery.
- The frontend gates indexer-dependent features through the capability system,
  provides an indexing dialog, exposes settings, and invalidates index-backed
  queries after indexing.
- The indexer is a normal LinuxIO release artifact with first-party systemd
  socket, service, and timer units; Capability Manager only reports its health.

## Definition of complete

- A supported LinuxIO release builds, installs, upgrades, detects, starts, and
  removes the `backend/indexer` binary and its systemd units.
- The indexer daemon HTTP/SSE API is defined once, used by both the daemon and
  bridge client, and kept separate from the generated LinuxIO bridge API.
- Every indexer call has bounded input/output, context cancellation, and
  consistent unavailable/error behavior.
- File-browser and Docker mutations either update the indexer or have a defined
  reconciliation path; stale index data never silently becomes authoritative
  for filesystem operations.
- The UI handles absent, starting, stale, failed, and successfully indexed
  states without making unrelated file-browser features unusable.
- Unit, contract, installer, browser, and disposable-host checks cover the
  supported lifecycle.

## Plan

### 1. Build the first-party binary and define the API once

- [x] Add a separate `backend/indexer` binary. It owns filesystem scanning,
  SQLite/index storage, the Unix-socket HTTP/SSE server, idle shutdown, and the
  private daemon/worker process modes.
- [x] Define the complete indexer wire API once in a small shared package under
  `backend/indexer/api`: routes, request/response types, status codes, config,
  and SSE event types. The daemon and `handlers/indexer` bridge client both use
  it; do not duplicate these types in `filebrowser` or the frontend.
- [x] Keep `backend/bridge/apischema` as the separate source of truth for the
  public LinuxIO bridge API. Adapt the shared indexer API into those public
  routes and regenerate frontend types from the bridge schema only.
- [x] Decide which indexer configuration fields are LinuxIO-owned, which are
  passed through, and which require a daemon restart or a full reindex.
- [x] Add an explicit daemon idle-timeout field to the canonical config; keep it
  distinct from SQLite connection idle settings and preserve the current
  default period.
- [x] Define one protocol/version check for the in-repo daemon and bridge; do
  not maintain an external indexer release compatibility matrix.

### 2. Define socket activation, idle behavior, and periodic indexing

- [x] Add `packaging/systemd/linuxio-indexer.socket` for `/run/linuxio/indexer.sock` with
  `Accept=no`, root-only ownership/mode, and no group or world access.
- [x] Make `linuxio-indexer.socket` depend on the web-server activation socket with
  `Requires=linuxio-webserver.socket`, `After=linuxio-webserver.socket`, and
  `BindsTo=linuxio-webserver.socket`, so the indexer activity socket is present
  only with the application entrypoint.
- [x] Add `packaging/systemd/linuxio-indexer.service` to consume the activated socket
  through systemd's inherited activation file descriptors and run
  `/usr/local/bin/linuxio-indexer`. The service owns the database and scanner;
  requests never start a second database owner.
- [x] Make the optional read-only TCP listener a privileged, generated
  `linuxio-indexer-tcp.socket` tied to `linuxio-webserver.socket`; pass both
  enabled activation descriptors to the same daemon.
- [x] Define idle behavior: after the configured inactivity period, and only
  when no request, SSE stream, or indexing operation is active, the daemon
  exits while the Unix and optional TCP sockets remain available for activation.
- [x] Add `packaging/systemd/linuxio-indexer-index.service` and
  `packaging/systemd/linuxio-indexer-index.timer`. Preserve periodic indexing with one
  configurable monotonic interval; the timer asks the daemon to
  perform the index through the activity socket rather than running a second
  scanner process. Its private trigger mode is a socket client, not another
  database/scanner owner.
- [x] Serialize timer, manual full-index, and scoped reindex operations and
  define behavior when one is already running.
- [x] Tie the service, timer, and socket to `linuxio.target` with explicit
  `PartOf`/install relationships so stopping LinuxIO stops periodic work and
  does not unexpectedly start the web server.
- [x] Make `linuxio.target` `Wants=linuxio-indexer.socket`; keep indexer failure
  non-fatal to the rest of LinuxIO.
- [x] Keep the existing timer-setting route, but make its interval update the
  systemd timer drop-in and enabled state through one daemon-reload/restart
  path; remove any external-CLI assumption.
- [x] Move the activity socket to `/run/linuxio/indexer.sock`, make
  `linuxio-webserver.socket` want it during standalone activation, and make the
  webserver service weakly start the daemon in parallel without defeating idle
  exit or making indexer failure fatal.

### 3. Ship the binary and units with LinuxIO

- [x] Add `build-indexer` to `Makefile`, the normal build graph, `clean`, and
  the backend verification path; inject the same version/commit metadata as
  the other Go binaries.
- [x] Add `linuxio-indexer` or the chosen binary artifact to the release
  workflow, tarball, checksum list, executable checks, and release artifacts.
- [x] Update `install-linuxio-binaries.sh` and `localinstall.sh` to install the
  indexer binary and all `packaging/systemd/linuxio-indexer*` units atomically,
  stopping and removing the former standalone `indexer*` units and binary on
  upgrade while preserving its data.
- [ ] Remove the legacy standalone-indexer and YAML systemd-field migration
  paths (`interval`, `socket_path`, and `listen_addr`) once supported upgrades
  no longer depend on them.
- [x] Update `uninstall.sh` to remove the indexer binary, socket, service, and
  timer units, including any persistent index data only when the uninstall
  policy explicitly requests data removal.
- [x] Deploy strict YAML at `/etc/linuxio/indexer/config.yaml`; keep the HTTP
  API JSON, preserve the file on upgrade, and restrict the service's writable
  config path to the indexer subdirectory. Keep skipped scan roots explicit in
  `exclude_paths` (packaged as `/proc` and `/dev`) instead of hiding them in the
  scanner.
- [x] Define architecture support, upgrade rollback, preserved index data,
  service enablement, and daemon-reload/start ordering.
- [ ] Verify service sandboxing, index data ownership, scanner permissions,
  root-bridge socket access, graceful shutdown, and no unrelated
  feature failure when the indexer service is down.

### 4. Remove capability installation and consolidate the bridge client

- [x] Remove the indexer `Install`/`OptionalComponent` capability path and its
  installer implementation, tests, UI install metadata, and install-dialog
  expectations. Capability detection remains available for health and feature
  gating; installation is handled by the LinuxIO package installer.
- [x] Put the shared Unix-socket client and endpoint helpers behind
  `handlers/indexer`, importing only `backend/indexer/api`. Remove the parallel
  raw client and protocol decoders currently embedded in `handlers/filebrowser`.
- [x] Keep daemon wire fields private to the shared API/client boundary; expose
  only LinuxIO API shapes from `apischema` and regenerate frontend types after
  contract changes.
- [x] Validate and normalize paths at the LinuxIO boundary, including root,
  subpaths, symlinks, and paths outside the file-browser filesystem root.
- [x] Bound query limits and all decoded upstream bodies, preserve useful HTTP
  status/error identity, and propagate caller context through every request.
- [x] Standardize status transitions, conflict handling, stream closure,
  cancellation, and capability updates across config, search, statistics,
  mutations, and indexing Tasks.
- [x] Define the minimum reconciliation policy for failed asynchronous `add`,
  `delete`, and `reindex` notifications. Prefer the next scoped/full index and
  visible diagnostics; add a durable queue only if measurement proves it is
  needed.
- [x] Verify that Docker stack indexing and file-browser mutations use the same
  path and entry contract as manual indexing.

### 5. Finish frontend behavior

- [x] Define the user-visible states for unavailable, socket-activating,
  indexing, stale, failed, and empty indexes in search, folder sizes, and
  settings.
- [x] Make restart-required configuration changes actionable and explicit;
  distinguish “saved”, “daemon restarted”, and “full index required”.
- [x] Verify timer editing, manual full/scoped indexing, progress recovery after
  reload/reconnect, conflict feedback, and dialog close/reopen behavior.
- [x] Keep indexer capability gating aligned with backend authorization and
  invalidation; no frontend fallback may present stale index results as current.

### 6. Resolve licensing and publish documentation

- [x] Confirm the indexer source and all new dependencies can ship under the
  repository license; add required copyright/license notices and a
  third-party notice file if dependencies require one.
- [x] Update the global `README.md`: list the indexer feature and binary,
  explain that it ships with LinuxIO rather than Capability Manager, document
  socket activation/idle behavior and periodic indexing, and link to the
  canonical indexer documentation.
- [x] Add `docs/indexer.md` as the canonical operator/developer guide covering
  architecture, API ownership, socket/service/timer units, data path,
  configuration, permissions, lifecycle, troubleshooting, and recovery.
- [x] Add the guide to `docs/README.md` and keep the TODO focused on unfinished
  work rather than repeating the full API reference. Update
  `docs/capabilities.md` so the indexer is no longer described as a capability
  installation example.

### 7. Test the shipped boundary

- [x] Add indexer binary tests for API decoding/encoding, scanner/database
  behavior, idle shutdown, operation serialization, and private process modes.
- [x] Add bridge contract tests for every supported endpoint using the shared
  API types and a fake Unix socket server, including malformed payloads,
  oversized responses, timeouts, cancellation, conflicts, unavailable
  services, and SSE reconnects.
- [x] Add mutation reconciliation tests covering create, overwrite, copy, move,
  delete, directory changes, and Docker stack entry updates.
- [x] Add installer tests for the indexer artifact, unit installation,
  checksum/atomic replacement, preserved data, rollback, and uninstall.
- [x] Add frontend tests for capability transitions, stale/error states,
  settings restart/full-index notices, search gating, and recovered Tasks.
- [x] Add a disposable-host smoke test for install → socket activation → full
  index → search/dir-size → mutation update → timer run → scoped reindex →
  idle shutdown → uninstall.
- [x] Run `make generate` whenever Go-owned contracts change, then finish with
  the repository Make verification target required by the changed layers.

## Completion checklist

- [x] The `backend/indexer` binary and its API source of truth are complete.
- [x] Socket activation, idle behavior, timer execution, and operation locking
  are verified.
- [x] Release installation, upgrade, rollback, and uninstall are verified.
- [x] The backend has one indexer client/contract boundary.
- [x] Indexer capability installation has been removed.
- [x] Licensing, `README.md`, `docs/README.md`, and `docs/indexer.md` are
  complete.
- [x] Optional capability behavior and mutation reconciliation are verified.
- [ ] Frontend states and Task recovery are verified in browser coverage.
- [x] Generated files are current.
- [ ] Disposable-host smoke test passes on a clean systemd host.
