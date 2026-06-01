# Handler Patterns

This document covers handler style and package organization. Route contracts, frontend shape, transport, modes, jobs, and endpoint creation live in the canonical [API Contract](./api-contract.md).

## `handlers.go` Layout

`handlers.go` is only route wiring and IPC adapter code. It may contain:

- `RegisterHandlers`
- `handle*` adapter functions or methods

It must not contain package state types, package variables, constants, helper functions, validators, parsers, or domain implementations. Put those in named files beside it, for example:

- `handler_state.go` for small adapter state structs
- `*_operation.go` for mutation/job orchestration
- domain-specific files such as `timers.go`, `health.go`, `config_operations.go`, or `terminal_session.go`

Every adapter in `handlers.go` should receive a typed request and return through `bridgeipc.EmitResult`:

```go
func handleListTimers(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
    result, err := ListTimers(ctx)
    return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUnitInfo(ctx context.Context, req apischema.UnitNameRequest, emit bridgeipc.Events) error {
    result, err := GetUnitInfo(ctx, req.UnitName)
    return bridgeipc.EmitResult(emit, result, err)
}
```

The implementation belongs outside `handlers.go`:

```go
func ListTimers(ctx context.Context) ([]apischema.Timer, error) {
    // actual implementation
}
```

Do not call `emit.Result(...)` or `emit.Error(...)` directly from `handlers.go`; use `bridgeipc.EmitResult` so result and error mapping stay consistent.

## Context

Every handler must accept the caller `context.Context` and pass it to every callee. Any callee that performs I/O, blocks on external state, launches a command, or loops over filesystem/sysfs entries should accept `ctx context.Context` as its first parameter.

Use context-aware APIs whenever they exist:

- use `exec.CommandContext(ctx, ...)`, never bare `exec.Command`, in handler code
- use gopsutil `WithContext` variants where available
- add `ctx.Err()` guards at the top of loops over sysfs, procfs, or file entries
- add `ctx.Err()` checks before starting work through libraries that do not support contexts

Some libraries, such as `ghw`, do not expose context-aware calls. In those cases the function should still accept `ctx` for consistent handler flow and should document the limitation:

```go
// ghw has no context support; ctx is accepted for consistent handler flow.
```

Some primitives are not cancellable once entered. Check `ctx.Err()` before acquiring them and document the limitation:

```go
// sync.Mutex.Lock is not cancellable once entered.
mu.Lock()
```

The same applies to `sync.RWMutex` and `syscall.Flock`.

Mock variables that wrap blocking work must accept `context.Context`. Command wrappers should use the `exec.CommandContext` shape:

```go
func(context.Context, string, ...string) *exec.Cmd
```

Sampler and reader mocks should keep their domain-specific return shape while taking `context.Context`, for example:

```go
func(context.Context) map[string]gopsnet.IOCountersStat
func(context.Context) ([]gopsnet.InterfaceStat, error)
```

## Logging

Request lifecycle logging is centralized in `backend/common/ipc/bridge`. The router logs the route, mode, user, outcome, duration, and error at debug level.

Handler adapters do not log. Route operation functions do not emit `"... requested"` logs either; they validate typed request fields and call domain functions.

Domain functions log meaningful work:

- successful state changes, such as `user created` or `group deleted`
- important no-op decisions, such as `group members unchanged`
- contextual failures or fallbacks where the domain has useful detail

Never log raw passwords, tokens, or full unreviewed request payloads.

## Route Namespaces

Route namespaces describe the product or domain surface, not the implementation transport. Do not use transport names such as `dbus` for bridge routes.

D-Bus-backed operations still use domain namespaces:

- `control.*` for host/session control
- `updates.*` for package and update operations
- `systemd.*` for units, timers, and sockets
- `network.*` for network configuration
- `hostname.*` for hostname changes
- `datetime.*` for time, timezone, and NTP operations

The `jobs.*` namespace is reserved by `bridgeipc`.

## File Naming

Handler packages should name files after the domain operation they implement, not after IPC mechanics.

| Location | Allowed naming | Avoid |
|----------|----------------|-------|
| `backend/common/ipc/bridge` | Framework terms such as `jobs.go`, `job_primitives.go`, `router.go` | Domain-specific handler code |
| `backend/bridge/handlers/<domain>` | Domain terms such as `package_update_operation.go`, `log_follow_operation.go`, `terminal_session.go`, `smart_test_operation.go` | Generic `jobs.go`, `stream.go`, `rpc.go`, `handler.go` |

Use `operation` for job-backed mutations or long-running work, `follow` for log/watch-style jobs, and `session` for true duplex routes. The route mode still lives in the route spec; the filename should help humans find the domain behavior without implying a second IPC subsystem.

## Request Validation

Request JSON is decoded before the handler runs. Use typed fields directly:

```go
func handleStartService(ctx context.Context, req apischema.ServiceNameRequest, emit bridgeipc.Events) error {
    if req.ServiceName == "" {
        return bridgeipc.NewError("missing service name", 400)
    }
    return bridgeipc.EmitResult(emit, nil, StartUnit(ctx, req.ServiceName))
}
```

Return `bridgeipc.NewError(message, code)` for typed client errors.

## Privilege

Declare route-level privilege in `apischema`. See [Privilege Pattern](./privilege_pattern.md) for the policy rule.

Handlers may still validate operation-specific policy, but they should not duplicate the route-level admin check.
