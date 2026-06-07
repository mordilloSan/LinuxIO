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
| `frontend/src/api/react-query.ts` | Endpoint factory: direct Promise call, React Query hooks, query keys/options, route mode checks, retry policy, request shaping. |
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

const cpu = await linuxio.system.get_cpu_info();
const size = await linuxio.filebrowser.dir_size("/srv/data");
const job = await linuxio.jobs.cancel("job-123");

const { data: unit } = linuxio.systemd.get_unit_info.useQuery("ssh.service", {
  refetchInterval: 2000,
});

const startContainer = linuxio.docker.start_container.useMutation();
startContainer.mutate({ containerId });
```

Every generated endpoint exposes:

| Member | Use |
|--------|-----|
| `endpoint(...input)` | Framework-agnostic Promise call. |
| `endpoint.useQuery(...input, options?)` | React Query hook for query routes. |
| `endpoint.useMutation(options?)` | React Query hook for job routes. |
| `endpoint.queryKey(...input)` | Stable React Query key. |
| `endpoint.queryOptions(...input, options?)` | Options for `queryClient.fetchQuery()` / `ensureQueryData()`. |

`useQuery` and `queryOptions` both accept normal React Query options, including `select` for transformed output data.

Input is generated from the Go request contract:

| Go request shape | Direct/query input | Wire request |
|------------------|--------------------|--------------|
| `apischema.NoRequest` | `linuxio.system.get_cpu_info()` | `{}` |
| one required JSON field | `linuxio.filebrowser.dir_size(path)` | `{ "path": path }` |
| multi-field or optional object | `linuxio.docker.system_prune(request)` | `request` |

React Query mutations use the full generated request object as their mutation variable:

```typescript
linuxio.jobs.cancel.useMutation().mutate({ jobId });
linuxio.docker.start_container.useMutation().mutate({ containerId });
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

func handleGetUnitInfo(ctx context.Context, req apischema.UnitNameRequest, emit bridgeipc.Events) error {
    result, err := GetUnitInfo(ctx, req.UnitName)
    return bridgeipc.EmitResult(emit, result, err)
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

func handlePackageSearch(ctx context.Context, req apischema.PackageSearchRequest, emit bridgeipc.Events) error {
    result, err := SearchPackages(ctx, req.Query)
    return bridgeipc.EmitResult(emit, result, err)
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

The current contract shape is intentionally JSON-first and Go-owned. Runtime route binding is typed, and TypeScript generation still reads Go type metadata. The remaining cleanup is about making that boundary easier to reason about, not changing the public frontend API again.

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

### 2. Decide JSON Codecs Versus Protobuf-Style Codegen

JSON envelopes are the current transport contract:

```json
{"route":"handler.command","request":{}}
```

The remaining question is how far to push generated transport code.

Option A: keep `encoding/json`.

- Lowest churn.
- Human-readable payloads.
- Still uses reflection internally inside Go's JSON package.
- Good enough unless request volume or payload size becomes a real problem.

Option B: generate Go JSON codecs.

- Keeps JSON on the wire.
- Removes most JSON reflection from hot paths.
- More generator work and more generated Go to review.
- Useful if we want strict generated decoders without adopting protobuf.

Option C: protobuf or protobuf-like schemas.

- Strongest generated transport boundary.
- Less readable wire payloads.
- Bigger migration because the frontend and Go bridge need generated codec packages.
- Best reserved for a deliberate transport project, not mixed into handler cleanup.

Recommendation for now: keep JSON envelopes, keep `encoding/json`, and only revisit generated codecs if profiling or schema drift justifies it.

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
linuxio.docker.start_container.useMutation();
```

Remaining cleanup:

1. Keep `frontend/src/api/generated/*` generated only.
2. Keep `frontend/src/api/react-query.ts` as the small runtime factory for direct calls and React Query hooks.
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
