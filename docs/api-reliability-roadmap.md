# API Reliability, Recovery, and Notifications Roadmap

## Status

This is the canonical dependency-ordered roadmap for the remaining API
reliability work. It starts from the completed transport migration and connects
connection-loss behavior, Task lifetime, durable execution, and the planned
notification system.

Detailed contracts remain in their focused documents:

- [API Contract](./api-contract.md) describes implemented API behavior.
- [API Transport Simplification Plan](./api-transport-simplification-plan.md)
  records the completed Query/Job and Task-runner cleanup.
- [Handler Patterns](./bridge_handler_patterns.md) defines handler code style.
- [Durable Operations and Transient Units](./transient-units-plan.md) defines the durable Task
  execution pilot.
- [Notifications](./notifications.md) defines the notification product and
  storage contract.

The repository [`ToDo`](../ToDo) links here instead of duplicating these plans.

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
- **Simple handlers:** transport, scheduling, persistence, and presentation do
  not leak into ordinary domain handlers.

## Current Baseline

The Query/Job migration is complete. The generated route contract currently
contains 203 Calls, 18 Tasks, and 9 Channel routes implemented with
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

The remaining reliability constraint is that server notifications are not
implemented; the navbar history is local toast history only. Notifications
build on the completed transport, Task ownership, and durable-operation
boundaries rather than adding a parallel runtime.

## Target Model

~~~text
Browser
├── TanStack Query + Call descriptors
├── payload-specific Channels
└── Task client
        │
Bridge
├── Call registry             bounded request/result
├── Channel registry          live stream with explicit resume semantics
└── Task service
    ├── session Task          in memory, exact-session owner
    └── durable Task          persistent record + external execution owner

Server-side persistent state
└── Notification store        bounded, per-user records
~~~

| Primitive | Execution owner | Loss behavior |
|-----------|-----------------|---------------|
| Call | Request context | Retry only explicitly safe reads; an unacknowledged mutation can have an unknown outcome. |
| Channel | Live connection | Resume only through the payload's cursor, offset, sequence, or external session identity. |
| Session Task | Bridge process | A watcher may detach; ending the owning session or bridge cancels the Task. |
| Durable Task | External executor plus persistent operation record | A later bridge discovers the same operation by stable ID. |
| Notification | Persistent per-user store | A reconnecting client receives an authoritative snapshot before live changes. |

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

## Phase 3: Task Lifetime, Identity, and Session Activity

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
| File operations, `docker.compose`, `virt.create`, `filebrowser.index`, `packages.update`, `system.install_capability`, and `storage.run_smart_test` (17 routes) | Session Task. Each is canceled with its owning bridge/session. |
| `control.app_update` | Durable Task owned by authenticated numeric UID. A persistent record and deterministic systemd transient unit own recovery. |

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

- [x] Every Task declaration has an explicit lifetime. Phase 3 initially landed
  all routes as session-bound; Phase 4 promotes only `control.app_update`.
- [x] Owner plumbing distinguishes exact `SessionID` from durable numeric UID;
  `control.app_update` uses UID scope and the other 17 routes use session scope.
- [x] Bridge shutdown calls `CancelTasksForSession` before closing its transport.
- [x] Session IDs remain internal authorization values and are redacted from
  public Task snapshots and serialized owner models.
- [x] Passive WebSocket traffic does not refresh activity; only the explicit
  `FlagActivity` bit does.

## Phase 4: Durable Task Pilot

Implement the [transient-unit plan](./transient-units-plan.md) for
`control.app_update` only.

A durable Task requires both:

1. an external execution owner; and
2. a persistent operation record.

Use a bounded service-owned directory with one JSON record per operation,
protected by the repository's existing file-lock patterns and written through
the atomic utility that fsyncs the temporary file and parent directory. Active
records are never pruned; the pilot retains terminal records for 30 days and at
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

For app update, replace the current `systemd-run --wait --pipe` path with
`StartTransientUnit` through the existing D-Bus stack. The update remains an
explicitly privileged root operation; the initiating UID owns its record but is
not automatically the unit's execution user. journald owns logs, while the
operation record owns typed state and result. Bridge-owned pipes are not a
durability mechanism.

### Required fault matrix

These are different events with deliberately different outcomes:

| Event | Pilot behavior |
|-------|----------------|
| Page reload | The browser retains the canonical operation ID and target, detaches its watch without canceling, then converges through authenticated `/api/update-status`. |
| WebSocket reconnect | Repeating the same UID/UUID/fingerprint claim returns the existing Task/record; a different request fingerprint conflicts. |
| Bridge death and later reauthentication | The systemd unit and record outlive the bridge. A replacement bridge reattaches each active record as a real UID-owned Task, validates the exact unit identity, and resumes observation without launching a second unit; watch and cancellation work from the replacement session. |
| Host restart | The JSON record survives but the transient unit does not promise reboot survival. A previously running record with no typed result becomes terminal `unknown`; recovery never starts a replacement automatically. |

Do not claim survival for an event unless the external executor and persistent
record both survive it. Cancellation becomes terminal only after the external
owner confirms it stopped.

### Phase 4 exit criteria

- [x] `control.app_update` is the only durable Task; all other Tasks remain
  session-bound by default.
- [x] Starts use a Web-Crypto UUID as the Task and operation identity, with
  idempotent same-fingerprint claims and conflict on reuse for other input.
- [x] The bounded UID-scoped store atomically persists one sanitized record per
  operation and never prunes active records.
- [x] The updater runs in a deterministic, identity-checked systemd transient
  unit started through D-Bus; journald remains diagnostic-only and a typed
  result file determines completion.
- [x] Page reload, reconnect, bridge recovery, conservative host-restart
  `unknown`, and stop-confirmed cancellation have focused automated coverage.
- [x] `/api/update-status` requires authentication and hides records owned by a
  different UID as missing.
- [x] Recovered operations are reattached to the router Task registry, remain
  cancelable from a replacement same-UID session, and count toward singleton
  admission; the locked store also enforces the singleton across processes.

## Phase 5: Persistent Notifications

Implement [Notifications](./notifications.md) after Task ownership and terminal
state rules are stable.

The first version is deliberately small:

- one bounded per-user JSON snapshot store using atomic replacement and sidecar
  locking;
- Calls for list, read/unread changes, mark-all-read, and clear;
- one `notifications.watch` Channel that emits an authoritative snapshot plus
  revision on open and whenever that persisted revision changes;
- server-created terminal Task notifications deduplicated by stable operation
  ID;
- TanStack Query as the frontend server-state cache and Sonner as presentation
  only.

Do not persist every toast or progress frame. Do not create durable
notifications from recovered frontend Task events. Task completion remains
authoritative if notification persistence fails; durable Task recovery
reconciles a missing notification by stable operation ID. Remove local
toast-history persistence only when the server-backed navbar cuts over, so
there is one history owner.

Preferences, a full history page, and global disk/Docker/system producers are
later slices. Add them only after the per-user core and reconnect behavior are
proven.

## Phase 6: Extend from Evidence

After the app-update and notification vertical slices:

- recover PackageKit work through PackageKit when supported;
- recover SMART tests from drive state;
- promote Docker or VM work only with explicit idempotency and convergence;
- add a bridge worker subcommand only for an in-process Task with a demonstrated
  durability requirement;
- add cursor replay or a database only if bounded snapshot behavior is measured
  to be insufficient.

## Performance and Safety Gates

Do not claim a performance gain from architectural simplification alone.
Capture a baseline and compare:

- Call p50/p95 latency and allocations;
- request-decoding CPU before considering generation;
- Task start acknowledgement latency;
- progress replay and Channel memory under a slow consumer;
- notification insert/list latency and file size;
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
| [TrueNAS](https://api.truenas.com/v25.10/jobs.html) | Stable long-running operation IDs, queryable state/progress/result, cancellation, and optional live updates. | Persistent state is authoritative after reconnect; push is for freshness, not the only recovery path. |
| [Cockpit](https://cockpit-project.org/blog/protocol-for-web-access-to-system-apis.html) | A per-user bridge, multiplexed Channels, and delegation to D-Bus/systemd/process owners. | Keep live Channels simple and let native Linux services own external work. |
| [Unraid](https://docs.unraid.net/API/how-to-use-the-api/) | A typed query/mutation API with explicit authentication and errors. | Keep bounded Calls typed; GraphQL does not solve Task durability. |
| [CasaOS](https://github.com/IceWhaleTech/CasaOS-Gateway) | Local services behind a route-registering gateway. | Service isolation is useful, but LinuxIO does not need another gateway layer. |
| [ZimaOS](https://github.com/IceWhaleTech/ZimaOS) | OS-owned OTA and offline update delivery. | Treat it as deployment inspiration; its public repository does not establish a reusable Task recovery protocol. |

## Documentation Ownership

- `api-contract.md`: the implemented contract and clearly labelled local
  follow-up; cross-cutting future work stays in this roadmap.
- `api-transport-simplification-plan.md`: migration history and remaining
  transport deletion.
- `bridge_handler_patterns.md`: current handler style.
- `transient-units-plan.md`: durable execution and recovery mechanics.
- `notifications.md`: notification storage, API, Channel, and frontend behavior.
- this roadmap: phase ordering and cross-cutting decisions.
- `ToDo`: one short entry linking this roadmap.

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
- the app-update durable pilot survives every event it claims to survive;
- notifications have one persistent server owner and one frontend cache owner;
- no feature relies on a live event stream as its only recovery source;
- measurements and fault tests meet the agreed gates;
- the focused documents and `ToDo` match the implemented state.
