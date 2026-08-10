# API Contract

This is the canonical guide for LinuxIO's Go-owned API contract between the frontend and the bridge.

## Summary

LinuxIO exposes three deliberately different operation shapes:

- **Call** is bounded request/response work.
- **Channel** is a live stream with an explicit resume/reconnect contract.
- **Task** is tracked background work with identity, progress, and watcher
  recovery. Seventeen routes are session-bound; `control.app_update` opts into
  a persistent UID-owned record and external executor.

Task progress/data events are not persistent history. The current navbar keeps
toast history in the browser; the planned server notification store is separate
from Task snapshots.

- Go owns route names, modes, request types, and result types. Route declarations live with each handler family's registration in `backend/bridge/handlers/<domain>/handlers.go`.
- TypeScript API files under `frontend/src/api/generated` are generated. Do not edit them by hand.
- API requests use JSON stream-open envelopes: `{"route":"handler.command","request":{...}}`.
- The relay/mux framing is still binary for stream multiplexing, terminal bytes, and Task data.
- Handlers receive typed Go request structs, not string args.

## Runtime Flow

```text
frontend linuxio endpoint
    |
    v
JSON stream-open envelope
    |
    v
WebSocket/yamux byte relay
    |
    v
bridge parses route + request JSON
    |
    v
apischema route decoder
    |
    v
typed handler / runner / duplex function
```

For no-request routes, the frontend sends an empty request object:

```json
{"route":"system.get_cpu_info","request":{}}
```

For request routes:

```json
{"route":"docker.start_container","request":{"containerId":"abc"}}
```

## Contract Files

| File | Role |
|------|------|
| `backend/bridge/handlers/<domain>/handlers.go` | One `apischema.Bindings(...)` table per handler family. Each normal entry contains the route contract and typed handler binding together. The `tasks` family publishes the reserved Task-service contracts while `TaskService` supplies their runtime implementations. |
| `backend/bridge/handlers/register.go` | Single handler-family composition table. Runtime registration, codegen, and tests all read from this one list. Edit this only when adding a new handler family. |
| `backend/bridge/apischema/contracts.go` | Shared request structs and small shared responses. |
| `backend/bridge/apischema/models.go` | API response/domain models reflected into TypeScript. |
| `backend/bridge/apischema/schema.go` | Contract helpers, route policy, and typed registration adapters. |
| `backend/common/ipc/bridge/request_decoder.go` | Shared strict request decoder for normal contracts and the reserved Task service. |
| `backend/common/tools/linuxio-api-gen` | Generator for frontend client/types/route metadata. |
| `frontend/src/api/generated/client.ts` | Generated concrete `linuxio` object. Calls are TanStack descriptors or descriptor factories; Tasks use their separate lifecycle factory. |
| `frontend/src/api/generated/linuxio-types.ts` | Generated API models and schema types. |
| `frontend/src/api/generated/route-metadata.ts` | Generated route modes and sparse retry-safe Call policy. |

## Frontend API Files

| File | Role |
|------|------|
| `frontend/src/api/index.ts` | Public barrel. Feature code should import from `@/api`. |
| `frontend/src/api/calls.ts` | Framework-independent typed `call()` transport and consumer of the generated retry-safety policy. |
| `frontend/src/api/call-react-query.ts` | Final TanStack descriptor builders and the shared bounded-mutation lifecycle. It has no dependency on Task lifecycle or streams. |
| `frontend/src/api/task-react-query.ts` | Task-only mutation integration: `useTaskAction`, `useTaskStreamAction`, completion waiting, watching, and progress handling. It imports the shared mutation lifecycle; Call fetching never imports Task lifecycle. |
| `frontend/src/api/endpoint-types.ts` | Type-only aggregate that maps generated routes to Call descriptors or Task capabilities without merging their runtime factories. |
| `frontend/src/api/linuxio-core.ts` | Low-level JSON Call path, deadline ownership, bounded retry, and connection-outcome classification. API internals only. |
| `frontend/src/api/linuxio.ts` | Typed `openChannel()` transport, connection hooks, terminal/Task stream helpers, and the app-update Task data stream. |
| `frontend/src/api/StreamMultiplexer.ts` | WebSocket stream multiplexer, relay frame encoding, stream lifecycle, singleton connection management. |
| `frontend/src/api/stream-helpers.ts` | Helpers for binding stream callbacks, awaiting result frames, and writing byte chunks. |
| `frontend/src/api/tasks.ts` | Task snapshot guards, cancellation classification, and `waitForTaskCompletion()`. |
| `frontend/src/api/task-state.ts` | Shared terminal Task-state predicate. |
| `frontend/src/api/operation-query-invalidations.ts` | Default query invalidations shared by direct actions and Tasks. |
| `frontend/src/api/capabilities.ts` | Frontend capability manifest and state helpers. |

## Route Modes And Kinds

Every route has one mode:

| Mode | Use |
|------|-----|
| `bridgeipc.ModeCall` | Bounded request/response work. React Query caching versus mutation behavior is chosen only at the frontend callsite. |
| `bridgeipc.ModeTask` | Tracked work with Task identity, progress, watcher recovery, and an explicit session or durable lifetime. Only `control.app_update` currently opts into durable execution. |
| `bridgeipc.ModeDuplex` | Long-lived Channels, including server-producing logs and bidirectional terminals; resumption is explicit in each Channel protocol. |

Every route has one schema kind:

| Kind | Go binding |
|------|------------|
| `KindHandler` | `.Handle(func(context.Context, TRequest) (TResult, error))` or `.HandleVoid(func(context.Context, TRequest) error)`. |
| `KindTaskRunner` | `.Run(func(context.Context, *bridgeipc.Task, TRequest) (TResult, error), policy)`; the compiler checks the terminal result before the binding erases it at the bridge registry boundary. |
| `KindDuplex` | `func(context.Context, net.Conn, TRequest) error` |

Use `apischema.NoRequest` for no request payload and `apischema.NoResponse` for no result payload. They are API contract marker types owned by `apischema`.

Calls and Task runners are typed at their binding. Task progress uses
`task.ReportProgress()` with a route-declared `WithTaskProgress[T]`; Task data
uses the focused `tasks.data` attachment Channel. Ordinary handlers have no
universal emitter surface.

## Frontend Shape

```typescript
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";

import { call, linuxio, useCallMutation } from "@/api";

// A no-request Call is already a TanStack descriptor.
const { data: cpu } = useSuspenseQuery(linuxio.system.get_cpu_info);

// A request-bearing Call is a descriptor factory. TanStack options compose
// directly at the observer.
const { data: unit } = useQuery({
  ...linuxio.systemd.get_unit_info({ unitName: "ssh.service" }),
  refetchInterval: 2000,
});

// Imperative cache-backed reads use QueryClient with the same descriptor.
const size = await queryClient.fetchQuery(
  linuxio.filebrowser.dir_size({ path: "/srv/data" }),
);

// Bounded commands get mutation ergonomics without owning Task behavior.
const validateCompose = useCallMutation(linuxio.docker.validate_compose, {
  error: "Validation failed",
});
const result = await validateCompose.mutateAsync({ content });

const startContainer = useCallMutation(linuxio.docker.start_container, {
  invalidates: [linuxio.docker.list_containers.queryKey],
  success: "Container started",
  error: "Failed to start container",
  toast: { to: "/docker", label: "Open Docker" },
});
startContainer.mutate({ containerId });
```

For a Call with request data, the generated value is a descriptor factory:

```typescript
useQuery(linuxio.example.read_item({ id }));
```

Call options are ordinary TanStack options composed at the use site:

```typescript
useSuspenseQuery({
  ...linuxio.system.get_cpu_info,
  refetchInterval: 5000,
});
```

Imperative code uses the separate typed transport function rather than making
the descriptor double as a Promise API:

```typescript
await call("system.get_cpu_info");
await call("docker.start_container", { containerId });
```

Every generated Call exposes one of two shapes:

| Shape | Use |
|-------|-----|
| No-request descriptor (`linuxio.system.get_cpu_info`) | Pass directly to `useQuery`, `useSuspenseQuery`, route loaders, or QueryClient. |
| Request descriptor factory (`linuxio.systemd.get_unit_info(request)`) | Bind the complete generated request object, then use the returned descriptor in the same places. |

Task endpoints are generated through a separate factory. They expose the
framework-agnostic Promise call and query key plus:

| Member | Use |
|--------|-----|
| `endpoint.useTaskAction(config?)` | React Query mutation lifecycle for a Task route: waits for completion, unwraps the final result, and applies declarative invalidation/toasts. |
| `endpoint.useTaskStreamAction(config?)` | Task-route mutation lifecycle with live progress: starts the Task, watches its stream, and surfaces `onTaskStart`/`onOpen`/`onProgress`. The returned mutation also exposes `watch(task, variables)` for page-reload recovery. |

Feature code imports TanStack Query's read primitives directly. Calls
pass their descriptor directly (`useSuspenseQuery(linuxio.system.get_cpu_info)`)
or invoke their request descriptor factory. Bounded writes use
`useCallMutation`.
Progress work uses the separate Task module's
`useTaskAction`/`useTaskStreamAction` or the background-Tasks layer,
while imperative cache work uses QueryClient directly. Feature code does not
invent query keys/functions or use raw mutations. Guard tests
(`frontend/src/constants/apiLayering.test.ts`) enforce that boundary.

Route loaders use `loadRouteQueries` from `src/routes/-loader.ts` to wait for the request transport and pass typed Call descriptors to the shared browser QueryClient. It requires a live `isUpdateBlocked` getter from router context rather than the mux, rechecks it after readiness, and rejects without querying while an update is active. Loader failures propagate to TanStack Router; intent preloads are marked speculative so QueryCache suppresses their global error toast.

The same guard file also fences the byte/mux-level transport primitives (`encodeString`, `bindStreamHandlers`, `getStreamMux`, …): feature code opens typed Channels with `openChannel(route, request)` and uses the stream lifecycle hooks (`useLiveStream`/`useLogStream`/`useStreamResult`). Only a short, shrink-only allowlist may import lower-level primitives from `@/api`.

Call descriptors compose with normal React Query options, including `select` for transformed output data. `useCallMutation` and `useTaskAction` take an `ActionConfig` — `invalidates` (query keys, static or derived from result/variables), `success`/`error` (toast message strings or callbacks; the error string is only a fallback, the server error message wins), `warning` (an extractor like `(result) => result.warning`; a non-empty return fires a warning toast that replaces the string-form success toast — invalidation and callback-form `success` still run), `toast` (toast metadata for notification-history links), and `options` as a raw React Query options escape hatch.

### Choosing a member (decision table)

| Situation | Use |
|-----------|-----|
| Data rendered by this component | `useQuery(callDescriptor)`. For on-demand panels, compose `enabled` and other options at the callsite. |
| Data needed inside an effect, loader, or event handler, then handed to something else | `queryClient.fetchQuery(callDescriptor)` when cache sharing matters; `call(route, request)` for transport-only work. |
| Call invoked as a command (validate, generate download, resolve path) | `useCallMutation(endpoint, config)` for declarative pending/error/feedback behavior. |
| Single bounded mutation triggered by one user action | `useCallMutation(endpoint, config)`, fired with `mutate`. |
| Mutation whose live progress the UI renders, or that must survive page reload | `useTaskStreamAction` (+ `watch` with `useActiveTaskRecovery` for recovery). Don't pick it just to customize an error string. |
| Several mutations sequenced or looped in one flow (batch delete, multi-step save) | Create the actions **configless** with a comment, `await mutateAsync` per step/item inside `try/catch`, and aggregate the outcome into one toast. The catch owns flow control only — never re-toast an error a config already toasted. |
| Long-running transfer that must outlive the page and show in the navbar | The background-Tasks layer (`useBackgroundTaskActions`), not a component-local Task action. |
| Optimistic updates / seeding cache from a result | QueryClient `cancelQueries`/`setQueryData` with the descriptor's `queryKey`. |

Refreshes after mutations come from the invalidation manifest by default — do not add `refetch()` calls or `onSuccess` refresh props on top of it; if a list does not refresh, fix the manifest entry instead.

Query invalidation is manifest-driven: `frontend/src/api/operation-query-invalidations.ts` maps each Call or Task route to the Call caches it makes stale, using the same centralized query keys. It is the single source of truth for mutation lifecycles — `useCallMutation`, `useTaskAction`, and `useTaskStreamAction` use it as the default `invalidates` for locally awaited work, and the recovered-Tasks stream applies it to Tasks that finish with no local handler (page reload, another session). Call sites only pass `invalidates` to override the manifest (`[]` opts out; a function derives keys from result/variables). The few path-precise File Browser invalidations use QueryClient directly and are source-guarded.

Input is generated from the Go request contract:

| Go request shape | Frontend input | Wire request |
|------------------|----------------|--------------|
| Call + `apischema.NoRequest` | `linuxio.system.get_cpu_info` descriptor or `call("system.get_cpu_info")` | `{}` |
| Call + request struct | descriptor factory or `call(route, request)` with the complete object | request object |
| one required JSON field | `linuxio.filebrowser.dir_size({ path })` | `{ "path": path }` |
| multi-field or optional object | `linuxio.docker.system_prune(request)` | `request` |

Mutation actions use the full generated request object as their mutation variable:

```typescript
useCallMutation(linuxio.tasks.cancel).mutate({ taskId });
useCallMutation(linuxio.docker.start_container).mutate({ containerId });
```

## Request Decoding and Call Reliability

Every normal route and reserved `tasks.*` request uses the shared
`bridge.JSONRequestDecoder`. It uses `encoding/json/v2.Unmarshal` with
`RejectUnknownMembers(true)`. The v2 defaults require exact, case-sensitive
member matches and reject duplicate names, invalid UTF-8, and trailing data.
A missing or `null` request retains the existing empty-object behavior.
Required-field meaning remains handler/domain validation; use pointer fields
only when the wire contract must distinguish an absent value from its zero
value.

The HTTP login body and indexer configuration patch are separate request
boundaries, but apply the same strict v2 member and syntax rules directly.
Response encoding and upstream-service decoding retain the compatible
`encoding/json` API; in Go 1.27 that API uses the v2 engine with v1 semantics.

Retry safety is Go-owned route metadata. Add `apischema.RetrySafe()` only to a
Call that can be repeated after connection loss without a user-visible
mutation. Code generation emits the sparse policy consumed by both Call
descriptors and imperative `call()`. Absence means no retry, and Task starts are
always issued once. An explicitly safe Call may make one reconnect attempt;
both attempts share the original absolute deadline.

The Call transport exposes three outcome classes:

| Outcome | Contract |
|---------|----------|
| `connection_unavailable` | Readiness or stream-open send failed before the request SYN was enqueued. The operation was not sent. |
| backend result or error | The bridge confirmed the outcome. Numeric or string error codes are preserved. |
| `outcome_unknown` | The request SYN was enqueued, then the stream closed before a result. The operation may have been accepted. |

The relay protocol has no server-side stream-open acknowledgement, so a close
after the browser enqueues the SYN is conservatively `outcome_unknown`, even
when a later relay failure may have prevented bridge dispatch. Caller aborts
remain `AbortError`, request deadlines remain `timeout`, and neither is retried
or relabelled. Channel and Task-watch close behavior remains payload-specific;
their generic pre-result close code is still `connection_closed`.

Only a route marked `RetrySafe` retries either named connection-loss outcome.
Default/no-policy Calls, including mutations, never retry. The shared TanStack
Query policy also refuses a second Query-layer attempt for these two codes, so
it cannot multiply the transport decision. Frontend feature code branches on a
structured code, never on error message text.

## Backend Handler Shapes

Call route:

```go
var api = apischema.Bindings(
    apischema.Call[apischema.NoRequest, *apischema.CPUInfoResponse](
        "system.get_cpu_info",
        apischema.RetrySafe(),
    ).Handle(handleGetCPUInfo),
)
```

Codegen and route coverage read `Routes`, which is derived from the binding table. Runtime registration also reads the same binding table, so a normal route is declared once.

Task runner route:

```go
var dockerTaskRoutes = dockerTaskBindings(runtime.Runtime{}).Routes()

func dockerTaskBindings(rt runtime.Runtime) apischema.BindingSet {
    return apischema.Bindings(
        apischema.TaskRunner[apischema.DockerComposeRequest, ComposeTaskResult](
            "docker.compose",
            apischema.WithTaskProgress[ComposeTaskMessage](),
        ).Run(runDockerComposeTask, bridgeipc.TaskDefault),
    )
}

func RegisterTaskRoutes(router *bridgeipc.Router, rt runtime.Runtime) {
    dockerTaskBindings(rt).Register(router)
}
```

The route's `ComposeTaskResult` is both the generated terminal-result contract
and the runner's Go return type. `TaskSnapshot` is the immediate response to
starting any Task; it is not a route's terminal result type.

Duplex route:

```go
var Routes = routeBindings(runtime.Runtime{}).Routes()

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
    return apischema.Bindings(
        apischema.DuplexRoute[apischema.TerminalOpenRequest, apischema.NoResponse](
            "terminal.open",
            apischema.NoEndpoint(),
        ).Duplex(func(ctx context.Context, stream net.Conn, req apischema.TerminalOpenRequest) error {
            return HandleTerminalSession(ctx, rt, stream, req)
        }),
    )
}

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
    routeBindings(rt).Register(router)
}
```

## Tasks

Tasks are reserved for work that emits progress, continues independently of a
watching component, or needs recovery by identity. Bounded mutations complete
through a single direct `useCallMutation` request/response.

### Task lifetime, ownership, and activity

Seventeen Task routes declare `SessionTask()`. Their owner is the exact
authenticated `SessionID`; logout, expiry, session deletion, bridge failure,
or bridge shutdown cancels the in-memory Task. `control.app_update` alone
declares `DurableTask()` and is owned by the authenticated numeric UID, so a
later session for that UID can observe the same operation.

`TaskOwner.SessionID` is an internal authorization value and is never emitted
in public Task snapshots or serialized API models. Public owner fields are
limited to non-secret identity data.

WebSocket keepalive, passive relay frames, Task progress, and Channel traffic
only validate session expiry. The outer relay `FlagActivity` bit is the sole
activity signal: the frontend emits it on throttled document interaction and
selected interactive stream data, and the server strips it before forwarding
the payload and refreshes session activity.

Starting a generated Task endpoint returns a `TaskSnapshot` immediately. On the
frontend, `useTaskAction` awaits the terminal state via
`waitForTaskCompletion()`: a failed Task rejects with a `LinuxIOError` carrying
the Task's error message/code, and success resolves with the unwrapped typed
`TaskSnapshot.result`. A Task start is never retried after connection loss; Task
recovery begins only after the client has received its snapshot identity. If
the watch stream cannot be opened (the mux dropped between Task start and
watch), completion waiting falls back to the explicitly retry-safe `tasks.get`
Call; `useTaskStreamAction` instead fails fast because it promises live
progress, and the recovered-Tasks stream can pick the Task up.

The app-update exception receives a canonical Web-Crypto UUID before start.
That UUID is both its Task ID and persistent operation ID. The backend binds it
to the UID, route, and a safe fingerprint of the requested version; repeating
the same claim attaches to the existing operation while incompatible reuse is
a conflict. Closing the app-update stream only detaches observation. Explicit
abort invokes `tasks.cancel`, and cancellation is terminal only after systemd
confirms that the exact unit stopped. A replacement bridge reattaches active
records as Tasks before accepting requests, so a later session for the same UID
retains watch/cancel access and the recovered updater still occupies singleton
admission.

`GET /api/update-status?id=<uuid>` is session-authenticated and UID-scoped. It
reads the persistent operation record, reconciles a typed executor result when
available, and reports `running`, `ok`, `error`, or `unknown`. Missing and
different-UID records are indistinguishable. The endpoint is the browser's
convergence path across reloads and the LinuxIO service restart performed by
the updater.

Built-in Task routes:

| Route | Use |
|-------|-----|
| `tasks.get` | Fetch one owned Task snapshot. |
| `tasks.list` | List owned Tasks. |
| `tasks.cancel` | Cancel one owned Task. |
| `tasks.watch` | Progress/result stream. Closing detaches; aborting cancels. |
| `tasks.data` | Upload/download/archive data stream. |
| `tasks.events` | Lifecycle event stream. |

The `tasks.*` namespace is reserved by `bridgeipc`. The `tasks` handler family
provides the generated route catalog, and its registration callback asks the
router's `TaskService` to install the matching Call and Channel
implementations. The general router has no route-prefix dispatch branch.

## Streams

Channels are multiplexed over `/ws`. Use typed `openChannel()` or an existing
payload-specific helper instead of constructing envelopes directly.

| Function | Route | Use |
|----------|-------|-----|
| `openTerminalStream(cols, rows)` | `terminal.open` | Host shell. |
| `openContainerStream(containerId, shell, cols, rows)` | `container.open` | Container shell. |
| `openChannel("docker.logs.follow", request)` | `docker.logs.follow` | Direct server-producing container log Channel. |
| `openChannel("logs.service.follow", request)` | `logs.service.follow` | Direct server-producing unit log Channel. |
| `openChannel("logs.general.follow", request)` | `logs.general.follow` | Direct journal Channel with backlog progress and cursor resume. |
| `openAppUpdateStream(runId, version?)` | `control.app_update` | Task-backed app update output. |
| `openTaskWatchStream(taskId)` | `tasks.watch` | Task progress/result. |
| `openTaskDataStream(taskId, offset?)` | `tasks.data` | Binary Task data. |
| `openTaskEventsStream()` | `tasks.events` | Task lifecycle events. |

Terminal and container sessions are bidirectional Channels. The three log
routes are direct server-producing Channels and never create a Task. App update
uses a Task plus its binary data Channel because the operation survives the
watching component and deliberately severs the bridge during restart. Its
persistent UID-owned record and external systemd executor, rather than the
in-memory Task or stream, are authoritative across that bridge exit.

### Channel lifecycle and ownership

- The router owns route lookup, decoding, privilege checks, and the stream
  until it calls the Channel handler. The handler owns the `net.Conn` only for
  that call; the yamux/relay layer remains responsible for closing it.
- A Channel has at most one reader and one writer per direction. Code that needs
  multiple producers must serialize their frames before writing.
- Writes are synchronous and provide backpressure. Do not add an unbounded
  queue between a producer and the connection.
- Direct log Channels use `ReceiveOnlyChannelContext`; Task watch/events use
  focused monitors with the same write-deadline rule. Client close, abort, or
  disconnect therefore interrupts a blocked server write. Direct Channel
  cleanup restores deadlines without taking ownership of the connection.
- Task-watch close detaches without cancelling the Task; Task-watch abort
  cancels it. Other payloads document their own close-versus-abort rule.
- JSON Channels with a terminal outcome emit exactly one result or error frame
  followed by close. Indefinite event Channels end when either side closes.
- Reconnect is never transparent: logs use cursors, transfers use offsets, Task
  watches use Task identity and replay/current state, and terminals use their
  external session identity.

## Adding An Endpoint

For the common case where request/result structs already exist, adding a route touches one handler-family file:

1. `backend/bridge/handlers/<domain>/handlers.go` for one `apischema.Bindings(...)` entry and the typed handler adapter.

If the request or response type is new, also add the Go struct in `backend/bridge/apischema/contracts.go` or `backend/bridge/apischema/models.go`.
If the handler family is new, add one entry to `backend/bridge/handlers/register.go`.

The practical checklist:

1. Define or reuse exported Go request/response structs in `backend/bridge/apischema`.
2. Add one binding entry to `backend/bridge/handlers/<domain>/handlers.go`.
3. Implement the typed handler, runner, or duplex function in that handler package.
4. Ensure the family `Routes` is derived from the binding set.
5. Run `make generate`.
6. Use the generated endpoint from `@/api`.

Example:

```go
type PackageSearchRequest struct {
    Query string `json:"query"`
}

type PackageSearchResult struct {
    Items []string `json:"items"`
}
```

```go
var api = apischema.Bindings(
    apischema.Call[apischema.PackageSearchRequest, apischema.PackageSearchResult](
        "packages.search",
    ).Handle(handlePackageSearch),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
    api.Register(router)
}

func handlePackageSearch(ctx context.Context, req apischema.PackageSearchRequest) (apischema.PackageSearchResult, error) {
    return SearchPackages(ctx, req.Query)
}
```

After `make generate`, the frontend gets:

```typescript
const result = await call("packages.search", { query });
```

For a stream-only route, use `apischema.NoEndpoint()` in the route declaration and add a focused stream opener in `frontend/src/api/linuxio.ts` only when it carries real payload-specific behavior.

Keep each domain route contract in the same binding table that attaches its
handler or runner, even when the public route name belongs to a different
frontend namespace. For example, `appupdate` owns the `control.version` binding
because it owns the implementation, and `packages` owns the
`system.install_capability` binding because it runs the installer Task. The
reserved Task-service catalog is the deliberate exception described above.

## Privilege

Declare privilege in the route spec:

```go
var api = apischema.Bindings(
    apischema.Call[apischema.NoRequest, apischema.NoResponse](
        "monitoring.restart",
        apischema.Privileged(),
    ).HandleVoid(handleRestart),
)
```

The dispatcher checks the authenticated session before running the route. Handlers may still validate operation-specific policy, but they should not duplicate the route-level admin gate.

## Implementation Boundaries and Follow-up

The current contract shape is intentionally JSON-first and Go-owned. Runtime
route binding is typed, and TypeScript generation still reads Go type metadata.
Call fetching and Task lifecycle have separate, final runtime factories; the
completed transport migration is recorded in
[API Transport Simplification Plan](./api-transport-simplification-plan.md).

### 1. Keep Reflection Generator-Only

Goal: `reflect` is allowed in `backend/common/tools/linuxio-api-gen`, but not in runtime dispatch.

Current acceptable uses:

| File | Reason |
|------|--------|
| `backend/common/tools/linuxio-api-gen/main.go` | Reflects Go structs into generated TypeScript. |
| `backend/bridge/apischema/contracts.go` | Stores `reflect.Type` metadata for the generator through `TypeSpec`. |
| `*_test.go` files | Test comparison/introspection only. |

Remaining runtime cleanup:

1. Keep `apischema/schema.go` free of runtime reflection.
2. If `TypeSpec` starts feeling too runtime-shaped, move the type metadata into a codegen-only package or generated manifest and keep runtime route registration data-only.

### 2. Request decoding

The implemented request path uses one strict `encoding/json/v2.Unmarshal` into
the typed Go request struct. Unknown or case-mismatched fields, duplicate
names, invalid UTF-8, and trailing values fail before handler dispatch; scalar
type failures are `*json.SemanticError`. JSON remains readable and Go remains
the source of truth.

Generated request decoders are not implemented. Reconsider them only if a
profile shows decoding is material or a concrete contract needs generated
presence tracking. The shared v2 decoder supplies the strict envelope policy
without generating a decoder for every route.

### 3. Keep Route Declarations Local

Goal: adding a normal endpoint should still be one local binding-table edit plus any new request/result structs.

Rules:

1. One `apischema.Bindings(...)` block owns route string, mode, request type, result type, policy, and handler/runner attachment.
2. Do not export `RouteX` variables unless another package genuinely needs that route value.
3. `Routes = api.Routes()` remains the codegen/catalog source for that family.
4. `backend/bridge/handlers/register.go` changes only when adding or removing a handler family.

The only unavoidable second file for a new route is the shared contract file when the route needs a new exported request or response model.

### 4. Tighten Shared Contracts

Goal: `apischema/contracts.go` and `apischema/models.go` stay reviewable.

Next cleanup passes:

1. Move highly domain-specific request structs closer to their handler family if they are not reused elsewhere.
2. Keep only genuinely shared fragments in `contracts.go`.
3. Keep API response/domain models in `models.go` only when they are actually generated for frontend use.
4. Periodically run a usage scan before moving or deleting contract types.

### 5. Frontend API Surface

Goal: feature code imports one generated `linuxio` surface and does not know about transport details.

Current shape:

```typescript
await call("tasks.cancel", { taskId });
useSuspenseQuery(linuxio.system.get_cpu_info);
useCallMutation(linuxio.docker.start_container, {
  invalidates,
  success,
  error,
});
await call("system.get_cpu_info");
```

Remaining cleanup:

1. Keep `frontend/src/api/generated/*` generated only.
2. Keep stream helpers in `frontend/src/api/linuxio.ts` because streams are not normal request/response endpoints.
3. Keep `calls.ts` and `call-react-query.ts` small and Task-independent; do not add per-domain wrappers.

### 6. Verification Gates

Before considering this API contract work settled, run:

```bash
make generate
make test
```

Final scans should show:

```bash
rg "DecodeJSONArg|serializeStringArg" backend frontend/src
rg -F 'join("\0")' backend frontend/src
rg -F 'route\0' backend frontend/src
rg "reflect\\.|fn\\.Call|ValueOf" backend/bridge/apischema/schema.go backend/bridge/handlers
```

Expected result: no legacy string transport helpers, no `DecodeJSONArg`, and no runtime reflection in `apischema`.

## Verification

For API contract work, run:

```bash
make generate
make test
```

For claims that depend on browser navigation or stream behavior, also run:

```bash
make test-frontend-browser
```
