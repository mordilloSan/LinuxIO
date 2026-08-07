# LinuxIO Docs

Start with [API Contract](./api-contract.md) for bridge/frontend API work.

## API And Bridge

| Doc | Use |
|-----|-----|
| [API Contract](./api-contract.md) | Canonical guide for Go-owned API routes, generated frontend types, JSON request envelopes, jobs, streams, and adding endpoints. |
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
| [Card Query Render Boundaries Plan](./card-query-render-boundaries-plan.md) | Plan for keeping polled query updates inside live card slots, sharing the production React Compiler with Vitest, and guarding component ownership. |

## Product Areas

| Doc | Use |
|-----|-----|
| [Docker Icons](./docker-icons.md) | Docker icon resolution and labels. |
| [Notifications](./notifications.md) | Planned notification system design. |
| [Transient Units Plan](./transient-units-plan.md) | Planned bridge-survivable jobs via systemd transient units. |
