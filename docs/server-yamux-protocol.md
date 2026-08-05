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
// Webserver side: client that opens streams (wrapped by relay.NewYamuxClient)
session, _ := yamux.Client(conn, relay.YamuxConfig(), nil)
stream, _ := session.Open(context.Background())

// Bridge side: server that accepts streams (wrapped by relay.NewYamuxServer)
session, _ := yamux.Server(conn, relay.YamuxConfig(), nil)
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

1. On login, `bridge.StartBridge()` dials the auth daemon over a Unix socket.
2. The auth daemon validates PAM credentials, checks sudo, then forks `linuxio-bridge`.
3. No new data-path socketpair is created. The auth daemon **reuses the accepted
   connection**: it `dup2`s the webserver↔auth-daemon socket onto the bridge's
   FD 3, then execs the bridge. The separate FD 4 socketpair described below is
   only a startup control channel.
4. The bridge inherits one endpoint of a **bidirectional Unix socketpair at FD
   4** (the launcher sets `PROTO_FLAG_READY_ACK` in the bootstrap to advertise
   it). The bridge parses its bootstrap and completes initialization that does
   not create Yamux, writes `PROTO_STARTUP_READY`, and blocks reading FD 4 for
   `PROTO_STARTUP_GO`. On a fatal error before READY it instead writes
   `PROTO_STARTUP_ERROR` plus a short message and exits.
5. After READY, the auth daemon records login accounting and writes the
   **complete** authentication OK response on FD 3. Only if that write succeeds
   does it send GO back to the bridge on FD 4. The bridge validates GO, closes
   FD 4, and only then calls `relay.NewYamuxServer` on FD 3 and starts accepting
   streams. A successful authentication response therefore means the bridge
   reached the pre-Yamux rendezvous; after the response write, the launcher
   attempts to release it. The response does not by itself prove that GO was
   delivered or that subsequent Yamux creation succeeded.
6. This READY/GO barrier protects response framing. Before GO, the launcher is
   the only process allowed to write FD 3; the bridge cannot create Yamux, which
   may emit control traffic immediately. After the complete OK frame is written,
   GO transfers transport ownership to the bridge. If the OK write fails, the
   launcher does not send GO and terminates the bridge, so Yamux bytes cannot
   precede, overlap, or corrupt the authentication response.
7. Death before READY (EOF), silence, an invalid status byte, EOF while waiting
   for GO, or an invalid GO byte fails closed. From request receipt, the launcher
   gives authentication and startup work one absolute 20-second budget,
   targeting a 10-second error-delivery margin inside the webserver's 30-second
   read deadline. Within that budget, the READY phase defaults to 10 seconds;
   `LINUXIO_BRIDGE_READY_TIMEOUT` accepts 1–20 seconds and is clipped to the
   remaining request budget. The sudo child wait uses that same remaining
   budget. The launcher does not interrupt synchronous PAM calls; if PAM returns
   after the deadline, it fails before bridge launch. See
   `backend/auth/linuxio_protocol.h`.
8. The webserver keeps its end of the connection it dialed. That `net.Conn` now reaches the forked bridge directly — the auth daemon is no longer in the data path.
9. `relay.NewYamuxClient(conn)` wraps that connection into a yamux client session.
10. The session is stored in `yamuxSessions` keyed by `SessionID` for subsequent WebSocket connections.

```go
// bridge/bridge.go — called at login
func StartBridge(ctx context.Context, sm *session.Manager, sessionID, username, password, remoteHost string, verbose bool) (*session.Session, error) {
    result, _ := Authenticate(req) // dials auth daemon; conn now reaches the forked bridge
    sess, _ := sm.CreateSession(sessionID, result.User, result.Privileged)
    attachBridgeSession(sess, result.Conn)
    return sess, nil
}

func attachBridgeSession(sess *session.Session, conn net.Conn) error {
    yamuxSession, _ := relay.NewYamuxClient(conn) // webserver = yamux client
    yamuxSessions.sessions[sess.SessionID] = yamuxSession
    return nil
}
```

On the bridge side:

```go
// backend/bridge/cmd — bridge process entry point (ready.go + yamux.go)
const clientConnFD = 3
clientFile := os.NewFile(uintptr(clientConnFD), "client-conn")
clientConn, _ := net.FileConn(clientFile)  // openClientConnection()
handleYamuxSession(..., clientConn, ..., startup.ready)
// startup.ready writes READY and blocks for GO before NewYamuxServer(conn).
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
- Bridge route dispatch enforces authorization from the session privilege state (`sess.Privileged`) for routes registered with bridge privilege metadata
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
| Yamux config + wrappers | `backend/common/ipc/relay/yamux.go` |

## See Also

- [Process & Systemd Architecture](./process-systemd-architecture.md) - The four binaries, socket activation, and how the bridge connection is created at login
- [API Contract](./api-contract.md) - Go-owned API contract and generated frontend client
- [Handler Patterns](./bridge_handler_patterns.md) - Handler package style and adapter conventions
