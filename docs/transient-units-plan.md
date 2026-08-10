# Durable operations and transient units

## Summary

Most Tasks should remain session-bound. They represent an interactive request,
and cancelling them when the bridge/session goes away is the least surprising
behavior. Durability is an explicit opt-in for operations whose work and
security model justify surviving a bridge restart or websocket loss.

A durable operation has all of the following:

- ownership by a numeric UID; the initiating session is used only for initial
  authorization and its raw credential is not persisted,
- a stable client-visible operation/Task ID, allocated before execution,
- one atomically written persistent operation record, and
- an external execution owner that can outlive the bridge.

The first pilot should be `control.app_update`. A systemd transient unit is one
possible execution owner for subprocess work; it is not a universal Task
framework. PackageKit transactions, drive jobs, or a daemon that already owns
state may be the better recovery owner for other routes. See the related API
contract and retry guidance in [`api-reliability-roadmap.md`](./api-reliability-roadmap.md).

## Goals and non-goals

Goals:

- Make the durable-vs-session-bound choice explicit per route.
- Let a restarted bridge recover operation state and report a final result.
- Preserve UID ownership and authorization across sessions and restarts.
- Use existing `godbus`/`dbusclient` paths where systemd is selected.
- Keep the first implementation small: one service-owned, one-record-per-operation
  atomic JSON store, using existing file-lock and fsync patterns and no
  database.

Non-goals:

- Moving every Task into a process supervisor or making every Task durable.
- Treating a unit name, a journal entry, or an in-memory bridge map as the
  source of truth.
- Adding a new go-systemd dependency or a temporary dependency spike.
- Making journald a typed progress/result protocol; journald is for logs.

## Current model and required correction

Today `TaskOwner` is assembled from the session and task execution is a
goroutine in the bridge. Its context is detached from the request stream but
still ends with the bridge process. All current Tasks explicitly use the
session lifetime and exact `SessionID` owner scope. That `SessionID` is an
internal authorization value, not public Task data, and is never serialized in
the owner model.

For a durable route, `TaskOwner` must not mean “the session that happens to be
connected now.” The durable owner is the authenticated numeric UID. The raw
creating `SessionID` must not be retained in the durable record because it is a
session-cookie credential; later sessions for the same UID may read or cancel
the operation according to the route policy, while a different UID may not.
Do not infer ownership from a transient unit name, and do not make a thin
in-bridge map authoritative: either can be lost or forged independently of the
operation record.

## Durable operation record

Allocate a stable operation/Task ID before handing work to an external owner.
The service-owned record is the recovery and authorization source of truth. A
minimal JSON record contains:

```text
id, route, uid, created_at, updated_at,
state (queued|launching|running|completed|failed|canceled|unknown),
route-defined safe request fingerprint/idempotency key, executor kind and handle,
started_at, finished_at, exit/error summary, and result reference
```

Write exactly one record per operation through the existing atomic writer,
which fsyncs the temporary file and parent directory, while holding the
repository's existing lock. Persist `launching` with the deterministic executor
name before starting work, then persist `running` after observing acceptance.
If the bridge dies between those writes, recovery queries that exact executor
name and adopts or fails the recorded operation; it never launches a second
executor blindly. A missing or malformed record is an explicit recovery error,
not permission to guess from unit names. The record contains no raw request,
credential, environment secret, or command output.

The store is intentionally file-based and bounded for the pilot. Active records
are never pruned. Retain terminal records for 30 days and at most the newest 200
per UID, pruning only terminal records under the same lock. Introduce a database
only when query volume or multi-process ownership demonstrates that need.

## External execution owners

Choose an executor per operation class:

| Route/class | Durable? | Recovery owner | Notes |
| --- | --- | --- | --- |
| `control.app_update` (pilot) | Explicit opt-in | systemd transient unit wrapping the existing subprocess | Persist ID/UID/handle first; observe exit and report result. |
| PackageKit transaction | Candidate, if PackageKit exposes stable recovery | PackageKit transaction/job state | Query PackageKit after bridge restart; do not duplicate its state in a unit. |
| Drive or daemon-owned job | Route-dependent | Existing daemon/job API | Reattach by its typed job ID. |
| Docker/compose SDK work | Usually session-bound initially | None (bridge Task) | A worker subcommand may be considered only after the pilot. |
| Interactive reads or cancellable UI work | No | Bridge/session | Keep current semantics. |

Classification must record whether work is idempotent or resumable, what
cancellation means, and which component can authoritatively report completion.

## systemd transient units (one executor)

For subprocess-shaped work, call
`org.freedesktop.systemd1.Manager.StartTransientUnit` through the existing
[`dbusclient`](../backend/bridge/internal/dbusclient/interfaces.go) and
`godbus` dependency. Do not add go-systemd for a spike. The unit handle is
metadata in the operation record, never the operation identity.

Persist the operation record (including UID, command policy, and deterministic
unit name) in `launching` before starting the unit. Set `User=`/`Group=` from the
route's execution policy, never automatically from the record owner. The app
update pilot remains a privileged root operation; its initiating UID owns the
record but cannot redefine the command or execution identity. Apply a bounded
runtime limit, resource limits appropriate to the route, and a deterministic
but non-authoritative unit name containing the operation ID. `CollectMode` and
cleanup prevent leaks, but cleanup must not erase the operation record.

The unit's exit status is a coarse terminal signal. Standard output/error may
go to journald for diagnostics, but journald is not typed state. Typed progress
and results must use a small, authenticated operation channel or a service-owned
result file tied to the operation ID; define its schema with the pilot.

## Lifecycle, cancellation, and restart

- **Create:** authorize UID, validate the Web-Crypto-generated canonical UUID
  and route-defined safe request fingerprint, write `queued`, then write
  `launching` with the deterministic executor name.
  Start once with collision-safe semantics and record `running` only after
  acceptance. None of these separate filesystem/D-Bus actions is described as
  one atomic transaction.
- **Observe:** poll/query the executor and update the record. Reconnects read
  the record, not a bridge map. A missing executor is `unknown` until the
  route-specific recovery policy resolves it.
- **Complete:** persist `completed`/`failed`/`canceled` with a concise error or
  result reference. Repeated completion observations must be idempotent.
- **Cancel:** authorize the owning UID, mark cancellation intent, then invoke
  the executor's typed cancellation operation (`StopUnit`, PackageKit cancel,
  or daemon API). Do not claim cancellation until observed or explicitly mark
  `unknown`.
- **Bridge/session loss:** session-bound Tasks stop as today. Durable work
  continues under its external owner; a new bridge reads records and resumes
  observation. A websocket disconnect never deletes durable state.
- **Host restart:** systemd/PackageKit/daemon semantics decide whether work
  resumes, fails, or is unknown. On startup reconcile records with the selected
  owner and expose that state to clients; do not promise resume unless the
  executor guarantees it.

Security requirements are part of the route contract: validate UID ownership on
every read/cancel, restrict command and environment construction, prevent
cross-user result paths, and ensure privileged operations use the existing
capability/authorization checks. Runtime limits and bounded polling protect
against wedged workers; they do not replace cancellation.

## Phasing

1. **Classify routes.** Inventory Task routes and document durability,
   idempotency/resume, cancellation, and recovery owner. Keep the default
   session-bound.
2. **Build the store.** Implement the bounded service-owned JSON record with the
   existing lock and fsync-capable atomic-write conventions, stable IDs, UID
   checks, terminal retention, and tests for torn writes, duplicate completion,
   malformed records, and crash recovery from `launching`.
3. **Pilot `control.app_update`.** Persist first; launch the existing update
   subprocess as a systemd transient unit through `dbusclient`; capture typed
   terminal state and retain journald only as logs. Do not rely on `--wait` or
   `--pipe` for durability.
4. **Reconcile and exercise failure.** Restart the bridge, disconnect clients,
   cancel from a second session of the same UID, and test host-restart/unknown
   outcomes. Verify authorization and idempotency.
5. **Add executors selectively.** Prefer PackageKit or daemon job APIs where
   they already own recovery. Consider a validated bridge worker subcommand for
   an in-process route only after evidence from the pilot.

## Open questions and safeguards

- What exact app-update command/result schema is stable enough to persist?
- Which routes may be started as a system unit, and which require a privileged
  helper or existing daemon?
- Do pilot measurements support the initial 30-day and 200-terminal-record
  retention limits per UID without deleting useful audit evidence too early?
- What state should clients see for `unknown`, and how does the API expose a
  retry or reconciliation action?

The implementation should answer these in the pilot and update the API
roadmap, while keeping the durable surface area deliberately narrow.
