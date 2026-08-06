# API Transport Simplification Plan

## Status

Proposed.

This plan replaces the current Query/Job/Runner/Duplex API framework with two
transport primitives, Call and Channel, while modeling Task as an application
service built on top of them.

The migration is deliberately additive. LinuxIO will keep its existing
WebSocket, yamux, relay framing, route names, and wire envelopes while the new
API is introduced and measured. Legacy abstractions are removed only after all
callers have migrated and equivalent behavior has been verified.

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
- Remove generated runtime endpoint objects and route-mode checks.
- Improve or preserve latency, throughput, cancellation, reconnect behavior,
  authorization, and owner isolation.
- Migrate incrementally with route-level compatibility and rollback points.

## Non-goals

The initial migration will not:

- Rewrite WebSocket, yamux, or relay framing.
- Replace JSON request and response encoding.
- Make every Task survive bridge termination.
- Expose arbitrary command execution or D-Bus access to the browser.
- Generate custom backend JSON request decoders.
- Redesign every background-job presentation component at once.
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

## Phase 0: Classification and baseline

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

All current Job and Runner routes must be explicitly classified. Log-follow
routes and transfer routes should not remain Tasks merely because they currently
use job infrastructure.

Freeze new uses of the following during the migration:

- Mode and Kind additions
- Handler-form Job routes
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

- Every route is classified.
- Every current Job has an explicit lifetime and recovery requirement.
- Representative measurements are reproducible.
- The intended Call, Channel, and Task semantics are documented.
- No new code expands the legacy abstractions.

## Phase 1: Add the new foundation

Introduce the new primitives beside the existing API without migrating feature
callers.

### Backend

- Add direct Call registration and dispatch.
- Make Call handlers return one result directly.
- Write one result frame and close without a stream emitter.
- Add direction-neutral Channel registration and dispatch.
- Put task lifecycle behind a focused TaskService.
- Preserve privilege checks, request decoding, owner checks, cancellation,
  deadlines, and error codes.
- Keep the current Query and Duplex registration methods as temporary adapters
  to Call and Channel.
- Keep current Job runners working through the Task compatibility path.
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
    progress: ComposeJobMessage;
    result: ComposeJobResult;
  };
}

interface Channels {
  "terminal.open": {
    request: TerminalOpenRequest;
  };
}
~~~

The generated contract should expose helpers such as:

- RouteRequest
- RouteResult
- RouteProgress
- CallRoute
- TaskRoute
- ChannelRoute

It must not construct endpoint objects or React hooks.

### Frontend

Add small handwritten modules:

- calls.ts
- call-react-query.ts
- channels.ts
- tasks.ts
- task-react-query.ts

Initially keep the existing generated client and endpoint factory unchanged as a
compatibility surface.

### Phase 1 exit criteria

- Existing features behave unchanged.
- New and legacy dispatch produce equivalent frames and errors.
- Wrong primitive use fails during TypeScript compilation.
- No route names or wire envelopes change.
- Authorization, owner isolation, cancellation, and deadlines remain covered.

## Phase 2: Vertical-slice pilots

Migrate a small set of representative routes before broad conversion:

| Route | Shape | What it proves |
|---|---|---|
| system.get_cpu_info | Call | Cached request/response read |
| docker.start_container | Call | Side-effectful bounded action |
| logs.general.follow | Channel | Server-producing stream without a backing Job |
| terminal.open | Channel | Bidirectional interactive stream |
| docker.compose | Task | Progress, result, cancellation, attachment, and recovery |

Add an upload or download pilot after the first slices prove the basic design.
That pilot must cover binary throughput, slow readers, cancellation, and resume
offsets.

For each pilot:

- Preserve the request and response wire shape.
- Preserve query keys and invalidation behavior.
- Preserve toasts and feature-specific UI behavior.
- Compare legacy and candidate frame traces.
- Compare old and new latency, allocations, round trips, and code size.
- Keep the old path available until the candidate passes its acceptance gate.

### Phase 2 performance gates

- No additional network round trips.
- No more than a 10 percent Call p95 latency regression.
- No more than a 5 percent sustained Channel throughput regression.
- Bounded memory with slow consumers.
- No lost, duplicated, or reordered frames.
- No cancellation or reconnect regression.
- The new callsite and framework code are materially smaller.

If the pilot fails to reduce complexity or adds meaningful runtime overhead,
stop before broad migration and revise the design.

## Phase 3: Migrate bounded Calls

Convert every bounded Query route to Call, one handler family at a time.

Backend Query and Action are no longer different transport concepts. Whether a
Call is cached or treated as a mutation is decided at the frontend callsite.

### Frontend migration

Replace legacy endpoint usage incrementally:

~~~text
endpoint.queryOptions(...)
    -> callQueryOptions("route", request, options)

endpoint.useAction(...)
    -> useCallMutation("route", config)

endpoint.useFetcher()
    -> useCallFetcher("route")

endpoint.useCache()
    -> useCallCache("route")

endpoint(...)
    -> call("route", request)
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
[route, request]
~~~

Keep operation-query-invalidations as application metadata. Invalidation is not
part of the wire contract and should not be generated into transport code.

### Phase 3 exit criteria

- Every bounded route uses Call.
- No new code uses the legacy Query registration or endpoint API.
- Query and mutation behavior remains covered.
- Route count and request/result contract parity are maintained.
- Domain-by-domain migrations pass the combined repository checks.

## Phase 4: Migrate Channels

Move all long-lived streaming behavior to Channel:

- Terminal and VM/container consoles
- General, service, and Docker log following
- Task progress and event subscriptions
- Upload and download streams
- Task data attachment

Reclassify log-follow routes from Job-backed streams to direct server-producing
Channels unless they have a separately justified detached lifetime.

Keep StreamMultiplexer and the existing framing helpers. They own real transport
complexity: framing, reconnect, authentication close handling, buffering,
scrollback, cancellation, flow control, resize, and binary data.

Route-specific wrappers remain appropriate when they provide an actual payload
protocol, such as terminal resize controls or resumable transfer offsets.

### Phase 4 exit criteria

- No log-follow route requires a Task merely to stream data.
- All Channel directions and control messages are explicit.
- Resume and reconnect behavior is tested per payload.
- No separate SSE abstraction exists.
- Binary transfer memory remains bounded under backpressure.

## Phase 5: Simplify Tasks

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

Convert filebrowser.resource_patch and virt.create from emitter-form Jobs to the
single Task runner shape. After their migration, remove:

- Handler-form Jobs
- HandleEvents
- jobEmitter
- Progress and data capabilities from ordinary Calls
- Hard-coded job primitive dispatch from the general Router

Reclassify the existing Job routes:

- Bounded work becomes Call.
- Session streaming becomes Channel.
- Genuine detached or recoverable work remains Task.
- Process-survivable work becomes a durable Task only in Phase 6.

Frontend task helpers should remain thin. Feature-specific progress rendering,
toasts, and labels belong in feature descriptors or components rather than one
large universal Task configuration object.

### Phase 5 exit criteria

- There is one Task runner shape.
- Task lifecycle is isolated from general Call and Channel routing.
- The Router does not contain a special jobs route switch.
- Ownership, queueing, rate limits, cancellation, timeout, result replay, and
  progress replay remain covered.
- In-memory Tasks are not described as bridge-survivable.

## Phase 6: Connection loss and durable Tasks

Define loss semantics per primitive.

### Calls

- Retry read-only Calls only when they are explicitly safe.
- Never blindly retry an ambiguous mutating Call.
- Report an unknown outcome when the connection is lost after a mutation may
  have been accepted.
- For self-severing operations, confirm success through a route-specific
  convergence check.

### Tasks

- Give Task starts a client-generated operation or idempotency identifier.
- Allow reconnecting clients to discover whether an ambiguously acknowledged
  Task start succeeded.
- Preserve the same Task identity across reconnect and reattachment.

### Channels

- Resume only through the payload's explicit cursor, offset, session identity,
  or sequence contract.
- Do not apply a universal automatic retry policy.

For the classified durable subset, implement
[Bridge-Survivable Jobs via systemd Transient Units](./transient-units-plan.md).
Keep ordinary session-bound Tasks in memory.

### Phase 6 exit criteria

- Page reload, WebSocket reconnect, bridge death, and host restart are distinct
  tested conditions.
- Session-bound work has explicit termination behavior.
- Durable Tasks survive bridge restart, retain identity, and complete exactly
  once from the client's perspective.
- Nothing claims durability unless an external executor owns the work.

## Phase 7: Remove the legacy framework

Delete compatibility layers only after repository searches show no consumers.

Expected removals include:

- frontend/src/api/generated/client.ts
- frontend/src/api/generated/route-metadata.ts
- createEndpoint
- Runtime route-mode assertions
- QueryEndpoint, JobEndpoint, and related capability types
- Mode and Kind
- Universal Events
- streamEmitter and jobEmitter
- Old Query, Job, JobRunner, and Duplex registration adapters
- HandleEvents
- RequestShape
- Job-backed log-stream wrappers
- Legacy jobs route aliases, if wire compatibility is no longer required

Keep generated domain models, the flat type maps, StreamMultiplexer, relay
framing, and feature-specific stream presentation.

Update:

- [API Contract](./api-contract.md)
- [Server Yamux Protocol](./server-yamux-protocol.md)
- [Bridge-Survivable Jobs](./transient-units-plan.md)
- ToDo
- Source guards and architecture tests

Only consider a new wire-protocol version after cleanup, and only if profiling
shows the current relay framing is a bottleneck.

## Generator strategy

Initially trim the current generator rather than replacing it:

- Continue generating TypeScript domain models.
- Generate flat type-only Call, Task, and Channel maps.
- Stop generating endpoint objects, hooks, cache behavior, or route-mode
  metadata.
- Preserve deterministic formatting and generated-file checks.

After the vertical slice, compare the trimmed generator with a standard
JSON-Schema-based tool. Adopt a replacement only if it reduces total maintained
code and dependencies while correctly preserving Go JSON tags, optional fields,
nested models, route request types, route result types, and Task progress types.

Generated backend request decoders remain paused until profiling demonstrates
that request decoding is a meaningful bottleneck. Strict request validation and
runtime performance are separate decisions.

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

### Correctness and fault matrix

Cover:

- Valid and invalid request envelopes
- Missing, unknown, duplicate, and incorrectly typed fields
- Authentication and privilege failures
- Owner isolation
- Context cancellation and deadlines
- Error identity and status preservation
- Channel ordering, close, flow control, and backpressure
- WebSocket loss during Call, Channel, and Task operations
- Authentication close behavior
- Task queueing, rate limiting, cancellation, result replay, and progress replay
- Bridge restart for both session-bound and externally durable Tasks

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
- Introduce new primitives before migrating callers.
- Keep the legacy frontend facade for unmigrated domains.
- Migrate one domain or vertical slice per change.
- Preserve old registration adapters until repository searches show zero
  consumers.
- Keep each phase independently revertible.
- Delete compatibility code only after contract parity and runtime behavior have
  been demonstrated.

## Expected reduction

The predictable deletions are:

- Approximately 884 lines of generated client and route metadata
- Most of the current 763-line React Query endpoint factory
- Several hundred backend lines involving Mode, Kind, emitters, bindings, and
  special job dispatch
- Job-backed log-stream adapters
- Additional background-job glue after Task consolidation

Generated model definitions and most of StreamMultiplexer should remain. A
realistic initial target is approximately 1,500 to 2,500 lines of
framework/generated-runtime deletion, with potentially more after Task
presentation and recovery are consolidated.

The more important reduction is conceptual:

~~~text
Current:
    Query / Action / Job handler / Job runner / Duplex / SSE-like events /
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
- Adding a Task requires one runner and explicit lifetime/recovery semantics.
- The compiler rejects using a Call route as a Task or Channel.
- No generated endpoint objects or runtime route-mode checks remain.
- No universal Events interface remains.
- No handler-form Job remains.
- The general Router does not own Task registry, scheduling, or replay details.
- Connection-loss behavior is explicit and tested.
- Durable Tasks have an external execution owner.
- Performance is no worse than the agreed gates.
- Canonical documentation and ToDo entries match the implemented architecture.
