# Server Yamux Protocol

## Core Principle

**The server is a stateless byte relay.** It never parses JSON, never inspects payloads, never knows about "API calls" vs "terminals". It only knows about streams and bytes.

```
Server's job:
  1. Accept WebSocket connections
  2. Route frames between WebSocket ↔ Yamux based on streamID
  3. Nothing else
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   LinuxIO WebServer                     │
│                                                         │
│  WebSocket Handler         Yamux Session Pool           │
│  ┌──────────────┐         ┌──────────────────┐          │
│  │ Upgrade HTTP │────────►│ Get Yamux Session│          │
│  │ → WebSocket  │         │ (by SessionID)   │          │
│  └──────┬───────┘         └────────┬─────────┘          │
│         │                          │                    │
│         │  WebSocket Frames        │  Yamux Frames      │
│         │  [streamID][flags][data] │  yamux protocol    │
│         │                          │                    │
│         ▼                          ▼                    │
│  ┌─────────────────────────────────────────────┐        │
│  │         Pure Byte Relay Loop                │        │
│  │  - Read from WebSocket → Write to Yamux     │        │
│  │  - Read from Yamux → Write to WebSocket     │        │
│  │  - No parsing, no inspection, just routing  │        │
│  └─────────────────────────────────────────────┘        │
│                                                         │
└──────────────────────┬──────────────────────────────────┘
                       │
                       │ Inherited net.Conn (created at login)
                       │ — no socket to dial, no reconnect loop
                       ▼
              ┌────────────────┐
              │     Bridge     │
              │  (Handlers)    │
              └────────────────┘
```

## Protocol Layers

### Layer 1: WebSocket Multiplexer Frame

Browser sends/receives binary WebSocket messages:

```
┌─────────────┬─────────────┬─────────────────────────────────┐
│ Stream ID   │ Flags       │ Payload                         │
│ (4 bytes BE)│ (1 byte)    │ (StreamFrame bytes from bridge) │
└─────────────┴─────────────┴─────────────────────────────────┘
```

**Flags:**
```
0x01 = SYN   Open new stream
0x04 = DATA  Data frame
0x08 = FIN   Close stream gracefully
0x10 = RST   Abort stream
```

**Example — Open terminal stream:**
```
[00 00 00 01][01][StreamFrame bytes]
│            │   │
│            │   └─ Payload: [0x80][streamID][len]["terminal\0120\032"]
│            └─ SYN flag
└─ Stream ID: 1
```

### Layer 2: Yamux Protocol (WebSocket ↔ Bridge)

Standard yamux multiplexing using `github.com/libp2p/go-yamux/v5`.

```go
// Webserver side: client that opens streams
session, _ := yamux.Client(conn, ipc.YamuxConfig(), nil)
stream, _ := session.Open(context.Background())

// Bridge side: server that accepts streams
session, _ := yamux.Server(conn, ipc.YamuxConfig(), nil)
stream, _ := session.Accept()
```

**Yamux configuration:**
```go
func YamuxConfig() *yamux.Config {
    cfg := yamux.DefaultConfig()
    cfg.AcceptBacklog          = 256
    cfg.EnableKeepAlive        = true
    cfg.KeepAliveInterval      = 35 * time.Second
    cfg.ConnectionWriteTimeout = 20 * time.Second
    cfg.MaxStreamWindowSize    = 16 * 1024 * 1024 // 16 MB per stream
    return cfg
}
```

**Server never parses yamux frames** — the library handles it:
- Window updates
- Ping/pong
- Stream lifecycle
- Flow control

Server just reads/writes bytes from/to streams.

## Bridge Connection Model

The bridge is **not** a long-running server that the webserver dials. Instead:

1. On login, `bridge.StartBridge()` calls the auth daemon over a Unix socket.
2. The auth daemon validates PAM credentials, checks `sudo -v`, then forks `linuxio-bridge`.
3. The forked bridge receives `FD 3` — one half of a `socketpair` — as its network connection.
4. The webserver receives the other half as a `net.Conn` from the auth daemon response.
5. `ipc.NewYamuxClient(conn)` wraps that connection into a yamux client session.
6. The session is stored in `yamuxSessions` keyed by `SessionID` for subsequent WebSocket connections.

```go
// bridge/bridge.go — called at login
func StartBridge(sm *session.Manager, sessionID, username, password string, verbose bool) (*session.Session, error) {
    result, _ := Authenticate(req) // calls auth daemon, gets net.Conn back
    sess, _ := sm.CreateSessionWithID(sessionID, result.User, result.Privileged)
    attachBridgeSession(sess, result.Conn)
    return sess, nil
}

func attachBridgeSession(sess *session.Session, conn net.Conn) error {
    yamuxSession, _ := ipc.NewYamuxClient(conn) // webserver = yamux client
    yamuxSessions.sessions[sess.SessionID] = yamuxSession
    return nil
}
```

On the bridge side:

```go
// bridge/main.go — bridge process entry point
const clientConnFD = 3
clientFile := os.NewFile(uintptr(clientConnFD), "client-conn")
clientConn, _ := net.FileConn(clientFile)  // bridge = yamux server
handleYamuxSession(clientConn)             // yamux.Server(conn, ...)
```

## Server Implementation

### WebSocket Upgrade

```go
// wsAuthMiddleware validates the session before upgrading
sess := sm.ValidateFromRequest(r)
if sess == nil {
    // Upgrade first, then send close code 1008 ("no-session")
    // so the frontend can distinguish auth failure from network error
    conn.WriteControl(websocket.CloseMessage,
        websocket.FormatCloseMessage(1008, "no-session"), ...)
    return
}

// WebSocketRelayHandler — the actual handler
sess := session.SessionFromContext(r.Context())
conn, _ := upgrader.Upgrade(w, r, nil)
yamuxSession, _ := bridge.GetYamuxSession(sess.SessionID)
// start relay...
```

### Relay Loop (The Entire Server Logic)

```go
// Parse frame header: [streamID:4][flags:1][payload:N]
streamID := binary.BigEndian.Uint32(data[0:4])
flags    := data[4]
payload  := data[5:]

if flags&FlagSYN != 0 {
    // Open new yamux stream, write payload, start relayFromBridge goroutine
    stream, _ := yamuxSession.Open(ctx)
    stream.Write(payload)
    go relayFromBridge(stream, streamID, ws)

} else if flags&FlagDATA != 0 {
    // Forward data to existing yamux stream
    streams[streamID].Write(payload)

} else if flags&FlagFIN != 0 {
    // Forward payload to bridge (e.g., OpStreamClose frame), but do NOT
    // close the stream yet — wait for bridge to respond and close its side
    streams[streamID].Write(payload)

} else if flags&FlagRST != 0 {
    // Abort stream immediately
    streams[streamID].Close()
}
```

```go
func relayFromBridge(stream net.Conn, streamID uint32, ws *websocket.Conn) {
    buf := make([]byte, 4096)
    for {
        n, err := stream.Read(buf)
        if n > 0 {
            sendFrame(ws, streamID, FlagDATA, buf[:n])
        }
        if err != nil {
            sendFrame(ws, streamID, FlagFIN, nil)
            closeStream(streamID)
            return
        }
    }
}
```

**That's the entire server logic.** No JSON, no routing, no business logic.

## Stream Lifecycle

### Browser Opens Stream

```
Browser                 Server                  Bridge
  │                       │                       │
  │─ WebSocket: SYN ─────►│                       │
  │  [streamID=1][0x01]   │                       │
  │  [payload=...]        │                       │
  │                       │── yamuxSession.Open() ►│
  │                       │── stream.Write(payload)│
  │                       │── go relayFromBridge() │
```

### Bridge Sends Data

```
  │                       │◄─── stream.Write() ───│
  │◄─ WebSocket: DATA ────│   (bytes from bridge) │
  │  [streamID=1][0x04]   │                       │
  │  [payload=...]        │                       │
```

### Browser Closes Stream

```
  │─ WebSocket: FIN ─────►│                       │
  │  [streamID=1][0x08]   │                       │
  │  [payload=...]        │                       │
  │                       │── stream.Write(payload)│ (forwards close frame)
  │                       │   (waits for bridge)  │
  │                       │◄── stream.Read() EOF ─│ (bridge closes)
  │◄─ WebSocket: FIN ─────│                       │
```

**Note:** On FIN, the server forwards the payload (typically an `OpStreamClose` frame) to the bridge and waits for the bridge to close the stream. It does not immediately close the yamux stream.

### Bridge Closes Stream

```
  │                       │◄─ stream.Read() EOF ──│
  │◄─ WebSocket: FIN ─────│   (yamux EOF)         │
  │  [streamID=1][0x08]   │                       │
```

## Session Management

### Yamux Session Pool

One yamux session per authenticated login (`SessionID`):

```go
var yamuxSessions = struct {
    sync.RWMutex
    sessions map[string]*ipc.YamuxSession // SessionID → session
}{}

// Lookup at WebSocket open time
yamuxSession, err := bridge.GetYamuxSession(sess.SessionID)
```

**Key points:**
- One yamux session per login (= one bridge process per login)
- Multiple WebSocket connections (tabs/windows) share the same session
- Session is keyed by `SessionID`, not username — a user can have multiple concurrent sessions
- Session survives WebSocket disconnects
- When the bridge process dies, the yamux session closes → the HTTP session is terminated → all WebSocket connections for that session receive close code 1008

### Multiple Tabs Example

```
User with two browser tabs, one session:

Tab 1: WebSocket A ──┐
                      ├─► YamuxSession (SessionID="abc") ─► Bridge process
Tab 2: WebSocket B ──┘

Each WebSocket:
  - Has its own connection to server
  - Routes frames through the SAME yamux session
  - Can open streams (yamux handles streamID deduplication)
```

## Error Handling

### WebSocket Errors

```go
// Connection lost
if !isExpectedWSClose(err) {
    log.Warn("WebSocket closed unexpectedly")
}
// Close all streams opened by this WebSocket
relay.closeAll()
```

**Note:** Yamux session stays open. Other WebSocket connections from the same session still work.

### Yamux / Bridge Errors

```go
// Stream read error (bridge closed stream or died)
n, err := stream.Read(buf)
if err != nil {
    sendFrame(ws, streamID, FlagFIN, nil)  // notify browser
    closeStream(streamID)
}

// Yamux session dies (bridge process exited)
// → yamuxSession.OnClose fires → session.Terminate() → CloseWebSocketForSession()
// → all WebSocket connections for the session receive close code 1008
```

## Performance

### Why This is Fast

1. **Zero JSON parsing** — server never touches payloads
2. **Zero allocation** — just copies bytes between connections
3. **Multiplexed** — one connection handles everything
4. **Stateless** — server tracks only `streamID → yamux stream` mappings
5. **16 MB window** — large transfer chunks without stalls

## Security

### Authentication

WebSocket upgrade requires valid session cookie, enforced by `wsAuthMiddleware`:

```go
sess, err := sm.ValidateFromRequest(r)
if err != nil {
    // Upgrade first, reject with close code 1008 ("no-session")
    // so browsers can distinguish auth failure from network error
}
```

**After authentication:**
- Server does not re-check permissions on each frame
- Bridge handles authorization (`sess.Privileged` flag, `RequirePrivilegedIPC`)
- Stream isolation: each session has a separate bridge process and yamux session

### Payload Opacity

Server never inspects payload content:

```go
// ✓ What server does
stream.Write(payload) // Just forward bytes

// ✗ What server DOESN'T do
json.Unmarshal(payload, &req) // Never parses
```

## File Locations

| Component | File |
|-----------|------|
| WebSocket handler + relay | `backend/webserver/web/websocket.go` |
| Auth middleware (`wsAuthMiddleware`) | `backend/webserver/web/websocket.go` |
| Yamux session pool (`GetYamuxSession`) | `backend/webserver/bridge/bridge.go` |
| Bridge launch (`StartBridge`) | `backend/webserver/bridge/bridge.go` |
| Yamux config + wrappers | `backend/common/ipc/yamux.go` |

## See Also

- [Frontend API](./frontend-api.md) - Client-side implementation
- [Bridge Handler API](./bridge-handler-api.md) - How bridge handles streams
