# Cockpit-Style Session Activity And Job-Safe Idle Cleanup

## Summary

Implement LinuxIO session idle handling like Cockpit: real browser/user activity
refreshes the interactive session over the existing WebSocket transport, while
polling, WebSocket pings, yamux traffic, logs, and progress output do not count
as activity.

Do not add `POST /auth/activity`. Add a tiny WebSocket mux control message
instead. Active jobs get a separate server-side idle inhibitor so they keep the
bridge alive even when the browser is closed.

## Key Changes

- Add a WebSocket mux control frame:
  - `streamID = 0`
  - `FlagCTRL = 0x20`
  - payload is UTF-8 JSON: `{"channel":"session-control","command":"active"}`
- Backend handles this control frame inside the webserver and never forwards it
  to yamux.
- Keep normal stream payloads opaque; this is the only deliberate exception to
  the current "pure byte relay" rule.
- Refactor session refresh semantics:
  - `ValidateFromRequest` validates only.
  - WebSocket pong, read-loop traffic, API polling, and bridge output no longer
    refresh idle time.
  - Explicit `session-control active` refreshes `LastAccess`, `LastRefresh`, and
    `IdleUntil`.
- Add job idle inhibition:
  - queued/running jobs prevent idle GC from deleting the session/closing the
    bridge.
  - job progress output does not refresh activity.
  - browser-closed jobs remain alive until completion, cancellation, bridge
    failure, or the 12h absolute timeout.

## Implementation Notes

- Frontend:
  - Add a session activity reporter mounted only in the authenticated app shell.
  - Listen for real user events: `mousemove`, `mousedown`, `keydown`,
    `touchmove`, and captured `scroll`.
  - Throttle activity sends to once every 10 seconds, matching Cockpit.
  - Send control frames through `StreamMultiplexer`; do not use HTTP.
- Backend webserver:
  - Add `FlagCTRL` beside existing mux flags.
  - In `readLoop`, handle control frames before `SYN/DATA/FIN/RST`.
  - Remove session refresh from WebSocket pong handling and generic frame reads.
- Session manager:
  - Add explicit activity recording, keeping `Refresh` as a compatibility
    wrapper if useful.
  - Add an idle-inhibitor callback/hook used by validation, GC, and
    `ActiveSessions`.
  - Absolute timeout is never inhibited.
- Job inhibitor:
  - Add a webserver-side bridge helper that asks the bridge for
    `jobs list active`.
  - If no yamux session exists, do not inhibit.
  - If yamux exists but the active-job query transiently fails, fail open for
    that GC cycle and log a warning; the 12h absolute timeout remains the hard
    cap.
- Docs:
  - Create this design note in `docs/session-activity-timeout-plan.md`.
  - Update `docs/server-yamux-protocol.md` when implementation lands to
    document `FlagCTRL`.

## Test Plan

- Go session tests:
  - `ValidateFromRequest` does not extend idle time.
  - explicit activity extends idle time.
  - idle-expired sessions are rejected without an inhibitor.
  - idle-expired sessions survive while an inhibitor reports active jobs.
  - absolute-expired sessions are deleted even with active jobs.
- WebSocket/backend tests:
  - `session-control active` refreshes the session.
  - pong/read-loop traffic does not refresh the session.
  - invalid or unknown control frames are ignored/logged without reaching yamux.
- Job lifecycle scenarios:
  - browser closed, no active jobs: session is GC'd after idle timeout and
    bridge stops cleanly.
  - browser closed, active job running: session/bridge stay alive until the job
    finishes.
  - active job finishes after idle deadline: next GC removes the session.
  - bridge killed externally: session still terminates as `bridge_failure`.
- Verification commands:
  - `make test-backend`
  - `make tsc`
  - targeted manual journal test with browser close, active job, and no-job
    cases.

## Assumptions

- Keep current defaults: `IdleTimeout = 15m`, `RefreshThrottle = 60s`,
  `AbsoluteTimeout = 12h`.
- No countdown modal in this revision.
- Terminals, log streams, Docker polling, and progress streams are transport
  activity, not human activity.
- Active jobs are LinuxIO-specific protection; Cockpit does not appear to treat
  progress output as session activity.
