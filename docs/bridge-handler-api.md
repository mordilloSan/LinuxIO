# Bridge Handler API

The bridge is the backend process that understands LinuxIO application semantics. The webserver relays authenticated WebSocket/yamux traffic; the bridge parses stream-open payloads, routes them to handlers, runs operations as the session user, and writes relay frames back to the client.

## Core Flow

```text
webserver /ws
    |
    v
yamux stream accepted by bridge
    |
    v
first frame must be OpStreamOpen
    |
    v
ParseStreamOpenPayload: <stream-type>\0<arg1>\0...
    |
    v
handlers.GetStreamHandler(streamType)
    |
    +-- bridge        -> generic.HandleBridgeStream -> ipc registry
    +-- terminal      -> PTY stream
    +-- container     -> Docker exec stream
    +-- service-logs  -> log stream
    +-- general-logs  -> log stream
    +-- docker-logs   -> Docker log stream
    +-- app-update    -> self-update stream
    +-- jobs-*        -> job progress/data/event streams
```

The stream handler registry lives in `backend/bridge/handlers/register.go`:

```go
var streamHandlers = map[string]func(runtime.Runtime, net.Conn, []string) error{}
```

It is populated once by `RegisterAllHandlers(rt)` and then treated as read-only.

## Runtime

Most bridge registration functions receive `runtime.Runtime`:

```go
type Runtime struct {
    Session *session.Session
    Store   *settings.UserStore
}
```

Use `rt.Session` for session identity and privilege state, `rt.Store` for per-user settings, and `rt.Username()` / `rt.Privileged()` for convenience accessors.

## Registration Layers

LinuxIO currently has three related registration layers.

### Stream Handlers

Stream handlers are keyed by stream type and receive the raw `net.Conn`:

```go
func(runtime.Runtime, net.Conn, []string) error
```

Current stream registrations:

| Package | Stream Types |
|---------|--------------|
| `generic` via `register.go` | `bridge` |
| `control` | `app-update` |
| `terminal` | `terminal`, `container` |
| `jobs` | `jobs-attach`, `jobs-data`, `jobs-events` |
| `logs` | `general-logs`, `service-logs`, `docker-logs` |

There are no current direct `fb-*`, `pkg-update`, `docker-compose`, or `docker-indexer` stream handlers. Those operations are job types and use the generic job streams.

### IPC JSON Handlers

Normal request/response commands use the IPC registry in `backend/common/ipc`:

```go
ipc.RegisterFunc("system", "get_cpu_info", func(ctx context.Context, args []string, emit ipc.Events) error {
    return emit.Result(result)
})
```

The public path is a `bridge` stream. Its initial payload is:

```text
bridge\0<handlerType>\0<command>\0<arg1>\0<arg2>...
```

`generic.HandleBridgeStream` looks up the handler with `ipc.Get(handlerType, command)`, adds the session to the context, executes the handler, and closes the stream after the handler returns.

Most packages use the thin helper in `backend/bridge/handlers/internal/rpc`:

```go
rpc.Register("docker", rt, []rpc.Command{
    {Name: "list_containers", Handler: handlers.handleListContainers},
    {Name: "system_prune", Handler: handlers.handleSystemPrune},
})
```

`rpc.Command{Privileged: true}` wraps the handler with `privilege.RequirePrivilegedIPC`.

### Job Runners

Long-running and resumable operations are registered with `backend/bridge/jobs`:

```go
bridgejobs.RegisterRunner("file.upload", runUploadJob)
bridgejobs.RegisterRecoverer("file.indexer", recoverIndexerJob)
bridgejobs.RegisterDataAttacher("file.upload", attachFileTransferData)
```

Jobs are started through JSON IPC:

```text
bridge -> jobs.start(jobType, ...args)
```

Then the frontend attaches with:

| Stream Type | Purpose |
|-------------|---------|
| `jobs-attach` | Progress and final result for a job |
| `jobs-data` | Binary upload/download/archive data for jobs with data attachers |
| `jobs-events` | Job lifecycle events and active job snapshots |

## IPC Handler Contract

`backend/common/ipc/handler.go` defines the handler interface:

```go
type Handler interface {
    Execute(ctx context.Context, args []string, emit Events) error
}

type HandlerFunc func(ctx context.Context, args []string, emit Events) error
```

The `Events` interface supports:

| Method | Frame | Notes |
|--------|-------|-------|
| `Data([]byte)` | `OpStreamData` | Raw bytes |
| `Progress(any)` | `OpStreamProgress` | JSON-serialized progress payload |
| `Result(any)` | `OpStreamResult` | `{ "status": "ok", "data": ... }`; does not close the stream |
| `Error(error, code)` | `OpStreamResult` | `{ "status": "error", "error": "...", "code": code }`; does not close the stream |
| `Close(reason)` | `OpStreamClose` | Reason is currently not serialized |

On success, handlers should emit one final `Result`. The bridge closes the stream after `Execute` returns. If a handler returns an error, `generic.HandleBridgeStream` writes an error result with code `500` and then closes the stream. If an IPC handler needs another status code, it must emit the error explicitly.

## Helper APIs

Use `rpc.Arg`, `rpc.RequireArgs`, and `rpc.DecodeJSONArg` for argument handling:

```go
name, err := rpc.Arg(args, 0)
if err != nil {
    return err
}

var req CreateUserRequest
req, err = rpc.DecodeJSONArg[CreateUserRequest](args, 0)
if err != nil {
    return err
}
```

Use `rpc.EmitResult` for common `(result, err)` functions:

```go
result, err := ListContainers(ctx)
return rpc.EmitResult(emit, result, err)
```

## Relay Protocol

Relay frames are defined in `backend/common/ipc/protocol.go`.

```text
[opcode:1][streamID:4 big-endian][length:4 big-endian][payload:N]
```

| Opcode | Hex | Meaning |
|--------|-----|---------|
| `OpStreamOpen` | `0x80` | Initial stream-open payload |
| `OpStreamData` | `0x81` | Binary data |
| `OpStreamClose` | `0x82` | Graceful close |
| `OpStreamResize` | `0x83` | Terminal resize payload `[cols:2][rows:2]` |
| `OpStreamProgress` | `0x84` | JSON progress payload |
| `OpStreamResult` | `0x85` | JSON result payload |
| `OpStreamAbort` | `0x86` | Client cancellation |

`ReadRelayFrame` and `WriteRelayFrame` enforce a 16 MiB maximum payload size. The yamux config also sets `MaxStreamWindowSize` to 16 MiB.

Convenience writers:

```go
ipc.WriteProgress(w, streamID, progress)
ipc.WriteResultOK(w, streamID, data)
ipc.WriteResultError(w, streamID, message, code)
ipc.WriteResultOKAndClose(w, streamID, data)
ipc.WriteResultErrorAndClose(w, streamID, message, code)
ipc.WriteStreamClose(w, streamID)
```

## Abort Support

For raw stream handlers that need cancellation, use `ipc.AbortContext` or handle `OpStreamAbort` in the stream loop.

```go
ctx, cancelFn, cleanup := ipc.AbortContext(parent, stream)
defer cleanup()

callbacks := &ipc.OperationCallbacks{
    Cancel: cancelFn,
    Progress: func(bytes int64) {
        _ = ipc.WriteProgress(stream, 0, progress)
    },
}
```

Many newer long-running operations use job contexts instead. `jobs.cancel` cancels the job, `jobs-attach` cancels the job when it receives `OpStreamAbort`, and `jobs-events` treats close or abort as detaching from the event feed. `jobs-data` delegates close and abort behavior to the job's data attacher.

## Current Registration

`RegisterAllHandlers(rt)` registers JSON handlers for these packages:

```text
system
accounts
docker
filebrowser
indexer
jobs
config
control
power
dbus
terminal
wireguard
storage
shares
```

It then registers raw stream handlers for:

```text
control
terminal
jobs
logs
```

Some JSON handler packages also register job runners as part of their `RegisterHandlers` call:

| Package | Job Types |
|---------|-----------|
| `filebrowser` | `file.compress`, `file.extract`, `file.copy`, `file.move`, `file.indexer`, `file.upload`, `file.download`, `file.archive`, `file.chmod` |
| `docker` | `docker.compose`, `docker.indexer` |
| `dbus` | `package.update` |
| `storage` | `storage.smart_test` |

## Stream Types Reference

### Universal JSON Stream

| Type | Args | Frames |
|------|------|--------|
| `bridge` | `[handlerType, command, ...args]` | `OpStreamResult`, optional `OpStreamProgress` / `OpStreamData`, then `OpStreamClose` |

### Terminal Streams

| Type | Args | Frames |
|------|------|--------|
| `terminal` | `[cols, rows]` | Raw PTY bytes via `OpStreamData` |
| `container` | `[containerID, shell, cols, rows]` | Docker exec bytes via `OpStreamData` |

### Log And Update Streams

| Type | Args | Frames |
|------|------|--------|
| `docker-logs` | `[containerID, tail]` | `OpStreamData` |
| `service-logs` | `[serviceName, lines]` | `OpStreamData` |
| `general-logs` | `[lines, timePeriod, priority, identifier, ...fieldFilters]` | `OpStreamData` |
| `app-update` | `[runId, version?]` | `OpStreamData`, `OpStreamResult` |

### Job Streams

| Type | Args | Frames |
|------|------|--------|
| `jobs-attach` | `[jobID]` | `OpStreamProgress`, `OpStreamResult` |
| `jobs-data` | `[jobID, offset?]` | `OpStreamData`, `OpStreamProgress`, `OpStreamResult` |
| `jobs-events` | `[]` | `OpStreamProgress` containing job events |

## Handler Package Layout

```text
backend/bridge/handlers/
├── register.go             stream and JSON registration
├── generic/                universal bridge stream dispatcher
├── internal/rpc/           command registration and arg helpers
├── jobs/                   jobs.* IPC commands and jobs-* streams
├── system/                 system.* IPC handlers
├── accounts/               accounts.* IPC handlers
├── docker/                 docker.* IPC handlers and docker job runners
├── filebrowser/            filebrowser.* IPC handlers and file job runners
├── indexer/                indexer.* IPC handlers
├── config/                 config.* IPC handlers
├── control/                control.* IPC handlers and app-update stream
├── power/                  power.* IPC handlers
├── dbus/                   dbus.* IPC handlers and package job runner
├── terminal/               terminal.* IPC handlers plus terminal/container streams
├── wireguard/              wireguard.* IPC handlers
├── storage/                storage.* IPC handlers and SMART job runner
├── shares/                 shares.* IPC handlers
└── logs/                   general/service/docker log streams
```

## Privilege Enforcement

For handlers registered through `rpc.Register`, set `Privileged: true`:

```go
rpc.Register("power", rt, []rpc.Command{
    {Name: "set_profile", Handler: handleSetProfile, Privileged: true},
})
```

This wraps the command with `privilege.RequirePrivilegedIPC(rt.Session, handler)`. If the session is not privileged, the handler returns `operation requires administrator privileges`; the generic bridge currently sends that returned error as an error result with code `500`.

## See Also

- [Frontend API](./frontend-api.md)
- [Server Yamux Protocol](./server-yamux-protocol.md)
- [Privilege Pattern](./PRIVILEGE_PATTERN.md)
