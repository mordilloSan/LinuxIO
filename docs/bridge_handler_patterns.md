# Handler Patterns

This document covers handler style and package organization. Route contracts, frontend shape, transport, modes, Tasks, and endpoint creation live in the canonical [API Contract](./api-contract.md).

## `handlers.go` Layout

`handlers.go` is only route wiring and IPC adapter code. It may contain route
binding variables/functions (`api`, `Routes`, `routeBindings`) in addition to:

- `RegisterHandlers`
- `handle*` adapter functions or methods

It must not contain domain state, package variables/constants unrelated to route
registration, helper functions, validators, parsers, or domain implementations.
Put those in named files beside it, for example:

- `handler_state.go` for small adapter state structs
- `*_operation.go` for mutation/Task orchestration
- domain-specific files such as `timers.go`, `health.go`, `config_operations.go`, or `host_session.go`

Every Call adapter in `handlers.go` receives a typed request. A result-bearing
adapter returns `(Result, error)`; the compiler checks `Result` against the type
declared in the binding:

```go
func handleListTimers(ctx context.Context, _ apischema.NoRequest) ([]apischema.Timer, error) {
    return ListTimers(ctx)
}

func handleGetUnitInfo(ctx context.Context, req apischema.UnitNameRequest) (apischema.UnitInfo, error) {
    return GetUnitInfo(ctx, req.UnitName)
}
```

Void routes declare `apischema.NoResponse` and use `HandleVoid`; the adapter
returns the implementation error directly and the binding keeps the wire result
null:

```go
func handleSetMTU(ctx context.Context, req apischema.InterfaceMTURequest) error {
    return SetMTU(ctx, req.Iface, req.MTU)
}
```

The implementation belongs outside `handlers.go`:

```go
func ListTimers(ctx context.Context) ([]apischema.Timer, error) {
    // actual implementation
}
```

Handlers do not receive a universal emitter. Calls and Mutations use
`func(context.Context, Request) (Result, error)` (or `error` with
`HandleVoid` and `MutationVoid`); Channels use
`func(context.Context, net.Conn, Request) error` for the lifetime of the stream.
A write, quick or long, is a Mutation. The runners that were Task routes keep
`func(context.Context, *bridgeipc.Task, Request) (Result, error)` through
`apischema.MutationTask`, which hands them their task; a new handler takes no
task and reports through `bridgeipc.ReportProgress(ctx, …)`. The compiler checks
each terminal result against its route declaration. `Progress[T]()` (on Task
routes `WithTaskProgress[T]()`) declares the route-specific detail carried by
the common `TaskProgress<T>` envelope. Detail types implement
`ProgressEnvelope()`; a Mutation handler passes the envelope to
`bridgeipc.ReportProgress(ctx, …)`, a Task runner to `task.ReportProgress()`.
Task data uses the focused data-attachment Channel.

## Mutations

A route that changes the host is declared with `apischema.Mutation` (or
`apischema.MutationVoid`) and runs as a job in the caller's job host (see
[API Contract](./api-contract.md#tasks-and-jobs)). The declaration takes the
handler, and Go infers the request and result types from it:

From `backend/bridge/handlers/docker/background_operations.go`:

```go
apischema.Mutation(
    "docker.update_container",
    handleUpdateContainer,
    apischema.Privileged(),
    apischema.Progress[apischema.DockerContainerUpdateProgress](),
)

func handleUpdateContainer(ctx context.Context, req apischema.DockerContainerUpdateRequest) (apischema.DockerContainerUpdateResult, error) {
    if err := dockerupdate.ValidateTarget(req.ContainerID); err != nil {
        return apischema.DockerContainerUpdateResult{}, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
    }
    // A job queues behind another update: lock wait 0 waits until ctx ends.
    result, err := updateContainer(ctx, req.ContainerID, 0, func(phase, message string) {
        bridgeipc.ReportProgress(ctx, apischema.DockerContainerUpdateProgress{Phase: phase, Message: message}.ProgressEnvelope())
    })
    if err != nil {
        return apischema.DockerContainerUpdateResult{}, err
    }
    if result.Updated {
        slog.Info("updated Docker container", "component", "docker", "subsystem", "update",
            "container", result.ContainerName, "old_image", result.PreviousImageID, "new_image", result.NewImageID)
    }
    return result, nil
}
```

- The handler has the Call signature. Its `ctx` is cancelled by `tasks.cancel`,
  a stream abort or the host stopping, never by the browser going away, so
  pass it to every callee and clean up partial work when it ends.
- Report progress with `bridgeipc.ReportProgress(ctx, detail.ProgressEnvelope())`.
  Outside a job it does nothing, so the same function can serve a Call.
- Name the target with a method on the request type, not in the declaration
  (`apischema.DockerContainerUpdateRequest` aliases the type in
  `backend/common/dockerupdate/types.go`):
  `func (r DockerContainerUpdateRequest) Target() []string { return []string{r.ContainerID} }`.
  The host then refuses a second job of the route on the same target while the
  first runs. A request type without `Target()` is never refused as a
  duplicate.
- Log the completed change at Info once, as for any host mutation; the task
  runner logs a failed job, so the handler returns its error without logging it.

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

Request lifecycle logging is centralized in `backend/bridge/internal/bridgeipc`. The router logs the route, mode, user, outcome, duration, and error at debug level.

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

The `tasks.*` namespace is reserved by `bridgeipc`. Its public contracts live in
the `tasks` handler family, while the router's `TaskService` registers and owns
their implementations. Domain packages must not register routes in that
namespace.

## Where Shared Code Lives

`backend/bridge/handlers/import_boundary_test.go` encodes the layering rules;
this section states them in words:

- **Handler families are leaves.** A route family (`docker`, `storage`,
  `capabilities`, ...) lives in `backend/bridge/handlers/<family>`. It imports
  `apischema`, `bridge/internal/*`, `common/*` and the daemon `api` packages
  (`indexer/api`, `monitoring/api`), and nothing outside `bridge/handlers`
  imports a family, apart from the composition root (`bridge/cmd`) and the
  contract generator (`bridge/internal/apigen`), which read the registry in
  `bridge/handlers/register.go`. Families never import the registry.
- **Families do not import each other**, except for the edges listed with a
  reason in `allowedHandlerEdges` (capability probes, the indexer and
  monitoring clients, logind logoff on account deletion, the Docker
  mount-ordering drop-in). A new edge needs a one-line reason there; code two
  families need is a library instead.
- **`common` never imports a process tree.** `backend/common/*` imports only
  other `common` packages.
- **The webserver imports only `common` and its own packages.**
- **Daemons import only `common` and their own tree.** `indexer` and
  `monitoring` keep their internals private; the bridge reaches them only
  through `indexer/api` and `monitoring/api`.
- **Worker binaries import only `common`.** This covers `cli`, `docker-update`
  and `btrfs-timer`.

Where a shared library goes depends on who needs it:

| Needed by | Home | Examples |
|-----------|------|----------|
| One route family | `backend/bridge/handlers/<family>` | everything a family alone uses |
| Several bridge families only | `backend/bridge/internal/<name>` | `usercmd`, `acpagents`, `loginhistory`, `bridgeipc`, `goroutinelabel`, `streamrelay`, `dockerclient`, `userconfig`, `apigen` |
| The bridge and another binary (CLI, workers, daemons) | `backend/common/<name>` | `systemd`, `dbusclient`, `btrfs`, `alerts`, `atomicfile`, `hostcmd`, `hwprobe`, `iolimit`, `osrelease`, `updatestatus`, `dockerupdate`, `jobs`, `version` |

There is no catch-all `utils` package; each shared helper lives in a package
named for its responsibility. The Docker update engine lives in
`common/dockerupdate`, shared by the scheduled `docker-update` binary and the
`docker.update_container` job. The `capabilities` family owns `system.get_capabilities`
and the capability registry that `packages` reads to install them.

## File Naming

Handler packages should name files after the domain operation they implement, not after IPC mechanics.

| Location | Allowed naming | Avoid |
|----------|----------------|-------|
| `backend/bridge/internal/bridgeipc` | Framework terms such as `tasks.go`, `task_primitives.go`, `router.go` | Domain-specific handler code |
| `backend/bridge/handlers/<domain>` | Domain terms such as `package_update_operation.go`, `package_update_operation.go`, `journal_follow.go`, `host_session.go`, `smart_test_operation.go` | Generic `tasks.go`, `stream.go`, `rpc.go`, `handler.go` |

Use `operation` for Mutations, Task-backed work, or other long-running work, `follow` for
log/watch Channels, and `session` for bidirectional Channels. The route mode
still lives in the route spec; the filename should help humans find the domain
behavior without implying a second IPC subsystem.

## Request Validation

Request JSON is decoded before the handler runs. Normal route bindings and the
reserved Task service use the same strict `encoding/json/v2.Unmarshal` path
with `RejectUnknownMembers(true)`. Unknown or case-mismatched fields, duplicate
names, invalid UTF-8, and trailing data are rejected; scalar type failures are
`*json.SemanticError`. A missing or `null` request produces the typed zero
value. Required fields remain domain validation because zero values alone
cannot prove presence. Use typed fields directly:

```go
func handleStartService(ctx context.Context, req apischema.ServiceNameRequest) error {
    if req.ServiceName == "" {
        return fmt.Errorf("%w: service name", bridgeipc.ErrInvalidArgs)
    }
    return StartUnit(ctx, req.ServiceName)
}
```

Wrap a status sentinel with `%w` for typed client errors: `ErrInvalidArgs` (400),
`ErrForbidden` (403), `ErrNotFound` (404), `ErrConflict` (409). The text after
the sentinel names the subject, not the condition. Use
`bridgeipc.NewError(message, code)` only for a code no sentinel expresses.

## Call Retry Safety

Calls default to no retry. Add `apischema.RetrySafe()` only when repeating the
Call after a lost result cannot cause a user-visible mutation:

```go
apischema.Call[apischema.NoRequest, apischema.SystemInfo](
    "system.get_system_info",
    apischema.RetrySafe(),
).Handle(handleGetSystemInfo)
```

The Go option is emitted into generated route metadata; handlers and frontend
call sites must not infer safety from `get_`, `list_`, or another route-name
pattern. Task starts and mutating Calls remain no-retry; a Mutation is never
retried as a Call, its client resends or reattaches by the job ID instead.
Return structured
`bridgeipc.Error` codes when feature code must distinguish outcomes; human error
messages are presentation, not control flow.

## Privilege

Declare route-level privilege in `apischema`. See [Privilege Pattern](./privilege_pattern.md) for the policy rule.

Handlers may still validate operation-specific policy, but they should not duplicate the route-level admin check.
