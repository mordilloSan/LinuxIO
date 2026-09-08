# Server Yamux Protocol

## Core Principle

**The WebSocket handler relays opaque payload bytes.** It tracks streams, session activity and connection lifetime, but leaves route parsing and authorization to the bridge. Native HTTP file downloads use a separate streaming handler.

```
Server's job:
  1. Accept WebSocket connections
  2. Route frames between WebSocket ↔ Yamux based on streamID
  3. Enforce origin/session checks and clean up transport resources
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
0x20 = Activity marker (outer relay metadata)
```

Activity is payload-blind metadata. The browser may send stream ID 0 with an empty payload and only this flag for explicit document activity. Interactive DATA frames may combine Activity with DATA. The relay observes and strips Activity before dispatching SYN, DATA, FIN, or RST; it never parses route JSON or other payload bytes. Passive requests, resize, FIN, and RST frames do not carry Activity.

**Example — Open terminal stream:**
```
[00 00 00 01][01][StreamFrame bytes]
│            │   │
│            │   └─ Payload: [0x80][streamID][len][JSON open envelope]
│            └─ SYN flag
└─ Stream ID: 1
```

The open envelope contains `route` and `request`, for example
`{"route":"terminal.open","request":{"cols":120,"rows":32}}`.

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
2. PAM authenticates the user once. The root auth daemon then runs
   `sudo -n -l -U <user> -u root -- /usr/local/bin/linuxio-bridge` as a
   non-interactive policy query and forks `linuxio-bridge`; sudo does not
   execute the bridge.
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
   remaining request budget. The sudo policy-query child wait uses that same
   remaining budget. The launcher does not interrupt synchronous PAM calls; if
   PAM returns after the deadline, it fails before bridge launch. See
   `backend/auth/linuxio_protocol.h`.
8. The webserver keeps its end of the connection it dialed. That `net.Conn` now reaches the forked bridge directly — the auth daemon is no longer in the data path.
9. `relay.NewYamuxClient(conn)` wraps that connection into a yamux client session.
10. The session is stored in `yamuxSessions` keyed by `SessionID` for subsequent WebSocket connections.

```go
// bridge/bridge.go — called at login
func StartBridge(sm *session.Manager, sessionID, username, password, remoteHost string, verbose bool) (*session.Session, error) {
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

`wsAuthMiddleware` validates the session before upgrading. Both authenticated
and unauthenticated upgrades enforce the same origin policy. Invalid sessions
receive close code 1008 (`no-session`) after upgrade, allowing the frontend to
distinguish an authentication failure from a network error.

For valid sessions, `WebSocketRelayHandler` creates the relay, registers the tab,
rechecks session validity, and starts its ping loop and reader.

### Relay Loop

The WebSocket reader parses only the five-byte outer header and consumes the
Activity flag. SYN registers a stream immediately, then opens yamux in a worker
with a ten-second timeout. DATA, FIN and RST enter that stream's FIFO queue, so
opening a stream or waiting for yamux window credit cannot block other requests
or WebSocket control frames.

Each stream has room for 16 queued messages. Queue overflow cancels only that
stream; it does not impose a WebSocket message size limit. Each bridge write has
a 20-second deadline, including time spent waiting for stream window credit.
Cancellation closes the yamux stream to interrupt blocked reads and writes.

FIN forwards its payload and waits for the bridge's final response. RST closes
the transport after earlier queued DATA, preserving an explicit `OpStreamAbort`
sent immediately before RST. Ordinary transport closure does not itself mean
that a bridge mutation was cancelled; see [API cancellation semantics](api-contract.md).

Each bridge reader starts with a buffer for the five-byte outer header and
4 KiB of payload, growing once to 32 KiB when a read fills it. It reads directly
into that buffer and reuses it after the synchronous WebSocket write. Small
reads are sent immediately. A mutex serializes
WebSocket data writes, while Gorilla's concurrent-safe `WriteControl` handles
pings. A failed data or ping write shuts down the connection and its streams.

The HTTP handler cancels and joins its workers when the connection ends. The
shared yamux session remains available to other tabs.

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
    sessions map[string]*relay.YamuxSession // SessionID → session
}{}

// Lookup when a WebSocket stream opens
yamuxSession, err := bridge.GetYamuxSession(sess.SessionID)
```

**Key points:**
- One yamux session per login (= one bridge process per login)
- Multiple WebSocket connections (tabs/windows) share the same session
- Session is keyed by `SessionID`, not username — a user can have multiple concurrent sessions
- Session survives WebSocket disconnects
- Stream IDs belong to one WebSocket relay. Replacing a socket closes its frontend streams, even if the old close event is still pending; recovery opens new streams.
- Tab registration, removal and expiry detachment share one mutex. Network closes happen outside that lock, and a post-registration session recheck covers expiry racing an upgrade.
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

EOF or a bridge read error closes that stream and notifies the browser with FIN.
An open or write failure closes the stream with RST if another worker has not
already closed it. Stream cleanup runs once, even when readers and writers fail
together.

If the yamux session dies, its `OnClose` callback terminates the HTTP session.
`CloseWebSocketForSession` closes all of that login's WebSocket connections with
code 1008.

## Performance

The relay avoids JSON decoding. Outgoing bridge data uses a reusable buffer
per stream, avoiding a separate allocation and copy for each outer frame. Small
responses keep a 4 KiB payload buffer; bulk output grows to 32 KiB.
Larger available reads also reduce WebSocket framing and write calls for bulk
output. Incoming messages still use `ReadMessage` and have no relay size limit.

The frontend writes both protocol headers into the final send buffer, copying
each upload payload once. Scrollback is allocated only when binary stream data
arrives, saving the default 64 KiB allocation for JSON-only requests. Circular
buffer operations use views when copying portions of an existing buffer.

Incoming progress and result JSON is decoded directly from a view of the
receive buffer before invoking callbacks. Binary DATA keeps its own copy so
handlers and detached buffers can retain it safely. In-memory upload chunks
also use views: the multiplexer copies each chunk into the final send frame
synchronously. Chunk pacing, protocol bytes, and JSON encoding are unchanged.

The 16 MiB yamux window is a flow-control setting, independent of WebSocket
message size. Stream queues and write deadlines isolate stalled consumers;
they do not eliminate flow-control waits within an individual stream.

Reproduce the relay framing benchmark with:

```sh
make test-go-quiet GO_TEST_PKGS=./webserver/web GO_TEST_FLAGS='-run=NoTests -bench=BenchmarkRelayFromBridge -benchmem -count=5 -race=false'
```

On an Intel Core Ultra 7 265H with Go 1.27.1, five-run medians for a 1 MiB
loopback transfer changed from 1.038 ms to 0.205 ms, 1,285,325 to 49,414 allocated
bytes, and 1,292 to 142 allocations. This measures relay framing and loopback
writes, including the receiving benchmark client; it does not measure file I/O
or deployment network throughput.

## Security

### Authentication

The upgrader uses [Gorilla's default origin policy](https://pkg.go.dev/github.com/gorilla/websocket#hdr-Origin_Considerations):
when `Origin` is present, its host and port must match the request's `Host`.
This also protects unauthenticated upgrades. Go's HTTP
[`CrossOriginProtection`](https://pkg.go.dev/net/http#CrossOriginProtection)
allows GET requests, so it does not replace the WebSocket origin check.
The Vite `/ws` proxy preserves both browser Host and Origin (`changeOrigin:
false`); it does not rewrite Origin to bypass validation. Clients without an
Origin header still require session authentication.

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
- Server does revalidate session lifetime on incoming messages and pongs; only explicit activity refreshes idle expiry
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
