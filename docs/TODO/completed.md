# Completed or closed TODOs

- [x] Completed the [first-party monitoring integration](./linuxio-monitoring.md):
  shipped `linuxio-monitoring`, its systemd unit, fixed read/control sockets
  and root peer gate, strict YAML configuration, SQLite history, initial live
  payload with one-second reuse, and bridge Docker metrics/admin integration.
  Build, release, installation, CLI and built-in capability wiring are present.
  The remaining measurement routes now use the daemon's shared live contract;
  static hardware reads are cached, the read-only Processes page includes
  program grouping, and Docker/Podman metrics use Moby with fixture and delta
  regression coverage. See [Monitoring](../monitoring.md) for the implemented
  contract. Development-host installation and login smoke checks remain unverified.
- [x] Completed Phases 1–3 and topology of the
  [Docker management roadmap](./docker-management-roadmap.md): container
  inspect/lifecycle controls, shared create/edit form with rollback recovery,
  volume browsing and ZIP backups, network management, and network/host-port
  visualization. The completed requirements are
  [archived below](#docker-management-phases-13-and-topology); Phase 4 activity,
  audit and Engine events remain open.

- [x] Completed the first-party indexer integration and simplification. LinuxIO
  owns the root-only socket-activated service, timer, reduced configuration,
  rebuildable cache, and browser Task recovery. The final design and operating
  contract are documented in [Filesystem Indexer](../indexer.md).
- [x] Documented indexer YAML/database/socket paths and bridge per-user
  configuration storage. See
  [Configuration and Storage Layout](../configuration-storage-layout.md).
- [x] Completed resilient bridge configuration storage: independently persisted
  core and UI documents now fall back from the authenticated home to
  `/var/lib/linuxio/users/<uid>` and then memory; invalid core documents are
  quarantined at startup; degraded storage is logged and surfaced in the UI.
  See [Bridge Configuration Storage](../config-storage.md).
- [x] Completed
  File-Browser Read Paths and API Efficiency:
  replaced `resource_get` with narrow listing, tree, text, and permission reads;
  removed single-file sibling scans; added bounded cancellable enumeration; and
  aligned frontend query ownership and invalidation with the focused contracts.
- [x] Split bridge per-user state into bridge-owned functional settings and
  frontend-produced UI snapshots, each independently created in its own
  YAML file and lock; retained atomic flat-file writes and frontend-only UI
  behavior while making the backend authoritative for persisted UI defaults;
  made strict decode plus pure validation reset only the affected
  file on content failure while I/O/security failures fail; removed legacy
  conversion, permissive salvage, field repair, and filesystem-based Docker
  folder mutation; made YAML, lock, and atomic replacement ownership follow
  the authenticated UID/GID in privileged and unprivileged bridge modes. No
  embedded database or JSON configuration was added.
- [x] Closed the router policy settings question: the bridge `TaskPolicy`
  values stay compile-time constants. They are cross-user admission limits, so
  the per-user config file is the wrong scope, and no system-wide settings
  store exists; revisit only if a real deployment hits a limit.
- [x] Completed the phased native Docker update engine work: native registry
  digest checks replaced Watchtower-based checks,
  Compose-managed containers update through their Compose project, opted-in
  standalone containers recreate transactionally with rollback, and scheduled
  execution moved to a LinuxIO systemd runner.
- [x] Completed Phases 1–5.5 of the
  [API Reliability, Recovery, and Notifications Roadmap](./api-reliability-roadmap.md):
  strict standard-library request decoding with an explicit safe-retry policy
  and honest connection-loss outcomes; Task lifetime/owner scope and session
  activity semantics; the durable `docker.update_container` Task proven with a
  persistent operation record and external execution owner; app updates kept
  session-bound with piped installer feedback and ordered post-result restart;
  generic Task progress standardized while preserving typed route detail; and
  visible, entity-scoped mutation feedback restored across the frontend.
- [x] Completed the Call/Channel/Task transport migration: all Tasks use one
  typed runner/result shape, ordinary handlers have no emitter, and
  `TaskService` registers the reserved `tasks.*` Calls and Channels without a
  special dispatch branch.
- [x] **6. Total background-work review:** migrated bounded mutations to direct actions,
  retained only real progress/recovery Tasks, added typed handler bindings and
  compile-time route-mode endpoint safety, and eliminated contract drift.
- [x] Moved the React Compiler backend from Babel to Oxc (2026-07-09), then
  replaced the frozen general-transformer binding with the official dedicated
  `oxc-transform-react` package and added separate reporting for recoverable
  function bailouts and explicit compiler opt-outs (2026-08-16).
- [x] Upgraded `oxc-transform-react` to 0.145.0, removed the temporary function-
  outlining safeguard after `oxc-project/oxc#25548` shipped, aligned compiler
  coverage with production, and passed the frontend, coverage, and browser gates
  (2026-08-19).
- [x] Replaced the custom React Compiler Vite plugin with the native
  `@vitejs/plugin-react` 6.1 integration after upstream support shipped. Kept
  React 19 compilation production-only in Vite, compiled production modules but
  not test-only modules under Vitest, and left the browser fixture uncompiled.
  Accepted the upstream integration's development JSX and source maps under
  Vitest, warning diagnostics, and fatal-error handling after verifying the
  resulting production, test, compiler-coverage, and browser behavior
  (2026-08-23).
- [x] Replaced the ESLint stack with Oxlint, tsgolint, and JS plugins.
- [x] Completed the post-migration frontend UI architecture cleanup.
- [x] Fixed file-browser progress updates rerendering the entire browser.
- [x] Replaced per-item chmod with `filebrowser.chmod_batch`.
- [x] Replaced the Job API with `useTaskAction` / `useTaskStreamAction` and
  watch-based recovery; Query fetching has no Task lifecycle dependency.
- [x] Fixed file-browser copy behavior for symlinks.
- [x] Completed the file-browser batch-operation follow-ups.
- [x] Implemented batch Tasks for copy, move, delete, and uploads.
- [x] Implemented monitoring.
- [x] Added virtualized rendering for logs and the file browser.
- [x] Fixed invalid PackageKit `InfoEnum` debug output.
- [x] Closed the TanStack Router migration proposal as not worthwhile.
- [x] Split the Iconify registry into shell and route-specific chunks.
- [x] Split `AuthGuard` into a lightweight gate and lazy authenticated
  providers.
- [x] Removed render-time state updates from the dashboard network card.
- [x] Removed Space Grotesk and Material Icons in favor of local assets.
- [x] Kept route preloading intent-based rather than preloading every route.
- [x] Consolidated small utility helpers.
- [x] Documented and enforced the bridge handler pattern.
- [x] Generated the shared Go/React API contracts.
- [x] Removed the binary protocol in favor of JSON.
- [x] Stopped loading configuration on every page load.
- [x] Aligned user configuration with the UI.
- [x] Fixed the NFS distribution dependency.
- [x] Implemented power management.
- [x] Reviewed bridge and server logging.
- [x] Removed `StartSimpleNetInfoSampler`.
- [x] Moved chunk size into user configuration.
- [x] Added configurable card/table view modes.
- [x] Implemented Docker auto-update and the Docker Compose UI.
- [x] Made builds reproducible from the same source tree.
- [x] Fixed indexer status checks and calls.
- [x] Added real streaming for service and Docker logs.
- [x] Improved application-update progress feedback.
- [x] Removed Gin.
- [x] Adopted Quantum Filebrowser as the main navigator.
- [x] Added login-time file-browser user provisioning.
- [x] Added the session ID to file-browser headers.
- [x] Removed the generated file-browser configuration after container creation.
- [x] Ensured bridge cleanup terminates owned PTY sessions where required.
- [x] Made the WebSocket persistent.
- [x] Removed the main program's startup configuration-file requirement.
- [x] Removed session IDs from the bridge socket and binary paths.
- [x] Synchronized the global and file-browser themes.
- [x] Consolidated post-login theme settings into one user configuration file.
- [x] Separated bridge and server code.
- [x] Reduced API calls when changing themes.
- [x] Added a global read-only API for general information.
- [x] Cancelled session-bound work when the bridge receives an exit command.
- [x] Added WireGuard testing and latest-handshake reporting.
- [x] Ensured session garbage collection also terminates its bridge.
- [x] Tested multiple users.
- [x] Added bridge-binary integrity validation.
- [x] Multiplexed traffic over a single connection.
- [x] Made update reporting more responsive with D-Bus/WebSocket events.

## Docker management: Phases 1–3 and topology

Source reviewed on 2026-09-05 against the bridge Docker handlers, rollback
transaction and tests, container forms/actions, volume/network panels, and
topology page. This records implemented scope, not a claim that every runtime
or browser verification item in the roadmap has been exercised.

### Phase 1: container lifecycle and inspect

Add typed API routes for:

- [x] inspect container;
- [x] pause and unpause container;
- [x] kill container with `SIGKILL`; and
- [x] remove container with an explicit `force` option.

Keep start, stop, restart, update, logs, terminal, and monitoring behavior.

Load inspect data when the user selects a container. Show these sections in the
current resource-details layout:

- overview and health;
- image, command, entrypoint, restart policy, user, and working directory;
- environment variables, masked by default;
- ports and labels; and
- mounts and networks.

Start or Stop remains the primary action. Restart, Pause, Unpause, Kill, Edit,
and Remove use the action menu. State guards prevent invalid actions. Kill and
Remove require clear confirmation when they can interrupt work or destroy data.

### Phase 2: create and rollback-aware edit

Use one container form for Create and Edit. It supports:

- [x] name and image;
- [x] command and entrypoint;
- [x] environment variables;
- [x] published ports;
- [x] named volumes and bind mounts;
- [x] networks and aliases;
- [x] restart policy; and
- [x] user and working directory.

Keep Basics open. Collapse optional sections unless they contain values. Create
uses a local image or pulls the image when Docker cannot find it, then starts
the container only when the user requests it.

Editing recreates a standalone container through the rollback transaction that
the native image updater already uses:

1. Inspect the original and validate the edited configuration.
2. Merge edited fields into a full copy of the original configuration.
3. Show the user a concise change summary and downtime warning.
4. Stop and rename the original as the rollback container.
5. Create, start, and verify the replacement.
6. Remove the rollback container after verification succeeds.
7. Remove a failed replacement and restore the original name and running state.

The transaction journal restores the original after a bridge or process failure.
Preflight rejects auto-remove containers, unsafe dependencies, and network
configurations that the transaction cannot preserve. Compose-managed containers
offer **Edit stack** because the Compose file owns their configuration.

This phase provides failure rollback. Container configuration history and a
manual “roll back to an older version” interface remain deferred.

### Phase 3: volumes and networks

#### Volumes

- [x] Finish Create Volume with name, driver, and optional labels.
- [x] Remove the current forced deletion and let Docker reject in-use volumes.
- [x] Show the containers that use each volume and their running state.
- [x] Add **Browse in Navigator** for an accessible volume mountpoint.
- [x] Add **Download backup** through the existing `filebrowser.archive` task.

Navigator remains the only file-management interface. Download backup creates a
ZIP archive and uses the current task progress and browser download flow. The
confirmation lists attached running containers and warns that active writers
can produce an inconsistent archive. Custom volume drivers with no accessible
host mountpoint show an explanation instead of Browse and Backup actions.

#### Networks

- [x] Send the existing Driver and Internal fields to Docker.
- [x] Add attachable, IPv6, subnet, and gateway fields.
- [x] Put driver options behind an Advanced section.
- [x] Connect an unattached container, with optional aliases.
- [x] Disconnect an attached container after confirmation.
- [x] Protect Docker's default networks and report in-use deletion errors.

The current network details panel and connected-container table host these
actions. Docker requires recreation for network configuration changes, so this
plan does not add a misleading Edit Network action.

#### Topology follow-up

- [x] Visualize Docker networks and their attached containers.
- [x] Visualize host-wide published port bindings across containers.
- [x] Add an interactive topology map with application icons, collapsible
  Compose groups, URL-backed container/network selection, and an inspector.
  Connections represent network attachments; live RX/TX activity represents
  container totals, not per-connection or per-port traffic. Activity expires
  when samples stop advancing, supports pausing animation and reduced motion,
  and remains optional when monitoring is unavailable. Narrow layouts use
  selectable lists with the same inspector and published bindings.
  Match monitoring's shortened container IDs to Docker's full inventory IDs
  so CPU, memory, and network measurements reach the topology inspector/cards.
  Layered grain streams travel in opposite RX/TX directions along active
  attachments, with irregular spacing and soft edges. Their speed is decorative;
  they reflect container totals, not per-network traffic, and share the pause
  and reduced-motion controls.
- [x] Add Storage topology with a read-only block-device and mount inventory,
  device-to-mount and application-path relationships, URL selection, backing
  device/LVM details, capacity and optional live disk activity. Preserve
  multi-parent devices and nested memory mount boundaries; support keyboard
  navigation, narrow layouts, reduced motion, and paused/stale activity.
  See [Storage Topology](../storage-topology.md) for ownership and limits.
