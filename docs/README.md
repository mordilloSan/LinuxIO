# LinuxIO Docs

Start with [API Contract](./api-contract.md) for bridge/frontend API work.

For repository setup, checks, builds, and configurable Make inputs, see
[Development targets and overrides](./development.md).

## API And Bridge

| Doc | Use |
|-----|-----|
| [API Contract](./api-contract.md) | Canonical implemented contract for Go-owned routes, generated frontend types, JSON request envelopes, Calls, Channels, Tasks, and adding endpoints. |
| [Handler Patterns](./bridge_handler_patterns.md) | Handler package style: `handlers.go` layout, context propagation, logging, naming, validation. |
| [Privilege Pattern](./privilege_pattern.md) | How to decide and declare privileged routes. |
| [Capabilities](./capabilities.md) | Detecting optional host tooling, gating features/routes on it, and the UI install flow. |
| [Process & Systemd Architecture](./process-systemd-architecture.md) | The four binaries (CLI, webserver, auth, bridge), systemd socket activation, `linuxio.target`, and privilege separation. |
| [Production Diagnostic Data Policy](./production-diagnostics.md) | Credentials and permitted correlation identifiers in journald, tracebacks, pprof, support artifacts, and core-dump controls. |
| [Server Yamux Protocol](./server-yamux-protocol.md) | Lower-level WebSocket/yamux byte relay and mux framing. |

## Frontend

| Doc | Use |
|-----|-----|
| [TanStack Router](./tanstack-router.md) | Canonical routing guide: file conventions, where loaders and guards go, search validation, child-route tabs, error/pending defaults, and adding a route. |
| [Table Row Gestures](./table-row-gestures.md) | The one row-interaction contract for every data table: click, long press, double click, Escape; which props opt a table in, and why column defs must be stable. |
| [Docker Icons](./docker-icons.md) | Docker icon resolution and labels. |
