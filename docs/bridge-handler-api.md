# Bridge Handler API

## Core Principle

**The bridge is the only component that understands application semantics.** It parses StreamFrames, routes to handlers based on stream type, and sends responses.

```
Bridge's job:
  1. Accept yamux streams from webserver (via inherited FD)
  2. Parse StreamFrame opcode + payload
  3. Route to appropriate handler based on stream type
  4. Handler sends response frames
  5. Close stream when done
```

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                         Bridge                                │
│                                                               │
│  ┌─────────────────────────────────────────────────────┐    │
│  │           Yamux Session (server-side)                │    │
│  │  - Accepts yamux streams from webserver             │    │
│  │  - One session per authenticated login              │    │
│  └────────────────────┬────────────────────────────────┘    │
│                       │                                       │
│  ┌────────────────────┴────────────────────────────────┐    │
│  │           Stream Router                              │    │
│  │  - Reads initial OpStreamOpen frame                 │    │
│  │  - Dispatches on stream type:                       │    │
│  │    • "bridge"             → BridgeHandler (JSON)    │    │
│  │    • "terminal"           → TerminalHandler         │    │
│  │    • "container"          → ContainerHandler        │    │
│  │    • "docker-logs"        → DockerLogsHandler       │    │
│  │    • "fb-upload"          → FileUploadHandler       │    │
│  │    • ... (see full list below)                      │    │
│  └────────────────────┬────────────────────────────────┘    │
│                       │                                       │
│       ┌───────────────┼───────────────┐                     │
│       ▼               ▼               ▼                     │
│  ┌─────────┐   ┌──────────┐   ┌──────────┐                │
│  │  Bridge │   │ Terminal │   │   File   │  ... handlers  │
│  │ Handler │   │ Handler  │   │ Handler  │                │
│  │ (JSON)  │   │          │   │          │                │
│  └────┬────┘   └──────────┘   └──────────┘                │
│       │                                                      │
│  ┌────▼────────────────────────────────────────────────┐   │
│  │  IPC Handler Registry  (ipc.Register / RegisterFunc) │   │
│  │  "system" → get_cpu_info, get_memory_info, ...       │   │
│  │  "docker" → list_containers, start_container, ...    │   │
│  │  "dbus"   → list_services, reboot, ...              │   │
│  │  ...                                                  │   │
│  └───────────────────────────────────────────────────────┘   │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

## Two-Tier Handler System

The bridge uses two separate registration systems:

### 1. IPC (JSON) Handlers — `ipc.RegisterFunc`

Used for all request/response calls (the majority of the API). Registered per `(handlerType, command)` pair. All go through the `"bridge"` stream type.

```go
ipc.RegisterFunc("system", "get_cpu_info", func(ctx context.Context, args []string, emit ipc.Events) error {
    info := getCPUInfo()
    return emit.Result(info)
})
```

The `"bridge"` stream handler (`generic.HandleBridgeStream`) parses the args `["handlerType", "command", arg1, arg2, ...]` and dispatches to the registered IPC handler.

### 2. Stream Handlers — `streamHandlers` map

Used for persistent or binary streams (terminal, file I/O, logs). Registered directly into a `map[string]func(*session.Session, net.Conn, []string) error` keyed by stream type.

```go
streamHandlers["terminal"] = HandleTerminalStream
streamHandlers["fb-upload"] = HandleUploadStream
```

## StreamFrame Protocol

### Frame Structure

```
┌─────────────┬─────────────┬─────────────┬───────────────────┐
│ Opcode      │ Stream ID   │ Length      │ Payload           │
│ (1 byte)    │ (4 bytes BE)│ (4 bytes BE)│ (N bytes)         │
└─────────────┴─────────────┴─────────────┴───────────────────┘
```

### Opcodes

```go
const (
    OpStreamOpen     byte = 0x80 // Open stream: payload = "type\0arg1\0arg2..."
    OpStreamData     byte = 0x81 // Binary data: payload = raw bytes
    OpStreamClose    byte = 0x82 // Close stream: payload = empty
    OpStreamResize   byte = 0x83 // Terminal resize: payload = [cols:2][rows:2]
    OpStreamProgress byte = 0x84 // Progress update: payload = JSON
    OpStreamResult   byte = 0x85 // Final result: payload = JSON ResultFrame
    OpStreamAbort    byte = 0x86 // Abort operation: client requests cancellation
)
```

Maximum payload size: **16 MiB** (enforced by `ReadRelayFrame`).

### Reading / Writing Frames

```go
// Read a frame from any io.Reader (yamux stream, net.Conn, etc.)
func ReadRelayFrame(r io.Reader) (*StreamFrame, error)

// Write a frame to any io.Writer
func WriteRelayFrame(w io.Writer, f *StreamFrame) error
```

Convenience helpers:

```go
ipc.WriteResultOK(w, streamID, data)         // Sends OpStreamResult {status:"ok", data:...}
ipc.WriteResultError(w, streamID, msg, code) // Sends OpStreamResult {status:"error", ...}
ipc.WriteProgress(w, streamID, progress)     // Sends OpStreamProgress
ipc.WriteStreamClose(w, streamID)            // Sends OpStreamClose
```

## Handler Interface

### IPC Handler

```go
// Handler is the interface all JSON/IPC handlers must implement.
type Handler interface {
    Execute(ctx context.Context, args []string, emit Events) error
}

// HandlerFunc is a function adapter.
type HandlerFunc func(ctx context.Context, args []string, emit Events) error

// BidirectionalHandler extends Handler for streams that also receive client data.
type BidirectionalHandler interface {
    Handler
    ExecuteWithInput(ctx context.Context, args []string, emit Events, input <-chan []byte) error
}
```

### Events Interface

```go
type Events interface {
    Data(chunk []byte) error      // OpStreamData  — raw binary chunk
    Progress(progress any) error  // OpStreamProgress — JSON-serialized progress
    Result(result any) error      // OpStreamResult {status:"ok", data:...}
    Error(err error, code int) error // OpStreamResult {status:"error", ...}
    Close(reason string) error    // OpStreamClose — explicit early termination
}
```

The framework closes the stream automatically when `Execute()` returns. Handlers only need to call `Close()` if they want to abort early.

### Stream Handler Signature

Raw stream handlers (terminal, filebrowser, logs, etc.) use a different signature:

```go
func HandleTerminalStream(sess *session.Session, stream net.Conn, args []string) error
```

- `sess` carries `SessionID`, `Privileged`, and `User` (username, UID, GID).
- `stream` is the raw yamux `net.Conn` — reads and writes StreamFrames directly.
- `args` are the null-separated arguments from the OpStreamOpen payload.

## Handler Examples

### IPC Handler (Request/Response)

```go
// In system/handlers.go

func RegisterHandlers(sess *session.Session) {
    ipc.RegisterFunc("system", "get_cpu_info",
        func(ctx context.Context, args []string, emit ipc.Events) error {
            info, err := fetchCPUInfo()
            if err != nil {
                return err // framework sends error result automatically
            }
            return emit.Result(info)
        })

    // Privileged handler — wrapped at registration time
    ipc.RegisterFunc("system", "dangerous_op",
        privilege.RequirePrivilegedIPC(sess, func(ctx context.Context, args []string, emit ipc.Events) error {
            return doSomethingPrivileged()
        }))
}
```

### IPC Handler with Progress

```go
ipc.RegisterFunc("dbus", "install_package",
    func(ctx context.Context, args []string, emit ipc.Events) error {
        if len(args) < 1 {
            return ipc.ErrInvalidArgs
        }
        packageID := args[0]

        for step := range installSteps(packageID) {
            emit.Progress(map[string]any{
                "type":    "status",
                "message": step.Description,
                "pct":     step.Percent,
            })
        }
        return emit.Result(nil)
    })
```

### Raw Stream Handler (Terminal)

```go
// In terminal/stream.go

func HandleTerminalStream(sess *session.Session, stream net.Conn, args []string) error {
    // args[0] = cols, args[1] = rows
    cols, rows := 120, 32
    if len(args) >= 2 {
        cols, _ = strconv.Atoi(args[0])
        rows, _ = strconv.Atoi(args[1])
    }

    cmd := exec.Command("/bin/bash")
    ptyFile, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
    if err != nil {
        return err
    }
    defer ptyFile.Close()

    // PTY → stream
    go func() {
        buf := make([]byte, 32*1024)
        for {
            n, err := ptyFile.Read(buf)
            if n > 0 {
                ipc.WriteRelayFrame(stream, &ipc.StreamFrame{Opcode: ipc.OpStreamData, Payload: buf[:n]})
            }
            if err != nil {
                return
            }
        }
    }()

    // stream → PTY (reads OpStreamData / OpStreamClose / OpStreamResize / OpStreamAbort)
    for {
        frame, err := ipc.ReadRelayFrame(stream)
        if err != nil {
            return nil
        }
        switch frame.Opcode {
        case ipc.OpStreamData:
            ptyFile.Write(frame.Payload)
        case ipc.OpStreamResize:
            cols := binary.BigEndian.Uint16(frame.Payload[0:2])
            rows := binary.BigEndian.Uint16(frame.Payload[2:4])
            pty.Setsize(ptyFile, &pty.Winsize{Rows: rows, Cols: cols})
        case ipc.OpStreamClose, ipc.OpStreamAbort:
            return nil
        }
    }
}
```

### Stream Handler with Abort Support

For long-running operations, use `ipc.AbortContext` to get a context that is cancelled on `OpStreamAbort`:

```go
func handleCompress(stream net.Conn, args []string) error {
    ctx, cancelFn, cleanup := ipc.AbortContext(context.Background(), stream)
    defer cleanup()

    // Pass cancelFn to OperationCallbacks
    callbacks := &ipc.OperationCallbacks{
        Cancel: cancelFn,
        Progress: func(bytes int64) {
            ipc.WriteProgress(stream, 0, FileProgress{Bytes: bytes, Total: total})
        },
    }

    return compressFiles(ctx, paths, dest, callbacks)
}
```

## Registration

### RegisterAllHandlers

```go
// backend/bridge/handlers/register.go

func RegisterAllHandlers(sess *session.Session) {
    // "bridge" is the universal JSON dispatcher
    streamHandlers["bridge"] = generic.HandleBridgeStream

    // Register IPC handlers (JSON request/response)
    system.RegisterHandlers(sess)
    monitoring.RegisterHandlers()
    accounts.RegisterHandlers()
    docker.RegisterHandlers(sess)
    filebrowser.RegisterHandlers()
    config.RegisterHandlers(sess)
    control.RegisterHandlers()
    dbus.RegisterHandlers()
    terminal.RegisterHandlers(sess)
    wireguard.RegisterHandlers()
    storage.RegisterHandlers()
    shares.RegisterHandlers()

    // Register raw stream handlers
    control.RegisterStreamHandlers(streamHandlers)   // app-update
    terminal.RegisterStreamHandlers(streamHandlers)  // terminal, container
    filebrowser.RegisterStreamHandlers(streamHandlers) // fb-*
    dbus.RegisterStreamHandlers(streamHandlers)      // pkg-update
    docker.RegisterStreamHandlers(streamHandlers)    // docker-logs, docker-compose, docker-indexer*
    logs.RegisterStreamHandlers(streamHandlers)      // service-logs, general-logs
}
```

### Handler Package Structure

```
backend/bridge/handlers/
├── register.go               // RegisterAllHandlers, GetStreamHandler, streamHandlers map
├── generic/                  // HandleBridgeStream — JSON IPC dispatcher
├── system/                   // system.* IPC handlers
├── monitoring/               // monitoring.* IPC handlers
├── accounts/                 // accounts.* IPC handlers
├── docker/                   // docker.* IPC + docker-logs/docker-compose/docker-indexer streams
├── dbus/                     // dbus.* IPC + pkg-update stream
├── terminal/                 // terminal.* IPC + terminal/container streams
├── filebrowser/              // filebrowser.* IPC + fb-* streams
├── config/                   // config.* IPC handlers
├── control/                  // control.* IPC + app-update stream
├── wireguard/                // wireguard.* IPC handlers
├── storage/                  // storage.* IPC handlers
├── shares/                   // shares.* IPC handlers
├── logs/                     // service-logs, general-logs stream handlers
└── indexer/                  // indexer client (used by filebrowser and docker)
```

## Stream Types Reference

### Universal JSON Stream

| Type | Args | Frames Sent | Description |
|------|------|-------------|-------------|
| `bridge` | `[handlerType, command, ...args]` | OpStreamResult | Dispatches to IPC handler registry |

### Terminal Streams (Persistent)

| Type | Args | Frames Sent | Closes When |
|------|------|-------------|-------------|
| `terminal` | `[cols, rows]` | OpStreamData (PTY output) | Client closes or PTY exits |
| `container` | `[containerID, shell, cols, rows]` | OpStreamData (exec output) | Client closes or exec exits |

### Docker Streams

| Type | Args | Frames Sent | Closes When |
|------|------|-------------|-------------|
| `docker-logs` | `[containerID, tail]` | OpStreamData (log lines) | Container removed or client closes |
| `docker-compose` | `[action, projectName, composePath?]` | OpStreamData (output) | Operation completes |
| `docker-indexer` | none | OpStreamProgress, OpStreamResult | Indexing complete |
| `docker-indexer-attach` | none | OpStreamProgress, OpStreamResult | Attach to running indexer |

### File Browser Streams

| Type | Args | Frames Sent | Closes When |
|------|------|-------------|-------------|
| `fb-upload` | `[path, size, override?]` | OpStreamProgress, OpStreamResult | Upload complete |
| `fb-download` | `[path]` | OpStreamData, OpStreamProgress, OpStreamResult | Download complete |
| `fb-archive` | `[format, ...paths]` | OpStreamData, OpStreamProgress, OpStreamResult | Archive streamed |
| `fb-compress` | `[format, dest, ...paths]` | OpStreamProgress, OpStreamResult | Archive created |
| `fb-extract` | `[archive, dest?]` | OpStreamProgress, OpStreamResult | Extraction complete |
| `fb-reindex` | `[path?]` | OpStreamProgress, OpStreamResult | Reindex complete |
| `fb-indexer-attach` | none | OpStreamProgress, OpStreamResult | Attach to running reindex |
| `fb-copy` | `[source, destination]` | OpStreamProgress, OpStreamResult | Copy complete |
| `fb-move` | `[source, destination]` | OpStreamProgress, OpStreamResult | Move complete |

### System / Log Streams

| Type | Args | Frames Sent | Closes When |
|------|------|-------------|-------------|
| `pkg-update` | `[...packageIDs]` | OpStreamProgress, OpStreamResult | Update complete |
| `app-update` | `[runId, version?]` | OpStreamData, OpStreamResult | Update script exits |
| `service-logs` | `[serviceName, lines]` | OpStreamData | Client closes |
| `general-logs` | `[lines, timePeriod, priority, identifier]` | OpStreamData | Client closes |

## Error Handling

```go
// Send error result and close
ipc.WriteResultErrorAndClose(stream, streamID, "not found", 404)

// Send success result and close
ipc.WriteResultOKAndClose(stream, streamID, data)

// Inside ipc.RegisterFunc — just return an error
ipc.RegisterFunc("system", "some_cmd", func(ctx context.Context, args []string, emit ipc.Events) error {
    return fmt.Errorf("something went wrong") // framework wraps this as error result
})
```

## Privilege Enforcement

Use `privilege.RequirePrivilegedIPC` at registration time:

```go
func RegisterHandlers(sess *session.Session) {
    ipc.RegisterFunc("wireguard", "add_interface",
        privilege.RequirePrivilegedIPC(sess, handleAddInterface))
}
```

`sess.Privileged` is set by the auth daemon (based on `sudo -v`) and is immutable for the session lifetime.

## See Also

- [Frontend API](./frontend-api.md) - Client-side implementation
- [Server Yamux Protocol](./server-yamux-protocol.md) - Server relay implementation
- [Privilege Pattern](./PRIVILEGE_PATTERN.md) - Authorization pattern
