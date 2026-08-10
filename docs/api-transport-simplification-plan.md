# API Transport Simplification Plan

## Status

Complete as of 2026-08-10. The Query-to-Call, Job-to-Task, log-stream-to-Channel,
and final Task-runner migrations are complete, with no dual runtime or
compatibility facade. The current route inventory is 203 Call, 18 Task, and 9
Channel routes implemented with `ModeDuplex`.
`ModeQuery`, `apischema.Query`, `createQueryEndpoint`, the legacy React Query
endpoint module, and all `.queryOptions`/`.useAction`/`.useFetcher`/`.useCache`
consumers are removed.

All 18 Task routes now use one typed runner/result shape. `TaskService` owns and
registers the reserved `tasks.*` Calls and Channels, and the ordinary-handler
emitter surface and Router prefix-dispatch branch are removed. `Mode`, `Kind`,
the generated Task endpoint factory, and route metadata remain because they
still enforce real registration, code-generation, and lifecycle boundaries;
removing them would add replacement code rather than simplify the system.
Connection-loss, session/durable Task, strict validation, decoder, and
notification work is tracked in the canonical
[API reliability roadmap](./api-reliability-roadmap.md), not duplicated here.

This plan replaces the original Query/Job/Runner/Duplex API framework with two
transport primitives, Call and Channel, while modeling Task as an application
service built on top of them.

LinuxIO keeps its existing WebSocket, yamux, relay framing, route names, and wire
envelopes. Each route is cut over vertically after equivalent behavior is
verified; source control is the rollback path, not a parallel compatibility
runtime.

This plan assumes that the browser-to-bridge API is internal to LinuxIO. If
LinuxIO later needs a supported external API, that should be designed as a
separate surface rather than forcing request/response, terminal, streaming, and
durable-task semantics into one contract.

## Goals

- Reduce the API to a small set of honest execution concepts.
- Preserve compile-time request, result, progress, and route safety.
- Separate transport behavior from React Query caching and mutation behavior.
- Keep long-lived byte and message streams on one multiplexed Channel model.
- Reserve Task for work that genuinely needs detachment, tracking, recovery, or
  durability.
- Reduce generated runtime values to typed Call descriptors and the separate
  Task lifecycle surface.
- Improve or preserve latency, throughput, cancellation, reconnect behavior,
  authorization, and owner isolation.
- Migrate one complete route slice at a time, replacing the old path instead of
  running parallel compatibility paths.

## Non-goals

The initial migration will not:

- Rewrite WebSocket, yamux, or relay framing.
- Replace JSON request and response encoding.
- Make every Task survive bridge termination.
- Expose arbitrary command execution or D-Bus access to the browser.
- Generate custom backend JSON request decoders.
- Redesign every background-Task presentation component at once.
- Introduce a separate SSE transport.

## Target architecture

At the transport level LinuxIO needs two primitives:

~~~text
Call(request) -> result

Channel(openRequest) <-> data/control -> close
~~~

Task is a service implemented with Calls and Channels:

~~~text
Task.Start(...)  -> Call returning a task ID
Task.Get/List    -> Call
Task.Cancel(...) -> Call
Task.Watch(id)   -> Channel
Task.Data(id)    -> Channel
~~~

Frontend cache behavior is independent of the backend transport:

~~~text
Call + useQuery    = cached read
Call + useMutation = bounded action
~~~

There is no backend Query-versus-Action distinction. There is no universal
Events interface exposing data, progress, result, error, and close to every
handler.

### Backend handler shapes

The intended handler shapes are:

~~~text
Call:
    func(context.Context, Request) (Result, error)

Task:
    func(context.Context, *Task, Request) (Result, error)

Channel:
    func(context.Context, net.Conn, Request) error
~~~

Application handlers remain typed. Type erasure, JSON decoding, and route
dispatch are confined to the transport boundary.

### Channel lifecycle

All long-lived transports share one lifecycle:

~~~text
open -> ready -> data/control -> done/close
~~~

Payload implementations define which messages and directions are valid:

- Logs and event subscriptions are server-producing Channels.
- Terminals and consoles are bidirectional Channels.
- Uploads and downloads are binary Channels.
- Task progress and task data attachment are Channels.

Reconnect behavior remains payload-specific rather than hidden inside one
universal retry mechanism:

- Terminals reattach to a persistent terminal session.
- Logs reopen from a cursor.
- Downloads resume from an offset.
- Uploads either resume explicitly or abort.
- Task watches replay from a sequence number or current snapshot.

## Phase 0: Classification and baseline (transport classification complete)

Create an architecture decision record and classify every existing route across
independent dimensions:

- Call, Channel, or Task
- Request-owned, bridge-owned, or externally durable
- JSON or binary payload
- Client-to-server, server-to-client, or bidirectional
- Retry-safe, idempotent, or unknown-on-disconnect
- Privileged or unprivileged
- Cancellation behavior
- Reconnect and resume requirements
- Query invalidations and other application-side completion behavior

All former Job and Runner routes must be explicitly classified. Log-follow
routes and transfer routes must not remain Tasks merely because they once used
the Job infrastructure.

The migration freeze covered the following surfaces:

- Mode and Kind additions
- Handler-form Task routes
- HandleEvents
- Raw Events emitters
- New generated endpoint capabilities

Record a baseline before changing behavior:

- Route counts by current and proposed shape
- Handwritten framework LOC
- Generated runtime LOC
- Generated type LOC
- Frontend production bundle size
- Call frame count and round trips
- Call p50, p95, and p99 latency
- Channel open-to-first-byte latency and sustained throughput
- Task submit-to-ID and first-progress latency
- Go allocations, CPU, and heap profiles
- Stream memory usage under backpressure

Use deterministic in-process fixtures for transport and handler measurements.
Host integration measurements involving Docker, systemd, D-Bus, or filesystem
I/O must be reported separately so host variance is not mistaken for framework
overhead.

### Phase 0 exit criteria

- [x] Every route is classified as Call, Channel, or Task.
- [x] Every former Job route was reclassified for the transport migration.
- [ ] A reproducible baseline is captured for each future performance-sensitive
  change; the migration itself did not establish a universal benchmark result.
- [x] The intended Call, Channel, and Task semantics are documented.
- [x] No new code expands the removed Query/Job abstractions.

## Phase 1: Establish the final primitives (complete)

Introduce each final primitive with its first complete route migration. Do not
add an unused facade or a forwarding layer over the legacy framework.

### Backend

- Add direct Call registration and dispatch.
- Make Call handlers return one result directly.
- Write one result frame and close without a stream emitter.
- Add direction-neutral Channel registration and dispatch.
- Introduce a focused `TaskService` for lifecycle state and Task-control
  registration.
- Preserve privilege checks, request decoding, owner checks, cancellation,
  deadlines, and error codes.
- Change a route's registration only when its handler and frontend consumers
  move to the final primitive in the same slice.
- Do not forward Query, Job, or Duplex through Call, Channel, or Task. Legacy
  registrations remain only for routes that have not migrated and shrink with
  every slice.
- Preserve route names and the existing stream-open JSON envelope.

The initial implementation should use the existing relay opcodes and yamux
streams. A new wire protocol is not part of this phase.

### Generated contract

Generate type-only operation maps alongside the existing generated client:

~~~ts
interface Calls {
  "system.get_cpu_info": {
    request: void;
    result: CPUInfoResponse;
  };
}

interface Tasks {
  "docker.compose": {
    request: DockerComposeRequest;
    progress: ComposeTaskMessage;
    result: ComposeTaskResult;
  };
}

interface Channels {
  "terminal.open": {
    request: TerminalOpenRequest;
  };
}
~~~

The generated contract exposes helpers such as:

- RouteRequest
- RouteResult
- RouteProgress
- CallRoute
- TaskRoute
- ChannelRoute

It must not generate React hooks or mix Task lifecycle into Call descriptors.

### Frontend

The final frontend API is split by responsibility:

- calls.ts
- call-react-query.ts
- channels.ts
- tasks.ts
- task-react-query.ts

Do not add a second frontend facade. The generated `linuxio` namespace now
contains plain Call descriptors/factories and separate Task endpoints; it no
longer exposes the legacy Query endpoint capabilities.

Call descriptors and Task endpoints use separate runtime factories, so React
Query fetching does not own Task completion, watching, or progress-stream
behavior. `task-react-query.ts` may reuse the shared mutation lifecycle in
`call-react-query.ts`; the dependency never points from Call fetching to Task.

### Phase 1 exit criteria

- Existing features behave unchanged.
- New and legacy dispatch produce equivalent frames and errors.
- Wrong primitive use fails during TypeScript compilation.
- No route names or wire envelopes change.
- Authorization, owner isolation, cancellation, and deadlines remain covered.
- No migrated route has parallel legacy and final runtime paths.

## Phase 2: Vertical-slice pilots (complete)

Migrate a small set of representative routes before broad conversion:

| Route | Shape | Status | What it proves |
|---|---|---|---|
| system.get_cpu_info | Call | Migrated | Cached request/response read as a direct TanStack descriptor |
| docker.start_container | Call | Migrated | Side-effectful bounded action through `useCallMutation` |
| logs.general.follow | Channel | Migrated | Server-producing stream without a backing Task |
| terminal.open | Channel | Final primitive already in use | Bidirectional interactive stream |
| docker.compose | Task | Migrated | Progress, result, cancellation, watching, and recovery |

Each pilot is a direct cutover. Establish parity with focused tests, then remove
the old backend registration and frontend call path in the same slice; do not
ship dual dispatch or fallback behavior.

Upload and download paths subsequently retained Task identity plus their binary
data Channels. Their tests cover bounded transfer memory, cancellation, and
resume offsets.

For each pilot:

- Preserve the request and response wire shape.
- Preserve query keys and invalidation behavior.
- Preserve toasts and feature-specific UI behavior.
- Compare legacy and candidate frame traces.
- Compare old and new latency, allocations, round trips, and code size.
- Cut over only after the candidate passes its acceptance gate; source control
  is the rollback path.

### Reusable performance gates

For future transport changes, preserve the same round-trip count and bounded
memory under slow consumers. Compare latency and throughput with identical
fixtures, a documented workload and environment, enough samples to show normal
variance, and a retained baseline artifact. Do not use an arbitrary percentage
or “materially smaller” claim without that evidence. Ordering, cancellation,
and reconnect behavior remain correctness gates rather than performance
tradeoffs.

## Phase 3: Migrate bounded Calls (complete)

Convert every bounded Query route to Call, one handler family at a time.

Completed: all 203 bounded routes now use direct Call registration. Reads use
plain TanStack descriptors or request-bound descriptor factories; bounded
writes use `useCallMutation`. No route retains Query registration or a legacy
endpoint factory.

Backend Query and Action are no longer different transport concepts. Whether a
Call is cached or treated as a mutation is decided at the frontend callsite.

### Frontend migration

Replace legacy endpoint usage incrementally:

~~~text
endpoint.queryOptions(...)
    -> useQuery(callDescriptor)

endpoint.useAction(...)
    -> useCallMutation(callDescriptor, config)

endpoint.useFetcher()
    -> queryClient.fetchQuery(callDescriptor) or call("route", request)

endpoint.useCache()
    -> queryClient operations using callDescriptor.queryKey

endpoint(...)
    -> call("route", request)
~~~

No-request reads are descriptors themselves, so the intended cached-read syntax
is:

~~~ts
useSuspenseQuery(linuxio.system.get_cpu_info);
~~~

Routes with request data are descriptor factories:

~~~ts
useQuery(linuxio.example.read_item({ id }));
~~~

TanStack options are composed directly rather than hidden behind another
wrapper:

~~~ts
useSuspenseQuery({
  ...linuxio.system.get_cpu_info,
  refetchInterval: 5000,
});
~~~

Standardize request arguments:

~~~ts
call("system.get_cpu_info");
call("docker.start_container", { containerId });
~~~

No-request Calls take no request argument. Every Call with request data accepts
the complete typed request object. Remove scalar-versus-object RequestShape
logic.

Query keys remain deterministic and include the route and request:

~~~text
["linuxio", handler, command, request?]
~~~

Keep operation-query-invalidations as application metadata. Invalidation is not
part of the wire contract and should not be generated into transport code.

### Phase 3 exit criteria

- [x] Every bounded route uses Call.
- [x] No code uses the legacy Query registration or endpoint API.
- [x] Query and mutation behavior remains covered.
- [x] Route count and request/result contract parity are maintained.
- [x] Domain-by-domain migrations pass the combined repository checks.

## Phase 4: Migrate Channels (complete)

Move all long-lived streaming behavior to Channel:

- Terminal and VM/container consoles
- General, service, and Docker log following
- Task progress and event subscriptions
- Upload and download streams
- Task data attachment

Reclassify the former Job-backed log-follow routes as direct server-producing
Channels unless they have a separately justified detached lifetime.

The first Channel slice moves `logs.general.follow`, `logs.service.follow`, and
`docker.logs.follow` directly to Channel registration and typed `openChannel()`
callers. Their former Job registrations and route-specific forwarding functions are
removed in the same cutover.

Keep StreamMultiplexer and the existing framing helpers. They own real transport
complexity: framing, reconnect, authentication close handling, buffering,
scrollback, cancellation, flow control, resize, and binary data.

Route-specific payload code remains appropriate when it implements real Channel
semantics, such as terminal resize controls or resumable transfer offsets. It
must not forward to a Task watch or data stream.

### Phase 4 exit criteria

- [x] No log-follow route requires a Task merely to stream data.
- [x] All Channel directions and control messages are explicit.
- [x] Resume and reconnect behavior is tested per payload.
- [x] No separate SSE abstraction exists.
- [x] Binary transfer memory remains bounded under backpressure.

## Phase 5: Simplify Tasks (complete)

Expose one Task service:

~~~text
Start
Get
List
Cancel
Watch
Data
~~~

Separate its internal responsibilities:

- Task state and registry
- Scheduling and admission policy
- Owner and privilege enforcement
- Cancellation and timeouts
- Progress snapshots and bounded replay
- Watch transport
- Optional binary data attachment
- Execution substrate

The Job-to-Task runtime and public namespace cutover preserved execution,
ownership, recovery, queueing, replay, and transfer-data semantics while
deleting the old Job names and paths. The final cleanup then:

- converted `filebrowser.resource_patch` and `virt.create` to typed Task
  runners;
- bound every Task runner's terminal result to its declared route result;
- declared generated progress types on Task routes;
- removed handler-form Tasks, `HandleEvents`, the universal Events interface,
  and `taskEmitter`; and
- made `TaskService` register `tasks.get/list/cancel/watch/data/events` as
  ordinary Call or Channel routes, removing the Router's prefix switch.

The former Job routes were reclassified as follows:

- Bounded work becomes Call.
- Session streaming becomes Channel.
- Genuine detached or recoverable work remains Task.
- Process-survivable work becomes a durable Task only under the
  [API reliability roadmap](./api-reliability-roadmap.md).

Frontend task helpers should remain thin. Feature-specific progress rendering,
toasts, and labels belong in feature descriptors or components rather than one
large universal Task configuration object.

### Phase 5 exit criteria

- [x] There is one typed Task runner/result shape.
- [x] Task lifecycle controls are registered through `TaskService` as normal
  Call and Channel routes.
- [x] The Router has no special `tasks.*` route switch.
- [x] Obsolete emitter surfaces were deleted; `Mode` and `Kind` remain only
  where they enforce a real boundary.
- [x] Ownership, queueing, rate limits, cancellation, timeout, result replay,
  and progress replay remain covered.
- [x] In-memory Tasks are not described as bridge-survivable.

## Reliability follow-up

Connection-loss semantics, session-bound and durable Tasks, strict request
validation, decoder strategy, and server-side notifications are maintained in
the canonical [API reliability roadmap](./api-reliability-roadmap.md). This
transport plan records only the primitive cutover and its transport-specific
cleanup; it does not duplicate those designs or claim them implemented.

## Phase 6: Final deletion review (complete)

Repository searches confirmed that the Query endpoint, Job API, handler-form
Task, universal Events interface, `HandleEvents`, `taskEmitter`, and obsolete
registration adapters have no production consumers and are removed.

The following are final surfaces rather than compatibility layers:

- `createTaskEndpoint` provides Task start/wait/watch/cancel semantics that a
  Call descriptor does not have;
- `TaskEndpoint` types keep those capabilities separate from Calls;
- runtime route modes protect Task endpoint construction, and generated route
  metadata supports compile-time and source-guard checks;
- `Mode` and `Kind` validate backend registration and drive code generation;
- `RequestShape` normalizes generated Task inputs; and
- payload-specific Channel helpers retain cursor, offset, terminal, resize, or
  Task-data behavior.

Keep generated domain models and type maps, `StreamMultiplexer`, relay framing,
and feature-specific stream presentation. Revisit a retained surface only when
a measured change produces a clear net deletion without weakening type or
lifecycle safety.

Only consider a new wire-protocol version after cleanup, and only if profiling
shows the current relay framing is a bottleneck.

## Generator strategy

Keep the current small Go-owned generator rather than adding a second schema
tool:

- Continue generating TypeScript domain models.
- Generate flat type-only Call, Task, and Channel maps.
- Generate plain Call descriptor/factory invocations and thin Task endpoint
  factory invocations; do not generate React hooks, cache policy, feature
  behavior, or a compatibility facade.
- Generate route-mode metadata while it enforces Task construction and source
  guards.
- Preserve deterministic formatting and generated-file checks.

After the vertical slice, compare the trimmed generator with a standard
JSON-Schema-based tool. Adopt a replacement only if it reduces total maintained
code and dependencies while correctly preserving Go JSON tags, optional fields,
nested models, route request types, route result types, and Task progress types.

Generated backend request decoders are not planned unless profiling
demonstrates that request decoding is a meaningful bottleneck. Strict request
validation is a separate reliability decision tracked in the
[API reliability roadmap](./api-reliability-roadmap.md).

## Validation strategy

### Calls

Measure:

- p50, p95, and p99 end-to-end latency
- Handler-only latency
- JSON bytes
- Frame count and round trips
- Allocations per operation
- CPU and heap profiles

Compare legacy and candidate paths using identical encoded requests and results.

### Channels

Measure:

- Open-to-first-byte latency
- Sustained throughput
- CPU and allocations
- Maximum buffered bytes under a slow reader
- Ordering and loss
- Close and error latency
- Terminal resize behavior
- Binary and text handling

Test transfers at representative small, medium, and large sizes.

### Tasks

Measure:

- Submit-to-ID latency
- Queue wait and start latency
- First-progress latency
- Completion latency
- Attach, reconnect, and replay latency
- Cancellation at queued, running, and completing stages
- Duplicate or lost progress events
- Registry memory under queued and active load

Transport correctness still covers authentication, authorization, owner
isolation, cancellation, deadlines, error identity, frame ordering, close
semantics, flow control, and backpressure. Connection-loss behavior, strict
request validation, and session/durable Task fault matrices belong to the
[API reliability roadmap](./api-reliability-roadmap.md).

## Required repository checks

For generated or cross-boundary changes:

1. Run make generate.
2. Run make test.

For browser navigation, reconnect, terminal, stream, transfer, or chunk-loading
behavior, additionally run make test-frontend-browser. Run
make setup-frontend-browser first only if the browser dependency is absent.

Do not run Make verification concurrently with implementation or another Make
invocation. Inspect the complete worktree after generation and verification.

## Rollout and rollback

- Keep existing route names and wire framing throughout the migration.
- Introduce a final primitive with a complete consumer migration, not as an
  unused compatibility layer.
- Delete a legacy frontend surface as soon as its final consumer moves.
- Migrate one domain or vertical slice per change, deleting that slice's old
  registration and call path at cutover.
- Keep each phase independently revertible.
- Demonstrate contract parity and runtime behavior before cutover; rollback is
  source control, not a second production path.

## Reduction

The completed slices removed the former React Query endpoint factory, generated
Query factories, Query route mode, the Job API, Job-backed log-stream adapters,
handler-form Tasks, and the universal emitter. The final deletion review kept
only the typed Task and mode/kind surfaces that still own real behavior or
enforce a contract.

Generated models and most of `StreamMultiplexer` remain because they implement
real contract and transport behavior. Use the final commit diff to report code
reduction; do not preserve a speculative line-count target.

The more important reduction is conceptual:

~~~text
Before migration:
    Query / Action / Task handler / Task runner / Duplex /
    generated endpoint capabilities

Target:
    Call / Channel / Task
~~~

## Definition of done

The migration is complete when:

- Every operation is classified as Call, Channel, or Task.
- Adding a bounded operation requires one typed contract, one typed handler, and
  a normal Call callsite.
- Query versus mutation is purely frontend cache policy.
- Adding a Channel requires an open contract and only the payload-specific
  behavior it actually uses.
- Adding a Task requires one runner; lifetime and recovery follow the
  [API reliability roadmap](./api-reliability-roadmap.md).
- The compiler rejects using a Call route as a Task or Channel.
- Generated runtime values are limited to Call descriptors and thin Task
  endpoints; no generated React hooks, cache policy, or compatibility facade
  remains.
- No universal Events interface remains.
- No handler-form Task remains.
- `TaskService` owns Task state, replay, attachments, and primitive
  implementations; the Router owns common dispatch and admission without a
  namespace-specific Task switch.
- Performance claims require a reproducible benchmark; the migration does not
  claim a gain from code reduction alone.
- Canonical documentation and ToDo entries match the implemented architecture.
