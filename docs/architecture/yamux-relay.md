# Yamux Relay Architecture

## Overview

The LinuxIO server acts as a transparent byte relay between WebSocket clients and the bridge via yamux streams. The server never parses payloads - it only routes bytes by stream ID.

**Ultimate Goal:** One WebSocket connection per session with multiple streams for ALL communication:
- Persistent streams (terminal, logs) - stay open
- Request/response streams (API calls) - open → request → response → close

## Architecture

```
Browser                    Server                         Bridge
   │                         │                              │
   │════ WebSocket ══════════│══════ Yamux Session ════════│
   │                         │                              │
   │── Stream 1 (terminal) ──│────────────────────────────►│ PTY (persistent)
   │── Stream 3 (get_cpu) ───│────────────────────────────►│ → response → close
   │── Stream 5 (docker_ls) ─│────────────────────────────►│ → response → close
   │◄─ Stream 7 (push event) │◄────────────────────────────│ bridge-initiated
```

**Key Points:**
- 1 WebSocket per session (singleton)
- 1 yamux session per user (persistent)
- N streams per session (multiplexed)
- Server is pure byte relay (0 JSON parsing)

## Stream Types

### Persistent Streams (stay open)
| Type | Description | Status |
|------|-------------|--------|
| `terminal` | PTY session | ✅ Done |
| `container-logs` | Docker log tailing | Planned |
| `file-watch` | File system events | Planned |

### Request/Response Streams (open → close)
| Type | Description | Status |
|------|-------------|--------|
| `api` | JSON API calls | ⏳ In Progress |
| `fb-download` | Binary file transfer | ✅ Done |
| `fb-upload` | Binary file upload | ✅ Done |
| `fb-archive` | Multi-file archive download | ✅ Done |
| `fb-compress` | Create archive from paths | ✅ Done |
| `fb-extract` | Extract archive to destination | ✅ Done |

### Bridge-Initiated Streams (push)
| Type | Description | Status |
|------|-------------|--------|
| `docker-event` | Container state changes | Planned |
| `system-alert` | Disk full, high CPU | Planned |
| `service-status` | systemd unit changes | Planned |

## Protocol Layers

### Layer 1: WebSocket Multiplexer (Frontend ↔ Server)

Binary WebSocket messages:
```
┌─────────────┬─────────────┬─────────────────────────────────┐
│ Stream ID   │ Flags       │ Payload (StreamFrame bytes)     │
│ (4 bytes BE)│ (1 byte)    │ (N bytes)                       │
└─────────────┴─────────────┴─────────────────────────────────┘
```

**Flags:**
```
0x01 = SYN   (open new stream)
0x04 = DATA  (data frame)
0x08 = FIN   (close stream)
0x10 = RST   (abort stream)
```

### Layer 2: StreamFrame (Server ↔ Bridge)

```
┌─────────────┬─────────────┬─────────────┬───────────────────┐
│ Opcode      │ Stream ID   │ Length      │ Payload           │
│ (1 byte)    │ (4 bytes BE)│ (4 bytes BE)│ (N bytes)         │
└─────────────┴─────────────┴─────────────┴───────────────────┘
```

**Opcodes:**
```go
OpStreamOpen   = 0x80  // payload = "type\0arg1\0arg2"
OpStreamData   = 0x81  // payload = raw bytes
OpStreamClose  = 0x82  // payload = empty
OpStreamResize = 0x83  // payload = [cols:2][rows:2]
```

## Request/Response Pattern (Future API Migration)

Current API call (JSON over HTTP):
```
Browser → POST /api/system/cpu → Server → JSON encode → Bridge → response → JSON decode → Browser
         ════════════════════════════════════════════════════════════════════════════════
         Multiple HTTP connections, JSON parsing at every hop
```

Future API call (stream):
```
Browser                          Server                      Bridge
   │                               │                           │
   │── open stream ───────────────►│                           │
   │── [type=api][{"method":"get_cpu"}] ──────────────────────►│
   │                               │                           │── handle
   │◄─────────────────── [{"cpu":45}] ─────────────────────────│
   │── close stream ──────────────►│                           │
   │                               │                           │
   ════════════════════════════════════════════════════════════
   Single WebSocket, server just routes bytes, no JSON parsing
```

**Benefits:**
- Server becomes stateless relay (no request parsing)
- Single connection handles everything
- Cancel any request by closing its stream
- Progress updates on same stream (file uploads)

## Implementation Status

| Phase | Description | Status |
|-------|-------------|--------|
| Phase 1 | Yamux layer in bridge | ✅ Done |
| Phase 2 | Server yamux client | ✅ Done |
| Phase 3 | WebSocket binary relay | ✅ Done |
| Phase 4 | Terminal direct streaming | ✅ Done |
| Phase 5 | Persistent streams (singleton mux) | ✅ Done |
| Phase 6 | Bridge-initiated push | ⏳ Planned |
| Phase 7 | Migrate API calls to streams | ⏳ In Progress |
| Phase 8 | File transfer streams | ✅ Done |
| Phase 9 | Remove legacy `/ws` system | ⏳ Planned |

## What's Done (Phases 1-5, 8)

### Backend
- `backend/common/ipc/yamux.go` - Yamux session helpers
- `backend/common/ipc/stream_relay.go` - StreamFrame protocol
- `backend/bridge/main.go` - Yamux server with auto-detection
- `backend/bridge/handlers/terminal/stream.go` - PTY streaming
- `backend/server/bridge/bridge.go` - Yamux client session pool
- `backend/server/web/websocket_relay.go` - Pure byte relay

### Frontend
- `frontend/src/services/StreamMultiplexer.ts` - Singleton WebSocket mux
- `frontend/src/hooks/useStreamMux.ts` - React hook
- `frontend/src/pages/main/terminal/Terminal.tsx` - Stream-based terminal

### Features Implemented (Phases 1-5)
- Terminal ~1ms latency (was ~60ms polling)
- Stream persistence across navigation
- 64KB circular scrollback buffer
- Frame buffering for split StreamFrames
- Terminal reset (close stream → new PTY)
- Auto-focus on navigation

### Phase 8: File Transfers
- `backend/bridge/handlers/filebrowser/stream.go` - All file transfer handlers
- `frontend/src/contexts/FileTransferContext.tsx` - Stream-based transfers

**Stream Types:**
- `fb-download` - Single file download (1MB chunks, progress every 2MB)
- `fb-upload` - Single file upload (512KB progress intervals for flow control)
- `fb-archive` - Multi-file archive streaming
- `fb-compress` - Create archive on disk
- `fb-extract` - Extract archive to destination

**Features:**
- Progress tracking via `OpStreamProgress` (0x84)
- Result frames via `OpStreamResult` (0x85)
- Upload flow control (4MB window, 512KB ACK intervals)
- Cancellation via `stream.abort()`

## What's Planned

### Phase 6: Bridge-Initiated Push
Bridge opens streams to push events:
```go
stream, _ := yamuxSession.Open()
ipc.WriteRelayFrame(stream, &StreamFrame{
    Opcode:  OpStreamData,
    Payload: []byte(`{"type":"docker_died","id":"abc123"}`),
})
```

### Phase 7: API Migration (In Progress)
Replace HTTP handlers with stream handlers:
```go
// Instead of: router.GET("/api/system/cpu", handleCPU)
// Bridge handles: stream type "api" with method "get_cpu"
```

**Done:**
- `backend/bridge/handlers/api/stream.go` - API stream handler
- `frontend/src/utils/streamApi.ts` - Stream API client
- `frontend/src/hooks/useStreamApi.ts` - React Query hooks
- CPU info endpoint migrated as proof of concept

**Remaining:**
- Migrate all HTTP endpoints to use stream API
- Remove HTTP handlers once migration complete

### Phase 9: Legacy `/ws` Cleanup
The old `/ws` WebSocket system needs to be removed after Phase 7:

**Legacy code to remove:**
| File | Reason |
|------|--------|
| `backend/server/web/websocket.go` | Old JSON WebSocket handler |
| `backend/server/web/channels.go` | Route subscription (unused) |
| `backend/server/web/progress.go` | Legacy progress broadcaster |
| `frontend/src/contexts/WebSocketContext.tsx` | Old WS context |

**Still using legacy `/ws`:**
- Folder uploads (HTTP + `/ws` progress) - needs migration to streams
- Container terminal - needs migration to yamux streams

## File Locations

| Component | File |
|-----------|------|
| WebSocket relay | `backend/server/web/websocket_relay.go` |
| Yamux session | `backend/server/bridge/bridge.go` |
| StreamFrame protocol | `backend/common/ipc/stream_relay.go` |
| Yamux helpers | `backend/common/ipc/yamux.go` |
| Bridge routing | `backend/bridge/main.go` |
| Terminal handler | `backend/bridge/handlers/terminal/stream.go` |
| File transfer handler | `backend/bridge/handlers/filebrowser/stream.go` |
| API stream handler | `backend/bridge/handlers/api/stream.go` |
| Frontend mux | `frontend/src/utils/StreamMultiplexer.ts` |
| Frontend stream API | `frontend/src/utils/streamApi.ts` |
| Frontend hook | `frontend/src/hooks/useStreamMux.ts` |
| Stream API hooks | `frontend/src/hooks/useStreamApi.ts` |
| Terminal UI | `frontend/src/pages/main/terminal/Terminal.tsx` |
| File transfer context | `frontend/src/contexts/FileTransferContext.tsx` |

## Performance

| Metric | Current (HTTP) | Streams |
|--------|----------------|---------|
| Connections | N per request | 1 per session |
| Terminal latency | ~60ms | ~1ms |
| JSON ops in server | 6 per request | 0 |
| Request cancellation | Hacky | `stream.close()` |
| Server-push | Not possible | Native |

## Security

1. **Authentication**: WebSocket upgrade requires valid session cookie
2. **Stream isolation**: Each stream tied to authenticated session
3. **Bridge secret**: Validated per yamux session
4. **Payload opacity**: Server never inspects content
