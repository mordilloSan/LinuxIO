# LinuxIO Docs

Start with [API Contract](./api-contract.md) for bridge/frontend API work.

## API And Bridge

| Doc | Use |
|-----|-----|
| [API Contract](./api-contract.md) | Canonical implemented contract for Go-owned routes, generated frontend types, JSON request envelopes, Calls, Channels, Tasks, and adding endpoints. |
| [API Reliability Roadmap](./api-reliability-roadmap.md) | Active sequencing for remaining reliability, durable-operation follow-up, notifications, and scheduled execution work. |
| [Handler Patterns](./bridge_handler_patterns.md) | Handler package style: `handlers.go` layout, context propagation, logging, naming, validation. |
| [Privilege Pattern](./privilege_pattern.md) | How to decide and declare privileged routes. |
| [Capabilities](./capabilities.md) | Detecting optional host tooling, gating features/routes on it, and the UI install flow. |
| [Process & Systemd Architecture](./process-systemd-architecture.md) | The four binaries (CLI, webserver, auth, bridge), systemd socket activation, `linuxio.target`, and privilege separation. |
| [Server Yamux Protocol](./server-yamux-protocol.md) | Lower-level WebSocket/yamux byte relay and mux framing. |

## Frontend

| Doc | Use |
|-----|-----|
| [TanStack Router](./tanstack-router.md) | Canonical routing guide: file conventions, where loaders and guards go, search validation, child-route tabs, error/pending defaults, and adding a route. |
| [E2E Testing](./e2e-testing.md) | End-to-end test setup and conventions. |

## Product Areas

| Doc | Use |
|-----|-----|
| [Docker Icons](./docker-icons.md) | Docker icon resolution and labels. |
| [Durable Operations Architecture](./durable-operations-architecture.md) | Implemented durable-operation records, recovery, transient-unit adapter, and the `control.app_update` and `docker.update_container` routes. |
| [Notifications](./notifications.md) | Planned persistent per-user notification store, Calls, Channel, and frontend behavior. |
| [Scheduled Execution](./scheduled-execution.md) | Planned persistent schedules, execution ownership, run summaries, and notifications. |
