# API Contract

This is the canonical guide for LinuxIO's Go-owned API contract between the frontend and the bridge.

## Summary

LinuxIO exposes three deliberately different operation shapes:

- **Call** is bounded request/response work.
- **Mutation** is a change to the host that runs as a job in the caller's job
  host. One stream carries the job's snapshot, progress and result; the job
  outlives the stream, the browser tab and the login.
- **Channel** is a live stream with an explicit resume/reconnect contract.

A Mutation's job is a **task** to the `tasks.*` routes, which read its
snapshot (`tasks.get`, `tasks.events`), follow it (`tasks.watch`), cancel it
and carry its bytes (`tasks.data`).

Every write is a Mutation except five Calls: `logging.set_debug`, the three
network bridge-handoff routes, which keep their own durable record, and
`storage.btrfs_replication_ssh_key`, which creates the key on first read.
Components follow a Mutation with `endpoint.useMutation`; the background tray
starts one with `endpoint.start`, which resolves with the job's first snapshot
and detaches, then follows it with `tasks.watch` and `tasks.data`
([Jobs](./jobs.md#writing-a-route-that-runs-as-a-job)). This browser-to-bridge API is an internal product
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
| ---| ---|
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
| ---| ---|
| `frontend/src/api/index.ts` | Public barrel. Feature code should import from `@/api`. |
| `frontend/src/api/calls.ts` | Framework-independent typed `call()` transport and consumer of the generated retry-safety policy. |
| `frontend/src/api/call-react-query.ts` | Final TanStack descriptor builders and the shared bounded-mutation lifecycle. It has no dependency on Task lifecycle or streams. |
| `frontend/src/api/mutation-react-query.ts` | Mutation endpoints: `defineMutation`, `start`, and `useMutation` with `onStart`, `onProgress`, `signal` and `watch`. Transport recovery lives in `linuxio-core.ts`. |
| `frontend/src/api/endpoint-types.ts` | Type-only aggregate that maps generated routes to Call descriptors or Mutation endpoints without merging their runtime factories. |
| `frontend/src/api/linuxio-core.ts` | Low-level JSON Call path, deadline ownership, bounded retry, and connection-outcome classification; the Mutation stream path (`mutationRequest`, `watchMutation`, `startJob`) with its job `id`, resend and reattach. API internals only. |
| `frontend/src/api/linuxio.ts` | Typed `openChannel()` transport, connection hooks, terminal/Task stream helpers, and the app-update Task data stream. |
| `frontend/src/api/StreamMultiplexer.ts` | WebSocket stream multiplexer, relay frame encoding, stream lifecycle, singleton connection management. |
| `frontend/src/api/stream-helpers.ts` | Helpers for binding stream callbacks, awaiting result frames, and writing byte chunks. |
| `frontend/src/api/tasks.ts` | Terminal-state and cancellation classification. |
| `frontend/src/api/task-state.ts` | Shared terminal Task-state predicate. |
| `frontend/src/api/operation-query-invalidations.ts` | Default query invalidations shared by direct actions and Tasks. |
| `frontend/src/api/capabilities.ts` | Frontend capability manifest and state helpers. |

## Route Modes And Kinds

Every route has one mode:

| Mode | Use |
| ---| ---|
| `bridgeipc.ModeCall` | Bounded request/response work: reads, and the quick writes not yet migrated to `Mutation`. React Query caching versus mutation behavior is chosen only at the frontend callsite. |
| `bridgeipc.ModeMutation` | A change to the host that runs as a job. The bridge relays the stream to the caller's job host, which runs the handler and records the job; a `BrowserBound()` route runs in the bridge on the same stream. No client timeout. See [Tasks and jobs](#tasks-and-jobs). |
| `bridgeipc.ModeDuplex` | Long-lived Channels, including server-producing logs and bidirectional terminals; resumption is explicit in each Channel protocol. |

Every route has one schema kind:

| Kind | Go binding |
| ---| ---|
| `KindHandler` | `.Handle(func(context.Context, TRequest) (TResult, error))` or `.HandleVoid(func(context.Context, TRequest) error)`. |
| `KindMutation` | `apischema.Mutation(name, fn, opts...)` with `fn func(context.Context, TRequest) (TResult, error)`, or `apischema.MutationVoid(name, fn, opts...)` with `fn func(context.Context, TRequest) error`. Go infers the request and result types from `fn`. `apischema.MutationTask(name, fn, opts...)` hands `fn` its `*bridgeipc.Task` too, for runners that report through it or name files after its ID. |
| `KindDuplex` | `func(context.Context, net.Conn, TRequest) error` |

Use `apischema.NoRequest` for no request payload and `apischema.NoResponse` for no result payload. They are API contract marker types owned by `apischema`.

Calls and Mutations are typed at their binding.
`apischema.Progress[T]()` declares the
route-specific detail inside the common `TaskProgress<T>` envelope; the detail
type implements `ProgressEnvelope()`. A Mutation handler reports it with
`bridgeipc.ReportProgress(ctx, detail.ProgressEnvelope())`, which does nothing
outside a job; a `MutationTask` runner may pass it to `task.ReportProgress()`. Task data
uses the focused `tasks.data` attachment Channel. Ordinary handlers have no
universal emitter surface.

Mutation options:

- `apischema.Privileged()`, as for every route that needs root.
- `apischema.Progress[T]()` for the typed progress detail.
- `apischema.BrowserBound()` keeps the route in the bridge because its bytes
  come from or go to the browser: `filebrowser.upload`, `upload_batch` and
  `archive`. Logout cancels its jobs.
- `apischema.Exclusive()` runs one job of the route at a time.
- A request type that implements `apischema.Targeted`, `Target() []string`,
  names what the job works on (a container, a pool); the binding detects it
  once. A second job of the same route on the same target is refused while the
  first runs. `DockerContainerUpdateRequest` returns its container ID.
- A request field tagged `secret:"true"`, such as a password, is left out of
  the request the job record keeps. `TestMutationRequestsKeepTheirSecretsOutOfTheRecord`
  fails for a Mutation request field named like a password or key without it.
- `.Resumable(apischema.ResumeHooks[Req, Res]{Suspend, Resume})` on the
  binding lets a job continue after a shutdown or reboot. It panics unless the
  route is `Privileged()` and has no secret fields. Four routes declare it:
  `storage.btrfs_balance`, `storage.btrfs_scrub_start`,
  `storage.btrfs_device_replace` and `storage.btrfs_device_remove`. On a
  shutdown the host runs `Suspend`, stores its checkpoint in the record and
  then cancels the context; at the next boot `Resume` runs once, and only for a
  `shutdown` or `reboot` interruption.

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
| ---| ---|
| No-request descriptor (`linuxio.system.get_cpu_info`) | Pass directly to `useQuery`, `useSuspenseQuery`, route loaders, or QueryClient. |
| Request descriptor factory (`linuxio.systemd.get_unit_info(request)`) | Bind the complete generated request object, then use the returned descriptor in the same places. |

Mutation endpoints are generated with `defineMutation`. They expose the Promise
call (`linuxio.docker.update_container(request)` resolves with the typed
result), the query key, and:

| Member | Use |
| ---| ---|
| `endpoint.useMutation(config?)` | The `ActionConfig` of `useCallMutation` plus `onStart(snapshot, variables)`, called once when the job exists, and `onProgress(progress, snapshot, variables)` per progress frame. `signal` (or a function of the variables returning one) stops following when aborted: `closeOnAbort: "abort"`, the default, cancels the job, `"close"` detaches and leaves it running. The returned mutation also exposes `watch(taskId, variables)` to adopt a running job of the route after a reload or from another tab. |
| `endpoint.start(request)` | Starts the job and resolves with its first snapshot, then detaches. For the background tray, which follows jobs itself. |

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
generated `useMutation`, and Calls run as commands use `useCallMutation`.
Transfers that show in the navbar use the background-Tasks layer, while
imperative cache work uses QueryClient directly. Feature code does not
invent query keys/functions or use raw mutations. Guard tests
(`frontend/src/test/guards/apiLayering.test.ts`) enforce that boundary.

Route loaders use `loadRouteQueries` from `src/routes/-loader.ts` to wait for the request transport and pass typed Call descriptors to the shared browser QueryClient. It requires a live `isUpdateBlocked` getter from router context rather than the mux, rechecks it after readiness, and rejects without querying while an update is active. Loader failures propagate to TanStack Router; intent preloads are marked speculative so QueryCache suppresses their global error toast.

The same guard file also fences the byte/mux-level transport primitives (`encodeString`, `bindStreamHandlers`, `getStreamMux`, …): feature code opens typed Channels with `openChannel(route, request)` and uses the stream lifecycle hooks (`useLiveStream`/`useLogStream`/`useStreamResult`). Only a short, shrink-only allowlist may import lower-level primitives from `@/api`.

Call descriptors compose with normal React Query options, including `select` for transformed output data. `useCallMutation` and `useMutation` take an `ActionConfig` — `invalidates` (query keys, static or derived from result/variables), `success`/`error` (toast message strings or callbacks; the error string is only a fallback, the server error message wins), `warning` (an extractor like `(result) => result.warning`; a non-empty return fires a warning toast that replaces the string-form success toast — invalidation and callback-form `success` still run), `toast` (toast metadata such as an action link), and `options` as a raw React Query options escape hatch.

### Choosing a member (decision table)

| Situation | Use |
| ---| ---|
| Data rendered by this component | `useQuery(callDescriptor)`. For on-demand panels, compose `enabled` and other options at the callsite. |
| Data needed inside an effect, loader, or event handler, then handed to something else | `queryClient.fetchQuery(callDescriptor)` when cache sharing matters; `call(route, request)` for transport-only work. |
| Call invoked as a command (validate, generate download, resolve path) | `useCallMutation(endpoint, config)` for declarative pending/error/feedback behavior. |
| Single bounded mutation triggered by one user action | `useCallMutation(endpoint, config)`, fired with `mutate`. |
| Route generated as a Mutation (`RouteMode` `mutation`) | `endpoint.useMutation(config)`, with `onProgress` when the UI renders progress and `watch(taskId, variables)` in a `useActiveTaskRecovery` callback to adopt the running job after a reload. |
| Several mutations sequenced or looped in one flow (batch delete, multi-step save) | Create the actions **configless** with a comment, `await mutateAsync` per step/item inside `try/catch`, and aggregate the outcome into one toast. The catch owns flow control only — never re-toast an error a config already toasted. |
| Long-running transfer that must outlive the page and show in the navbar | The background-Tasks layer (`useBackgroundTaskActions`), not a component-local Task action. |
| Optimistic updates / seeding cache from a result | QueryClient `cancelQueries`/`setQueryData` with the descriptor's `queryKey`. |

Refreshes after mutations come from the invalidation manifest by default — do not add `refetch()` calls or `onSuccess` refresh props on top of it; if a list does not refresh, fix the manifest entry instead.

Query invalidation is manifest-driven: `frontend/src/api/operation-query-invalidations.ts` maps each Call or Mutation route to the Call caches it makes stale, using the same centralized query keys. It is the single source of truth for mutation lifecycles — `useCallMutation` and `useMutation` use it as the default `invalidates` for locally awaited work, and the recovered-Tasks stream applies it to Tasks that finish with no local handler (page reload, another session). Call sites only pass `invalidates` to override the manifest (`[]` opts out; a function derives keys from result/variables). The few path-precise File Browser invalidations use QueryClient directly and are source-guarded.

Input is generated from the Go request contract:

| Go request shape | Frontend input | Wire request |
| ---| ---| ---|
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
| ---| ---|
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
snapshot the client reattaches with `tasks.watch {taskId: id}` instead. Each is
tried once, so a resend that reached the job still reattaches once; a further
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

Mutation whose runner takes its task:

```go
apischema.MutationTask("docker.compose", func(ctx context.Context, task *bridgeipc.Task, req apischema.DockerComposeRequest) (ComposeTaskResult, error) {
    return runDockerComposeTask(ctx, task, rt.Username(), rt.Store, req)
}, apischema.Privileged(), apischema.Progress[ComposeTaskMessage](), apischema.WithTaskMetadata(func(req apischema.DockerComposeRequest) bridgeipc.TaskMetadata {
    return bridgeipc.TaskMetadata{Identity: []string{req.ProjectName}, Label: "Docker compose " + req.Action, Action: req.Action, ProjectName: req.ProjectName}
})),
```

The route's `ComposeTaskResult` is both the generated terminal-result contract
and the runner's Go return type. `WithTaskMetadata` names what the tray and
recovery show, and its identity is the job's target.

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
| ---| ---| ---|
| `config.get` | `void -> AppConfig` | Retry-safe read of bridge-owned functional settings. |
| `config.set` | `ConfigSetPayload -> ConfigSetResult` | Partial update of functional settings. |
| `config.get_ui` | `void -> UIConfig` | Retry-safe read of effective UI preferences, including backend-owned defaults. |
| `config.set_ui` | `ConfigUISetPayload -> ConfigSetResult` | Full UI snapshot replacement; omitted fields resolve to backend defaults. |

`AppConfig.appSettings` contains only `showHiddenFiles` and `chunkSizeMB`.
Docker, job, and dismissal fields remain on `AppConfig`. Among the dismissals,
`historySeenAt` (on `Dismissals` and on `ConfigDismissalsPayload`) is when the
user last saw a loaded job history, as an RFC 3339 time: `config.set` refuses
a value that does not parse with 400 and keeps the stored one, stores a valid
one in UTC with nanoseconds (trailing zeros trimmed) and never later than the
bridge's clock, an empty value clears it, and a payload changes only the dismissals it names. The notice at
sign-in reads it ([Alerts](./alerts.md#navbar)). `UIConfig` contains
theme, navigation, collapsed-section, view-mode, layout-order, and related
presentation fields. The frontend combines these responses into its internal
effective configuration; that combined type is not a wire contract.

`UIConfig.collapsedSections` maps a page key to the names of the sections
collapsed on that page. The frontend owns both names: a page declares its
sections as an `as const` tuple beside them (`SectionPage` in
`frontend/src/hooks/useSectionToggles.ts`), which types every read and toggle,
and the bridge stores the names without reading them. A section that is not
listed is expanded, so adding a section, or a sectioned page, changes no Go
and writes no default. `config.set_ui` trims the names and drops blank ones,
repeats and pages with nothing collapsed; it refuses with 400 a map of more
than 64 pages, a page with more than 32 sections, or a name longer than 64
bytes, and `ui.yaml` is held to the same bounds.

`UIConfig.themeColors` holds the colour overrides of each scheme as a map
from `ThemeColorKey` to a CSS colour. The keys, their order in `ui.yaml` and
the colours a new configuration starts with are one table,
`themeColorDefs` in `backend/bridge/internal/userconfig/themecolors.go` (an
empty default there means the key has none: it stays unset and the UI derives
the colour, as `codeInsetBackground` and `panePictureBackground` do), so a
new theme colour is one line of Go: the stored type, validation, defaults and
both conversions iterate the table, and `apischema.StringEnums` publishes its
keys as the `ThemeColorKey` union. The generator renders a Go map keyed by a
string enum as `Partial<Record<Enum, Value>>`, which keeps the keys typed on
the frontend; any other map stays `Record<string, Value>`. On `config.set_ui`
a key that is absent or `null` is left out of the stored overrides, so like
any omitted UI field it resolves to the backend default on the next read; an
invalid colour is refused, and a key that is not a theme colour is refused
with 400 by the handler, because a map lets the request decoder accept any
key. `ui.yaml` is read over the defaults: a key replaces that colour, a
`null` key or scheme unsets it (the bridge itself never writes one), and an
unknown key or scheme fails the strict decode.

`system.get_capabilities` uses the same pattern: its result is a map from
`CapabilityName` to `CapabilityStatus`. Its names live in the capability
registry in `backend/bridge/handlers/capabilities`, which imports `apischema`
and so cannot be imported by it; that package adds them to
`apischema.StringEnums` when it is loaded, and the generator loads it through
the handler registry. See [Capabilities](./capabilities.md#wire-shape).

WireGuard interface hooks use it too: `WireGuardInterface.hooks` (from
`wireguard.list_interfaces`) and `WireGuardUpdateInterfaceRequest.hooks` map a
`WireGuardHookKind` to its command lines. The kinds are wg-quick's
`[Interface]` keys, and `apischema.WireGuardHookKinds` is the one list of them
in the order the config file holds them; the handler reads, writes, compares,
applies and lists hooks by looping over it, and refers by name only to PostUp
and PostDown, where it adds the NAT rules. The list response carries every
kind, with `[]` for one the interface does not use. The update is partial: a
kind that is absent or `null` is unchanged, an empty list removes that kind,
blank lines are dropped, and a kind outside the list is refused with 400.

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

## Notification settings

The alert routes are all `Privileged()`; [Alerts](./alerts.md) describes the
settings file and what each producer raises. The wire types live in
`backend/bridge/apischema/alerts.go`, `schedules.go` and `packages.go`.

| Route | Contract | Semantics |
| ---| ---| ---|
| `alerts.get_notifications` | `void -> NotificationSettingsView` | Read. Over a stored file that does not parse it answers the defaults with `settingsUnreadable: true` instead of an error, and logs the warning `notification settings unreadable, showing the defaults`; any other read error fails. |
| `alerts.set_notifications` | `NotificationSettings -> NotificationSettingsView` | One locked read, change and write of `notifications.json`. Keeps the stored per-schedule choices. Over a file that does not parse it starts from the defaults; when the request is valid, the old file is copied to `notifications.json.unreadable` (root `0600`, replacing an older one; a failed copy fails the save) just before the new one replaces it, so the new file has neither those choices nor the SMTP password. A refused request (400) leaves the unparsable file as it is and keeps no copy. The frontend refetches `alerts.get_notifications` and `alerts.list` after it. |
| `alerts.test_notifications` | `void -> SuccessResponse` | Sends a test alert to every configured channel, whatever `levels` says. |
| `alerts.list`, `alerts.mark_all_seen` | `void -> AlertList` | List, count as unseen and mark seen only the alerts whose level `levels` ticks for the browser (`error` always). |

- `NotificationChannels` is `{ browser, email, webhook }`.
- `NotificationSettings` (the request) gains `levels?: Record<string,
  NotificationChannels>` (keys `info`, `warning`, `error`) and `report?:
  Record<string, string>` (keys `docker_updates`, `scheduled_scripts`,
  `snapshots`, `resync`, `browser_jobs`). A sent map decides alone: a level or
  kind it leaves out takes its default, and `{}` is a sent map. A request
  without them, which an older page sends, sets the levels from its
  `minSeverity`, the Docker report from its `dockerUpdates`, and keeps the
  other stored kinds. `minSeverity` and `dockerUpdates` stay on both types;
  the stored file's copies are what an older build acts on. With `levels`
  sent, the request's `minSeverity` is neither used nor validated and is
  stored as derived from the levels; with `report` sent, the same holds for
  `dockerUpdates`.
- `NotificationSettingsView` gains `levels` and `report`, which always name
  every level and kind, and `settingsUnreadable?: boolean`. It never carries
  the SMTP password, only `smtpPasswordSet`.
- A refused request answers **400**: an unknown kind or level, a value the kind
  does not offer, a webhook URL that is not http or https, a bad SMTP setting
  or address (the last three answered as server errors before). The message
  reads, for example, `invalid arguments: invalid notification settings:
  invalid report "every_run" for browser_jobs`.
- `ScheduleStatus.notify: string` is the schedule's own choice: `failures`,
  `every_run`, or `""` for the line for scheduled scripts. It is read from
  `notifications.json`; over a file that does not parse, `schedules.list` and
  `schedules.get` show `""` and log it at debug level.
- `ScheduleCreateRequest.notify?: string` stores a choice with the new
  schedule, before its files are written, and takes it back when the create
  saved no definition. `ScheduleUpdateRequest.notify?: string` keeps the
  stored choice when absent and clears it when `""`. An update whose options
  are the stored ones (the same definition revision) and whose choice
  differs writes `notifications.json` alone: no unit file, script, definition
  or timer changes, so it is accepted while the script runs. An update that
  changes the options stores the choice after the checks that can refuse it
  and before the units change. Any other value answers 400. With a settings
  file that does not parse, an update that names a choice (even `""`) or a
  create that names one answers **409** (`conflict: the notification settings
  could not be read; saving them on the Notifications settings page repairs
  them: parse …`) and changes nothing; a stored file that parses but that
  validation refuses answers 409 too (`the stored notification settings are
  not valid; saving them on the Notifications settings page repairs them`);
  an update or create without a choice works.
  `schedules.delete` clears the choice and resolves the schedule's
  `jobs/finished-<id>` notice, and only warns when either cannot be done; the
  frontend refetches `alerts.list` after it.
  The generated `schedules.create` now takes the request object (its kind
  changed from `field` to `object`).
- `AutoUpdateState.options.notify` reads `on_failure` on a host with no
  notify drop-in (an older build read `never` there and flagged it with
  `failures_unreported`, which is gone): a failed update is reported there
  by the run-record hook.
  `support.notify_policies` offers `on_failure`, `on_change` and `always` on
  APT and `on_failure` and `always` on DNF and Mint; `never` is offered by
  none but stays in `AutoUpdateNotify` and is still accepted by
  `updates.set_auto_updates`, which then writes no drop-in.

## Tasks and jobs

A Mutation runs as a **job**: a Task in the task service of the caller's job
host, identified by the client's UUID, with a record in the job store. A
`BrowserBound()` Mutation runs as a session-owned task in the bridge. Both are
discovered and controlled through the same `tasks.*` routes. Bounded writes
that are still Calls complete through a single direct `useCallMutation`
request/response.

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
any tab or login of the same uid; `tasks.get`, `tasks.watch` and `tasks.cancel`
of a job the host no longer holds in memory are answered from its record.

Refusals arrive as the error frame, before any job starts:

| Code | Cause |
| ---| ---|
| `400` | The `id` is not a canonical non-zero UUID, or the request fails decoding. |
| `403` | A `Privileged()` route from a session that is not sudo-authorized. |
| `409` | Same target: `<route> is already running on this target`. |
| `409` | The `id` was used for another route or request. |
| `409` | The host already runs 256 jobs (`too many running jobs`), a safety valve. |
| `409` | `update_pending: LinuxIO update waits for N running jobs` (`1 running job`), `update_pending: LinuxIO was updated; new jobs can start shortly` when the older host runs no job, or `update_pending: LinuxIO update waits for running jobs` when the bridge cannot read its store: the job host refused the bridge's hello because it is of an older build, and an update waits for its jobs to end. The shared mutation wrapper's toast shows the text after the `update_pending:` prefix. The `BrowserBound()` routes run in the bridge and are not refused. Which reads still work is listed under [Ended jobs, saved output and updates](#ended-jobs-saved-output-and-updates). |
| `503` | `job host unavailable`: the bridge has no connection to its job host yet; it keeps reconnecting. `tasks.get`, `tasks.cancel`, `tasks.watch` and `tasks.output` of a host job, and `tasks.history`, first wait up to 10 s for it. `job host is stopping` while the host shuts down. |

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
other login gets a host running as that user. The root host runs each admin's
jobs with handlers acting for that admin and their config store, as their
bridge would, while same-target admission spans all of them. See
[Process & Systemd Architecture](./process-systemd-architecture.md#login-and-jobs)
for how hosts start.

The host runs each job's handler in a goroutine whose context ends only on
`tasks.cancel`, a stream abort, or the host stopping. The bridge keeps all Calls,
Channels, `tasks.data`, and the `BrowserBound()` Mutations,
which it runs itself on the same stream.

### Task admission

Tasks and jobs run side by side: there is no queue, no per-user or per-route
limit, no start-rate limit and no runtime cap. A task ends when it finishes or
is canceled. These checks refuse with `409` instead of waiting:

- **Same target.** A second job of a Mutation route whose request `Target()`
  or `WithTaskMetadata` identity matches a running one (the same pool, disk,
  container, compose project or set of paths) is refused, as the kernel or device would refuse it. The target stays
  taken until the first handler has exited, even after a cancel. A request type
  without `Target()` has no target and is never refused as a duplicate.
- **Exclusive routes** (`apischema.Exclusive()`) run one at a time because
  they own a whole system resource: `packages.update`,
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
route, owner, target, state, timestamps, the full decoded request, the job's
public metadata, and the result or error. No API returns the request. The
target is the job's identity, stored once: the metadata does not repeat it,
and every snapshot of the record (`tasks.get`, `tasks.watch`, `tasks.list`,
`tasks.events`, a resend's answer) shows it as `metadata.identity`. A record
over the 1 MiB limit sheds, in this order, its result, its target, its
request and, last, its metadata, so a batch of thousands of paths keeps its
record. The host writes a job's live
progress to `/run/linuxio/jobs/<uid>/<id>.progress`, at most twice a second,
never into the record; the file is removed with the record. The one exception
is the network bridge handoff, which keeps its latest progress step and a
checkpoint (its phase, state and deadlines) in its record, because the job
stays running until a decision or the native rollback window ends it.

States are `running`, `completed`, `failed`, `canceled` and `interrupted`; an
`interrupted` record carries its reason: `shutdown`, `crash` or `reboot`. The
record is written when the job starts and on every state change, and fsynced
only when a job that ran longer than one second reaches a terminal state or when
its checkpoint or its cancel request changes. A job that completes in under one
second is deleted from the store. Terminal records are kept 30 days and at most
200 per store (the root host's holds every admin's jobs), pruned when a host
starts and every 24 hours while it runs. The runs of timers have a store and
limits of their own ([Scheduled runs](#scheduled-runs)).

`tasks.cancel` and a stream abort write `cancel_requested` to the record first
and only then cancel the job's context. The snapshot stays `running` while the
handler cleans up and carries `cancel_requested: true`: `tasks.cancel` returns
it, and `tasks.events` sends a `task.snapshot` frame for the change, so a client
can show the job as cancelling and withhold a second cancel. A watch stream
carries no such frame, only progress and the outcome. The record becomes
`canceled` only when the handler returns, whatever error it returns after the
cancel; the job's target stays taken until then. A job with `cancel_requested`
is never resumed, and a host stop that overlaps the cancel leaves it to end
`canceled`.

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
the last shutdown or a power loss interrupted: a record `interrupted` with
reason `shutdown` or `reboot`, still `Resumable`, with no `cancel_requested`,
whose executor ran in an earlier boot. Every other `interrupted` record (a
crash, a host stopped by hand in this boot, a cancel that overlapped a stop)
loses `Resumable` when a host starts, so a job is resumed once and never at a
later reboot. The outcome is written to the same record. There is no manual
resume.

### Scheduled runs

Work a systemd timer starts is not a job: no route starts it and no job host
runs it. Each run writes a record of its own from two hooks on its unit
([Process & Systemd Architecture](./process-systemd-architecture.md#scheduled-runs)):
`/var/lib/linuxio/jobs/scheduled/<id>.json`, with a copy of its output beside
it. The root job host reads those records, so the run shows in `tasks.list`
and `tasks.events` while it runs. Five task types:

| Type | Unit | `metadata.label` | `metadata.identity` |
| ---| ---| ---| ---|
| `scheduled.script` | `linuxio-schedule-<id>.service` | the schedule's name, `Scheduled task` when its definition cannot be read | `[<schedule id>]` |
| `scheduled.btrfs_snapshot` | `linuxio-btrfs-snapshot.service` | `Snapshots and replication` | none |
| `scheduled.btrfs_resync` | `linuxio-btrfs-resync@<uuid>.service` | `Pool resync` | `[<pool uuid>]` |
| `scheduled.docker_update` | `linuxio-docker-update.service` | `Container updates` | none |
| `scheduled.system_update` | the distribution's automatic update service: `apt-daily-upgrade.service`, `dnf-automatic.service`, `dnf5-automatic.service` or `mintupdate-automation-upgrade.service` | `System updates` | none |

- **Who sees them.** Only the root host serves them, so every admin session
  sees every scheduled run and other users see none. The snapshot's `owner`
  is root; its `id` is the run's systemd invocation ID written as a UUID.
- **`tasks.list`** adds the scheduled runs that run.
- **`tasks.events`** reads the scheduled store every 2 s while a client
  listens: a `task.snapshot` for each run that runs when the stream opens and
  for each run that begins, a `task.progress` when a run's progress file
  changes (the btrfs timer program and the Docker update runner write one;
  scripts report none, their feedback is their output), and the terminal
  event when it ends. It lists the store again after a record changes and at
  least every 30 s. A run shorter than one poll may never be sent.
- **`tasks.get`** answers for a scheduled run from its record, running or
  ended.
- **`tasks.cancel`** of a running scheduled run answers `409` `a scheduled run
  is stopped on its page`, and **`tasks.watch`** answers `409` `a running
  scheduled run is followed through tasks.events`. A script stops with
  `schedules.stop` and a timer's resync with `storage.btrfs_scrub_cancel`;
  both set `cancel_requested` on the record first, so the run ends `canceled`.
- **Outcomes.** `completed`; `failed` with a message such as `exited with
  status 3`, `killed by signal KILL` or `timed out`; `canceled`; `interrupted`
  with reason `shutdown` when the system was stopping as the run ended. A
  `running` record counts only while systemd runs the record's invocation in
  this boot; otherwise the host marks it `interrupted`, `reboot` for a run of
  an earlier boot and `crash` for one whose end hook never ran.

The Schedules page follows the latest run's output with `logs.service.follow`
and its `invocationId` ([Streams](#streams)). Its past runs and their saved
output come from `tasks.history` with the run's `unit` and from `tasks.output`
([Ended jobs, saved output and updates](#ended-jobs-saved-output-and-updates)).

### Ended jobs, saved output and updates

Three Calls serve the history page, a schedule's past runs and the update
banner. The bridge forwards each to the caller's job host, and answers them
itself while a LinuxIO update waits for an older host.

**`tasks.history`**: `TaskHistoryRequest` to `TaskHistory`, `RetrySafe()`,
open to every session.

| Request member | Meaning |
| ---| ---|
| `before` | The cursor: the end time of the last entry of the previous page, exactly as received (its `finished_at`, else `started_at`, else `created_at`; the times carry nanoseconds that a JavaScript `Date` drops). Absent: the newest page. |
| `limit` | Absent or 0: 100; more than 500 is capped. |
| `unit` | Only the runs of this systemd unit, from the root host's scheduled store. |

The answer holds `entries` and `more` (set when older entries remain). An
entry (`TaskHistoryEntry`) is `task`, a slim `TaskSnapshot` of the ended job,
`reason` (`shutdown`, `reboot` or `crash` for an interrupted job), `unit` (a
scheduled run's unit) and `output` (set when the run's saved output exists).
The slim snapshot keeps what a list of ended work shows (type, owner, times,
state, error, the display members of `metadata`) and leaves out the rest, so
a page of large records stays well inside a relay frame:

- no entry carries `result` or `progress`;
- a host job's entry carries no `metadata.identity` and no
  `metadata.packageIds`; a scheduled run keeps `metadata.identity`, which
  names its schedule or pool;
- `error.message` is cut to at most 4096 bytes, on a character boundary, with
  `…` appended when cut.

`tasks.get` of the same ID still answers the full snapshot. Entries are
terminal records, newest first by end time. A page never
splits entries that ended at the same instant: it grows past `limit` to take
all of them, and the next page starts strictly before that instant. The root
host merges its own jobs with the scheduled runs, so every admin session sees
every admin's jobs and every scheduled run; a user's host lists that user's
jobs and never a scheduled run. A job that completed in under one second left
no record and is not listed.

Errors: `400` for a negative `limit` or a request that fails decoding; `503`
when no job host connection comes within 10 s. While an update waits the
answer is an empty page at once: the older host cannot be asked.

**`tasks.output`**: `TaskOutputRequest` to `TaskOutput`, `RetrySafe()`,
`Privileged()`.

| Request member | Meaning |
| ---| ---|
| `taskId` | The scheduled run's ID. |
| `offset` | Where the chunk starts, in bytes. Absent: the chunk is the end of the output. |
| `limit` | Bytes. Absent or 0: 256 KiB; more than 1 MiB is capped. |

The answer is `content`, `offset` (where `content` starts) and `size` (of the
whole saved output, at most 10 MiB). `content` is valid UTF-8, with invalid
bytes replaced by U+FFFD. A chunk starts on a character boundary: one asked to
start inside a character starts after it, at most three bytes later, and
reports that `offset`. A client that ends each earlier chunk at the `offset`
the later chunk reported therefore cuts no character. The viewer on the history
page and the Schedules page reads the end first, opens scrolled to it, and
reads earlier chunks on request, keeping the text in view where it was. Only
the root host keeps saved outputs.

Errors: `403` for a session that is not sudo-authorized, refused before the
handler runs; `400` for an empty `taskId`, a negative `offset` or a negative
`limit`; `404` when the run saved no output, the ID is not a job ID, or the host
keeps no scheduled store (a user's host); `503` when no job host connection
comes within 10 s, as while an update waits.

**`tasks.host`**: `NoRequest` to `TaskHostStatus`, `RetrySafe()`, open to
every session. The answer is `update_pending`, set when the job host is of an
older build and refused the bridge's hello; `running`, the number of jobs the
host still runs; and, only while an update waits, `jobs`, their running
snapshots: label and the other display members of `metadata` and the last
progress, without `metadata.identity`, `metadata.packageIds` or
`cancel_requested`. The bridge waits up to 10 s until it has a connection or
its last connection attempt was refused for an older build; a failed attempt
does not end the wait. With neither in that time it answers
`update_pending: false, running: 0`. With a host of this build it asks the
host. While an update waits it cannot ask the older host, so it reads that
host's records: the bridge runs as the host's uid and reads that uid's job
store read-only, counting each `running` record of executor kind `host` whose
process still lives, with the progress file the host last wrote. After the
older host exits, the answer stays `update_pending: true, running: 0` until the
bridge's next connection attempt, at most 30 s later, starts and reaches the
new host.

While an update waits, the `tasks.*` routes behave as follows:

| Answered by the bridge from the older host's records | Answered empty at once | Fail with `503` after waiting 10 s for a host |
| ---| ---| ---|
| `tasks.host` | `tasks.history`, `tasks.list` (the bridge's own tasks only); `tasks.events` sends none of the host's jobs | `tasks.get`, `tasks.watch`, `tasks.cancel` and `tasks.output` of a host job |

Every new Mutation that the job host would run is refused with `409` and one
of the three `update_pending` messages of the refusal table above (with the
count, without jobs, or with the store unreadable). The three `BrowserBound()` Mutations,
`filebrowser.upload`, `filebrowser.upload_batch` and `filebrowser.archive`, run
in the bridge and are not affected, nor are Calls and Channels that do not go
to the job host. There is no
fallback for a job host of the same protocol built before these three routes:
it answers them `route not found`, and the last release (v0.52.0) has no job
host.

### Task lifetime, ownership, and activity

A `BrowserBound()` Mutation (`filebrowser.upload`, `upload_batch`, `archive`)
is owned by the exact authenticated `SessionID`; logout, expiry, session
deletion, bridge failure, or bridge shutdown cancels it, and its cleanup removes
the partial upload or the archive.

A Mutation job is owned by the numeric uid of its job host, not by a session.
Logout, session expiry, bridge exit and connection loss only detach from it;
the host keeps running it, and any login of the same uid sees it through
`tasks.list` and `tasks.events` and can watch or cancel it. The host holds a
terminal job in memory for 30 minutes. The username and `session_ref` in the
bridge's hello are recorded as the job's owner for display; authorization is
the host's uid. On the root host a live snapshot's `owner.uid` is the host uid
(0) while the stored record holds the real user's uid, so clients read
`owner.username` only.

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

A failed job rejects its Mutation with a `LinuxIOError` carrying the job's
error message and code; a completed one resolves with the typed result.
`endpoint.start` resolves with the first snapshot instead and leaves the
outcome to `tasks.watch`, `tasks.events` or the recovered-Tasks stream.

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
endpoint binds `detail` to its declared `Progress[T]()` type, so a feature-owned UI retains its concrete
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
| ---| ---|
| `tasks.get` | Fetch one owned Task or job snapshot. |
| `tasks.list` | List owned Tasks and the jobs the job host holds; on the root host also the scheduled runs that run. |
| `tasks.history` | A page of the ended jobs of the job host, newest first; on the root host also the scheduled runs. Open to every session. See [Ended jobs, saved output and updates](#ended-jobs-saved-output-and-updates). |
| `tasks.output` | Privileged. A chunk of a scheduled run's saved output. See [Ended jobs, saved output and updates](#ended-jobs-saved-output-and-updates). |
| `tasks.host` | Whether a LinuxIO update waits for the job host, and for which of its jobs. Open to every session. See [Ended jobs, saved output and updates](#ended-jobs-saved-output-and-updates). |
| `tasks.cancel` | Cancel one owned Task or job. |
| `tasks.watch` | Common progress envelope plus typed detail and terminal result. Closing detaches; aborting cancels. |
| `tasks.data` | Upload/download/archive data stream. Bridge only. |
| `tasks.events` | Owner-wide lifecycle events and coalesced common progress. An optional `ids` request field names jobs the caller saw running; the stream sends the snapshot of each that has ended (at most 256 are looked up). |

The bridge and the job host both serve `tasks.*`. The bridge answers
`tasks.get`, `tasks.cancel` and `tasks.watch` for its own Tasks and forwards a
job ID (a UUID) it does not hold to its host. `tasks.list` appends the host's
jobs to the bridge's Tasks, and `tasks.events` merges the host's event stream
into the bridge's, reopening it after the bridge reconnects to its host with
the `ids` of the jobs it saw running, so one stream shows both and a job that
ended while no host was connected is seen ending. The host's list is what it
holds in memory: running jobs and terminal ones for 30 minutes. Records from
before a host restart are not listed yet; `tasks.get`, `tasks.watch` and
`tasks.cancel` of such a job are answered from its record.

`jobs.hello` is an internal route, not generated for the frontend: the first
stream of a connection to a job host, carrying the protocol number and the
session facts. The host serves no manual resume; it resumes interrupted jobs
itself at start.

The `tasks.*` namespace is reserved by `bridgeipc`. The `tasks` handler family
provides the generated route catalog, and its registration callback asks the
router's `TaskService` to install the matching Call and Channel
implementations. The general router has no route-prefix dispatch branch.

## Streams

Channels are multiplexed over `/ws`. Use typed `openChannel()` or an existing
payload-specific helper instead of constructing envelopes directly.

| Function | Route | Use |
| ---| ---| ---|
| `openTerminalStream(cols, rows)` | `terminal.open` | Host shell. |
| `openContainerStream(containerId, shell, cols, rows)` | `container.open` | Container shell. |
| `openChannel("docker.logs.follow", request)` | `docker.logs.follow` | Direct server-producing container log Channel. |
| `openChannel("docker.logs.follow_all", request)` | `docker.logs.follow_all` | Direct server-producing merged log Channel for every running container; NDJSON records `{id, name, ts, line, stderr?}`, containers that start later are attached. |
| `openChannel("logs.service.follow", request)` | `logs.service.follow` | Direct server-producing unit log Channel. With `invocationId` (32 lowercase hex characters, `400` otherwise) it follows one run: only the lines of that invocation's own processes, control characters kept (`journalctl --all`), in the `short-iso` format a scheduled run's saved output uses. |
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

A new route that changes the host is a `Mutation` and, for work that reports
progress or may run long, not a `Call`. The binding entry
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
| ---| ---|
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

### 7. One Definition, Checked Rules

Goal: a fact is written once, and a rule is enforced by a test, not by this
document.

Each change to LinuxIO is made one feature at a time and is correct by
itself. What goes wrong goes wrong between features: a third answer to a
question two other places already answer, a list retyped beside the place
that owns it, a default stored where the reader was meant to derive one.
Review and unit tests do not catch these, because each copy is right when it
is written. A test that fails the build is what catches them. LinuxIO has
such guards for handler layout, context propagation, error and logging
conventions, import boundaries, the generated types and the frontend styling
boundary. The inconsistencies found so far were all in a rule with no guard,
a rule whose guard covers only a hand-kept list, or a question no rule
answered.

Rules:

1. A list of names, a limit, a default, a validation rule or a safety check
   has one definition. A table that other code iterates
   (`themeColorDefs` in `backend/bridge/internal/userconfig/themecolors.go`,
   the capability registry in `backend/bridge/handlers/capabilities/registry.go`,
   `apischema.WireGuardHookKinds`) is the usual shape.
2. Values that reach the frontend cross through the generator. A named
   string type listed in `apischema.StringEnums` becomes a TypeScript union;
   the entry takes its values from the Go definition (as `ThemeColorKey`,
   `CapabilityName` and `WireGuardHookKind` do), not from a second literal.
   A plain `string` field whose valid values a handler checks is a candidate
   for such a type. The frontend then cannot hold a different copy.
3. A frontend form applies the rule the server applies. Where the rule
   cannot be published as a type (a pattern, a numeric bound), the response
   carries the value or the form defers to the server's refusal; it does not
   keep its own looser or stricter version.
4. A rule comes with its guard. A guard that works from a hand-kept list is
   deny-by-default: it fails on anything not listed as an exception, so a new
   item cannot pass by not being mentioned.
5. A value derived while unset is not given a default by another layer.
   `ui.yaml` is read over `DefaultUIPreferences()`, so a default written
   there, or saved back by the next `config.set_ui`, replaces the derivation
   for good. `codeInsetBackground` and `panePictureBackground` have no
   backend default for this reason.

Where LinuxIO does not yet follow these rules is listed in
[the consistency backlog](./TODO/consistency-backlog.md).

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
