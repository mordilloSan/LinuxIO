# Bridge-Survivable Jobs via systemd Transient Units

## Summary

Long-running jobs today live entirely inside the per-session bridge process as
goroutines in a package-global registry ([`jobs.DefaultRegistry`](../backend/common/ipc/bridge/jobs.go)).
When the bridge dies — session GC, websocket loss, `ReasonBridgeFailure` — every
in-flight job dies with it. This is the open `ToDo` item #5:

> if session dies bridge is orphaned doing work? Right now bridge dies and all
> work is canceled.

The fix is **not** to build a bespoke process supervisor. systemd already is one.
For the subset of jobs that must outlive a session, hand the work to a **systemd
transient unit** via `org.freedesktop.systemd1.Manager.StartTransientUnit` (the
same call `systemd-run` makes). systemd (PID 1) owns the process, so it survives
the bridge; journald captures its output for free; reattachment becomes "find the
unit again", not "keep a goroutine alive".

We can reach `StartTransientUnit` through the **existing** `dbusclient` — no new
dependency is strictly required (decision below).

## Goals / Non-Goals

Goals:
- Survivable execution for a *classified* set of job types (not all jobs).
- A reattach contract: after a bridge restart, re-discover running units and
  resume reporting progress/result to clients.
- Central, durable job logs via journald.
- Reuse the already-hash-validated `linuxio-bridge` binary; do not introduce new
  binaries that need their own integrity validation
  ([`validateBridgeHash`](../backend/webserver/bridge/bridge.go)).

Non-Goals (for this effort):
- Moving the job *control plane* into the webserver/session manager. That is a
  separate, larger change; see "Relation to broader architecture".
- Making every job survivable. Most jobs are tied to session intent and should
  keep dying with the session.
- Replacing the in-process `Registry` for short/interactive jobs.

## Background: how jobs run today

- A request hits the bridge router, which builds an `Owner{SessionID, Username,
  UID}` from the session ([`ownerFromSession`](../backend/common/ipc/bridge/router.go))
  and calls `registry.CreateForOwner(...)` then `job.Start(runner)`.
- `Start` launches `go j.run(ctx, runner)` with `ctx` derived from
  `context.Background()` — detached from the request stream, but **bound to the
  bridge process lifetime**.
- Progress/result flow back over yamux to the webserver and out to the browser;
  there is no durable store. The webserver does not own the registry (zero
  references to it).

The registry already models per-session/user ownership, so the data model is
ready for survivable + reattachable jobs; only the *execution substrate* is
process-bound.

## The crux: a transient unit runs a command, not a goroutine

`StartTransientUnit` executes an `ExecStart` command line. That forces a clean
boundary, and splits today's jobs into two shapes:

1. **Already-subprocess jobs** — work that already shells out can be wrapped in a
   unit directly. Candidates:
   - app update (`control.app_update`) — already uses `cmd.Start()` with a 30-min
     `SingletonSystem` policy ([appupdate](../backend/bridge/handlers/appupdate/handlers.go)).
   - package install / offline updates ([packages](../backend/bridge/handlers/packages/handlers.go)).
   - docker image pulls for compose up / container updates.

2. **In-process Go jobs** — work implemented against a library (compose via the
   moby SDK, the indexer) has no command-line entry point. To make these
   survivable they need a worker subcommand on the bridge binary, e.g.
   `linuxio-bridge job-exec --type docker.compose --payload <file>`, which the
   transient unit's `ExecStart` invokes. This is the disciplined version of the
   earlier "each job a standalone process" idea — one validated binary, many
   transient invocations, not N new binaries.

**Recommendation:** start with shape (1). It delivers the survivability win with
no new execution path. Tackle shape (2) only after the reattach contract is
proven, and only for job types that justify it.

## Design

### Starting a unit

Add a helper in a new `bridge/internal/systemdrun` (or extend `dbusclient`) that
calls the manager interface already used by the systemd handlers
([`SystemdManagerIface`](../backend/bridge/internal/dbusclient/interfaces.go),
example call: `managerIface.CallStore(ctx, "StartUnit", ...)` in
[systemd.go](../backend/bridge/handlers/systemd/systemd.go)):

- Method: `StartTransientUnit(name string, mode string, properties a(sv), aux a(sa(sv)))`.
- `name`: deterministic, owner-scoped, e.g. `linuxio-<jobtype>-<jobid>.service`.
  Encode enough to re-discover after a restart (see reattach).
- `mode`: `"fail"` (or `"replace"` for singletons).
- Key properties:
  - `Description` — human label.
  - `ExecStart` — `a(sasb)`: the command (binary + argv + ignore-failure flag).
  - `User` / `Group` — run as the session user. **Set these explicitly**: the
    `dbusclient` talks to the *system* bus, so units are system units. Source
    identity from the session (`rt.Session.User`), consistent with the
    handler-layer `rt` convention.
  - `StandardOutput=journal`, `StandardError=journal` — central logging.
  - `RemainAfterExit=false`, `CollectMode=inactive-or-failed` — let systemd reap.
  - Optional `Environment`, `WorkingDirectory`, resource limits.

### Capturing progress and result

- **Logs:** read the unit's journal by name (`journalctl --unit=<name> --follow`
  or sd-journal with a `_SYSTEMD_UNIT` match) and republish as job progress
  events through the existing `Job.ReportProgress` / data path.
- **Structured progress / result:** journald is line-oriented text. For typed
  progress and a final result, have the worker write NDJSON to a known per-job
  path (e.g. `/run/linuxio/jobs/<jobid>.ndjson`) or a unix socket the bridge
  tails. The unit's exit code gives terminal success/failure as a fallback.

### Tracking and reattachment

- Keep a thin in-bridge record mapping `jobID -> unit name` (and the journal
  cursor last published).
- On bridge startup, list units matching the `linuxio-<jobtype>-*` pattern (or
  filter by a custom unit property), rebuild job records for any still
  active/failed-but-uncollected, and resume tailing from the stored cursor.
- Owner is recoverable from the unit name / properties, so post-restart clients
  can re-subscribe by `ListForOwner`.

### Lifecycle and cancellation

- Cancel → `StopUnit(name, "replace")`.
- Job TTL / cleanup → rely on `CollectMode=inactive-or-failed` plus an explicit
  `ResetFailedUnit` sweep (already wrapped in
  [systemd.go](../backend/bridge/handlers/systemd/systemd.go)).
- Absolute caps still apply: a unit should carry `RuntimeMaxSec` so a wedged job
  cannot run forever.

## Dependency decision

`StartTransientUnit`'s property marshaling is the only real friction: `a(sv)`
properties and the nested `ExecStart` `a(sasb)` are awkward to hand-build with
`godbus/dbus/v5`.

- **Option A — extend existing `dbusclient` (godbus, current dep).** No new
  dependency; one connection; consistent with the rest of the systemd code. Cost:
  hand-marshal the transient-unit property structs once.
- **Option B — add `github.com/coreos/go-systemd/v22/dbus`.** Provides
  `StartTransientUnit(name, mode, props, ch)` with typed property helpers. Cost:
  a second dbus connection + new dependency surface.

**Recommendation:** spike with Option B to validate behavior fast (property
helpers remove guesswork), then port the proven call into `dbusclient` (Option A)
to avoid the standing dependency — unless the spike shows the marshaling is more
trouble than the dep is worth.

## Job classification (decide before building)

Produce a short table of every `ModeJob` / runner route and mark each:
`survivable?` / `idempotent or resumable?` / `already a subprocess?`. Only
`survivable` types get a transient unit; everything else stays an in-process job.
First candidates (survivable + already subprocess): app update, package
install/offline updates, docker pulls.

## Phasing

1. **Spike** — `StartTransientUnit` from a throwaway call (Option B): run `/bin/sleep`
   as the session user, confirm it survives killing the bridge, and tail its
   journal. Proves the substrate end to end.
2. **One real job** — migrate app update (`control.app_update`): wrap the existing
   command in a unit, stream journal as progress, map exit code to result.
3. **Reattach** — implement unit re-discovery + journal-cursor resume on bridge
   restart; verify a job started pre-restart finishes and reports post-restart.
4. **Generalize** — extract a `RunAsTransientUnit(job, spec)` helper and migrate
   the other shape-(1) candidates.
5. **Worker subcommand (optional)** — add `linuxio-bridge job-exec` and migrate a
   shape-(2) job (e.g. `docker.compose`) only if justified.

## Risks / open questions

- **systemd availability.** Containers/minimal hosts may lack a usable system
  manager. Gate via the existing capability/availability checks
  ([dbusclient availability](../backend/bridge/internal/dbusclient/availability.go))
  and fall back to the current in-process job for non-survivable execution.
- **Privilege.** System units running as `User=` need the bridge to have rights
  to start them. Confirm against the existing privileged-route model
  ([privilege_pattern.md](./privilege_pattern.md)).
- **Unit name collisions / leaks.** Deterministic naming + `CollectMode` +
  a `ResetFailed` sweep must prevent accumulation across restarts.
- **Cross-session ownership.** With `SingleSessionPerUser = false`, two sessions
  for one user could both see the same user-owned units. Reattach must filter by
  `Owner.Matches`, not just username.
- **Structured result transport.** Journald-only loses typed results; the NDJSON/
  socket sidecar needs a small, well-defined schema.

## Relation to broader architecture

This plan covers the **data plane** (where survivable work executes). It composes
with, but does not require, a future **control-plane** move that lets the
webserver/session manager own a durable job index and central log. It also pairs
with the [Session Activity Timeout Plan](./session-activity-timeout-plan.md):
transient-unit jobs no longer need to inhibit bridge idle GC to stay alive,
simplifying that design for survivable types. Prerequisite cleanup: `ToDo` #6
("Total review of jobs code") should land first so this is built on understood
foundations.
