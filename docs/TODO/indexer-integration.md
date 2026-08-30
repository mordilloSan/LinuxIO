# Complete Indexer Integration

> **Status: Partially implemented.** LinuxIO already integrates with an indexer
> for file search, directory sizes, subfolders, Docker stack entries, manual
> indexing, settings, status, and capability detection. This plan closes the
> remaining ownership, distribution, reliability, and verification gaps.

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
- The current installation path still treats the indexer as an externally
  installed optional component; it must become a normal LinuxIO binary and
  systemd unit set.

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

- [ ] Add a separate `backend/indexer` binary. It owns filesystem scanning,
  SQLite/index storage, the Unix-socket HTTP/SSE server, idle shutdown, and the
  indexer CLI commands.
- [ ] Define the complete indexer wire API once in a small shared package under
  `backend/indexer/api`: routes, request/response types, status codes, config,
  and SSE event types. The daemon and `handlers/indexer` bridge client both use
  it; do not duplicate these types in `filebrowser` or the frontend.
- [ ] Keep `backend/bridge/apischema` as the separate source of truth for the
  public LinuxIO bridge API. Adapt the shared indexer API into those public
  routes and regenerate frontend types from the bridge schema only.
- [ ] Decide which indexer configuration fields are LinuxIO-owned, which are
  passed through, and which require a daemon restart or a full reindex.
- [ ] Add an explicit daemon idle-timeout field to the canonical config; keep it
  distinct from SQLite connection idle settings and preserve the current
  default period.
- [ ] Define one protocol/version check for the in-repo daemon and bridge; do
  not maintain an external indexer release compatibility matrix.

### 2. Define socket activation, idle behavior, and periodic indexing

- [ ] Add `packaging/systemd/indexer.socket` for `/run/indexer.sock` with
  `Accept=no`, explicit ownership/group/mode, and no world access.
- [ ] Make `indexer.socket` depend on the web-server activation socket with
  `Requires=linuxio-webserver.socket`, `After=linuxio-webserver.socket`, and
  `BindsTo=linuxio-webserver.socket`, so the indexer activity socket is present
  only with the application entrypoint.
- [ ] Add `packaging/systemd/indexer.service` to consume the activated socket
  through systemd's inherited activation file descriptors and run
  `/usr/local/bin/indexer serve`. The service owns the database and scanner;
  requests never start a second database owner.
- [ ] Define idle behavior: after the configured inactivity period, and only
  when no request, SSE stream, or indexing operation is active, the daemon
  exits while `indexer.socket` remains available for activation.
- [ ] Add `packaging/systemd/indexer-index.service` and
  `packaging/systemd/indexer-index.timer`. Preserve periodic indexing with one
  configurable interval and `Persistent=true`; the timer asks the daemon to
  perform the index through the activity socket rather than running a second
  scanner process. The `indexer index` command used by the timer is a socket
  client, not another database/scanner owner.
- [ ] Serialize timer, manual full-index, and scoped reindex operations and
  define behavior when one is already running.
- [ ] Tie the service, timer, and socket to `linuxio.target` with explicit
  `PartOf`/install relationships so stopping LinuxIO stops periodic work and
  does not unexpectedly start the web server.
- [ ] Make `linuxio.target` `Wants=indexer.socket`; keep indexer failure
  non-fatal to the rest of LinuxIO.
- [ ] Keep the existing timer-setting route, but make its interval update the
  canonical config and the systemd timer through one atomic daemon-reload/
  timer-restart path; remove any external-CLI assumption.

### 3. Ship the binary and units with LinuxIO

- [ ] Add `build-indexer` to `Makefile`, the normal build graph, `clean`, and
  the backend verification path; inject the same version/commit metadata as
  the other Go binaries.
- [ ] Add `linuxio-indexer` or the chosen binary artifact to the release
  workflow, tarball, checksum list, executable checks, and release artifacts.
- [ ] Update `install-linuxio-binaries.sh` and `localinstall.sh` to install the
  indexer binary and all `packaging/systemd/indexer*` units atomically.
- [ ] Update `uninstall.sh` to remove the indexer binary, socket, service, and
  timer units, including any persistent index data only when the uninstall
  policy explicitly requests data removal.
- [ ] Define architecture support, upgrade rollback, preserved index data,
  service enablement, and daemon-reload/start ordering.
- [ ] Verify service sandboxing, index data ownership, scanner permissions,
  socket group access for the bridge, graceful shutdown, and no unrelated
  feature failure when the indexer service is down.

### 4. Remove capability installation and consolidate the bridge client

- [ ] Remove the indexer `Install`/`OptionalComponent` capability path and its
  installer implementation, tests, UI install metadata, and install-dialog
  expectations. Capability detection remains available for health and feature
  gating; installation is handled by the LinuxIO package installer.
- [ ] Put the shared Unix-socket client and endpoint helpers behind
  `handlers/indexer`, importing only `backend/indexer/api`. Remove the parallel
  raw client and protocol decoders currently embedded in `handlers/filebrowser`.
- [ ] Keep daemon wire fields private to the shared API/client boundary; expose
  only LinuxIO API shapes from `apischema` and regenerate frontend types after
  contract changes.
- [ ] Validate and normalize paths at the LinuxIO boundary, including root,
  subpaths, symlinks, and paths outside the file-browser filesystem root.
- [ ] Bound query limits and all decoded upstream bodies, preserve useful HTTP
  status/error identity, and propagate caller context through every request.
- [ ] Standardize status transitions, conflict handling, stream closure,
  cancellation, and capability updates across config, search, statistics,
  mutations, and indexing Tasks.
- [ ] Define the minimum reconciliation policy for failed asynchronous `add`,
  `delete`, and `reindex` notifications. Prefer the next scoped/full index and
  visible diagnostics; add a durable queue only if measurement proves it is
  needed.
- [ ] Verify that Docker stack indexing and file-browser mutations use the same
  path and entry contract as manual indexing.

### 5. Finish frontend behavior

- [ ] Define the user-visible states for unavailable, socket-activating,
  indexing, stale, failed, and empty indexes in search, folder sizes, and
  settings.
- [ ] Make restart-required configuration changes actionable and explicit;
  distinguish “saved”, “daemon restarted”, and “full index required”.
- [ ] Verify timer editing, manual full/scoped indexing, progress recovery after
  reload/reconnect, conflict feedback, and dialog close/reopen behavior.
- [ ] Keep indexer capability gating aligned with backend authorization and
  invalidation; no frontend fallback may present stale index results as current.

### 6. Resolve licensing and publish documentation

- [ ] Confirm the indexer source and all new dependencies can ship under the
  repository license; add required copyright/license notices and a
  third-party notice file if dependencies require one.
- [ ] Update the global `README.md`: list the indexer feature and binary,
  explain that it ships with LinuxIO rather than Capability Manager, document
  socket activation/idle behavior and periodic indexing, and link to the
  canonical indexer documentation.
- [ ] Add `docs/indexer.md` as the canonical operator/developer guide covering
  architecture, API ownership, socket/service/timer units, data path,
  configuration, permissions, lifecycle, troubleshooting, and recovery.
- [ ] Add the guide to `docs/README.md` and keep the TODO focused on unfinished
  work rather than repeating the full API reference. Update
  `docs/capabilities.md` so the indexer is no longer described as a capability
  installation example.

### 7. Test the shipped boundary

- [ ] Add indexer binary tests for API decoding/encoding, scanner/database
  behavior, idle shutdown, operation serialization, and CLI commands.
- [ ] Add bridge contract tests for every supported endpoint using the shared
  API types and a fake Unix socket server, including malformed payloads,
  oversized responses, timeouts, cancellation, conflicts, unavailable
  services, and SSE reconnects.
- [ ] Add mutation reconciliation tests covering create, overwrite, copy, move,
  delete, directory changes, and Docker stack entry updates.
- [ ] Add installer tests for the indexer artifact, unit installation,
  checksum/atomic replacement, preserved data, rollback, and uninstall.
- [ ] Add frontend tests for capability transitions, stale/error states,
  settings restart/full-index notices, search gating, and recovered Tasks.
- [ ] Add a disposable-host smoke test for install → socket activation → full
  index → search/dir-size → mutation update → timer run → scoped reindex →
  idle shutdown → uninstall.
- [ ] Run `make generate` whenever Go-owned contracts change, then finish with
  the repository Make verification target required by the changed layers.

## Completion checklist

- [ ] The `backend/indexer` binary and its API source of truth are complete.
- [ ] Socket activation, idle behavior, timer execution, and operation locking
  are verified.
- [ ] Release installation, upgrade, rollback, and uninstall are verified.
- [ ] The backend has one indexer client/contract boundary.
- [ ] Indexer capability installation has been removed.
- [ ] Licensing, `README.md`, `docs/README.md`, and `docs/indexer.md` are
  complete.
- [ ] Optional capability behavior and mutation reconciliation are verified.
- [ ] Frontend states and Task recovery are verified in browser coverage.
- [ ] Disposable-host smoke test passes and all generated files are current.
