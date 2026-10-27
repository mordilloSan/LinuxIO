# API Contract

This is the canonical guide for LinuxIO's Go-owned API contract between the frontend and the bridge.

## Summary

LinuxIO exposes four deliberately different operation shapes:

- **Call** is bounded request/response work.
- **Mutation** is a change to the host that runs as a job in the caller's job
  host. One stream carries the job's snapshot, progress and result; the job
  outlives the stream, the browser tab and the login.
- **Channel** is a live stream with an explicit resume/reconnect contract.
- **Task** is the legacy shape for tracked background work with identity,
  progress, and watcher recovery, run in the bridge and owned by the login
  session. Task routes are being migrated to `Mutation`.

`docker.update_container` is the only Mutation so far. The 31 Task routes,
including `control.app_update`, are owned by the authenticated session; quick
writes are still Calls. This browser-to-bridge API is an internal product
surface. A future supported external API would be a separate contract.

Task progress and data streams are not a durable audit/history API. A Task
snapshot retains only the latest common progress envelope. A job's live
progress is also written to a file under `/run`, not to its record; that does
not turn `tasks.events` or `tasks.watch` into queryable history. Alerts live in a
root-owned file read through the `alerts` family and are separate from Task
snapshots.

- Go owns route names, modes, request types, and result types. Route declarations live with each handler family's registration in `backend/bridge/handlers/<domain>/handlers.go`.
- TypeScript API files under `frontend/src/api/generated` are generated. Do not edit them by hand.
- API requests use JSON stream-open envelopes: `{"route":"handler.command","request":{...}}`. A Mutation envelope also carries the client-chosen job `id`.
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
| `backend/bridge/apischema/contracts.go` | `TypeSpec` payload-type reflection and the `NoRequest`/`NoResponse` marker types. |
| `backend/bridge/apischema/<domain>.go` | Request/response/domain models for one route family (`docker.go`, `network.go`, ...), reflected into TypeScript. `values.go` holds the `Optional` helper for optional DTO fields. |
| `backend/bridge/apischema/schema.go` | Contract helpers, route policy, and typed registration adapters. |
| `backend/bridge/internal/bridgeipc/request_decoder.go` | Shared strict request decoder for normal contracts and the reserved Task service. |
| `backend/bridge/internal/apigen` | Generator for frontend client/types/route metadata. |
| `frontend/src/api/generated/client.ts` | Generated concrete `linuxio` object. Calls are TanStack descriptors or descriptor factories; Tasks use their separate lifecycle factory; Mutations use `defineMutation`. |
| `frontend/src/api/generated/linuxio-types.ts` | Generated API models and schema types. |
| `frontend/src/api/generated/route-metadata.ts` | Generated route modes (`RouteMode`: `call`, `task`, `mutation`, `duplex`) and sparse retry-safe Call policy. |

## Frontend API Files

| File | Role |
|------|------|
| `frontend/src/api/index.ts` | Public barrel. Feature code should import from `@/api`. |
| `frontend/src/api/calls.ts` | Framework-independent typed `call()` transport and consumer of the generated retry-safety policy. |
| `frontend/src/api/call-react-query.ts` | Final TanStack descriptor builders and the shared bounded-mutation lifecycle. It has no dependency on Task lifecycle or streams. |
| `frontend/src/api/mutation-react-query.ts` | Mutation endpoints: `defineMutation` and `useMutation` with `onStart`, `onProgress` and `watch`. Transport recovery lives in `linuxio-core.ts`. |
| `frontend/src/api/task-react-query.ts` | Task-only mutation integration: `useTaskAction`, `useTaskStreamAction`, completion waiting, watching, and progress handling. It imports the shared mutation lifecycle; Call fetching never imports Task lifecycle. |
| `frontend/src/api/endpoint-types.ts` | Type-only aggregate that maps generated routes to Call descriptors, Task capabilities or Mutation endpoints without merging their runtime factories. |
| `frontend/src/api/linuxio-core.ts` | Low-level JSON Call path, deadline ownership, bounded retry, and connection-outcome classification; the Mutation stream path (`mutationRequest`, `watchMutation`) with its job `id`, resend and reattach. API internals only. |
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
| `bridgeipc.ModeCall` | Bounded request/response work: reads, and the quick writes not yet migrated to `Mutation`. React Query caching versus mutation behavior is chosen only at the frontend callsite. |
| `bridgeipc.ModeMutation` | A change to the host that runs as a job. The bridge relays the stream to the caller's job host, which runs the handler and records the job; a `BrowserBound()` route runs in the bridge on the same stream. No client timeout. See [Tasks and jobs](#tasks-and-jobs). |
| `bridgeipc.ModeTask` | Legacy: tracked work with Task identity, progress and watcher recovery, run in the bridge and owned by the login session. Being migrated to `ModeMutation`; add no new Task routes. |
| `bridgeipc.ModeDuplex` | Long-lived Channels, including server-producing logs and bidirectional terminals; resumption is explicit in each Channel protocol. |

Every route has one schema kind:

| Kind | Go binding |
|------|------------|
| `KindHandler` | `.Handle(func(context.Context, TRequest) (TResult, error))` or `.HandleVoid(func(context.Context, TRequest) error)`. |
| `KindMutation` | `apischema.Mutation(name, fn, opts...)` with `fn func(context.Context, TRequest) (TResult, error)`, or `apischema.MutationVoid(name, fn, opts...)` with `fn func(context.Context, TRequest) error`. Go infers the request and result types from `fn`. |
| `KindTaskRunner` | Legacy. `.Run(func(context.Context, *bridgeipc.Task, TRequest) (TResult, error), policy)`; the compiler checks the terminal result before the binding erases it at the bridge registry boundary. |
| `KindDuplex` | `func(context.Context, net.Conn, TRequest) error` |

Use `apischema.NoRequest` for no request payload and `apischema.NoResponse` for no result payload. They are API contract marker types owned by `apischema`.

Calls, Mutations and Task runners are typed at their binding.
`apischema.Progress[T]()` (`WithTaskProgress[T]()` on Task routes) declares the
route-specific detail inside the common `TaskProgress<T>` envelope; the detail
type implements `ProgressEnvelope()`. A Mutation handler reports it with
`bridgeipc.ReportProgress(ctx, detail.ProgressEnvelope())`, which does nothing
outside a job; a Task runner passes it to `task.ReportProgress()`. Task data
uses the focused `tasks.data` attachment Channel. Ordinary handlers have no
universal emitter surface.

Mutation options:

- `apischema.Privileged()`, as for every route that needs root.
- `apischema.Progress[T]()` for the typed progress detail.
- `apischema.BrowserBound()` keeps the route in the bridge because its bytes
  come from or go to the browser. No route declares it yet.
- A request type that implements `apischema.Targeted`, `Target() []string`,
  names what the job works on (a container, a pool); the binding detects it
  once. A second job of the same route on the same target is refused while the
  first runs. `DockerContainerUpdateRequest` returns its container ID.
- `.Resumable(apischema.ResumeHooks[Req, Res]{Suspend, Resume})` on the
  binding lets a job continue after a shutdown or reboot. It panics unless the
  route is `Privileged()`. No route declares it yet.

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

Mutation endpoints are generated with `defineMutation`. They expose the Promise
call (`linuxio.docker.update_container(request)` resolves with the typed
result), the query key, and:

| Member | Use |
|--------|-----|
| `endpoint.useMutation(config?)` | The `ActionConfig` of `useCallMutation` plus `onStart(snapshot, variables)`, called once when the job exists, and `onProgress(progress, snapshot, variables)` per progress frame. The returned mutation also exposes `watch(taskId, variables)` to adopt a running job of the route after a reload or from another tab. |

```typescript
const update = linuxio.docker.update_container.useMutation({
  onProgress: (progress) => setPhase(progress.detail?.phase),
  error: "Container update failed",
});
update.mutate({ containerId });
```

Feature code imports TanStack Query's read primitives directly. Calls
pass their descriptor directly (`useSuspenseQuery(linuxio.system.get_cpu_info)`)
or invoke their request descriptor factory. Mutation routes use their
generated `useMutation`. Bounded writes still declared as Calls use
`useCallMutation`.
Progress work on legacy Task routes uses the separate Task module's
`useTaskAction`/`useTaskStreamAction` or the background-Tasks layer,
while imperative cache work uses QueryClient directly. Feature code does not
invent query keys/functions or use raw mutations. Guard tests
(`frontend/src/test/guards/apiLayering.test.ts`) enforce that boundary.

Route loaders use `loadRouteQueries` from `src/routes/-loader.ts` to wait for the request transport and pass typed Call descriptors to the shared browser QueryClient. It requires a live `isUpdateBlocked` getter from router context rather than the mux, rechecks it after readiness, and rejects without querying while an update is active. Loader failures propagate to TanStack Router; intent preloads are marked speculative so QueryCache suppresses their global error toast.

The same guard file also fences the byte/mux-level transport primitives (`encodeString`, `bindStreamHandlers`, `getStreamMux`, …): feature code opens typed Channels with `openChannel(route, request)` and uses the stream lifecycle hooks (`useLiveStream`/`useLogStream`/`useStreamResult`). Only a short, shrink-only allowlist may import lower-level primitives from `@/api`.

Call descriptors compose with normal React Query options, including `select` for transformed output data. `useCallMutation` and `useTaskAction` take an `ActionConfig` — `invalidates` (query keys, static or derived from result/variables), `success`/`error` (toast message strings or callbacks; the error string is only a fallback, the server error message wins), `warning` (an extractor like `(result) => result.warning`; a non-empty return fires a warning toast that replaces the string-form success toast — invalidation and callback-form `success` still run), `toast` (toast metadata such as an action link), and `options` as a raw React Query options escape hatch.

### Choosing a member (decision table)

| Situation | Use |
|-----------|-----|
| Data rendered by this component | `useQuery(callDescriptor)`. For on-demand panels, compose `enabled` and other options at the callsite. |
| Data needed inside an effect, loader, or event handler, then handed to something else | `queryClient.fetchQuery(callDescriptor)` when cache sharing matters; `call(route, request)` for transport-only work. |
| Call invoked as a command (validate, generate download, resolve path) | `useCallMutation(endpoint, config)` for declarative pending/error/feedback behavior. |
| Single bounded mutation triggered by one user action | `useCallMutation(endpoint, config)`, fired with `mutate`. |
| Route generated as a Mutation (`RouteMode` `mutation`) | `endpoint.useMutation(config)`, with `onProgress` when the UI renders progress and `watch(taskId, variables)` in a `useActiveTaskRecovery` callback to adopt the running job after a reload. |
| Legacy Task route whose live progress the UI renders, or that must survive page reload | `useTaskStreamAction` (+ `watch` with `useActiveTaskRecovery` for recovery). Don't pick it just to customize an error string. |
| Several mutations sequenced or looped in one flow (batch delete, multi-step save) | Create the actions **configless** with a comment, `await mutateAsync` per step/item inside `try/catch`, and aggregate the outcome into one toast. The catch owns flow control only — never re-toast an error a config already toasted. |
| Long-running transfer that must outlive the page and show in the navbar | The background-Tasks layer (`useBackgroundTaskActions`), not a component-local Task action. |
| Optimistic updates / seeding cache from a result | QueryClient `cancelQueries`/`setQueryData` with the descriptor's `queryKey`. |

Refreshes after mutations come from the invalidation manifest by default — do not add `refetch()` calls or `onSuccess` refresh props on top of it; if a list does not refresh, fix the manifest entry instead.

Query invalidation is manifest-driven: `frontend/src/api/operation-query-invalidations.ts` maps each Call or Task route to the Call caches it makes stale, using the same centralized query keys. It is the single source of truth for mutation lifecycles — `useCallMutation`, `useMutation`, `useTaskAction`, and `useTaskStreamAction` use it as the default `invalidates` for locally awaited work, and the recovered-Tasks stream applies it to Tasks that finish with no local handler (page reload, another session). Call sites only pass `invalidates` to override the manifest (`[]` opts out; a function derives keys from result/variables). The few path-precise File Browser invalidations use QueryClient directly and are source-guarded.

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
`bridgeipc.JSONRequestDecoder`. It uses `encoding/json/v2.Unmarshal` with
`RejectUnknownMembers(true)`. The v2 defaults require exact, case-sensitive
member matches and reject duplicate names, invalid UTF-8, and trailing data.
A missing or `null` request retains the existing empty-object behavior.
Required-field meaning remains handler/domain validation; use pointer fields
only when the wire contract must distinguish an absent value from its zero
value.

The HTTP login body, the relay stream-open envelope, the indexer `/add` body and
configuration patch, and the monitoring `/command` body are separate request
boundaries, but apply the same strict v2 member and syntax rules directly.

All production code uses `encoding/json/v2` (`TestImportBoundaries` rejects a
v1 import; raw JSON is `jsontext.Value`). Responses therefore follow v2 output
rules: nil slices encode as `[]` and nil maps as `{}`, never `null`; `<`, `>`
and `&` are not escaped; struct fields use `omitzero` (omit the Go zero value)
rather than `omitempty`, which in v2 would also drop `0`, `false`, and empty
values behind pointers. Decoders of external tool output pass
`MatchCaseInsensitiveNames(true)`; files LinuxIO persists are written with
`Deterministic(true)`. `time.Duration` has no v2 representation, so wire
structs carry `int64` nanoseconds instead.

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
Default/no-policy Calls, including mutating Calls, never retry. The shared
TanStack Query policy also refuses a second Query-layer attempt for these two
codes, so it cannot multiply the transport decision. Frontend feature code
branches on a structured code, never on error message text.

Mutations do not use this Call path. `mutationRequest` in
`frontend/src/api/linuxio-core.ts` generates a UUID v4 per call and sends it as
the envelope's `id`:

```json
{"route":"docker.update_container","request":{"containerId":"abc"},"id":"5d2a…-uuid"}
```

A Mutation whose `id` is not a canonical non-zero UUID is refused with `400`.
There is no client timeout: only the wait for the transport is bounded, never
the job. When the stream closes with `connection_closed` or
`connection_unavailable` before the `task.snapshot` frame, the client resends
the same envelope once; the job host attaches to the job with that `id`, or
answers from its record, so a resend never starts a second job. After the
snapshot the client reattaches with `tasks.watch {taskId: id}` instead. A second
loss rejects with `outcome_unknown` (`connection_unavailable` when no stream
could be opened at all). Aborting the caller's signal aborts the stream, which
cancels the job. A resend of an `id` with a different route or request is `409`.

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

Mutation route (`backend/bridge/handlers/docker/background_operations.go`):

```go
apischema.Mutation(
    "docker.update_container",
    handleUpdateContainer,
    apischema.Privileged(),
    apischema.Progress[apischema.DockerContainerUpdateProgress](),
)

func handleUpdateContainer(ctx context.Context, req apischema.DockerContainerUpdateRequest) (apischema.DockerContainerUpdateResult, error) {
    // ...
    result, err := updateContainer(ctx, req.ContainerID, 0, func(phase, message string) {
        bridgeipc.ReportProgress(ctx, apischema.DockerContainerUpdateProgress{Phase: phase, Message: message}.ProgressEnvelope())
    })
    // ...
}
```

The handler has the Call signature; `DockerContainerUpdateRequest.Target()`
makes the container the job's target.

Legacy Task runner route:

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

## Configuration Ownership

Per-user configuration is exposed as two resources because its fields have
different owners and failure consequences:

| Route | Contract | Semantics |
|---|---|---|
| `config.get` | `void -> AppConfig` | Retry-safe read of bridge-owned functional settings. |
| `config.set` | `ConfigSetPayload -> ConfigSetResult` | Partial update of functional settings. |
| `config.get_ui` | `void -> UIConfig` | Retry-safe read of effective UI preferences, including backend-owned defaults. |
| `config.set_ui` | `ConfigUISetPayload -> ConfigSetResult` | Full UI snapshot replacement; omitted fields resolve to backend defaults. |

`AppConfig.appSettings` contains only `showHiddenFiles` and `chunkSizeMB`.
Docker, job, and dismissal fields remain on `AppConfig`. `UIConfig` contains
theme, navigation, section visibility, view-mode, layout-order, and related
presentation fields. The frontend combines these responses into its internal
effective configuration; that combined type is not a wire contract.

This is an intentional change to the prior `config.get`/`config.set` shape.
Keeping UI fields in those messages would preserve the ownership ambiguity and
make omission impossible to define as reset-to-backend-default. The bridge and
frontend are shipped together, and the schema parity tests and generated client
cover all four route declarations.

The bridge first stores `config.yaml` and `ui.yaml` under the authenticated
user's `$HOME/.config/linuxio` directory. If that store cannot be opened, it
uses the UID-owned `/var/lib/linuxio/users/<uid>` fallback. If both disk stores
fail, the bridge starts with in-memory defaults and accepts changes for that
bridge session without writing files. Legacy `.linuxio-config.yaml`,
`.linuxio-ui.yaml`, and their sidecar locks in `$HOME` are not read, migrated,
or removed.
`config.get` reports `storageMode` as `home`, `fallback`, or `memory`; the
authenticated UI keeps a warning visible for both degraded modes and explains
that memory-mode changes are temporary.

Both disk stores use independent sidecar locks and atomic whole-file
replacement. A missing or invalid UI file is written as `{}`. The bridge
expands that sentinel, or any valid sparse UI document, over backend defaults
when serving `config.get_ui`; it does not rewrite the sentinel merely because
it was read. On the first preference change, the frontend sends one complete
effective snapshot to `config.set_ui`. There is no session-storage
configuration copy or local-storage theme cache. Concurrent UI replacements
are serialized but not field-merged; the last complete snapshot committed
wins.

At bridge startup a core document
that fails to decode or validate is renamed to
`config.yaml.broken-<UTC timestamp>` and replaced with defaults; an
existing quarantine name gets `(2)`, `(3)`, and so on rather than being
overwritten. The bridge logs both paths at warning level and starts. Read,
stat, symlink, lock, and write failures move startup to the next storage tier
without replacing the failed config artifact. A core update that finds an unreadable
document fails and leaves the file untouched; only the startup read
quarantines. The bridge logs the storage mode plus the core and UI paths when
the store is ready and logs every default-file creation and UI reset. The
bridge never uses filesystem observations to repair Docker folders.

All four disk artifacts (the two YAML files and their lock files), plus atomic
replacement files, use the authenticated session UID/GID even when the bridge
runs as root. LinuxIO resolves only that user's passwd home and verifies its
owner; it follows a symlinked passwd home only after verifying the resolved
directory's owner, and it neither falls back to the bridge process home nor
changes ownership of the home directory. Lock inodes are never unlinked and
recreated, so concurrent bridges continue to coordinate through `flock`.

## Tasks and jobs

A Mutation runs as a **job**: a Task in the task service of the caller's job
host, identified by the client's UUID, with a record in the job store. Legacy
Task routes run as session Tasks in the bridge. Both are discovered and
controlled through the same `tasks.*` routes. Bounded writes that are still
Calls complete through a single direct `useCallMutation` request/response.

### The mutation stream

One stream per Mutation carries the whole job:

1. The client opens the stream with the envelope and its `id`.
2. The server writes one progress frame
   `{"type":"task.snapshot","task":<TaskSnapshot>}`: the job exists and the
   `id` is claimed.
3. Zero or more progress frames, each a `TaskProgress` envelope.
4. Exactly one result frame (the typed result; `null` for a void route) or
   error frame, then close.

Steps 3 and 4 are the frames `tasks.watch` sends. A reattach with `tasks.watch`
gets no `task.snapshot` frame: `watchMutation` fetches the snapshot with
`tasks.get` first. Closing the stream detaches and the job runs on; aborting it
(`OpStreamAbort`) cancels the job. A resend of the same `id` with the same route
and request bytes attaches to the running job, or, when the host no longer holds
it, is answered from its record. `tasks.watch {taskId}` reattaches later from
any tab or login of the same uid.

Refusals arrive as the error frame, before any job starts:

| Code | Cause |
|------|-------|
| `400` | The `id` is not a canonical non-zero UUID, or the request fails decoding. |
| `403` | A `Privileged()` route from a session that is not sudo-authorized. |
| `409` | Same target: `<route> is already running on this target`. |
| `409` | The `id` was used for another route or request. |
| `409` | The host already runs 256 jobs (`too many running jobs`), a safety valve. |
| `409` | `update_pending: …`: the bridge and the job host speak different protocol numbers, because one of them predates an update. Reads keep working. |
| `503` | `job host unavailable`: the bridge has no connection to its job host yet; it keeps reconnecting. `job host is stopping` while the host shuts down. |

### Where mutations run

Every bridge connects at startup to the job host of the uid it runs as,
`linuxio-jobs@<uid>.service`, and keeps one yamux connection to
`/run/linuxio/jobs/<uid>.sock`, which systemd holds and which starts the host
when it is down, reconnecting with backoff from 1 s to 30 s.
The bridge decodes the request and checks privilege, then relays each Mutation
stream to the host unchanged, frames both ways. The host checks them again with
its own session: the uid comes from `SO_PEERCRED`, not from the bridge's hello,
and only the root host's sessions are privileged. A sudo-authorized login runs
a root bridge, so all admin logins share the root host, `linuxio-jobs@0`; any
other login gets a host running as that user. See
[Process & Systemd Architecture](./process-systemd-architecture.md#login-and-jobs)
for how hosts start.

The host runs each job's handler in a goroutine whose context ends only on
`tasks.cancel`, a stream abort, or the host stopping. The bridge keeps all Calls,
Channels, legacy Task routes, `tasks.data`, and the `BrowserBound()` Mutations,
which it runs itself on the same stream.

### Task admission

Tasks and jobs run side by side: there is no queue, no per-user or per-route
limit, no start-rate limit and no runtime cap. A task ends when it finishes or
is canceled. These checks refuse with `409` instead of waiting:

- **Same target.** A second job of a Mutation route whose request `Target()`
  matches a running one, or a second task of a Task route whose metadata
  identity does (the same pool, disk, container, compose project or set of
  paths), is refused, as the kernel or device would refuse it. The target stays
  taken until the first handler has exited, even after a cancel. A request type
  without `Target()` has no target and is never refused as a duplicate.
- **Exclusive routes** (`bridgeipc.TaskExclusive`, legacy Task routes only) run
  one at a time because they own a whole system resource: `packages.update`,
  `system.install_capability`, `control.app_update`,
  `docker.apply_daemon_storage` and `filebrowser.index`.
- **Host ceiling.** A job host refuses a new job while it runs 256.

Mutation admission is per job host, that is per uid; the root host is shared
by every admin login, so a privileged target is checked across logins. Task
admission is counted per bridge, that is per login; work shared across logins
is serialized by its owner (PackageKit, Docker, or the kernel).

### Job states and records

A job's record is `/var/lib/linuxio/jobs/<uid>/<id>.json` (`0600`, in a `0700`
directory owned by the uid), written by `backend/common/jobs`. It holds the
route, owner, target, state, timestamps, the full decoded request, and the
result or error. No API returns the request. The host writes a job's live
progress to `/run/linuxio/jobs/<uid>/<id>.progress`, at most twice a second,
never into the record; the file is removed with the record.

States are `running`, `completed`, `failed`, `canceled` and `interrupted`; an
`interrupted` record carries its reason: `shutdown`, `crash` or `reboot`. The
record is written when the job starts and on every state change, and fsynced
only when a job that ran longer than one second reaches a terminal state or when
its checkpoint changes. A job that completes in under one second is deleted from
the store. Terminal records are kept 30 days and at most 200 per uid, pruned
when a host starts.

`interrupted` (`TaskStateInterrupted`) is terminal. A job becomes interrupted
when its host stops under it (`systemctl stop`, shutdown: reason `shutdown`),
when its host died (marked by the next host of that uid: `crash`), or across a
reboot (marked by the next host of that uid, whose last host ran in an earlier
boot: `reboot`). A host
that stops interrupts its live tasks: a client attached then reads `job was
interrupted` (`409`) as the stream's error, the host waiting up to 5 s for the
bridge to close the stream before it closes the connection, and a resend of
the envelope is answered from the record with the same error. The root host raises
an alert (source `jobs`, key the job ID) for a root job that failed or was
interrupted, except a resumable job it resumes after a reboot; a user host
writes the record only. Successful and canceled jobs never alert.

The root host starts at boot and resumes each job of a `Resumable` route that
an earlier boot interrupted, once per boot; no route is resumable yet.

### Task lifetime, ownership, and activity

The 31 legacy Task routes, including `control.app_update`, declare
`SessionTask()`. Their owner is the exact authenticated `SessionID`; logout,
expiry, session deletion, bridge failure, or bridge shutdown cancels the
in-memory Task. Session Tasks exist only for these `TaskRunner` routes.

A Mutation job is owned by the numeric uid of its job host, not by a session.
Logout, session expiry, bridge exit and connection loss only detach from it;
the host keeps running it, and any login of the same uid sees it through
`tasks.list` and `tasks.events` and can watch or cancel it. The host holds a
terminal job in memory for 30 minutes. The username and `session_ref` in the
bridge's hello are recorded as the job's owner for display; authorization is
the host's uid. On the root host a live snapshot's `owner.uid` is therefore
the host uid (0), while the stored record names the real user's uid; the
frontend reads `owner.username` only.

`TaskOwner.SessionID` is an internal authorization value and is never emitted
in public Task snapshots, serialized API models, client-visible errors, or
diagnostic output. Public owner fields are limited to non-secret identity data;
diagnostics use the one-way `session_ref` defined by the
[Production Diagnostic Data Policy](./production-diagnostics.md).

WebSocket keepalive, passive relay frames, Task progress, and Channel traffic
only validate session expiry. The outer relay `FlagActivity` bit is the sole
activity signal: the frontend emits it on throttled document interaction and
selected interactive stream data, and the server strips it before forwarding
the payload and refreshes session activity.

Starting a generated legacy Task endpoint returns a `TaskSnapshot` immediately.
On the frontend, `useTaskAction` awaits the terminal state via
`waitForTaskCompletion()`: a failed Task rejects with a `LinuxIOError` carrying
the Task's error message/code, and success resolves with the unwrapped typed
`TaskSnapshot.result`. A Task start is never retried after connection loss; Task
recovery begins only after the client has received its snapshot identity. If the
watch stream cannot be opened (the mux dropped between Task start and watch),
completion waiting falls back to the explicitly retry-safe `tasks.get` Call;
`useTaskStreamAction` instead fails fast because it promises live progress, and
the recovered-Tasks stream can pick the Task up.

### Task progress

Snapshot-retained Task progress has one wire shape:

```typescript
interface TaskProgress<TDetail = unknown> {
  percentage?: number;
  phase?: string;
  message?: string;
  detail?: TDetail;
}
```

`percentage` is clamped to 0 through 100 by `TaskService`; `phase` is a stable
machine-readable stage; and `message` is concise presentation-ready status.
Generic Task lists and recovery use only those fields. A generated route
endpoint binds `detail` to its declared `Progress[T]()` (or
`WithTaskProgress[T]()`) type, so a feature-owned UI retains its concrete
counters, paths, package state, or compose event without teaching the global
Task UI every route shape.

Retained progress reaches both `tasks.events` and `tasks.watch` in this envelope.
`tasks.events` is the owner-wide discovery/lifecycle view and coalesces
progress; `tasks.watch` follows one Task, replays its bounded progress window,
and supplies its terminal result. Explicit transient output on `tasks.watch`
and payload-specific `tasks.data` flow-control frames remain route protocols;
they are not stored as Task progress or published on `tasks.events`.

`control.app_update` is intentionally session-bound. It downloads and verifies
the installer, then executes it with `systemd-run --wait --pipe` and
`--defer-restart`, relaying sanitized stdout/stderr directly as Task data. On
success it reloads systemd, writes the terminal status projection, reports the
terminal Task output/result, and only then restarts `linuxio.target`. It does
not claim page-, session-, or bridge-restart recovery.

For `control.app_update`, `GET /api/update-status?id=<uuid>` is
session-authenticated and UID-scoped. It reads a versioned, sanitized projection
under `/run/linuxio` and reports `running`, `ok`, `error`, or `unknown`. Missing,
legacy, mismatched-ID, and different-UID projections are indistinguishable. The
browser treats an exact target value from the public `LinuxIO Web Server`
version entry as restart proof even when the restart invalidates the previous
in-memory login session and the authenticated status request returns `401`.

Built-in Task routes:

| Route | Use |
|-------|-----|
| `tasks.get` | Fetch one owned Task or job snapshot. |
| `tasks.list` | List owned Tasks and the jobs the job host holds. |
| `tasks.cancel` | Cancel one owned Task or job. |
| `tasks.watch` | Common progress envelope plus typed detail and terminal result. Closing detaches; aborting cancels. |
| `tasks.data` | Upload/download/archive data stream. Bridge only. |
| `tasks.events` | Owner-wide lifecycle events and coalesced common progress. |

The bridge and the job host both serve `tasks.*`. The bridge answers
`tasks.get`, `tasks.cancel` and `tasks.watch` for its own Tasks and forwards a
job ID (a UUID) it does not hold to its host. `tasks.list` appends the host's
jobs to the bridge's Tasks, and `tasks.events` merges the host's event stream
into the bridge's, reopening it after the bridge reconnects to its host, so one
stream shows both. The host's list is what it holds in memory: running jobs and
terminal ones for 30 minutes. Records from before a host restart are not listed
yet.

Two `jobs.*` routes are internal, not generated for the frontend:
`jobs.hello` is the first stream of a connection to a job host and carries the
protocol number and the session facts; `jobs.resume {id}` asks the root job
host to run the `Resume` hook of an interrupted resumable job. `jobs.resume` is
served to root only; `linuxio-bridge jobs submit --resume` uses it.

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
| `openChannel("docker.logs.follow_all", request)` | `docker.logs.follow_all` | Direct server-producing merged log Channel for every running container; NDJSON records `{id, name, ts, line, stderr?}`, containers that start later are attached. |
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

If the request or response type is new, add the Go struct to the matching `backend/bridge/apischema/<domain>.go` file (or the nearest shared domain file when several families use it).
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

A new route that changes the host is a `Mutation`, not a `TaskRunner` and,
for work that reports progress or may run long, not a `Call`. The binding entry
takes the handler and Go infers the types:

```go
var api = apischema.Bindings(
    apischema.MutationVoid("packages.remove", handleRemovePackage, apischema.Privileged()),
)

func handleRemovePackage(ctx context.Context, req apischema.PackageRemoveRequest) error {
    return RemovePackage(ctx, req.Name)
}
```

Add `apischema.Progress[T]()` and call `bridgeipc.ReportProgress` when the job
reports progress, and give the request type a `Target() []string` method when
two jobs on the same object must not run together. After `make generate`, the
frontend gets `linuxio.packages.remove.useMutation(config)`, and the route
needs a row in `operation-query-invalidations.ts` when it makes Call caches
stale.

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

## Implementation Boundaries

The current contract shape is intentionally JSON-first and Go-owned. Runtime
route binding is typed, and TypeScript generation still reads Go type metadata.
Call fetching and Task lifecycle have separate runtime factories. Tasks are a
service layered over the existing Call and Channel transports, not a second wire
protocol; the bridge and a job host exchange the same relay frames over their
own yamux connection. Frontend query and mutation cache policy is independent of
the underlying transport shape. This API is internal to LinuxIO; any future
supported external API would be versioned and documented separately.

### 1. Keep Reflection Generator-Only

Goal: `reflect` is allowed in `backend/bridge/internal/apigen`, but not in runtime dispatch.

Current acceptable uses:

| File | Reason |
|------|--------|
| `backend/bridge/internal/apigen/main.go` | Reflects Go structs into generated TypeScript. |
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

Goal: the per-domain `apischema/<domain>.go` files and `apischema/contracts.go` stay reviewable.

Next cleanup passes:

1. Move highly domain-specific request structs closer to their handler family if they are not reused elsewhere.
2. Keep only genuinely shared fragments in `contracts.go`.
3. Keep API response/domain models in their domain file, and only when they are actually generated for frontend use.
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
