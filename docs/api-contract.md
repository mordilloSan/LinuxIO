# API Contract

This is the canonical guide for LinuxIO's Go-owned API contract between the frontend and the bridge.

## Summary

- Go owns route names, modes, request types, and result types. Route declarations live with each handler family's registration in `backend/bridge/handlers/<domain>/handlers.go`.
- TypeScript API files under `frontend/src/api/generated` are generated. Do not edit them by hand.
- API requests use JSON stream-open envelopes: `{"route":"handler.command","request":{...}}`.
- The relay/mux framing is still binary for stream multiplexing, terminal bytes, and job data.
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
| `backend/bridge/handlers/<domain>/handlers.go` | One `apischema.Bindings(...)` table per handler family. Each entry contains the route contract and the typed handler binding together. |
| `backend/bridge/handlers/register.go` | Single handler-family composition table. Runtime registration, codegen, and tests all read from this one list. Edit this only when adding a new handler family. |
| `backend/bridge/apischema/contracts.go` | Shared request structs and small shared responses. |
| `backend/bridge/apischema/models.go` | API response/domain models reflected into TypeScript. |
| `backend/bridge/apischema/schema.go` | Contract helpers, request decoders, and typed registration adapters. |
| `backend/common/tools/linuxio-api-gen` | Generator for frontend client/types/route metadata. |
| `frontend/src/api/generated/client.ts` | Generated concrete `linuxio` object. |
| `frontend/src/api/generated/linuxio-types.ts` | Generated API models and schema types. |
| `frontend/src/api/generated/route-metadata.ts` | Generated route mode metadata. |

## Frontend API Files

| File | Role |
|------|------|
| `frontend/src/api/index.ts` | Public barrel. Feature code should import from `@/api`. |
| `frontend/src/api/react-query.ts` | Endpoint factory: direct Promise call, React Query hooks (`useQuery`, `useJobAction`, `useJobStreamAction`), query keys/options, route mode checks, retry policy, request shaping. |
| `frontend/src/api/linuxio-core.ts` | Low-level JSON request path over the stream multiplexer. API internals only. |
| `frontend/src/api/linuxio.ts` | Stream utilities, connection hooks, stream openers, and job-backed stream wrappers. |
| `frontend/src/api/StreamMultiplexer.ts` | WebSocket stream multiplexer, relay frame encoding, stream lifecycle, singleton connection management. |
| `frontend/src/api/stream-helpers.ts` | Helpers for binding stream callbacks, awaiting result frames, and writing byte chunks. |
| `frontend/src/api/jobs.ts` | Job snapshot guards, local job-handling tracking, and `waitForJobCompletion()`. |
| `frontend/src/api/job-state.ts` | Shared terminal job-state predicate. |
| `frontend/src/api/capabilities.ts` | Frontend capability manifest and state helpers. |

## Route Modes And Kinds

Every route has one mode:

| Mode | Use |
|------|-----|
| `bridgeipc.ModeQuery` | Read-only, bounded request/response work. |
| `bridgeipc.ModeJob` | Mutations, cancellable work, long-running reads, logs, subscriptions. |
| `bridgeipc.ModeDuplex` | Interactive bidirectional sessions such as terminals. |

Every route has one schema kind:

| Kind | Go signature |
|------|--------------|
| `KindHandler` | `func(context.Context, TRequest, bridgeipc.Events) error` |
| `KindRunner` | `func(context.Context, *bridgeipc.Job, TRequest) (any, error)` |
| `KindDuplex` | `func(context.Context, net.Conn, TRequest) error` |

Use `apischema.NoRequest` for no request payload and `apischema.NoResponse` for no result payload. They are API contract marker types owned by `apischema`.

## Frontend Shape

```typescript
import { linuxio } from "@/api";

const { data: unit } = linuxio.systemd.get_unit_info.useQuery("ssh.service", {
  refetchInterval: 2000,
});

// Loader/effect reads go through a cache-backed fetcher, not bare endpoint
// calls or the query client:
const fetchDirSize = linuxio.filebrowser.dir_size.useFetcher();
const size = await fetchDirSize("/srv/data");

// Query routes used as event-driven commands get mutation ergonomics:
const validateCompose = linuxio.docker.validate_compose.useAction({
  error: "Validation failed",
});
const result = await validateCompose.mutateAsync({ content });

const startContainer = linuxio.docker.start_container.useJobAction({
  invalidates: [linuxio.docker.list_containers.queryKey()],
  success: "Container started",
  error: "Failed to start container",
  toast: { href: "/docker", label: "Open Docker" },
});
startContainer.mutate({ containerId });
```

Every generated endpoint exposes:

| Member | Use |
|--------|-----|
| `endpoint(...input)` | Framework-agnostic Promise call. API/jobs infrastructure only — feature code uses the hooks. |
| `endpoint.useQuery(...input, options?)` | React Query hook for render-driven reads on query routes. |
| `endpoint.useQueries(inputs, options?)` | `useQuery` over a dynamic list of inputs — one query per input, sharing the singular hook's cache entries and stream-mux gating. |
| `endpoint.useAction(config?)` | Mutation-style hook for query routes used as event-driven commands (validation, download generation, path resolution): `mutate`/`mutateAsync` with pending state and declarative toasts, no query caching. |
| `endpoint.useJobAction(config?)` | React Query hook for job routes: awaits job completion, unwraps the job result, declarative invalidation/toasts. |
| `endpoint.useJobStreamAction(config?)` | Job-route hook with live progress: starts the job, attaches to its stream, and surfaces `onJobStart`/`onOpen`/`onProgress` plus the `useJobAction` config. The returned mutation also exposes `attach(job, variables)`, which adopts an already-running job (page-reload recovery via `useActiveJobRecovery`) into the same config lifecycle — progress, toasts, invalidation, and pending state behave exactly as if `mutate(variables)` had started it. |
| `endpoint.useFetcher()` | Hook returning a stable imperative fetch through the query cache — for loaders and effects that need data at call time (chart backfill, lazy tree loads, workflow pre-checks). Same input shape and options as `useQuery`. |
| `endpoint.useCache()` | Hook returning a stable typed cache handle: `get`/`set` for one request's entry (optimistic updates, seeding an action's result), `invalidate`/`remove`/`cancel` for one entry or — with no input — the whole endpoint. |
| `endpoint.queryKey(...input)` | Stable React Query key. |
| `endpoint.queryOptions(...input, options?)` | Options object for API-layer plumbing (route loaders and the `useFetcher` implementation); feature code uses the hooks. |

Feature code (route-owned `-components/`, shared `components/`, `contexts/`, and non-jobs `hooks/`) never imports `@tanstack/react-query` at all: render-driven reads go through `useQuery`/`useQueries`, event-driven commands through `useAction`, writes through `useJobAction`/`useJobStreamAction` or the background-jobs layer, imperative loader/effect reads through `useFetcher`, and cache manipulation through `useCache`. The primitives live in `src/api/` and `src/hooks/backgroundJobs/` (with route loading/context/provider infrastructure as the only other React Query touchpoints); guard tests (`frontend/src/constants/apiLayering.test.ts`) enforce the boundary.

Route loaders use `loadRouteQueries` from `src/routes/-loader.ts` to wait for the request transport and return typed `endpoint.queryOptions(...)` results from the shared browser QueryClient. It requires a live `isUpdateBlocked` getter from router context rather than the mux, rechecks it after readiness, and rejects without querying while an update is active. Loader failures propagate to TanStack Router; intent preloads are marked speculative so QueryCache suppresses their global error toast.

The same guard file also fences the byte/mux-level transport primitives (`encodeString`, `bindStreamHandlers`, `getStreamMux`, …): feature code consumes streams through the `open*Stream` factories and the stream lifecycle hooks (`useLiveStream`/`useLogStream`/`useStreamResult`), and only a short, shrink-only allowlist of low-level consumers may import the primitives from `@/api`.

`useQuery` and `queryOptions` both accept normal React Query options, including `select` for transformed output data. `useAction` and `useJobAction` instead take an `ActionConfig` — `invalidates` (query keys, static or derived from result/variables), `success`/`error` (toast message strings or callbacks; the error string is only a fallback, the server error message wins), `warning` (an extractor like `(result) => result.warning`; a non-empty return fires a warning toast that replaces the string-form success toast — invalidation and callback-form `success` still run), `toast` (toast meta `{ href, label }` for notification-history links), and `options` as a raw React Query options escape hatch.

### Choosing a member (decision table)

| Situation | Use |
|-----------|-----|
| Data rendered by this component | `useQuery` (list of inputs: `useQueries`) — even for on-demand panels: gate with `enabled` and state instead of fetching imperatively. |
| Data needed inside an effect, loader, or event handler, then handed to something else (chart backfill, editor content, lazy tree loads) | `useFetcher` — add `staleTime/gcTime: CACHE_TTL_MS.NONE` when the read must not be cached. |
| Query route invoked as a command (validate, generate download, resolve path) where the result is consumed by the flow, not displayed | `useAction` — declarative toasts + `isPending`; omit the config when a surrounding workflow owns error handling (say so in a comment). |
| Single mutation triggered by one user action | `useJobAction` with declarative config (`success`/`warning`/`error` strings; side effects via a `success` callback or `options.onSettled`). Fire with `mutate`. |
| Mutation whose live progress the UI renders, or that must survive page reload via re-attach | `useJobStreamAction` (+ `attach` with `useActiveJobRecovery` for recovery). Don't pick it just to customize an error string. |
| Several mutations sequenced or looped in one flow (batch delete, multi-step save) | Create the actions **configless** with a comment, `await mutateAsync` per step/item inside `try/catch`, and aggregate the outcome into one toast. The catch owns flow control only — never re-toast an error a config already toasted. |
| Long-running transfer that must outlive the page and show in the navbar | The background-jobs layer (`useBackgroundJobActions`), not a job action. |
| Optimistic updates / seeding cache from a result | `useCache` handles; pair `cancel` before an optimistic `set`. |

Refreshes after mutations come from the invalidation manifest by default — do not add `refetch()` calls or `onSuccess` refresh props on top of it; if a list does not refresh, fix the manifest entry instead.

Query invalidation is manifest-driven: `frontend/src/constants/routeInvalidations.ts` maps each job route to the query caches it makes stale, and is the single source of truth for both lifecycles — `useJobAction`/`useJobStreamAction` use it as the default `invalidates` for locally awaited jobs, and the recovered-jobs stream applies it to jobs that finish with no local handler (page reload, another session). Call sites only pass `invalidates` to override the manifest (`[]` opts out; a function derives keys from result/variables). A guard test (`frontend/src/constants/routeInvalidations.test.ts`) keeps ad-hoc `queryClient.invalidateQueries` calls out of feature code.

Input is generated from the Go request contract:

| Go request shape | Direct/query input | Wire request |
|------------------|--------------------|--------------|
| `apischema.NoRequest` | `linuxio.system.get_cpu_info()` | `{}` |
| one required JSON field | `linuxio.filebrowser.dir_size(path)` | `{ "path": path }` |
| multi-field or optional object | `linuxio.docker.system_prune(request)` | `request` |

Job actions use the full generated request object as their mutation variable:

```typescript
linuxio.jobs.cancel.useJobAction().mutate({ jobId });
linuxio.docker.start_container.useJobAction().mutate({ containerId });
```

## Backend Handler Shapes

Handler route:

```go
var api = apischema.Bindings(
    apischema.Query[apischema.UnitNameRequest, apischema.UnitInfo](
        "systemd.get_unit_info",
    ).Handle(handleGetUnitInfo),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
    api.Register(router)
}

func handleGetUnitInfo(ctx context.Context, req apischema.UnitNameRequest) (apischema.UnitInfo, error) {
    return GetUnitInfo(ctx, req.UnitName)
}
```

Codegen and route coverage read `Routes`, which is derived from the binding table. Runtime registration also reads the same binding table, so a normal route is declared once.

Runner route:

```go
var packageUpdateRoutes = packageUpdateBindings().Routes()

func packageUpdateBindings() apischema.BindingSet {
    return apischema.Bindings(
        apischema.Runner[apischema.PackageUpdateRequest, apischema.JobSnapshot](
            "packages.update",
        ).Run(runPackageUpdateJob, bridgeipc.SingletonSystem),
    )
}

func RegisterJobRoutes(router *bridgeipc.Router) {
    packageUpdateBindings().Register(router)
}
```

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

## Jobs

All actions are jobs, including fast atomic mutations. If a job completes before the initial response is written, the initial `JobSnapshot` is already terminal. Otherwise the frontend can attach to shared job lifecycle streams.

On the frontend, `useJobAction` awaits the terminal state via `waitForJobCompletion()`: a failed job rejects with a `LinuxIOError` carrying the job's error message/code, and `useJobAction` resolves with the unwrapped `JobSnapshot.result`. If the attach stream cannot be opened (mux dropped between job start and attach), `waitForJobCompletion` falls back to polling `jobs.get` until the job is terminal — it never resolves mid-job; `useJobStreamAction` instead fails fast in that situation because it promises live progress, and the recovered-jobs stream picks the job up. Jobs awaited this way are marked locally handled so the background-jobs toasts do not duplicate them; `useJobStreamAction` accepts `markHandled: false` when the recovered-jobs stream should keep ownership of completion (progress rendered locally, toasts owned globally).

Built-in job routes:

| Route | Use |
|-------|-----|
| `jobs.get` | Fetch one owned job snapshot. |
| `jobs.list` | List owned jobs. |
| `jobs.cancel` | Cancel one owned job. |
| `jobs.attach` | Progress/result stream. |
| `jobs.data` | Upload/download/archive data stream. |
| `jobs.events` | Lifecycle event stream. |

The `jobs.*` namespace is reserved by `bridgeipc`.

## Streams

Streams are multiplexed over `/ws`. Use exported stream openers instead of constructing envelopes directly.

| Function | Route | Use |
|----------|-------|-----|
| `openTerminalStream(cols, rows)` | `terminal.open` | Host shell. |
| `openContainerStream(containerId, shell, cols, rows)` | `container.open` | Container shell. |
| `openDockerLogsStream(containerId, tail)` | `docker.logs.follow` | Job-backed container logs. |
| `openServiceLogsStream(serviceName, lines)` | `logs.service.follow` | Job-backed unit logs. |
| `openGeneralLogsStream(...)` | `logs.general.follow` | Job-backed journal logs. |
| `openAppUpdateStream(runId, version?)` | `control.app_update` | Job-backed app update output. |
| `openJobAttachStream(jobId)` | `jobs.attach` | Job progress/result. |
| `openJobDataStream(jobId, offset?)` | `jobs.data` | Binary job data. |
| `openJobEventsStream()` | `jobs.events` | Job events. |

Terminal and container streams are true duplex routes. Logs and app update expose stream-shaped frontend helpers, but their backend lifecycle is a job.

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
    apischema.Query[apischema.PackageSearchRequest, apischema.PackageSearchResult](
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
const result = await linuxio.packages.search(query);
```

For a stream-only route, set `NoEndpoint: true` in the route spec and add a focused stream opener in `frontend/src/api/linuxio.ts`.

Keep each route contract in the same binding table that attaches its handler or runner, even when the public route name belongs to a different frontend namespace. For example, `appupdate` owns the `control.version` binding because it owns the implementation, and `packages` owns the `system.install_capability` binding because it runs the installer job.

## Privilege

Declare privilege in the route spec:

```go
var api = apischema.Bindings(
    apischema.Job[apischema.NoRequest, apischema.NoResponse](
        "control.reboot",
        apischema.Privileged(),
    ).Handle(handleReboot),
)
```

The dispatcher checks the authenticated session before running the route. Handlers may still validate operation-specific policy, but they should not duplicate the route-level admin gate.

## Remaining Plan

The current contract shape is intentionally JSON-first and Go-owned. Runtime route binding is typed, and TypeScript generation still reads Go type metadata. The remaining cleanup is about making that boundary easier to reason about; frontend hook-surface refinements (like `useJobAction`) live in `react-query.ts` and do not touch the generated contract.

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

### 2. Generated Request Decoder Plan

JSON envelopes are the current transport contract:

```json
{"route":"handler.command","request":{}}
```

The current request decode path uses `encoding/json`. That keeps the runtime small and the payloads readable, but Go's JSON package interprets struct tags through runtime reflection. If we decide that request decoding should also be codegen-owned, the next step is generated JSON request decoders, not protobuf.

The developer workflow should not change:

1. Write or reuse the Go request struct.
2. Add the route binding.
3. Run `make generate`.

The generator then emits both frontend TypeScript and backend request decode code. Developers should not hand-write per-route decoders.

Example source contract:

```go
type ContainerIDRequest struct {
    ContainerID string `json:"containerId"`
}

var api = apischema.Bindings(
    apischema.Job[apischema.ContainerIDRequest, apischema.NoResponse](
        "docker.start_container",
    ).Handle(handleStartContainer),
)
```

Generated backend code would be conceptually:

```go
func decodeContainerIDRequest(raw []byte) (apischema.ContainerIDRequest, error) {
    // Generated from the Go struct:
    // - raw must be a JSON object
    // - "containerId" is allowed once
    // - unknown fields are rejected
    // - duplicate fields are rejected
    // - "containerId" must be a JSON string
    // - required fields are enforced by generated presence checks
}
```

Runtime flow:

```text
JSON envelope
    |
    v
generated route-specific request decoder
    |
    v
typed Go request struct
    |
    v
typed handler / runner / duplex function
```

This keeps JSON on the wire and keeps Go structs as the source of truth. It does not introduce `.proto` files or make frontend TypeScript the contract source.

Generated request decoders would give us:

- no request decode reflection in LinuxIO's API path
- duplicate field rejection
- unknown field rejection
- required field enforcement based on struct field presence and tags
- route/field-specific errors
- one generated decoder registry wired into route registration

Non-goals for this phase:

- Do not generate response encoders. Responses are produced by trusted Go code and can keep using `encoding/json`.
- Do not replace the relay frame format. Only the JSON request decoder changes.
- Do not add hand-written validators for every route.
- Do not move the source of truth from Go structs to `.proto` files.

#### Implementation Slices

1. Add a generated decoder registry target.
   - Extend `backend/common/tools/linuxio-api-gen` to emit `backend/bridge/apischema/generated_decoders.go`.
   - The generated file maps route names to `bridgeipc.RequestDecoder` functions.
   - `apischema.RouteSpec` registration uses the generated decoder when present.

2. Start with a narrow supported type set.
   - structs
   - strings, booleans, numbers
   - pointers for optional fields
   - slices and maps of supported values
   - nested structs
   - `json:"name"` and `json:"name,omitempty"` tags
   - `json:"-"` fields ignored

3. Fail loudly for unsupported request shapes.
   - The generator should return a clear error naming the route, request type, and unsupported field.
   - Avoid silent fallback for routes that claim to be generated.

4. Add golden tests.
   - valid object
   - unknown field
   - duplicate field
   - missing required field
   - wrong scalar type
   - optional field omitted
   - nested object
   - array
   - map
   - no-request route

5. Roll out behind a temporary fallback.
   - During migration, generated decoders can cover supported routes.
   - Unsupported routes keep the strict stdlib decoder until the generator supports them.
   - Remove the fallback once all request types are covered.

6. Keep the route authoring model stable.
   - Normal route additions still happen in one `apischema.Bindings(...)` table.
   - New request structs still live in Go.
   - `make generate` updates frontend types/client and backend decoders together.

#### Decision Boundary

Stay with `encoding/json` if we only need:

- JSON envelopes
- typed Go request structs
- readable payloads
- low generator complexity
- acceptable runtime reflection inside Go's JSON package

Move to generated JSON request decoders if we require:

- no runtime reflection on request decode
- duplicate-key rejection
- required-field enforcement
- precise route/field errors
- contract validation generated from Go structs

Consider protobuf-style codegen only if we are willing to:

- make schema files the protocol source of truth
- give up plain JSON payloads for the request body
- maintain generated codec packages on both Go and TypeScript sides

#### Why Not Response Encoders First

Requests are the untrusted input boundary. They are small, route-specific, and security-sensitive. Generated request decoders improve validation exactly where external input enters the bridge.

Responses are different: LinuxIO's own Go code creates them. They are often larger and more varied, and generated response encoders would add substantially more generated code for less safety gain. Keep response encoding on `encoding/json` unless profiling proves it matters.

#### Immediate Low-Churn Strictness

Before generated decoders, the small standard-library improvement is to switch request decoding from `json.Unmarshal` to `json.Decoder` with `DisallowUnknownFields()` and a trailing-token check. That catches unknown top-level fields and malformed trailing JSON without changing the transport or adding generated backend code.

This is not a substitute for generated decoders because it does not reject duplicate keys and does not enforce required fields. It is only the low-churn guardrail if we want stricter behavior before a generator pass.

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
await linuxio.system.get_cpu_info();
await linuxio.jobs.cancel(jobId);
linuxio.system.get_cpu_info.useQuery();
linuxio.docker.start_container.useJobAction({ invalidates, success, error });
```

Remaining cleanup:

1. Keep `frontend/src/api/generated/*` generated only.
2. Keep `frontend/src/api/react-query.ts` as the small runtime factory for direct calls and React Query hooks (including `useJobAction`).
3. Keep stream helpers in `frontend/src/api/linuxio.ts` because streams are not normal request/response endpoints.
4. Avoid adding another hand-written typed API layer.

### 6. Verification Gates

Before considering this API contract work settled, run:

```bash
make generate
cd backend && go test ./...
make tsc-only
make lint-only
make golint-only
make build-vite
git diff --check
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
cd backend && go test ./...
make tsc-only
```

For broader frontend changes, also run:

```bash
make lint-only
make build-vite
git diff --check
```
