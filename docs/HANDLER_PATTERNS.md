# Bridge Handler Patterns

**Status:** Accepted/current
**Last updated:** 2026-05-10
**Companion docs:** [Bridge Handler API](./bridge-handler-api.md), [Frontend API](./frontend-api.md), [Privilege Pattern](./PRIVILEGE_PATTERN.md)

This document describes how bridge handler packages should be written. The wire protocol, relay frame format, stream inventory, and IPC `Events` contract live in [bridge-handler-api.md](./bridge-handler-api.md). This file is about code shape inside `backend/bridge/handlers`.

## Current Foundation

The handler migration proposed by the original ADR has landed. Current code uses:

| Area | Current Code |
|------|--------------|
| Runtime dependencies | `backend/bridge/runtime.Runtime` carries `Session` and `Store` |
| Settings domain | `backend/bridge/settings`; `handlers/config` contains IPC handlers only |
| IPC registration | `backend/bridge/handlers/internal/rpc` |
| Privilege wrapping | `rpc.Command{Privileged: true}` |
| Raw streams | `map[string]func(runtime.Runtime, net.Conn, []string) error` |
| Long-running operations | `backend/bridge/jobs`, attached through `jobs-*` streams |

`RegisterAllHandlers(rt)` in `backend/bridge/handlers/register.go` is the single top-level registration point. It registers JSON handlers first and raw stream handlers after that.

## Runtime

Every JSON handler package exposes:

```go
func RegisterHandlers(rt runtime.Runtime)
```

`Runtime` is passed by value and contains two pointers:

```go
type Runtime struct {
    Session *session.Session
    Store   *settings.UserStore
}
```

Use `rt.Session` when the handler needs session identity, session timing, UID, or privilege state. Use `rt.Store` for per-user settings. `rt.Username()` and `rt.Privileged()` are convenience accessors and assume a non-nil runtime.

Stream handlers also receive `runtime.Runtime` at execution time, not registration time.

## RPC Helper

`backend/bridge/handlers/internal/rpc` is intentionally small:

```go
type Command struct {
    Name       string
    Handler    ipc.HandlerFunc
    Privileged bool
}

func Register(component string, rt runtime.Runtime, commands []Command)
func Arg(args []string, i int) (string, error)
func RequireArgs(args []string, n int) error
func DecodeJSONArg[T any](args []string, i int) (T, error)
func EmitResult(emit ipc.Events, result any, err error) error
```

Do not add business-shape adapters here. Avoid helpers named like `NoArgCall`, `OneArgCall`, `LoggedCall`, `UserCall`, or anything that dispatches by argument count. A few explicit lines in a handler are easier to read than another adapter vocabulary.

Logging is not abstracted. Log the interesting domain action inline in the handler body.

## JSON Handler Patterns

### Stateless Handlers

Use free functions when a command does not need session or store state.

```go
func RegisterHandlers(rt runtime.Runtime) {
    rpc.Register("accounts", rt, []rpc.Command{
        {Name: "list_users", Handler: handleListUsers},
        {Name: "delete_user", Handler: handleDeleteUser},
    })
}

func handleListUsers(ctx context.Context, args []string, emit ipc.Events) error {
    result, err := ListUsers()
    return rpc.EmitResult(emit, result, err)
}
```

This is the normal shape for packages like `accounts`, `control`, `dbus`, `jobs`, `power`, `shares`, `storage`, `terminal`, and `wireguard`.

### Stateful Handlers

Use a small receiver when multiple commands need the same runtime state. Current code uses both forms:

```go
type systemHandlers struct {
    rt runtime.Runtime
}

func RegisterHandlers(rt runtime.Runtime) {
    h := systemHandlers{rt: rt}
    rpc.Register("system", rt, []rpc.Command{
        {Name: "get_health_summary", Handler: h.handleGetHealthSummary},
    })
}
```

```go
type configHandlers struct {
    username string
    store    *settings.UserStore
}

func RegisterHandlers(rt runtime.Runtime) {
    h := configHandlers{username: rt.Username(), store: rt.Store}
    rpc.Register("config", rt, []rpc.Command{
        {Name: "get", Handler: h.handleGetConfig},
        {Name: "set", Handler: h.handleSetConfig},
    })
}
```

Prefer carrying `runtime.Runtime` when the handler needs session fields or privilege state. Extracting `username` and `store` is fine for settings-only packages. Avoid per-command closure factories such as `handleX(username, store) ipc.HandlerFunc`.

### Argument Handling

Use `rpc.Arg` for a single positional argument and `rpc.RequireArgs` when several positions are required.

```go
func (h dockerHandlers) handleComposeUp(ctx context.Context, args []string, emit ipc.Events) error {
    projectName, err := rpc.Arg(args, 0)
    if err != nil {
        return err
    }
    composePath := ""
    if len(args) >= 2 {
        composePath = args[1]
    }
    result, err := ComposeUpWithStore(h.username, h.store, projectName, composePath)
    return rpc.EmitResult(emit, result, err)
}
```

For JSON payloads, decode at the IPC boundary:

```go
func handleCreateSambaShare(ctx context.Context, args []string, emit ipc.Events) error {
    if err := rpc.RequireArgs(args, 2); err != nil {
        return err
    }
    name := args[0]
    properties, err := rpc.DecodeJSONArg[map[string]string](args, 1)
    if err != nil {
        return err
    }
    err = CreateSambaShare(name, properties)
    return rpc.EmitResult(emit, map[string]any{"success": true, "name": name}, err)
}
```

Malformed or missing arguments currently return errors that `generic.HandleBridgeStream` serializes as code `500`, unless the handler explicitly emits its own error result. Keep that in mind for user-facing validation paths.

### Privileged Handlers

Privilege is registration metadata:

```go
func RegisterHandlers(rt runtime.Runtime) {
    rpc.Register("power", rt, []rpc.Command{
        {Name: "get_status", Handler: handleGetStatus, Privileged: true},
        {Name: "set_profile", Handler: handleSetProfile, Privileged: true},
    })
}
```

`rpc.Register` wraps privileged commands with `privilege.RequirePrivilegedIPC(rt.Session, handler)`. Handler bodies should not check `sess.Privileged` for the normal privilege gate. See [PRIVILEGE_PATTERN.md](./PRIVILEGE_PATTERN.md) for policy.

Current privileged packages include:

| Package | Commands |
|---------|----------|
| `indexer` | `get_config`, `get_status`, `set_config` |
| `power` | `get_status`, `start`, `set_profile`, `disable` |
| `system` | `list_failed_login_events` |

### Progress Handlers

Progress uses the same IPC handler signature. Pass `emit` down to the operation that reports progress.

```go
func handleResourcePatch(ctx context.Context, args []string, emit ipc.Events) error {
    slog.Info("resource_patch requested", "component", "filebrowser")
    result, err := resourcePatchWithProgress(ctx, args, emit)
    return rpc.EmitResult(emit, result, err)
}
```

Do not invent a separate progress-handler adapter.

## Job Patterns

Long-running or resumable work should be a job, not a package-specific raw stream. Register job runners from the owning package's `RegisterHandlers`.

```go
func RegisterHandlers(rt runtime.Runtime) {
    RegisterJobRunners(rt.Store)
    rpc.Register("filebrowser", rt, commands)
}
```

Jobs start through the JSON command `jobs.start(jobType, ...args)`. The frontend then uses the generic job streams:

| Stream | Use |
|--------|-----|
| `jobs-attach` | Progress and final result |
| `jobs-data` | Upload/download/archive bytes for jobs with data attachers |
| `jobs-events` | Active job snapshots and lifecycle events |

Current job runner owners:

| Package | Job Types |
|---------|-----------|
| `filebrowser` | `file.compress`, `file.extract`, `file.copy`, `file.move`, `file.indexer`, `file.upload`, `file.download`, `file.archive`, `file.chmod` |
| `docker` | `docker.compose`, `docker.indexer` |
| `dbus` | `package.update` |
| `storage` | `storage.smart_test` |

Only add a raw stream when the operation is truly interactive or stream-native. File operations, package updates, Docker compose actions, and indexer work should stay job-backed.

## Raw Stream Patterns

Raw stream handlers use the runtime-aware signature:

```go
func(runtime.Runtime, net.Conn, []string) error
```

Package stream registration only receives the registry map:

```go
func RegisterStreamHandlers(handlers map[string]func(runtime.Runtime, net.Conn, []string) error) {
    handlers["app-update"] = HandleAppUpdateStream
}
```

The dispatcher in `backend/bridge/main.go` reads the first `OpStreamOpen` frame, parses the stream type and args, fetches the stream handler from `handlers.GetStreamHandler`, and calls `handler(rt, stream, args)`.

Current raw stream registrations:

| Stream Type | Registered By | Handler |
|-------------|---------------|---------|
| `bridge` | `handlers/register.go` | `generic.HandleBridgeStream` |
| `app-update` | `control` | `control.HandleAppUpdateStream` |
| `terminal` | `terminal` | `terminal.HandleTerminalStream` |
| `container` | `terminal` | `terminal.HandleContainerTerminalStream` |
| `jobs-attach` | `jobs` | `jobs.HandleAttachStream` |
| `jobs-data` | `jobs` | `jobs.HandleDataStream` |
| `jobs-events` | `jobs` | `jobs.HandleEventsStream` |
| `general-logs` | `logs` | `logs.HandleGeneralLogsStream` |
| `service-logs` | `logs` | `logs.HandleServiceLogsStream` |
| `docker-logs` | `logs` | `docker.HandleDockerLogsStream` |

The old names `fb-upload`, `fb-download`, `fb-archive`, `fb-compress`, `fb-extract`, `fb-copy`, `fb-move`, `pkg-update`, `docker-compose`, and `docker-indexer` are not registered raw streams. They are job-backed behavior or stale names from earlier docs.

## Settings Boundary

Settings domain code lives in `backend/bridge/settings`. `backend/bridge/handlers/config` owns the IPC contract for config reads and writes:

| Code | Owns |
|------|------|
| `bridge/settings` | `Settings`, `UserStore`, defaults, validation, persistence, snapshots |
| `handlers/config` | `config.get`, `config.set`, JSON payload structs, IPC validation |

Do not import `handlers/config` from other handler packages to reach settings types. Import `bridge/settings`.

## Code Organization

Keep handler packages split by domain when the command set is large. The Docker package is the current example:

```text
handlers.go
handlers_containers.go
handlers_compose.go
handlers_icons.go
handlers_caddy.go
handlers_images_networks_volumes.go
handlers_info_prune.go
```

`handlers.go` should register commands and define the small receiver. Per-domain files should hold the handler bodies for that domain.

Business functions may still have legacy names such as `ListComposeProjectsWithStore` or `runArchiveJobWithStore`. Those are domain implementation details, not IPC adapter patterns. Do not introduce new compatibility wrappers just to preserve an old handler shape.

## Testing Guidance

The handler layer should stay thin. Prefer testing business functions directly when they do the real work.

When testing handler bodies, create the minimum runtime needed by the handler:

```go
rt := runtime.Runtime{
    Session: &session.Session{User: session.User{Username: "alice"}},
    Store:   store,
}
```

Handlers that call `rt.Username()`, `rt.Privileged()`, or read `rt.Session`/`rt.Store` need non-nil fields. Stream handlers that ignore runtime can use an empty runtime in narrowly scoped tests, but a populated runtime is clearer when setup is cheap.

Delete tests that only prove an adapter calls a function. That adapter layer is gone; test the handler behavior or the business function instead.

## Drift Checks

Useful checks when editing handler infrastructure:

```bash
rg 'handlers/config' backend/bridge
rg 'NoArgCall|OneArgCall|UserCall|LoggedCall|emitSystemCall|emitFilebrowser|dbusNoArgResult|oneArgActionHandler' backend/bridge/handlers
rg 'func RegisterHandlers' backend/bridge/handlers
rg 'func RegisterStreamHandlers' backend/bridge/handlers
```

Expected state:

| Check | Expected |
|-------|----------|
| `handlers/config` imports | Only the real `handlers/config` package and top-level registration |
| adapter-name search | No handler-shape adapters |
| `RegisterHandlers` | Accepts `runtime.Runtime` |
| `RegisterStreamHandlers` | Accepts only the stream registry map |

Run package tests for the area you touched. For shared handler infrastructure, run broader bridge tests.
