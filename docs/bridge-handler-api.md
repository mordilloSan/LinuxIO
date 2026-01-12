# Bridge Handler API

## Core Principle

**The bridge is the only component that understands application semantics.** It parses StreamFrames, routes to handlers based on stream type, and sends responses.

```
Bridge's job:
  1. Accept yamux streams from server
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
│  │           Yamux Session Listener                     │    │
│  │  - Accept yamux sessions from server                │    │
│  │  - One session per authenticated user               │    │
│  └────────────────────┬────────────────────────────────┘    │
│                       │                                       │
│  ┌────────────────────┴────────────────────────────────┐    │
│  │           Stream Acceptor                            │    │
│  │  - Accept new streams from session                  │    │
│  │  - Read initial StreamFrame (OpStreamOpen)          │    │
│  │  - Parse stream type from payload                   │    │
│  └────────────────────┬────────────────────────────────┘    │
│                       │                                       │
│  ┌────────────────────┴────────────────────────────────┐    │
│  │           Router                                     │    │
│  │  - Route to handler based on stream type:           │    │
│  │    • "terminal"    → TerminalHandler                │    │
│  │    • "container"   → ContainerHandler               │    │
│  │    • "json"        → JSONHandler                    │    │
│  │    • "fb-upload"   → FileUploadHandler              │    │
│  │    • "fb-download" → FileDownloadHandler            │    │
│  └────────────────────┬────────────────────────────────┘    │
│                       │                                       │
│          ┌────────────┴────────────┐                        │
│          ▼            ▼            ▼                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                 │
│  │ Terminal │  │   JSON   │  │   File   │  ... handlers   │
│  │ Handler  │  │ Handler  │  │ Handler  │                 │
│  └──────────┘  └──────────┘  └──────────┘                 │
│                                                               │
└───────────────────────────────────────────────────────────────┘
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
    OpStreamOpen     = 0x80  // Open stream: payload = "type\0arg1\0arg2..."
    OpStreamData     = 0x81  // Raw data: payload = bytes
    OpStreamClose    = 0x82  // Close stream: payload = empty
    OpStreamResize   = 0x83  // Terminal resize: payload = [cols:2][rows:2]
    OpStreamProgress = 0x84  // Progress update: payload = JSON
    OpStreamResult   = 0x85  // Final result: payload = JSON
)
```

### Reading Frames

```go
func ReadRelayFrame(stream *yamux.Stream) (*StreamFrame, error) {
    // Read header (9 bytes)
    header := make([]byte, 9)
    _, err := io.ReadFull(stream, header)
    if err != nil {
        return nil, err
    }

    // Parse header
    opcode := header[0]
    streamID := binary.BigEndian.Uint32(header[1:5])
    length := binary.BigEndian.Uint32(header[5:9])

    // Read payload
    payload := make([]byte, length)
    if length > 0 {
        _, err = io.ReadFull(stream, payload)
        if err != nil {
            return nil, err
        }
    }

    return &StreamFrame{
        Opcode:   opcode,
        StreamID: streamID,
        Payload:  payload,
    }, nil
}
```

### Writing Frames

```go
func WriteRelayFrame(stream *yamux.Stream, frame *StreamFrame) error {
    buf := make([]byte, 9+len(frame.Payload))

    buf[0] = frame.Opcode
    binary.BigEndian.PutUint32(buf[1:5], frame.StreamID)
    binary.BigEndian.PutUint32(buf[5:9], uint32(len(frame.Payload)))
    copy(buf[9:], frame.Payload)

    _, err := stream.Write(buf)
    return err
}
```

## Handler Interface

### Standard Handler Pattern

```go
type StreamHandler func(stream *yamux.Stream, args []string, userID string) error

// Register handlers
handlers := map[string]StreamHandler{
    "terminal":    HandleTerminalStream,
    "container":   HandleContainerStream,
    "json":        HandleJSONStream,
    "fb-upload":   HandleFileUpload,
    "fb-download": HandleFileDownload,
    "fb-compress": HandleFileCompress,
    "fb-extract":  HandleFileExtract,
}

// Main stream acceptor
func acceptStreams(session *yamux.Session, userID string) {
    for {
        stream, err := session.AcceptStream()
        if err != nil {
            return // Session closed
        }

        go handleStream(stream, userID)
    }
}

func handleStream(stream *yamux.Stream, userID string) {
    defer stream.Close()

    // Read initial frame (OpStreamOpen)
    frame, err := ReadRelayFrame(stream)
    if err != nil || frame.Opcode != OpStreamOpen {
        return
    }

    // Parse payload: "type\0arg1\0arg2\0..."
    parts := bytes.Split(frame.Payload, []byte{0})
    streamType := string(parts[0])
    args := make([]string, len(parts)-1)
    for i, part := range parts[1:] {
        args[i] = string(part)
    }

    // Route to handler
    handler, exists := handlers[streamType]
    if !exists {
        WriteRelayFrame(stream, &StreamFrame{
            Opcode:  OpStreamResult,
            Payload: []byte(`{"status":"error","error":"unknown stream type"}`),
        })
        return
    }

    // Execute handler
    handler(stream, args, userID)
}
```

## Handler Examples

### Persistent Stream (Terminal)

```go
func HandleTerminalStream(stream *yamux.Stream, args []string, userID string) error {
    // args[0] = cols, args[1] = rows
    cols, _ := strconv.Atoi(args[0])
    rows, _ := strconv.Atoi(args[1])

    // Spawn PTY
    cmd := exec.Command("/bin/bash")
    pty, err := pty.StartWithSize(cmd, &pty.Winsize{
        Rows: uint16(rows),
        Cols: uint16(cols),
    })
    if err != nil {
        return err
    }
    defer pty.Close()

    // Bidirectional relay
    done := make(chan error, 2)

    // PTY → Stream
    go func() {
        buf := make([]byte, 32*1024)
        for {
            n, err := pty.Read(buf)
            if err != nil {
                done <- err
                return
            }

            // Send as OpStreamData
            WriteRelayFrame(stream, &StreamFrame{
                Opcode:  OpStreamData,
                Payload: buf[:n],
            })
        }
    }()

    // Stream → PTY
    go func() {
        for {
            frame, err := ReadRelayFrame(stream)
            if err != nil {
                done <- err
                return
            }

            switch frame.Opcode {
            case OpStreamData:
                pty.Write(frame.Payload)
            case OpStreamResize:
                rows := binary.BigEndian.Uint16(frame.Payload[0:2])
                cols := binary.BigEndian.Uint16(frame.Payload[2:4])
                pty.Setsize(&pty.Winsize{Rows: rows, Cols: cols})
            case OpStreamClose:
                done <- io.EOF
                return
            }
        }
    }()

    // Wait for either direction to close
    <-done
    return nil
}
```

### Request/Response Stream (JSON)

```go
func HandleJSONStream(stream *yamux.Stream, args []string, userID string) error {
    // args[0] = handler type (e.g., "system")
    // args[1] = command (e.g., "get_cpu_info")
    // args[2...] = additional arguments

    handlerType := args[0]
    command := args[1]
    cmdArgs := args[2:]

    // Route to appropriate handler
    var result interface{}
    var err error

    switch handlerType {
    case "system":
        result, err = handleSystemCommand(command, cmdArgs)
    case "docker":
        result, err = handleDockerCommand(command, cmdArgs)
    case "dbus":
        result, err = handleDBusCommand(command, cmdArgs)
    // ... more handlers
    default:
        err = fmt.Errorf("unknown handler type: %s", handlerType)
    }

    // Send result
    var payload []byte
    if err != nil {
        payload, _ = json.Marshal(map[string]interface{}{
            "status": "error",
            "error":  err.Error(),
            "code":   500,
        })
    } else {
        payload, _ = json.Marshal(map[string]interface{}{
            "status": "ok",
            "data":   result,
        })
    }

    WriteRelayFrame(stream, &StreamFrame{
        Opcode:  OpStreamResult,
        Payload: payload,
    })

    // Close stream (request/response done)
    WriteRelayFrame(stream, &StreamFrame{
        Opcode: OpStreamClose,
    })

    return nil
}

func handleSystemCommand(command string, args []string) (interface{}, error) {
    switch command {
    case "get_cpu_info":
        return GetCPUInfo()
    case "get_memory_info":
        return GetMemoryInfo()
    case "reboot":
        return nil, Reboot()
    default:
        return nil, fmt.Errorf("unknown command: %s", command)
    }
}
```

### Progress Stream (File Upload)

```go
func HandleFileUpload(stream *yamux.Stream, args []string, userID string) error {
    // args[0] = destination path
    // args[1] = file size

    path := args[0]
    totalSize, _ := strconv.ParseInt(args[1], 10, 64)

    // Create file
    file, err := os.Create(path)
    if err != nil {
        sendError(stream, err)
        return err
    }
    defer file.Close()

    var written int64
    lastProgress := int64(0)
    progressInterval := int64(512 * 1024) // Report every 512KB

    // Read data frames
    for {
        frame, err := ReadRelayFrame(stream)
        if err != nil {
            return err
        }

        switch frame.Opcode {
        case OpStreamData:
            // Write chunk to file
            n, err := file.Write(frame.Payload)
            if err != nil {
                sendError(stream, err)
                return err
            }

            written += int64(n)

            // Send progress update
            if written-lastProgress >= progressInterval || written == totalSize {
                progress := map[string]interface{}{
                    "bytes": written,
                    "total": totalSize,
                    "pct":   float64(written) / float64(totalSize) * 100,
                }
                payload, _ := json.Marshal(progress)

                WriteRelayFrame(stream, &StreamFrame{
                    Opcode:  OpStreamProgress,
                    Payload: payload,
                })

                lastProgress = written
            }

            // Done?
            if written >= totalSize {
                sendSuccess(stream, nil)
                return nil
            }

        case OpStreamClose:
            // Client cancelled
            return nil
        }
    }
}

func sendSuccess(stream *yamux.Stream, data interface{}) {
    payload, _ := json.Marshal(map[string]interface{}{
        "status": "ok",
        "data":   data,
    })
    WriteRelayFrame(stream, &StreamFrame{
        Opcode:  OpStreamResult,
        Payload: payload,
    })
    WriteRelayFrame(stream, &StreamFrame{
        Opcode: OpStreamClose,
    })
}

func sendError(stream *yamux.Stream, err error) {
    payload, _ := json.Marshal(map[string]interface{}{
        "status": "error",
        "error":  err.Error(),
    })
    WriteRelayFrame(stream, &StreamFrame{
        Opcode:  OpStreamResult,
        Payload: payload,
    })
    WriteRelayFrame(stream, &StreamFrame{
        Opcode: OpStreamClose,
    })
}
```

## Modular Handler Design

### Handler Registration

```go
type HandlerRegistry struct {
    handlers map[string]StreamHandler
    mu       sync.RWMutex
}

func (r *HandlerRegistry) Register(streamType string, handler StreamHandler) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.handlers[streamType] = handler
}

func (r *HandlerRegistry) Get(streamType string) (StreamHandler, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    h, exists := r.handlers[streamType]
    return h, exists
}

// In main.go
registry := &HandlerRegistry{handlers: make(map[string]StreamHandler)}

// Register handlers
terminal.Register(registry)   // registers "terminal", "container"
filebrowser.Register(registry) // registers "fb-*"
api.Register(registry)         // registers "json"
```

### Handler Package Structure

```
backend/bridge/handlers/
├── terminal/
│   ├── stream.go        // HandleTerminalStream, HandleContainerStream
│   └── register.go      // Register function
├── filebrowser/
│   ├── stream.go        // HandleFileUpload, HandleFileDownload, etc.
│   └── register.go      // Register function
├── api/
│   ├── stream.go        // HandleJSONStream (routes to sub-handlers)
│   ├── system.go        // System commands
│   ├── docker.go        // Docker commands
│   ├── dbus.go          // DBus commands
│   └── register.go      // Register function
└── registry.go          // HandlerRegistry type
```

### Example Package (terminal/register.go)

```go
package terminal

func Register(registry *HandlerRegistry) {
    registry.Register("terminal", HandleTerminalStream)
    registry.Register("container", HandleContainerStream)
}

func HandleTerminalStream(stream *yamux.Stream, args []string, userID string) error {
    // Implementation...
}

func HandleContainerStream(stream *yamux.Stream, args []string, userID string) error {
    // Implementation...
}
```

## Stream Types Reference

### Persistent Streams (Long-lived)

| Type | Args | Frames Sent | Closes When |
|------|------|-------------|-------------|
| `terminal` | `[cols, rows]` | OpStreamData (PTY output) | Client closes or PTY exits |
| `container` | `[containerID, shell, cols, rows]` | OpStreamData (exec output) | Client closes or exec exits |

### Request/Response Streams (Ephemeral)

| Type | Args | Frames Sent | Closes When |
|------|------|-------------|-------------|
| `json` | `[handler, command, ...args]` | OpStreamResult | After result sent |

### Progress Streams (Ephemeral with progress)

| Type | Args | Frames Sent | Closes When |
|------|------|-------------|-------------|
| `fb-upload` | `[path, size]` | OpStreamProgress, OpStreamResult | Upload complete or error |
| `fb-download` | `[path]` | OpStreamData, OpStreamProgress, OpStreamResult | Download complete |
| `fb-compress` | `[paths..., dest, format]` | OpStreamProgress, OpStreamResult | Archive created |
| `fb-extract` | `[archive, dest]` | OpStreamProgress, OpStreamResult | Extraction complete |

## Error Handling

### Send Error to Client

```go
func sendError(stream *yamux.Stream, message string, code int) {
    payload, _ := json.Marshal(map[string]interface{}{
        "status": "error",
        "error":  message,
        "code":   code,
    })
    WriteRelayFrame(stream, &StreamFrame{
        Opcode:  OpStreamResult,
        Payload: payload,
    })
    WriteRelayFrame(stream, &StreamFrame{
        Opcode: OpStreamClose,
    })
}
```

### Handle Client Disconnect

```go
func HandleLongRunningTask(stream *yamux.Stream, args []string, userID string) error {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Monitor stream for close
    go func() {
        for {
            frame, err := ReadRelayFrame(stream)
            if err != nil || frame.Opcode == OpStreamClose {
                cancel() // Cancel context when client closes
                return
            }
        }
    }()

    // Do work with cancellable context
    return doWork(ctx, stream)
}
```

## Performance

### Optimize for Large Data

```go
// Reuse buffers
var bufferPool = sync.Pool{
    New: func() interface{} {
        return make([]byte, 32*1024)
    },
}

func streamFile(stream *yamux.Stream, path string) error {
    file, _ := os.Open(path)
    defer file.Close()

    buf := bufferPool.Get().([]byte)
    defer bufferPool.Put(buf)

    for {
        n, err := file.Read(buf)
        if n > 0 {
            WriteRelayFrame(stream, &StreamFrame{
                Opcode:  OpStreamData,
                Payload: buf[:n],
            })
        }
        if err == io.EOF {
            break
        }
    }
    return nil
}
```

## See Also

- [Frontend API](./frontendAPI.md) - Client-side implementation
- [Server Yamux Protocol](./server-yamux-protocol.md) - Server relay implementation
