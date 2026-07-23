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
| [UI-Architecture Follow-ups](./frontend-ui-architecture-followups.md) | Remaining cleanups after the API migration: controller prop-drilling, dialog job streams → `attach()`, auto-update dual writer, allowlisted stream consumers, `chmod_batch`. ToDo #13/#14. |
| [Rerender Plan](./frontend-rerender-plan.md) | Profiler-confirmed rerender hotspots and the fix order. ToDo #12. |
| [TanStack Router Architecture](./tanstack-router.md) | File-based routes, generated type tree, automatic code splitting, route-local loaders/static data, and global preload policy. |
| [E2E Testing](./e2e-testing.md) | End-to-end test setup and conventions. |

## Product Areas

| Doc | Use |
|-----|-----|
| [Docker Icons](./docker-icons.md) | Docker icon resolution and labels. |
| [Notifications](./notifications.md) | Planned notification system design. |
| [Session Activity Timeout Plan](./session-activity-timeout-plan.md) | Planned session idle and job-safe cleanup design. |
| [Transient Units Plan](./transient-units-plan.md) | Planned bridge-survivable jobs via systemd transient units. |
