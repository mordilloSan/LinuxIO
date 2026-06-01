# Handler Patterns

Bridge handlers are route based and contract driven. Pick the route mode first, define the Go request/result contract in `apischema`, then write the smallest typed adapter that calls domain code.

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

## Contract Source

The API contract lives in `backend/bridge/apischema`.

| File | Purpose |
|------|---------|
| `routes.go` | One `RouteSpec` per route: name, mode, kind, request, result, privilege, policy, and `NoEndpoint`. |
| `contracts.go` | Shared request structs and small shared responses. |
| `models.go` | API response/domain models reflected into TypeScript. |
| `schema.go` | Registration adapters and request decoders. |

Do not edit generated frontend API files. Run `make generate` after contract changes.

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

## Mode Selection

| Need | Mode |
|------|------|
| Immediate read-only result | `bridgeipc.ModeQuery` |
| Mutation/action | `bridgeipc.ModeJob` |
| Long-running read, log follow, watch feed, app update | `bridgeipc.ModeJob` |
| Interactive bidirectional session | `bridgeipc.ModeDuplex` |

When in doubt between query and job, ask: "can this change system state, run for a while, emit progress, or need cancellation?" If yes, use a job.

## Schema Kinds

| Kind | Signature |
|------|-----------|
| `apischema.KindHandler` | `func(context.Context, TRequest, bridgeipc.Events) error` |
| `apischema.KindRunner` | `func(context.Context, *bridgeipc.Job, TRequest) (any, error)` |
| `apischema.KindDuplex` | `func(context.Context, net.Conn, TRequest) error` |

Use `apischema.NoRequest()` for no request payload and `apischema.NoResponse()` for no result payload. Typed handler functions receive `bridgeipc.NoRequest` when the route has no request.

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

## Query Pattern

Add the route contract:

```go
{Kind: KindHandler, Route: "things.list", Mode: bridgeipc.ModeQuery, Request: NoRequest(), Result: TypeOf[[]Thing]()},
```

Wire the handler:

```go
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
    apischema.RegisterRoutes(router, "things", []bridgeipc.Command{
        {Name: "list", Mode: bridgeipc.ModeQuery, Handler: handleList},
    })
}

func handleList(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
    result, err := listThings(ctx)
    return bridgeipc.EmitResult(emit, result, err)
}
```

Queries should be read-only and bounded. They emit one result and return.

## Handler Job Pattern

Use `apischema.KindHandler` plus `bridgeipc.ModeJob` for ordinary mutations that can report through `bridgeipc.Events`.

```go
{Kind: KindHandler, Route: "things.create", Mode: bridgeipc.ModeJob, Request: TypeOf[ThingCreateRequest](), Result: TypeOf[ThingResult]()},
```

```go
func handleCreate(ctx context.Context, req apischema.ThingCreateRequest, emit bridgeipc.Events) error {
    result, err := createThing(ctx, req.Name)
    return bridgeipc.EmitResult(emit, result, err)
}
```

Handlers registered as jobs may report progress with `emit.Progress(...)`; adapters still finish with `bridgeipc.EmitResult(...)`.

Fast-complete behavior is automatic. A job that returns quickly produces a terminal initial `JobSnapshot`; the handler does not need special code.

## Runner Job Pattern

Use `apischema.KindRunner` when the implementation wants a `*bridgeipc.Job` directly.

```go
{Kind: KindRunner, Route: "things.reindex", Mode: bridgeipc.ModeJob, Request: NoRequest(), Result: TypeOf[JobSnapshot]()},
```

```go
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
    apischema.AttachRunner(router, apischema.RunnerBinding{
        Route:  "things.reindex",
        Runner: runReindex,
        Policy: bridgeipc.SingletonSystem,
    })
}

func runReindex(ctx context.Context, job *bridgeipc.Job, _ bridgeipc.NoRequest) (any, error) {
    job.ReportProgress(map[string]any{"phase": "scanning", "pct": 10})
    return doReindex(ctx)
}
```

## Stream-Style Jobs

Logs, subscriptions, and watch feeds are jobs with `bridgeipc.StreamDefault`.

For text data, emit a progress payload with `type: "data"`:

```go
job.ReportProgress(map[string]any{
    "type": "data",
    "data": line,
})
```

Frontend stream openers can adapt those job progress events back to `onData` bytes while the backend keeps one coherent lifecycle.

## Duplex Pattern

Only interactive bidirectional sessions should be duplex:

```go
{Kind: KindDuplex, Route: "terminal.open", Mode: bridgeipc.ModeDuplex, Request: TypeOf[TerminalOpenRequest](), Result: NoResponse(), NoEndpoint: true},
```

```go
apischema.AttachDuplex(router, apischema.DuplexBinding{
    Route: "terminal.open",
    Handle: func(ctx context.Context, stream net.Conn, req apischema.TerminalOpenRequest) error {
        return HandleTerminalSession(ctx, rt, stream, req)
    },
})
```

Use duplex for terminals and container shells. Do not use it for one-way logs or progress.

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

Prefer declaring privilege in the route spec:

```go
{Kind: KindHandler, Route: "hostname.set_hostname", Privileged: true, Mode: bridgeipc.ModeJob, Request: TypeOf[HostnameRequest](), Result: NoResponse()},
```

The dispatcher handles the check and logs the rejected start centrally.

## Frontend Contract

Frontend route metadata and endpoint types are generated from `apischema`.

| Backend mode | Frontend API |
|--------------|--------------|
| `bridgeipc.ModeQuery` | `linuxio.domain.command()` or `.useQuery()` |
| `bridgeipc.ModeJob` | `linuxio.domain.command()` or `.useMutation()`, returning `JobSnapshot` for job routes |
| `bridgeipc.ModeDuplex` | stream opener |

Direct/query calls get ergonomic input from the Go request:

- no request: `linuxio.system.get_cpu_info()`
- one required field: `linuxio.filebrowser.dir_size(path)`
- object request: `linuxio.docker.system_prune(request)`

Mutations use the full request object as the React Query mutation variable:

```ts
linuxio.docker.start_container.useMutation().mutate({ containerId })
```

Generated files under `frontend/src/api/generated` are not edited by hand.
