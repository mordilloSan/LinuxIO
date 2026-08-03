# Code Review Findings

- **Date:** 2026-08-03
- **Branch:** `dev/v0.17.0`, HEAD `a6c67227`
- **Scope:** full read of `backend/bridge/cmd/` and `backend/webserver/cmd/` (11 files, ~1,100 lines), plus independent verification of an external report covering `backend/webserver/web/tls_redirect.go`, `backend/webserver/bridge/bridge.go`, `backend/common/ipc/relay/protocol.go`, and `backend/auth/linuxio-auth.c`.
- **Focus:** idiomatic Go, performance, stability. Not a security audit.
- **Method:** every claim was cross-checked against the packages the code calls (router, relay, logging, session) before being flagged. Findings against files that carry uncommitted fixes in the working tree are marked accordingly; line numbers refer to the working tree at review time unless labeled HEAD.

At review time the working tree contained uncommitted fixes (with new tests) in
`backend/bridge/cmd/yamux.go`, `backend/common/ipc/relay/protocol.go`,
`backend/webserver/bridge/bridge.go`, and `backend/webserver/web/tls_redirect.go`.
Statuses below reflect that tree.

## Overall verdict (cmd folders)

Solid, modern Go. Exit-code discipline (`Run` returns an int, only `main` exits), the
`sync.Once` connection closer, non-blocking sends to buffered shutdown channels, the
hand-rolled `systemdListeners` (correct PID check, env unsetting, `CloseOnExec` —
matches go-systemd semantics without the dependency), and current idioms
(`WaitGroup.Go`, `strings.SplitSeq`, range-over-int) are all used correctly.

Performance: nothing to fix. The only hot paths are the per-request HTTP wrapper
(one `time.Now` + two atomics — negligible, and the right design for idle tracking)
and per-stream setup in yamux (a counter increment + a `strconv` — negligible).
Everything else is startup/shutdown code. The weak spot is shutdown-path stability
in the bridge, held together by sleeps and a dead signal channel.

Verified non-findings (checked and cleared):

- The ignored `Dispatch` error in `handleYamuxStream` is fine — `Router.Dispatch`
  writes the error frame to the peer and logs the outcome itself
  (`backend/common/ipc/bridge/router.go`).
- Logging before `syscall.Umask` in `bridge/cmd/root.go` is harmless — logging goes
  to journald, not files (`backend/common/logging/logging.go`).

---

## Findings — stability

### 1. `bridgeClosing` is dead code — OPEN

Declared at `backend/bridge/cmd/lifecycle.go:22` as "Global shutdown signal for all
handlers", closed at `lifecycle.go:106` — and never read anywhere in the codebase
(grepped all of `backend/`). Either handlers were supposed to select on it and
don't, or it's a leftover. The comment actively misleads: shutdown propagation
actually happens via `sessionCancel()` + `CancelForSession` + closing the conn.

### 2. Sleep-ordered shutdown — OPEN

`backend/bridge/cmd/lifecycle.go:105-111` has two bare `time.Sleep` calls (50ms,
100ms) sequencing cancel → close-conn. Sleep-based ordering is the classic source of
"works on my machine" shutdown races — if those delays exist to let a final response
flush, that's an event that can be synchronized on; if they're load-bearing, they
deserve named constants and a comment saying what breaks without them. Every
shutdown also eats the 150ms unconditionally.

Caution for the fix: do **not** replace the 100ms sleep with "wait for handlers,
then close the conn" — the `wg`-tracked goroutine is blocked in
`ymuxSession.Accept()` and only unblocks *when* the conn closes, so that reordering
deadlocks until the 5s grace timeout fires.

### 3. Misuse paths exit 0, but the auth daemon reads exit codes — OPEN

`backend/bridge/cmd/bootstrap.go:20-21` says startup failure is detected via exit
code 1. Yet `isDirectBridgeInvocation` (`backend/bridge/cmd/cli.go:28-31`) treats a
`Stdin.Stat()` error as "direct invocation", which makes `Run` print the
not-for-direct-use notice and return 0. If stat ever fails in a legitimate daemon
spawn, the daemon sees success while no bridge is running.

Separately, unknown args (`backend/bridge/cmd/cli.go:17-19`) and the webserver's
unknown subcommand (`backend/webserver/cmd/cli.go:69-74`) both print an error yet
return 0 — scripts and unit files can't distinguish a typo from success (and
`backend/webserver/cmd/cli_test.go:48` enshrines it). Convention is exit 2 for
usage errors.

### 4. Listener leak with hanging clients on partial activation failure — OPEN

In `backend/webserver/cmd/activation.go:44-58`, if wrapping fd N fails, the
listeners already created for fds 3..N-1 are neither closed nor returned. The caller
logs a warning and falls back to self-bind, so those systemd-provided sockets stay
open but unserved — connections to them hang forever instead of being refused.
Close the partial slice on the error path, and reconsider the fallback itself: when
systemd handed the process sockets but wrapping them failed, silently self-binding a
different socket masks the misconfiguration instead of surfacing it — failing the
start is the more honest behavior in that case.

### 5. Unbounded first-frame read per stream — FIXED in working tree

At HEAD, `handleYamuxStream` read the `OpStreamOpen` frame with no deadline; a
stream opened but never written parked a goroutine until the whole session died.
Session-level keepalive (confirmed on in `relay.YamuxConfig`) catches a dead peer
but not a live idle one. The peer is the trusted webserver, so this was hardening,
not a bug. A related inconsistency: the opcode-mismatch path closed silently while
the parse-failure path wrote a structured 400 back.

The uncommitted fix resolves all of it: `streamOpenReadTimeout = 5s` around the
first read (`backend/bridge/cmd/yamux.go:17,67-71`), a progressive frame reader
(see external finding E5), and a structured 400 on opcode mismatch
(`yamux.go:79`).

---

## Findings — idiom and cleanliness

### 6. Global mutable session state, used inconsistently — PARTIALLY ADDRESSED

The bridge keeps `bootCfg`, `sess`, and `wg` as package globals, yet
`runtime.Runtime` already carries the session. At HEAD, `handleYamuxSession` logged
via the global `sess` while `handleYamuxStream` shadowed that same global with
`sess := rt.Session` — shadowing a package-level var by design is a footgun.

The working tree fixes the yamux side (`handleYamuxSession` now uses `rt.Session`
throughout, and the package-level `streamCounter` atomic became a local plain
`uint64` — correct, since only the accept loop touches it). Still open: `bootCfg`,
`sess`, and `wg` remain package globals (`bootstrap.go:15-16`,
`lifecycle.go:25`), and `lifecycle.go:114` still logs via the global `sess`. Pick
one source of truth (the `Runtime`) and thread it; this is the biggest cleanliness
item and would also make the package testable.

### 7. Counter-plumbing in the webserver — OPEN

`newHTTPServer` returns `(*http.Server, *atomic.Int64, *atomic.Int64, error)` and
those two pointers thread through `startHTTPServer` → `serveWithSocketActivation` →
`startSocketIdleExitWatcher` (`backend/webserver/cmd/root.go:64-103`). A tiny
`activity` struct (`inFlight`, `lastHit`, maybe an `idle() bool` method) collapses
four signatures and gives the idle predicate a home.

### 8. slog anti-pattern in limits.go — OPEN

`backend/bridge/cmd/limits.go:33` and `:59` build the message with `fmt.Sprintf`
from the same values passed as attrs — every field appears twice per record.
Structured-logging idiom is a constant message plus attrs; journald consumers get
the fields anyway. Also `^uint64(0)` at `limits.go:42` works, but
`syscall.RLIM_INFINITY` states the intent.

### 9. Small nits — MOSTLY OPEN

- ~~`s := stream; sid := streamID` redundant copies in the yamux accept loop~~ —
  fixed in the working tree (closure now captures the per-iteration locals
  directly).
- "bridge boot" and "bridge starting" (`backend/bridge/cmd/root.go:43-54`) both log
  the uid two lines apart — one can go.
- `startHTTPServer`'s return type would read better as
  `(<-chan os.Signal, <-chan error)`.
- Usage text mixes `linuxio` and `linuxio-webserver` as the binary name
  (`backend/webserver/cmd/cli.go:84-97`).
- The `wg` comment at `backend/bridge/cmd/lifecycle.go:24` says "in-flight
  requests" but it actually tracks the single session goroutine (per-stream
  tracking is `streamWg`, waited transitively) — the mechanism is correct, the
  comment isn't.

---

## Verified external report

An externally produced report was independently verified against HEAD `a6c67227`
and the working tree. All eight items are real.

### E1. TLS redirect accept-loop DoS — REAL, FIXED in working tree

At HEAD, `tlsRedirectListener.Accept()` called `br.Peek(1)` inline in the accept
loop with no deadline. One client connecting and sending nothing stalled the loop —
no further connections could be accepted at all, unauthenticated, with a single
idle TCP connection. Graceful shutdown was also affected: `Accept` blocked in
`Peek` on a conn rather than in `Listener.Accept`, so closing the listener did not
unblock it. Highest-severity item in the report.

Fix (uncommitted, `backend/webserver/web/tls_redirect.go` + new
`tls_redirect_test.go`): async per-conn classify goroutines, 5s peek deadline,
active-conn map closed in `Close()`, results channel with `done` signal.

### E2. Bridge global self-deadlock — REAL, FIXED in working tree

At HEAD, `attachBridgeSession` called `SetOnClose` while holding
`yamuxSessions.Lock()`. `SetOnClose` invokes the callback synchronously if the
session already closed (`backend/common/ipc/relay/yamux.go:63-76`), and the
callback re-takes the same non-reentrant lock — self-deadlock if the bridge dies
between `NewYamuxClient` and registration. The mutex is process-global, so every
future login and bridge operation hangs forever. HEAD's callback also deleted the
map entry unconditionally, so an old session closing late could remove a new
session's mapping.

Fix (uncommitted, `backend/webserver/bridge/bridge.go` + new `bridge_test.go`):
publish replacement before closing the old session, register the callback outside
the lock, delete only when the stored instance matches.

### E3. Missing HTTP server timeouts — REAL, OPEN

The `http.Server` at `backend/webserver/cmd/root.go:90-94` has no
`ReadHeaderTimeout` or `IdleTimeout`. The redirect path now has 5s bounds
(post-E1 fix), but TLS conns handed to the HTTP server are unbounded pre-header.
Add header/idle timeouts; avoid a blanket `WriteTimeout`, which would break
WebSocket/streaming endpoints.

### E4. Capability handshake deadline covers only `Open` — REAL, OPEN

At `backend/webserver/bridge/bridge.go:124-136` the 5s context wraps only
`yamuxSession.Open`; the request write and response read ignore the context
entirely. This runs during `Login` while holding a slot from
`maxConcurrentLogins = 8` (`backend/webserver/auth/auth.go:15`). A
live-but-unresponsive bridge parks a slot indefinitely — yamux keepalive only
catches dead transports — so eight stuck logins lock everyone out.

### E5. Frame reader allocates declared payload before receiving — REAL at HEAD, FIXED in working tree

`ReadRelayFrame` does `make([]byte, length)` up to the 16 MiB cap
(`maxRelayPayloadSize`, `backend/common/ipc/relay/protocol.go:40`) before any
payload bytes arrive, and HEAD's bridge used it for the untrusted first frame.
Severity qualifier: the bridge's peer is the webserver, so exploiting this requires
an authenticated user to influence a declared frame length through a proxied path —
hardening rather than a direct exploit, but the mechanical claim is true.

Fix (uncommitted): `ReadRelayFrameProgressive` grows the buffer only as bytes
arrive (`protocol.go` + tests) and is used for the first frame at
`backend/bridge/cmd/yamux.go:68`.

### E6. Blocking lastlog lock — REAL, OPEN

`flock(fd, LOCK_EX)` in `update_lastlog` (`backend/auth/linuxio-auth.c:424`)
blocks login indefinitely if anything holds the lock on `/var/log/lastlog`. The
call site already ignores the return value (`(void)update_lastlog(...)`), so
`LOCK_NB` with best-effort skip is the right fix.

### E7. `ut_id` truncation and exec-status race — BOTH REAL, OPEN (narrow)

- **`ut_id` truncation:** the PID is printed as decimal and truncated into the
  4-byte `ut_id` field (`backend/auth/linuxio-auth.c:506-511`, same pattern in
  `record_login_end`). Any PID ≥ 10000 collides on its first four digits across
  sessions; start/end records still pair up since both use the same PID.
  Minor correctness (wrong `who`/`last` output under collision).
- **Exec-status race:** on pipe EOF, the parent does a one-shot
  `waitpid(WNOHANG)` (`linuxio-auth.c:~1961-1965`) to distinguish "exec'd fine"
  from "child `_exit()`'d pre-exec". In the kernel, `do_exit()` runs
  `exit_files()` (producing the EOF) before `exit_notify()` makes the child
  waitable, so a dead child can probe as "still running" and the daemon sends OK
  for a dead bridge. Narrow window, real per kernel `exit.c` ordering. Cleanest
  fix: have `child_die` (`linuxio-auth.c:966-972`, currently stderr-only) write a
  status byte to the exec-status pipe before `_exit(127)`, removing the ambiguity
  entirely rather than re-probing.

### E8. Dead shutdown channel / sleeps — REAL, OPEN

Same as findings 1 and 2 above (the external report double-counted them). See the
caution under finding 2 before changing the close ordering.

### E9. Per-start self-signed certificate churn — REAL, OPEN

`configureServerTLS` (`backend/webserver/cmd/root.go:181-189`) calls
`web.GenerateSelfSignedCert()` (`backend/webserver/web/certificate.go:16`) on every
server start: a fresh in-memory RSA-2048 key and certificate, never persisted.
Consequences compound with socket activation — the idle-exit watcher shuts the
process down when unused and systemd re-spawns it on the next connection, so a new
certificate is minted per idle cycle, not per reboot. Browser trust exceptions
break repeatedly, and RSA-2048 keygen adds cold-start latency to every
socket-activated wake. Fix direction: persist the key/cert on disk and regenerate
only when missing or expired; switching to ECDSA P-256 would also cut generation
cost.

### E10. 4 KiB WebSocket relay buffer — performance-only candidate, OPEN

`relayFromBridge` (`backend/webserver/web/websocket.go:442-443`) copies from the
yamux stream to the WebSocket through a per-stream 4 KiB buffer; each read becomes
one DATA frame, so the buffer size also caps frame size. For bulk transfers this
means ~16x more reads/frames than a 64 KiB buffer would produce. This is a
performance candidate only, not a bug — benchmark before changing it: larger
buffers cost memory per concurrent stream and change frame pacing for interactive
streams (terminals), which may matter more than bulk throughput.

---

## Remaining work

1. **Commit the working-tree fixes** for E1, E2, E5 (+ finding 5) — the two
   "fix first" items are genuine high-severity bugs and their fixes carry tests
   (`tls_redirect_test.go`, `bridge_test.go`, `protocol_test.go`).
2. **Finding 3** — correct bridge/webserver misuse exit codes (exit 2 for usage
   errors) and treat `Stdin.Stat` failure as an error rather than as direct
   invocation.
3. **Finding 4** — close partially wrapped systemd listeners on the error path and
   avoid the inappropriate self-bind fallback when activation sockets were handed
   over but could not be wrapped.
4. **Findings 1-2** — remove the dead `bridgeClosing` channel and the 50ms delay;
   do not blindly reorder the 100ms close (see the caution under finding 2).
5. **Optional cleanup** — activity-counter struct (finding 7), structured limit
   logging (finding 8), duplicate boot log and CLI usage wording (finding 9),
   remaining session globals (finding 6).

Separate confirmed audit findings still untouched:

- Missing `http.Server` handshake/header/idle timeouts (E3).
- Capabilities handshake lacks a stream deadline while holding a login slot (E4).
- C auth daemon: blocking lastlog lock (E6), four-byte `ut_id` collisions and the
  narrow `child_die` status-pipe race (E7).
- Per-start self-signed certificate churn (E9).
- The 4 KiB WebSocket relay buffer (E10) — performance-only; benchmark before
  changing it.
