# API Reliability, Recovery, and Notifications Roadmap

## Status

This is the canonical dependency-ordered roadmap for the remaining API
reliability work. It starts from the completed transport migration and connects
connection-loss behavior, Task lifetime, durable execution, and the planned
notification system. This roadmap is temporary: keep it until the remaining
phases are complete, then fold the verified lifecycle into the focused
implementation documents.

Focused implementation and design details remain in their own documents:

- [API Contract](../api-contract.md) describes implemented API behavior.
- [Handler Patterns](../bridge_handler_patterns.md) defines handler code style.
- [Durable Operations Architecture](../durable-operations-architecture.md)
  defines durable Task execution and recovery mechanics.
- [Notifications](./notifications.md) defines the notification product and
  storage contract.
- [Scheduled Execution](./scheduled-execution.md) defines the systemd timer,
  run-summary, and journald ownership boundaries.

The [TODO index](./README.md) links here instead of duplicating these
plans.

## Principles

Make the smallest change that establishes a real invariant:

- **Simple solutions:** no compatibility runtime, parallel API, or universal
  recovery framework.
- **Idiomatic code:** typed Go handlers, caller contexts, standard-library JSON,
  TanStack Query for frontend server state, and existing D-Bus/systemd
  boundaries.
- **Performance:** measure before replacing standard components or adding code
  generation; keep buffers, replay, retention, and persistent records bounded.
- **Safety:** no blind mutation retry, explicit owner scope, validated privilege
  boundaries, atomic persistence, and honest unknown-outcome reporting.
- **Recoverable events:** Every system-wide live event must have an
  authoritative snapshot or persistent record. Events invalidate or announce
  state; they do not become the state.
- **Simple handlers:** transport, scheduling, persistence, and presentation do
  not leak into ordinary domain handlers.
- **Native ownership:** systemd owns schedules and process execution, journald
  owns logs, and LinuxIO persists only the user-facing state those services do
  not represent.

## Current Baseline

The Query/Job migration is complete. The generated route contract currently
contains 202 Calls, 19 Tasks, and 9 Channel routes implemented with
`ModeDuplex`. React Query fetching is independent of Task lifecycle.

The transport cleanup is complete:

- every Task route uses the typed Task-runner form;
- the compiler checks each runner's terminal result against its route contract
  before the bridge registry erases the type;
- ordinary handlers have no progress/data emitter surface;
- `TaskService` owns and registers the reserved `tasks.*` Calls and Channels;
- the general router has no `tasks.*` primitive-dispatch branch; and
- server-producing Channels cancel blocked writers when their client closes,
  aborts, or disconnects.

The strict-input and explicit Call-policy phase is also complete:

- normal routes and the reserved `tasks.*` service share one strict
  standard-library request decoder;
- safe retry is declared in Go with `apischema.RetrySafe()` and emitted as a
  generated sparse policy map;
- Call transport failures distinguish pre-send unavailability from an unknown
  post-send outcome; and
- feature decisions use structured error codes rather than message text.

Uniform generic Task progress is also complete: snapshots and lifecycle events
carry the common percentage, phase, and message fields, generated route
endpoints retain typed detail, and generic recovery no longer guesses among
route-specific field names.

The remaining reliability constraints are:

- server alerts are not implemented and the navbar history is local toast
  history only; and
- future scheduled scripts need native timing, stable run identity, and log
  correlation without making the bridge a scheduler or a log database.

These slices build on the completed transport, Task ownership, and
durable-operation boundaries rather than adding a parallel runtime.

## Target Model

~~~text
Browser
├── TanStack Query + Call descriptors
├── payload-specific Channels
├── Task client
└── alert cache + presentation
        │
Bridge
├── Call registry             bounded request/result
├── Channel registry          live stream with explicit resume semantics
├── Task service
│   ├── session Task          in memory, exact-session owner
│   └── durable Task          persistent record + external execution owner
├── alert client              Calls + watch Channel over the alert daemon socket
└── scheduled-run projection  definitions + bounded run summaries

Alert daemon (root, standalone, socket-activated)
├── alert lifecycle           dedup, resolution, per-user seen state
└── routing and delivery      matchers and targets

Native Linux owners
├── systemd service/timer     schedule and process state
└── journald                  execution logs

Server-side persistent state
├── alert daemon SQLite       alerts, seen state, delivery attempts
└── scheduled-run directory   one bounded summary file per systemd invocation
~~~

| Primitive | Execution owner | Loss behavior |
|-----------|-----------------|---------------|
| Call | Request context | Retry only explicitly safe reads; an unacknowledged mutation can have an unknown outcome. |
| Channel | Live connection | Resume only through the payload's cursor, offset, sequence, or external session identity. |
| Session Task | Bridge process | A watcher may detach; ending the owning session or bridge cancels the Task. |
| Durable Task | External executor plus persistent operation record | A later bridge discovers the same operation by stable ID. |
| Schedule | Native systemd timer and service | systemd owns activation and process state even when no bridge is connected. |
| Run summary | One bounded file per systemd invocation in a root-owned run directory | A reconnecting client can query bounded execution history and then open the corresponding journal. |
| Alert | Standalone root alert daemon owning its own SQLite store | A reconnecting client receives authoritative lifecycle and per-user seen state before live changes. |
| Delivery | Alert router plus configured target | Matchers select targets; retry and outcome state do not alter the source alert or run. |

Task is a service composed from bounded control operations and Channels. It is
not a third wire protocol.

## Phase 1: Finish the Existing Transport Cleanup (complete)

Completed on 2026-08-10:

1. Converted `filebrowser.resource_patch` and `virt.create` to the single Task
   runner form.
2. Bound Task runner results as their declared Go result type and erased types
   only at the registry boundary.
3. Removed `HandleEvents`, `taskEmitter`, and progress/data capabilities from
   ordinary handlers.
4. Registered `tasks.get`, `tasks.list`, `tasks.cancel`, `tasks.watch`,
   `tasks.data`, and `tasks.events` through `TaskService` rather than a
   `tasks.*` primitive-dispatch branch.
5. Documented the Channel ownership, close, cancellation, concurrency,
   backpressure, terminal-frame, and payload-specific resume contract; added a
   regression test for cancellation unblocking a backpressured writer.

Do not begin a standalone Mode/Kind rewrite. After the exceptions above are
gone, remove duplicated registration state only when the replacement is a clear
net deletion.

### Phase 1 exit criteria

- [x] One typed Task runner shape remains.
- [x] Ordinary handlers never receive an emitter.
- [x] `TaskService` owns Task state and primitive implementations; the router
  has no namespace-specific primitive dispatch.
- [x] Call, Channel, and Task bindings reject invalid combinations.
- [x] Existing ownership, admission, cancellation, replay, and transfer tests
  still pass.

## Phase 2: Strict Input and Explicit Call Policy (complete)

Completed on 2026-08-10:

1. Routed normal contracts and reserved Task-service requests through one
   strict `encoding/json/v2` implementation.
2. Replaced frontend command-name inference with Go-owned `RetrySafe` metadata.
3. Split connection loss into `connection_unavailable` before stream-open send
   and `outcome_unknown` after it.
4. Kept mutation and Task starts at no retry and prevented TanStack Query from
   multiplying transport attempts.
5. Replaced the remaining feature decision based on error message text with a
   structured missing-path status.

### Request decoding

JSON envelopes and Go structs remain the source of truth. All route requests
use one standard-library path that:

- uses `encoding/json/v2.Unmarshal`;
- calls `RejectUnknownMembers(true)`;
- matches object members case-sensitively;
- rejects duplicate names, invalid UTF-8, and trailing input;
- accepts exactly one JSON value; and
- reports scalar type failures as `*json.SemanticError`.

Required-field meaning remains domain validation. Absence cannot safely be
inferred from a zero value or `omitempty`; use pointer presence only where the
wire contract must distinguish absent from zero.

Generated request decoders are not planned work. Reconsider them only if a
profile shows request decoding is material or a concrete contract requires
generated presence tracking. The shared v2 decoder provides the strict envelope
policy without generating a decoder for every route.

### Call policy

Call declarations opt into retry with `apischema.RetrySafe()`. The generator
emits one compact Call-policy map, and both generated query descriptors and
imperative `call()` consult it. The default is no retry. Only an explicitly
safe read may retry either connection-loss outcome, within its original
deadline.

The transport records whether failure happened before or after opening the
request stream and must distinguish:

| Outcome | Meaning |
|---------|---------|
| `connection_unavailable` | No request stream opened; the operation was not sent. |
| backend result or error | The server confirmed the outcome. |
| `outcome_unknown` | The stream opened, then closed before a result; a mutation may have been accepted. |

Do not add `expected_loss` or native-handoff metadata until it drives a concrete
generated-client behavior and has route-specific recovery tests.

### Self-severing Calls

A self-severing mutation is issued once. Feature code confirms its own
convergence condition after reconnect:

- reboot compares the boot identity;
- a network mutation reads the resulting interface configuration;
- logout confirms authentication state;
- power-off reports an acknowledged request or an unknown outcome, not success
  merely because the host became unreachable.

Structured error codes cross every layer unchanged. Frontend code must not
branch on error message text.

### Phase 2 exit criteria

- [x] Unknown or case-mismatched fields, duplicate names, invalid UTF-8, and
  trailing JSON values are rejected before a handler runs, including for
  reserved `tasks.*` routes.
- [x] Call retry safety is explicit Go-owned metadata; absence means no retry.
- [x] Pre-send connection failure and post-send unknown outcome have different
  structured codes.
- [x] Only explicitly safe Calls retry connection loss, at most once and within
  the original deadline; Task starts do not retry.
- [x] Backend and transport error codes reach feature decisions unchanged.

## Phase 3: Task Lifetime, Identity, and Session Activity (complete)

Every Task route declares one lifetime:

~~~text
session
durable
~~~

The lifetime selects an owner policy:

- **Session Task:** exact `SessionID`; cancel on logout, session deletion, idle or
  absolute expiry, and bridge failure.
- **Durable Task:** authenticated UID/user; visible from a later authenticated
  session for the same account.

Do not use one fuzzy owner-match function for both policies. The webserver's
session-deletion callback already closes the owned bridge; bridge shutdown must
also call `CancelTasksForSession` before process exit so cancellation is
explicit and testable rather than an incidental effect of process death.

Do not change every session Task merely to gain idempotency. For a durable
route, the generated Task start accepts a client-generated UUID operation ID,
generated with Web Crypto, and that ID is also the durable Task identity. The
backend validates its canonical form and claims `(owner scope, operation ID,
route, route-defined safe request fingerprint)`. Repeating the same start
returns the existing operation; reusing the ID for different input is a
conflict. The ID is not an authorization secret. The guarantee is at most one
accepted start for that identity, not exactly-once external side effects.
Session Tasks keep their existing server IDs until a separate migration
demonstrates value.

### Initial Task classification

| Route group | Initial lifetime / recovery owner |
|-------------|-----------------------------------|
| File operations, `docker.compose`, `virt.create`, `filebrowser.index`, `packages.update`, `system.install_capability`, and `storage.run_smart_test` (16 routes) | Session Task. Each is canceled with its owning bridge/session. |
| `control.app_update` | Session Task. Installer output is piped to the initiating Task; restart occurs only after terminal status/result publication. |
| `docker.update_container` | Durable Task owned by authenticated numeric UID, with a persistent record and deterministic systemd transient unit for recovery. |

### Session activity

Background work must not keep an idle session alive accidentally. Passive query
polling, Task progress, server-sent Channel data, and an open WebSocket do not
count as user activity. The relay's `FlagActivity` bit is the explicit,
throttled signal: the frontend emits it for document interaction and selected
interactive stream data, while the server strips the bit and refreshes the
session. Passive frames and WebSocket ping/pong only validate that the session
is still alive. Durable work continues independently of session activity;
session Tasks end with the session.

### Phase 3 exit criteria

- [x] Every Task declaration has an explicit lifetime: 17 session routes and
  one durable route, `docker.update_container`.
- [x] Owner plumbing distinguishes exact `SessionID` from durable numeric UID;
  Docker update uses UID scope and the other 17 routes use session scope.
- [x] Bridge shutdown calls `CancelTasksForSession` before closing its transport.
- [x] Session IDs remain internal authorization values and are redacted from
  public Task snapshots and serialized owner models.
- [x] Passive WebSocket traffic does not refresh activity; only the explicit
  `FlagActivity` bit does.

## Phase 4: Durable Task Foundation (complete)

The [Durable Operations Architecture](../durable-operations-architecture.md)
is implemented for the current durable route, `docker.update_container`.

A durable Task requires both:

1. an external execution owner; and
2. a persistent operation record.

Use a bounded service-owned directory with one JSON record per operation,
protected by the repository's existing file-lock patterns and written through
the atomic utility that fsyncs the temporary file and parent directory. Active
records are never pruned; the store retains terminal records for 30 days and at
most the newest 200 per UID. Do not add a database dependency before record
volume or query behavior requires one.

The record contains the stable operation ID, route/type, sanitized owner UID,
external executor identity, state and timestamps, bounded progress, result or
structured error, route-defined safe request fingerprint, and log cursor. It
contains no raw request, credential, or secret. A `launching` state and
deterministic executor name close the crash window around `StartTransientUnit`:
recovery queries that exact name, validates it against the record, and adopts or
fails the operation instead of starting a second unit. The name is a locator;
the record remains authoritative. A recovered `queued` record is safe to resume
because it proves the executor was never called; after `launching`, recovery
may only adopt the recorded executor and must never start a replacement.

The Docker update uses the identity-checked transient-unit boundary while
retaining Docker-specific admission, target validation, and typed result
semantics. Journald owns its logs and the operation record owns typed state and
result. App update is deliberately outside this durable model: a bridge-owned
pipe is its live feedback mechanism, not a durability mechanism.

### Required fault matrix

These are different events with deliberately different outcomes:

| Event | Durable-route behavior |
|-------|------------------------|
| App-update page reload | No recovery is claimed. The new provider starts unlocked and removes the legacy `linuxio.active-app-update` marker. |
| Docker-update page or transport loss | Detaching the UI does not cancel the systemd worker. Same-UID bridge recovery reconstructs an active Task from the persisted record and resumes observation without launching a second unit. |
| WebSocket reconnect | Repeating the same UID/UUID/fingerprint claim for Docker update returns the existing Task/record; a different request fingerprint conflicts. |
| Bridge death and later reauthentication | Docker's systemd unit and record outlive the bridge, and a replacement bridge reattaches it. App update does not claim recovery; its unit is runtime-bounded and receives `--defer-restart`. |
| Host restart | The JSON record survives but the transient unit does not promise reboot survival. A previously running record with no typed result becomes terminal `unknown`; recovery never starts a replacement automatically. |

Do not claim survival for an event unless the external executor and persistent
record both survive it. Cancellation becomes terminal only after the external
owner confirms it stopped.

### Phase 4 exit criteria

- [x] `docker.update_container` is durable; the other 17 Tasks, including app
  update, are session-bound.
- [x] Starts use a Web-Crypto UUID as the Task and operation identity, with
  idempotent same-fingerprint claims and conflict on reuse for other input.
- [x] The bounded UID-scoped store atomically persists one sanitized record per
  operation and never prunes active records.
- [x] The Docker durable executor runs in a deterministic, identity-checked
  systemd transient unit started through D-Bus; journald remains diagnostic-only
  and its typed result file determines completion.
- [x] Same-identity claims, persisted-result recovery without relaunch, bridge
  recovery, conservative host-restart `unknown`, cancellation before mutation,
  and stop-confirmed cancellation have focused automated coverage across the
  durable route.
- [x] Durable status and recovery require authentication and hide records owned
  by a different UID as missing. App update separately exposes a versioned,
  UID-scoped `/api/update-status` runtime projection.
- [x] Recovered operations are reattached to the router Task registry, remain
  cancelable from a replacement same-UID session, and count toward singleton
  admission; the locked store also enforces the singleton across processes.

## Phase 5: Uniform Generic Task Progress (complete)

Completed on 2026-08-10.

Adopt one envelope for snapshot-retained Task progress while preserving generated
route-specific detail:

~~~text
percentage?   generic completion from 0 through 100
phase?        stable machine-readable phase
message?      concise presentation-ready status
detail?       route-declared typed payload
~~~

`WithTaskProgress[T]` declares `T` as the detail contract and the generator
exposes `TaskProgress<T>` to the route-specific frontend endpoint. Global Task
snapshots and lifecycle events expose `TaskProgress<unknown>`, allowing generic
consumers to read the common fields without unsafe route heuristics. Retained
progress uses that envelope on both `tasks.events` and `tasks.watch`: the former
provides owner-wide discovery and recovery, while the latter follows one Task
with its typed detail and terminal result. Explicit transient output remains
local to a direct watcher and does not enter the owner-wide lifecycle stream.

Transient stream data remains distinct. `tasks.data` and progress-shaped
Channel control frames do not become durable Task progress merely because they
use the progress opcode.

### Phase 5 exit criteria

- [x] Every Task route reports the common progress envelope.
- [x] Generated route endpoints retain their concrete detail type.
- [x] Global Task recovery reads only the common percentage, phase, and message
  fields for generic presentation.
- [x] Route-owned UIs read their typed `detail`; no compatibility field-name
  guessing remains.
- [x] Progress remains bounded and coalesced on the owner-wide event stream.

## Phase 5.5: Mutation Feedback Consistency Gate (complete)

This completed gate repaired the post-transport frontend pending-state gaps
before persistent alert implementation. Uniform Task progress does not make
bounded Call mutations visible automatically: `isPending` must reach the action,
entity, dialog, or global surface that owns the user's expectation.

This is a frontend reliability gate, not notification infrastructure. It does
not require a database, another Task type, a global mutation registry, or a
Suspense boundary.

Completed on 2026-08-12. Docker action surfaces (audit Batch 1) and the VM,
WireGuard, NFS/CIFS, and account entity-scoped surfaces (audit Batch 2) were
completed on 2026-08-11. File Browser lifecycle ownership (Batch 3), the
remaining Network, TuneD, hostname, and health-card controls (Batch 4), and the
accessibility/regression pass (Batch 5) were completed on 2026-08-12.

### Phase 5.5 exit criteria

- [x] Every directly activated mutation has a visible working state until the
  mutation and its mapped invalidations settle.
- [x] Row and card feedback identifies the affected entity and action instead
  of disabling an unrelated page or table without explanation.
- [x] Closing a menu or dialog never leaves active work with no visible owner.
- [x] Tasks with meaningful progress use the common Task envelope; bounded
  Calls remain bounded Calls.
- [x] Optimistic and self-severing actions retain their intentional specialized
  feedback.
- [x] Repaired ownership boundaries have pending-state UI tests and pass
  `make check-frontend`.

## Phase 6: Persistent Alert Lifecycle

Implement [Notifications](./notifications.md) after uniform Task progress and
the mutation-feedback consistency gate.
The domain is an alert lifecycle rather than a persisted toast list:

- stable alert identity and source-defined deduplication key;
- severity, category, title, message, first/last occurrence, and count;
- active/resolved state distinct from seen/unseen and dismissed/restored state;
- authenticated Calls for list and lifecycle mutations;
- one snapshot-first watch Channel feeding the TanStack Query cache; and
- Sonner as presentation only.

A standalone root alert daemon, shaped like `linuxio-indexer` with its own
service, socket, and SQLite file, owns this store; bridges are clients and never
open the file. Seen state, deduplication, concurrent sessions, resolution, and
delivery attempts are relational application semantics; replaying journald or
rewriting per-user JSON snapshots is no longer the simpler reliable solution.
Run history is not in this database. Do not put raw logs, every toast, or
progress frames in it.

### Phase 6 exit criteria

- [ ] Reconnect receives an authoritative bounded alert snapshot before live
  changes.
- [ ] Seen, dismissal, restoration, recurrence, and source resolution have
  explicit tested transitions.
- [ ] Stable source keys make repeated creation idempotent.
- [ ] Alert persistence failure never changes the originating Task or systemd
  run outcome.
- [ ] The server-backed navbar replaces local toast-history persistence.

## Phase 7: Scheduled Execution and Run History

Implement [Scheduled Execution](./scheduled-execution.md). LinuxIO manages
declarative definitions; native systemd `.timer` and `.service` units own
calendar activation, overlap, process lifetime, timeout, and exit state.
journald owns stdout and stderr.

LinuxIO persists only a bounded run summary: stable run and definition IDs,
the exact unit and invocation identity, scheduled/started/finished timestamps,
terminal state, exit status, and a concise result or error. Summaries are one
file per invocation in a root-owned run directory, written by a short-lived
worker binary from the generated unit's `ExecStartPre=` and `ExecStopPost=`,
the `linuxio-docker-update` precedent; there is no scheduler daemon and no
database. The journal is opened by unit plus invocation identity; raw output
is never copied into the run directory. Unit operations use the existing D-Bus
boundary rather than shelling out to `systemctl`.

### Phase 7 exit criteria

- [ ] Creating, editing, enabling, disabling, and deleting a schedule converges
  to deterministic systemd service/timer definitions.
- [ ] The API reports next/last activation and current unit state from systemd.
- [ ] Each execution has a stable bounded summary correlated to one systemd
  invocation and its journal.
- [ ] Restarting or disconnecting the bridge does not stop scheduling or lose
  the authoritative execution owner.
- [ ] Overlap, timeout, cancellation, retention, privilege, and script-path
  policies are explicit and tested.

## Phase 8: Alert Sources, Routing, and Delivery

Integrate meaningful sources only after the alert core and scheduled runs are
stable. A failed run, SMART condition, update condition, or service failure may
raise or update an alert; ordinary log lines do not. Source recovery resolves
the same stable alert instead of creating an unrelated success notification.

Delivery follows an event/matcher/target model:

- events contain severity, source, type, timestamp, and allow-listed metadata;
- matchers select by severity and metadata, with calendar rules only when
  needed; and
- targets initially cover email or webhook-style delivery, with secrets stored
  separately and delivery attempts bounded and auditable.

Frequency, grouping, and retry belong to delivery policy. They do not redefine
whether the alert itself is active, seen, or dismissed.

## Phase 9: Converge and Extend from Evidence

After the progress, alert, and scheduled-run vertical slices:

- reassess the current durable route: keep its external systemd executor and
  stable results, but replace replacement-bridge Task reconstruction if the
  generic run status model gives the same honest post-login recovery with less
  machinery;
- recover PackageKit work through PackageKit when supported;
- recover SMART tests from drive state;
- extend durable execution to another route only with explicit idempotency and
  convergence;
- add a bridge worker subcommand only for an in-process Task with a demonstrated
  durability requirement;
- extend persistence or replay only when bounded behavior is measured to be
  insufficient.

## Performance and Safety Gates

Do not claim a performance gain from architectural simplification alone.
Capture a baseline and compare:

- Call p50/p95 latency and allocations;
- request-decoding CPU before considering generation;
- Task start acknowledgement latency;
- progress replay and Channel memory under a slow consumer;
- alert insert/list latency, database size, and delivery retry depth;
- scheduled-run reconciliation and journal-open latency;
- reconnect and convergence duration.

Safety tests cover invalid envelopes, privilege checks, exact owner scope,
pre-send and post-send loss, Task start deduplication, cancellation, bounded
retention, path/link sanitization, and bridge/host restart behavior.

Generated or cross-boundary changes run `make generate` followed by `make test`.
Browser reconnect, Channel, notification, and convergence behavior additionally
runs `make test-frontend-browser`.

## External Design Comparison

LinuxIO should adopt focused lessons, not another product's full protocol:

| Product | Useful documented pattern | Decision for LinuxIO |
|---------|---------------------------|----------------------|
| [TrueNAS Jobs](https://api.truenas.com/v25.10/jobs.html) and [Alerts](https://www.truenas.com/docs/scale/toptoolbar/alerts/) | Queryable jobs use common progress plus job-specific detail; alerts separately own severity, recurrence, dismissal, restoration, and delivery settings. | Use a common typed progress envelope and keep run state separate from alert lifecycle. |
| [Cockpit](https://cockpit-project.org/guide/latest/feature-systemd.html) | A per-user bridge delegates service state and execution to D-Bus/systemd and displays journald rather than mirroring logs. | Let native Linux services own schedules, processes, and logs. |
| [Proxmox](https://pbs.proxmox.com/docs/notifications.html) | Notification events carry severity and metadata; matchers route them to independent targets. | Add routing after the alert lifecycle, with matching and delivery as separate concerns. |
| [Unraid](https://docs.unraid.net/unraid-os/using-unraid-to/run-docker-containers/managing-and-customizing-containers/) | User Scripts exposes approachable cron schedules and integrates with a separate notification surface. | Preserve a simple scheduling UI, but use systemd timers rather than adding cron ownership to LinuxIO. |
| [CasaOS](https://github.com/IceWhaleTech/CasaOS-Gateway) | Local services behind a route-registering gateway. | Service isolation is useful, but LinuxIO does not need another gateway layer. |
| [ZimaOS](https://github.com/IceWhaleTech/ZimaOS) | OS-owned OTA and offline update delivery. | Treat it as deployment inspiration; its public repository does not establish a reusable Task recovery protocol. |

## Documentation Ownership

- `api-contract.md`: the implemented contract and clearly labelled local
  follow-up; cross-cutting future work stays in this roadmap.
- `bridge_handler_patterns.md`: current handler style.
- `durable-operations-architecture.md`: durable execution and recovery
  mechanics.
- `notifications.md`: alert lifecycle, metadata storage, API, Channel, routing,
  and frontend behavior.
- `scheduled-execution.md`: schedule, systemd unit, run-summary, and journald
  ownership.
- this roadmap: phase ordering and cross-cutting decisions.
- `TODO/README.md`: one short entry linking this roadmap.

## Definition of Done

This roadmap is complete when:

- the completed typed Task runner and normal `TaskService` registration
  boundary remains source-guarded;
- strict request decoding and explicit retry policy replace permissive decoding
  and name heuristics;
- connection loss reports confirmed failure, confirmed result, or unknown
  outcome honestly;
- every Task has an explicit lifetime and owner scope;
- session deletion cancels session Tasks in production;
- the durable route survives every event it claims to survive;
- all Tasks expose uniform generic progress while route UIs retain typed detail;
- alerts have one persistent server owner and one frontend cache owner;
- scheduled scripts remain systemd-owned and their bounded run summaries link
  to, rather than duplicate, journald logs;
- no feature relies on a live event stream as its only recovery source;
- measurements and fault tests meet the agreed gates;
- the focused documents and TODO index match the implemented state.
