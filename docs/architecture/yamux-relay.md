# Yamux Relay Architecture

## Overview

The LinuxIO server acts as a transparent byte relay between WebSocket clients and the bridge via yamux streams. The server never parses payloads - it only routes bytes by stream ID.

## Architecture

```
Browser                    Server                         Bridge
   │                         │                              │
   │──[muxID][flags][StreamFrame]──►│                       │
   │                         │──route(muxID)                │
   │                         │──────[StreamFrame]──────────►│ (yamux stream)
   │                         │                              │──parse StreamFrame
   │                         │                              │──process (terminal, etc)
   │                         │◄─────[StreamFrame]───────────│
   │◄──[muxID][flags][StreamFrame]──│                       │
```

**Key Points:**
- Server only knows: mux stream IDs and routing
- Server never parses StreamFrame payload
- 0 JSON operations in server for binary streams
- Bidirectional streaming (terminal, logs, events)

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

**Stream ID allocation:**
- Client-initiated: odd numbers (1, 3, 5, ...)
- Server uses mux stream ID to route to yamux streams

### Layer 2: StreamFrame (Server ↔ Bridge)

The payload relayed between server and bridge:
```
┌─────────────┬─────────────┬─────────────┬───────────────────┐
│ Opcode      │ Stream ID   │ Length      │ Payload           │
│ (1 byte)    │ (4 bytes BE)│ (4 bytes BE)│ (N bytes)         │
└─────────────┴─────────────┴─────────────┴───────────────────┘
```

**Opcodes (0x80+ to avoid conflict with JSON framing):**
```go
OpStreamOpen   = 0x80  // Open stream: payload = "type\0arg1\0arg2"
OpStreamData   = 0x81  // Data frame: payload = raw bytes
OpStreamClose  = 0x82  // Close stream: payload = empty
OpStreamResize = 0x83  // Resize terminal: payload = [cols:2][rows:2]
```

**Stream Types (in OpStreamOpen payload):**
- `terminal` - PTY session
- `container` - Container logs/exec (future)

## Session Lifecycle

### Login
```
1. User authenticates via /auth/login
2. Server creates session, launches bridge process
3. Bridge listens on Unix socket
4. Server opens yamux session to bridge socket
```

### Stream Open (e.g., Terminal)
```
1. Frontend: singleton StreamMultiplexer connects WebSocket to /ws/relay
2. Frontend: openStream("terminal", payload) sends SYN with StreamFrame
3. Server: creates yamux stream, relays StreamFrame to bridge
4. Bridge: detects 0x80 opcode, parses StreamFrame, spawns PTY
5. Bidirectional byte relay established
```

### Navigation (Persistent Streams)
```
1. User navigates away from terminal
2. Terminal component unmounts, detaches xterm handlers
3. Stream stays alive, PTY keeps running
4. Output buffered in 64KB circular scrollback buffer
5. User returns: reattach to existing stream
6. Scrollback replayed to xterm, then live output resumes
```

### Terminal Reset
```
1. User clicks reset button
2. Frontend: closes existing stream (sends FIN)
3. Bridge: receives EOF, terminates PTY
4. Frontend: opens new stream (sends SYN)
5. Bridge: spawns fresh PTY
6. Clean terminal with new shell session
```

### Logout / Session End
```
1. User logs out OR session expires OR browser closes
2. WebSocket connection closes
3. Server: streamRelay.closeAll() closes all streams
4. Bridge: all yamux streams receive EOF
5. Bridge: all PTYs terminated, cleanup runs
6. Server: yamux session closed
```

## File Locations

| Component | File |
|-----------|------|
| WebSocket relay | `backend/server/web/websocket_relay.go` |
| Yamux session management | `backend/server/bridge/bridge.go` |
| StreamFrame protocol | `backend/common/ipc/stream_relay.go` |
| Yamux helpers | `backend/common/ipc/yamux.go` |
| Bridge stream routing | `backend/bridge/main.go` |
| Terminal stream handler | `backend/bridge/handlers/terminal/stream.go` |
| Frontend multiplexer | `frontend/src/services/StreamMultiplexer.ts` |
| Frontend hook | `frontend/src/hooks/useStreamMux.ts` |
| Terminal component | `frontend/src/pages/main/terminal/Terminal.tsx` |

## Implementation Status

| Phase | Description | Status |
|-------|-------------|--------|
| Phase 1 | Yamux layer in bridge | Done |
| Phase 2 | Server yamux client | Done |
| Phase 3 | WebSocket binary relay | Done |
| Phase 4 | Terminal direct streaming | Done |
| Phase 5 | Persistent streams (singleton mux) | Done |
| Phase 6 | Bridge-initiated push | Planned |

## Frontend Features

### Singleton StreamMultiplexer
- Single WebSocket connection per session
- Streams indexed by type (`"terminal"`, `"container"`, etc.)
- Persists across component mounts/unmounts

### Stream Persistence
- Streams stay alive when navigating away
- PTY continues running in background
- Handler detachment allows buffering

### Scrollback Buffer
- 64KB circular buffer per stream (O(1) writes)
- Replayed when handler reattaches
- Shows previous output on navigation return

### Frame Buffering
- Handles split StreamFrames from yamux
- Accumulates partial frames until complete
- Ensures no data loss from stream-oriented transport

## Performance

| Metric | JSON WebSocket | Binary Relay |
|--------|----------------|--------------|
| Connections per session | N (one per request) | 1 yamux session |
| Terminal latency | ~60ms (polling) | ~1ms (streaming) |
| JSON ops in server | 6 per request | 0 |
| Bandwidth overhead | JSON wrapper | 9-byte header |

## Security

1. **Authentication**: WebSocket upgrade requires valid session cookie
2. **Stream isolation**: Each relay tied to authenticated session
3. **Bridge secret**: Validated per yamux session, not per stream
4. **Payload opacity**: Server never inspects StreamFrame content
