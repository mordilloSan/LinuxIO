# LinuxIO Docs

Start with [API Contract](./api-contract.md) for bridge/frontend API work.

For repository setup, checks, builds, and configurable Make inputs, see
[Development targets and overrides](./development.md).

Docs in this folder describe implemented behavior. Plans and roadmaps that have
not fully shipped live in [TODO](./TODO); a doc moves up into this folder once
its plan lands.

## API And Bridge

| Doc | Use |
|-----|-----|
| [API Contract](./api-contract.md) | Canonical implemented contract for Go-owned routes, generated frontend types, JSON request envelopes, Calls, Channels, Tasks, and adding endpoints. |
| [Bridge Configuration Storage](./config-storage.md) | Per-user core/UI files, home and `/var/lib/linuxio` fallback, memory-only degradation, corrupt-document recovery, ownership, and logging. |
| [Configuration and Storage Layout](./configuration-storage-layout.md) | Indexer paths and bridge per-user configuration files, with ownership and lifecycle. |
| [Durable Operations Architecture](./durable-operations-architecture.md) | The implemented durable-operation boundary: the shared operation record and store, external execution owners, recovery across bridge restart and websocket loss, and what a new durable route must prove before opting in. |
| [Handler Patterns](./bridge_handler_patterns.md) | Handler package style: `handlers.go` layout, context propagation, logging, naming, validation. |
| [Privilege Pattern](./privilege_pattern.md) | How to decide and declare privileged routes. |
| [Capabilities](./capabilities.md) | Detecting optional host tooling, gating features/routes on it, and the UI install flow. |
| [Process & Systemd Architecture](./process-systemd-architecture.md) | LinuxIO processes and helpers, systemd socket activation, `linuxio.target`, and privilege separation. |
| [Filesystem Indexer](./indexer.md) | Architecture, operation, bridge and daemon APIs, configuration, recovery, and administration. |
| [Monitoring Daemon](./monitoring.md) | Trust boundary and sockets, installed files, sampling semantics, HTTP API and plugin allowlists, configuration, capability, and troubleshooting. |
| [Third-Party Notices](./THIRD_PARTY_NOTICES.md) | Attribution and license notices for incorporated indexer and monitoring code and dependencies. |
| [Production Diagnostic Data Policy](./production-diagnostics.md) | Credentials and permitted correlation identifiers in journald, tracebacks, pprof, support artifacts, and core-dump controls. |
| [Server Yamux Protocol](./server-yamux-protocol.md) | Lower-level WebSocket/yamux byte relay and mux framing. |

## Host Integration

| Doc | Use |
|-----|-----|
| [Automatic Updates](./automatic-updates.md) | Per-distribution automatic-update providers, the systemd timers LinuxIO manages, and how the backend selects a provider from `/etc/os-release`. |

## Frontend

| Doc | Use |
|-----|-----|
| [TanStack Router](./tanstack-router.md) | Canonical routing guide: file conventions, where loaders and guards go, search validation, child-route tabs, error/pending defaults, and adding a route. |
| [Table Row Gestures](./table-row-gestures.md) | The one row-interaction contract for every data table: click, long press, double click, Escape; which props opt a table in, and why column defs must be stable. |
| [Docker Icons](./docker-icons.md) | Docker icon resolution and labels. |
