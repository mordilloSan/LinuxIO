# Code Review Findings

- **Date:** 2026-08-03
- **Original baseline:** `dev/v0.17.0`, HEAD `a6c67227`
- **Status updated:** 2026-08-03 against `dev/v0.17.0`, HEAD `bfc3405d`, plus
  the current working tree
- **Scope:** full read of `backend/bridge/cmd/` and `backend/webserver/cmd/` (11 files, ~1,100 lines), plus independent verification of an external report covering `backend/webserver/web/tls_redirect.go`, `backend/webserver/bridge/bridge.go`, `backend/common/ipc/relay/protocol.go`, and `backend/auth/linuxio-auth.c`.
- **Focus:** idiomatic Go, performance, stability. Not a security audit.
- **Method:** every claim was cross-checked against the packages the code calls (router, relay, logging, session) before being flagged. Original line numbers refer to the original review snapshot; resolution notes cite the current code.
- **Current code validation:** `make check-backend` passed after the E4
  working-tree fix listed below.

At the original review snapshot the working tree contained uncommitted fixes
(with new tests) in
`backend/bridge/cmd/yamux.go`, `backend/common/ipc/relay/protocol.go`,
`backend/webserver/bridge/bridge.go`, and `backend/webserver/web/tls_redirect.go`.
Those fixes, together with the bridge lifecycle/state cleanup, are now committed
in `a08258bd`. The CLI exit-code fixes are committed in `c80db2ec`; findings 4,
7, 8, and most of 9 are committed in `e595eada`; E3 is committed in `a5802171`.
The current working tree fixes E4. Statuses below reflect the current tree;
intervening unrelated commits are outside this review.

## Current status summary

- **Fixed in committed code:** findings 1-8 and most of 9; E1, E2, E3, E5, and
  E8.
- **Fixed in the current working tree:** E4.
- **Still open:** the channel-direction nit in finding 9; E6, E7, E9, and E10.

## Overall verdict (cmd folders)

Solid, modern Go. Exit-code discipline (`Run` returns an int, only `main` exits), the
`sync.Once` connection closer, non-blocking sends to buffered shutdown channels, the
hand-rolled `systemdListeners` (correct PID check, env unsetting, `CloseOnExec` —
matches go-systemd semantics without the dependency), and current idioms
(`WaitGroup.Go`, `strings.SplitSeq`, range-over-int) are all used correctly.

Performance: nothing urgent to fix. The only hot paths are the per-request HTTP
wrapper (one `time.Now` + two atomics — negligible, and the right design for idle
tracking) and per-stream setup in yamux (a counter increment + a `strconv` —
negligible). Everything else is startup/shutdown code. The original bridge
shutdown weakness described below has since been replaced by explicit
cancel/close/bounded-drain sequencing in `a08258bd`.

Verified non-findings (checked and cleared):

- The ignored `Dispatch` error in `handleYamuxStream` is fine — `Router.Dispatch`
  writes the error frame to the peer and logs the outcome itself
  (`backend/common/ipc/bridge/router.go`).
- Logging before `syscall.Umask` in `bridge/cmd/root.go` is harmless — logging goes
  to journald, not files (`backend/common/logging/logging.go`).

---

## Findings — stability

### 1. `bridgeClosing` is dead code — FIXED (`a08258bd`)

Declared at `backend/bridge/cmd/lifecycle.go:22` as "Global shutdown signal for all
handlers", closed at `lifecycle.go:106` — and never read anywhere in the codebase
(grepped all of `backend/`). Either handlers were supposed to select on it and
don't, or it's a leftover. The comment actively misleads: shutdown propagation
actually happens via `sessionCancel()` + `CancelForSession` + closing the conn.

Resolution: `a08258bd` removed `bridgeClosing` entirely. `runBridge` now owns the
session context and loop-completion channel, and `shutdownBridge` performs the
real cancellation and transport teardown directly
(`backend/bridge/cmd/lifecycle.go:35-69`).

### 2. Sleep-ordered shutdown — FIXED (`a08258bd`)

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

Resolution: `a08258bd` removed both sleeps and retained the load-bearing ordering:
cancel the session and registry work, close the client connection to release
yamux `Accept`, then wait up to five seconds on the explicit loop-completion
channel (`backend/bridge/cmd/lifecycle.go:55-69,92-100`).

### 3. Misuse paths exit 0, but the auth daemon reads exit codes — FIXED (`c80db2ec`)

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

Resolution: `c80db2ec` made `isDirectBridgeInvocation` return the `Stat` error,
which `run` reports as exit 1; direct invocation and unknown bridge arguments now
return usage exit 2. Unknown webserver commands also return 2. Focused CLI tests
cover these paths (`backend/bridge/cmd/cli_test.go` and
`backend/webserver/cmd/cli_test.go`).

### 4. Listener leak with hanging clients on partial activation failure — FIXED (`e595eada`)

In `backend/webserver/cmd/activation.go:44-58`, if wrapping fd N fails, the
listeners already created for fds 3..N-1 are neither closed nor returned. The caller
logs a warning and falls back to self-bind, so those systemd-provided sockets stay
open but unserved — connections to them hang forever instead of being refused.
Close the partial slice on the error path, and reconsider the fallback itself: when
systemd handed the process sockets but wrapping them failed, silently self-binding a
different socket masks the misconfiguration instead of surfacing it — failing the
start is the more honest behavior in that case.

Resolution: `systemdListeners` now creates wrappers for every inherited fd and
always closes those originals. `listenersFromFiles` closes any listeners already
converted when a later conversion fails. `serveWithSocketActivation` returns the
discovery error as handled, so self-bind occurs only when no activation listeners
were supplied; it also closes listeners if TLS setup fails
(`backend/webserver/cmd/activation.go:44-78`,
`backend/webserver/cmd/root.go:127-144`). Regression tests cover partial cleanup
and the no-fallback error result (`backend/webserver/cmd/activation_test.go`).

### 5. Unbounded first-frame read per stream — FIXED (`a08258bd`)

At the original baseline, `handleYamuxStream` read the `OpStreamOpen` frame with
no deadline; a stream opened but never written parked a goroutine until the whole
session died.
Session-level keepalive (confirmed on in `relay.YamuxConfig`) catches a dead peer
but not a live idle one. The peer is the trusted webserver, so this was hardening,
not a bug. A related inconsistency: the opcode-mismatch path closed silently while
the parse-failure path wrote a structured 400 back.

The fix, committed in `a08258bd`, resolves all of it:
`streamOpenReadTimeout = 5s` around the
first read (`backend/bridge/cmd/yamux.go:17,67-71`), a progressive frame reader
(see external finding E5), and a structured 400 on opcode mismatch
(`yamux.go:79`).

---

## Findings — idiom and cleanliness

### 6. Global mutable session state, used inconsistently — FIXED (`a08258bd`)

The bridge keeps `bootCfg`, `sess`, and `wg` as package globals, yet
`runtime.Runtime` already carries the session. At the original baseline,
`handleYamuxSession` logged
via the global `sess` while `handleYamuxStream` shadowed that same global with
`sess := rt.Session` — shadowing a package-level var by design is a footgun.

Resolution: `a08258bd` made bootstrap/session construction return local values,
removed the package-level `bootCfg`, `sess`, and `wg`, and threads
`runtime.Runtime` plus an explicit loop-completion channel through lifecycle
code. `handleYamuxSession` consistently uses `rt.Session`, and its stream counter
is a local `uint64`, which is correct because only the accept loop mutates it.

### 7. Counter-plumbing in the webserver — FIXED (`e595eada`)

`newHTTPServer` returns `(*http.Server, *atomic.Int64, *atomic.Int64, error)` and
those two pointers thread through `startHTTPServer` → `serveWithSocketActivation` →
`startSocketIdleExitWatcher` (`backend/webserver/cmd/root.go:64-103`). A tiny
`activity` struct (`inFlight`, `lastHit`, maybe an `idle() bool` method) collapses
four signatures and gives the idle predicate a home.

Resolution: `serverActivity` now owns both atomics and the `idleFor` predicate,
and one pointer is threaded through server construction, activation, and the
idle watcher (`backend/webserver/cmd/root.go:64-103,105-166,248-271`).

### 8. slog anti-pattern in limits.go — FIXED (`e595eada`)

`backend/bridge/cmd/limits.go:33` and `:59` build the message with `fmt.Sprintf`
from the same values passed as attrs — every field appears twice per record.
Structured-logging idiom is a constant message plus attrs; journald consumers get
the fields anyway. Also `^uint64(0)` at `limits.go:42` works, but
`syscall.RLIM_INFINITY` states the intent.

Resolution: both records now use constant slog messages with structured attrs
only. The infinity check uses the explicit `math.MaxUint64` constant
(`backend/bridge/cmd/limits.go:23-62`).

### 9. Small nits — PARTIALLY ADDRESSED (one open)

- ~~`s := stream; sid := streamID` redundant copies in the yamux accept loop~~ —
  fixed in `a08258bd` (the closure now captures the per-iteration locals
  directly).
- ~~"bridge boot" and "bridge starting" (`backend/bridge/cmd/root.go:43-54`) both
  log the uid two lines apart — one can go~~ — fixed in `e595eada` by
  retaining the richer `bridge boot` record.
- **OPEN:** `startHTTPServer`'s return type would read better as
  `(<-chan os.Signal, <-chan error)`.
- ~~Usage text mixes `linuxio` and `linuxio-webserver` as the binary name
  (`backend/webserver/cmd/cli.go:84-97`)~~ — fixed in `e595eada`; all usage
  and examples now say `linuxio-webserver`.
- ~~The `wg` comment at `backend/bridge/cmd/lifecycle.go:24` says "in-flight
  requests" but it actually tracks the single session goroutine (per-stream
  tracking is `streamWg`, waited transitively) — the mechanism is correct, the
  comment isn't~~ — fixed in `a08258bd` by replacing the global wait group with
  an explicit loop-completion channel.

---

## Verified external report

An externally produced report was independently verified against the original
baseline `a6c67227` and its working tree. All items below are real; their current
resolution status is recorded without removing the original findings.

### E1. TLS redirect accept-loop DoS — REAL, FIXED (`a08258bd`)

At the original baseline, `tlsRedirectListener.Accept()` called `br.Peek(1)`
inline in the accept loop with no deadline. One client connecting and sending
nothing stalled the loop — no further connections could be accepted at all,
unauthenticated, with a single idle TCP connection. Graceful shutdown was also
affected: `Accept` blocked in `Peek` on a conn rather than in `Listener.Accept`,
so closing the listener did not unblock it. Highest-severity item in the report.

Resolution (`a08258bd`, `backend/webserver/web/tls_redirect.go` + new
`tls_redirect_test.go`): async per-connection classification, a five-second peek
deadline, bounded redirect reads, active-connection cleanup in `Close`, and a
results channel with a `done` signal.

### E2. Bridge global self-deadlock — REAL, FIXED (`a08258bd`)

At the original baseline, `attachBridgeSession` called `SetOnClose` while holding
`yamuxSessions.Lock()`. `SetOnClose` invokes the callback synchronously if the
session already closed (`backend/common/ipc/relay/yamux.go:63-76`), and the
callback re-takes the same non-reentrant lock — self-deadlock if the bridge dies
between `NewYamuxClient` and registration. The mutex is process-global, so every
future login and bridge operation hangs forever. The original callback also
deleted the map entry unconditionally, so an old session closing late could
remove a new session's mapping.

Resolution (`a08258bd`, `backend/webserver/bridge/bridge.go` + new
`bridge_test.go`):
publish replacement before closing the old session, register the callback outside
the lock, delete only when the stored instance matches.

### E3. Missing HTTP server timeouts — REAL, FIXED (`a5802171`)

The `http.Server` at `backend/webserver/cmd/root.go:98-102` has no
`ReadHeaderTimeout` or `IdleTimeout`. The redirect path now has 5s bounds
(post-E1 fix), but TLS conns handed to the HTTP server are unbounded pre-header.
Add header/idle timeouts; avoid a blanket `WriteTimeout`, which would break
WebSocket/streaming endpoints.

Resolution: the server now sets a 10-second `ReadHeaderTimeout`, which net/http
also uses to bound the TLS handshake, and a two-minute `IdleTimeout` for inactive
keep-alive connections. `WriteTimeout` intentionally remains zero for WebSocket
and streaming handlers. `TestNewHTTPServerConnectionTimeouts` locks in all three
policy choices (`backend/webserver/cmd/root.go`,
`backend/webserver/cmd/root_test.go`).

### E4. Capability handshake deadline covers only `Open` — REAL, FIXED in working tree

At `backend/webserver/bridge/bridge.go:124-136` the 5s context wraps only
`yamuxSession.Open`; the request write and response read ignore the context
entirely. This runs during `Login` while holding a slot from
`maxConcurrentLogins = 8` (`backend/webserver/auth/auth.go:15`). A
live-but-unresponsive bridge parks a slot indefinitely — yamux keepalive only
catches dead transports — so eight stuck logins lock everyone out.

Resolution: the existing five-second context now supplies one absolute deadline
for stream open, request write, and response read, while still honoring an earlier
caller deadline. A real-yamux regression test holds an accepted stream open
without responding and verifies that `fetchSessionCapabilities` returns a timeout
promptly (`backend/webserver/bridge/bridge.go`,
`backend/webserver/bridge/bridge_test.go`).

### E5. Frame reader allocates declared payload before receiving — REAL, FIXED (`a08258bd`)

`ReadRelayFrame` does `make([]byte, length)` up to the 16 MiB cap
(`maxRelayPayloadSize`, `backend/common/ipc/relay/protocol.go:40`) before any
payload bytes arrive, and the original bridge path used it for the untrusted
first frame.
Severity qualifier: the bridge's peer is the webserver, so exploiting this requires
an authenticated user to influence a declared frame length through a proxied path —
hardening rather than a direct exploit, but the mechanical claim is true.

Resolution (`a08258bd`): `ReadRelayFrameProgressive` grows the buffer only as
bytes arrive (`protocol.go` + tests) and is used for the first frame at
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

### E8. Dead shutdown channel / sleeps — REAL, FIXED (`a08258bd`)

Same as findings 1 and 2 above (the external report double-counted them). See the
caution under finding 2; the committed fix preserves the required close-before-
wait ordering.

### E9. Per-start self-signed certificate churn — REAL, OPEN

`configureServerTLS` (`backend/webserver/cmd/root.go:198-206`) calls
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

1. **DONE (`a08258bd`):** E1, E2, E5, and finding 5 are committed with their
   regression tests (`tls_redirect_test.go`, `bridge_test.go`,
   `protocol_test.go`).
2. **DONE (`c80db2ec`):** finding 3's bridge/webserver usage exit codes and
   `Stdin.Stat` error handling are committed with focused tests.
3. **DONE (`e595eada`):** finding 4 closes original and partially wrapped
   activation listeners and returns activation discovery failures instead of
   silently self-binding.
4. **DONE (`a08258bd`):** findings 1-2 removed the dead shutdown channel and
   sleeps while preserving the required close-before-wait ordering.
5. **MOSTLY DONE:** finding 6 is committed; findings 7-8 and the duplicate boot
   log/CLI wording parts of finding 9 are committed in `e595eada`. The only
   remaining cleanup item from this group is the directional channel return type
   in `startHTTPServer`.

Separate confirmed audit findings:

- **DONE (`a5802171`):** missing `http.Server` handshake/header/idle timeouts
  (E3).
- **DONE in the working tree:** capabilities handshake stream deadline (E4).
- C auth daemon: blocking lastlog lock (E6), four-byte `ut_id` collisions and the
  narrow `child_die` status-pipe race (E7).
- Per-start self-signed certificate churn (E9).
- The 4 KiB WebSocket relay buffer (E10) — performance-only; benchmark before
  changing it.
