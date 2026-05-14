# Handler Patterns

Bridge handlers are route based. Pick the route mode first, then write the smallest handler shape that matches it.

## `handlers.go` Layout

`handlers.go` is only route wiring and IPC adapter code. It may contain:

- `RegisterHandlers`
- `handle*` adapter functions or methods

It must not contain package state types, package variables, constants, helper functions, validators, parsers, or domain implementations. Put those in named files beside it, for example:

- `handler_state.go` for small adapter state structs
- `handler_args.go` for shared route argument parsing
- `*_operation.go` for mutation/job orchestration
- domain-specific files such as `timers.go`, `health.go`, `config_operations.go`, or `terminal_session.go`

Every adapter in `handlers.go` should call one domain or operation function and return through `bridgeipc.EmitResult`:

```go
func handleListTimers(ctx context.Context, args []string, emit bridgeipc.Events) error {
    result, err := ListTimers(ctx)
    return bridgeipc.EmitResult(emit, result, err)
}
```

The implementation belongs outside `handlers.go`:

```go
func ListTimers(ctx context.Context) ([]TimerStatus, error) {
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

Request logging is centralized in `backend/common/ipc/bridge`. The router logs the route envelope at `debug` without raw arguments:

- route name
- route mode
- argument count
- session/user identifiers
- policy name when applicable
- outcome, status, and duration

Handler adapters do not log. Route operation functions do not emit `"... requested"` logs either; they parse, validate, map route arguments, and call domain functions.

Domain functions log meaningful work:

- successful state changes, such as `user created` or `group deleted`
- important no-op decisions, such as `group members unchanged`
- contextual failures or fallbacks where the domain has useful detail

Never log raw passwords, tokens, or full unreviewed argument payloads.

## Mode Selection

| Need | Mode |
|------|------|
| Immediate read-only result | `Query` |
| Mutation/action | `Job` |
| Long-running read, log follow, watch feed, app update | `Job` |
| Interactive bidirectional session | `Duplex` |

When in doubt between `Query` and `Job`, ask: "can this change system state, run for a while, emit progress, or need cancellation?" If yes, use `Job`.

## Route Namespaces

Route namespaces describe the product or domain surface, not the implementation transport. Do not use transport names such as `dbus` for bridge routes.

D-Bus-backed operations still use domain namespaces:

- `control.*` for host/session control
- `updates.*` for package and update operations
- `systemd.*` for units, timers, and sockets
- `network.*` for network configuration
- `hostname.*` for hostname changes
- `datetime.*` for time, timezone, and NTP operations

## File Naming

Handler packages should name files after the domain operation they implement, not after IPC mechanics.

| Location | Allowed naming | Avoid |
|----------|----------------|-------|
| `backend/common/ipc/bridge` | Framework terms such as `jobs.go`, `job_primitives.go`, `router.go` | Domain-specific handler code |
| `backend/bridge/handlers/<domain>` | Domain terms such as `package_update_operation.go`, `log_follow_operation.go`, `terminal_session.go`, `smart_test_operation.go` | Generic `jobs.go`, `stream.go`, `rpc.go`, `handler.go` |

Use `operation` for job-backed mutations or long-running work, `follow` for log/watch-style jobs, and `session` for true Duplex routes. The route mode still lives in the route table; the filename should help humans find the domain behavior without implying a second IPC subsystem.

## Query Pattern

```go
func handleList(ctx context.Context, args []string, emit bridgeipc.Events) error {
    result, err := listThings(ctx)
    return bridgeipc.EmitResult(emit, result, err)
}

router.Query("things.list", handleList)
```

Queries should be read-only and bounded. They emit one result and return.

## Job Pattern

Use `router.Job` for existing `Events` handlers and `router.JobRunner` when the implementation wants a `*bridgeipc.Job`.

```go
router.Job("things.create", handleCreate, bridgeipc.ActionDefault)
router.JobRunner("things.reindex", runReindex, bridgeipc.SingletonSystem)
```

Handlers registered with `router.Job` can report progress with `emit.Progress(...)`; `handlers.go` adapters still finish with `bridgeipc.EmitResult(...)`. Operation helpers outside `handlers.go` may use lower-level event APIs when they genuinely need to orchestrate progress.

Runners registered with `router.JobRunner` report progress directly:

```go
func runReindex(ctx context.Context, job *bridgeipc.Job, args []string) (any, error) {
    job.ReportProgress(map[string]any{"phase": "scanning", "pct": 10})
    return doReindex(ctx)
}
```

Fast-complete behavior is automatic. A runner that returns quickly produces a terminal initial `JobSnapshot`; the runner does not need special code.

## Stream-Style Jobs

Logs, subscriptions, and watch feeds are Jobs with `bridgeipc.StreamDefault`.

For text data, emit a progress payload with `type: "data"`:

```go
job.ReportProgress(map[string]any{
    "type": "data",
    "data": line,
})
```

Frontend stream openers can adapt those job progress events back to `onData` bytes while the backend keeps one coherent lifecycle.

## Duplex Pattern

Only interactive bidirectional sessions should be Duplex:

```go
router.Duplex("terminal.open", func(ctx context.Context, stream net.Conn, args []string) error {
    return openTerminal(ctx, stream, args)
})
```

Use Duplex for terminals and container shells. Do not use it for one-way logs or progress.

## Arguments

Use `bridgeipc` helpers:

```go
name, err := bridgeipc.Arg(args, 0)
if err != nil {
    return err
}

req, err := bridgeipc.DecodeJSONArg[CreateRequest](args, 0)
if err != nil {
    return err
}
```

Return `bridgeipc.NewError(message, code)` for typed client errors.

## Privilege

Declare privilege in the route table:

```go
{Name: "set_hostname", Mode: bridgeipc.ModeJob, Handler: handleSetHostname, Privileged: true}
```

The dispatcher handles the check and logs the rejected start centrally.

## Frontend Contract

Frontend route metadata lives in `frontend/src/api/route-metadata.ts`.

| Backend mode | Frontend API |
|--------------|--------------|
| `Query` | `.useQuery()` or `.call()` |
| `Job` | `.useMutation()` or `.call()`, returning `JobSnapshot` |
| `Duplex` | stream opener |

`.useQuery()` rejects non-query routes and `.useMutation()` rejects non-job routes.
