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
| `backend/bridge/handlers/<domain>/handlers.go` | One `RouteSpec` per route in that handler family plus the typed route-to-handler binding. |
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

Use `apischema.NoRequest()` for no request payload and `apischema.NoResponse()` for no result payload. Typed handlers receive `bridgeipc.NoRequest` when the route has no request.

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
| `bridgeipc.NoRequest` | `linuxio.system.get_cpu_info()` | `{}` |
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
var routes = apischema.NewRouteCatalog()

var RouteGetUnitInfo = routes.Query(
    "systemd.get_unit_info",
    apischema.TypeOf[apischema.UnitNameRequest](),
    apischema.TypeOf[apischema.UnitInfo](),
)

var Routes = routes.All()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
    apischema.RegisterRoutes(router,
        RouteGetUnitInfo.Handle(handleGetUnitInfo),
    )
}

func handleGetUnitInfo(ctx context.Context, req apischema.UnitNameRequest, emit bridgeipc.Events) error {
    result, err := GetUnitInfo(ctx, req.UnitName)
    return bridgeipc.EmitResult(emit, result, err)
}
```

The route spec is package-level because codegen and route coverage read `Routes`. The handler binding sits beside it in the same file as `RouteGetUnitInfo.Handle(handleGetUnitInfo)`, so the contract and dispatch target are reviewed together.

Runner route:

```go
var routes = apischema.NewRouteCatalog()

var RouteUpdate = routes.Runner(
    "packages.update",
    apischema.TypeOf[apischema.PackageUpdateRequest](),
    apischema.TypeOf[apischema.JobSnapshot](),
)

var Routes = routes.All()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
    apischema.AttachRunner(router, RouteUpdate.Run(runPackageUpdateJob, bridgeipc.SingletonSystem))
}
```

Duplex route:

```go
var routes = apischema.NewRouteCatalog()

var RouteOpen = routes.Duplex(
    "terminal.open",
    apischema.TypeOf[apischema.TerminalOpenRequest](),
    apischema.NoResponse(),
    apischema.NoEndpoint(),
)

var Routes = routes.All()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
    apischema.AttachDuplex(router, RouteOpen.Duplex(func(ctx context.Context, stream net.Conn, req apischema.TerminalOpenRequest) error {
        return HandleTerminalSession(ctx, rt, stream, req)
    }))
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

1. `backend/bridge/handlers/<domain>/handlers.go` for the `RouteSpec`, typed handler adapter, and registration in `RegisterHandlers`.

If the request or response type is new, also add the Go struct in `backend/bridge/apischema/contracts.go` or `backend/bridge/apischema/models.go`.
If the handler family is new, add one entry to `backend/bridge/handlers/register.go`.

The practical checklist:

1. Define or reuse exported Go request/response structs in `backend/bridge/apischema`.
2. Add one named route spec to `backend/bridge/handlers/<domain>/handlers.go`.
3. Implement the typed handler, runner, or duplex function in that handler package.
4. Bind it in the same file with `.Handle(...)`, `.Run(...)`, or `.Duplex(...)`.
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
var RouteSearch = routes.Query(
    "packages.search",
    apischema.TypeOf[apischema.PackageSearchRequest](),
    apischema.TypeOf[apischema.PackageSearchResult](),
)

var Routes = routes.All()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
    apischema.RegisterRoutes(router,
        RouteSearch.Handle(handlePackageSearch),
    )
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

## Privilege

Declare privilege in the route spec:

```go
var RouteReboot = routes.Job(
    "control.reboot",
    apischema.NoRequest(),
    apischema.NoResponse(),
    apischema.Privileged(),
)
```

The dispatcher checks the authenticated session before running the route. Handlers may still validate operation-specific policy, but they should not duplicate the route-level admin gate.

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
