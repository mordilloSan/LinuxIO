# LinuxIO Docs

Start with [API Contract](./api-contract.md) for bridge/frontend API work.

## API And Bridge

| Doc | Use |
|-----|-----|
| [API Contract](./api-contract.md) | Canonical guide for Go-owned API routes, generated frontend types, JSON request envelopes, jobs, streams, and adding endpoints. |
| [Handler Patterns](./BRIDGE_HANDLER_PATTERNS.md) | Handler package style: `handlers.go` layout, context propagation, logging, naming, validation. |
| [Privilege Pattern](./PRIVILEGE_PATTERN.md) | How to decide and declare privileged routes. |
| [Server Yamux Protocol](./server-yamux-protocol.md) | Lower-level WebSocket/yamux byte relay and mux framing. |

## Product Areas

| Doc | Use |
|-----|-----|
| [Docker Icons](./docker-icons.md) | Docker icon resolution and labels. |
| [Notifications](./notifications.md) | Planned notification system design. |
| [Session Activity Timeout Plan](./session-activity-timeout-plan.md) | Planned session idle and job-safe cleanup design. |
