# Frontend API

The LinuxIO frontend API is exported from `frontend/src/api/index.ts` and should be consumed through `@/api`.

Go owns the route, request, and response contracts in `backend/bridge/apischema`. The frontend imports generated TypeScript from `frontend/src/api/generated`.

## File Map

| File | Role |
|------|------|
| `index.ts` | Public barrel. Re-exports the generated `linuxio` client, route metadata, stream openers, job helpers, capability helpers, connection helpers, and generated API types. |
| `generated/client.ts` | Generated concrete `linuxio` object. Each route calls `createEndpoint(handler, command, requestShape)`. Do not edit. |
| `generated/linuxio-types.ts` | Generated API model and schema types reflected from Go structs. Includes `LinuxIOSchema`, `CommandInput`, `CommandRequest`, and `CommandResult`. Do not edit. |
| `generated/route-metadata.ts` | Generated `ROUTE_MODES` map plus `routeName()` and `getRouteMode()`. Do not edit. |
| `react-query.ts` | Endpoint factory. Adds the direct Promise call, React Query hooks, query keys/options, mode checks, retry policy, and scalar-to-object request wrapping. |
| `linuxio-core.ts` | Low-level JSON request path over the stream multiplexer. App code should not call this directly except API internals. |
| `linuxio.ts` | Stream-facing utilities: `useStreamMux`, `useIsUpdating`, connection status helpers, duplex stream openers, and job-backed stream wrappers. |
| `StreamMultiplexer.ts` | WebSocket stream multiplexer, relay frame encoding, stream lifecycle, singleton connection management, and `Stream`/frame types. |
| `stream-helpers.ts` | Helpers for binding stream callbacks, awaiting result frames, and writing byte chunks. |
| `jobs.ts` | Job snapshot guards, local job-handling tracking, and `waitForJobCompletion()`. |
| `job-state.ts` | Shared terminal job-state predicate. |
| `capabilities.ts` | Frontend capability manifest and helpers for mapping capability wire responses into UI state. |

## Public API Shape

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
| `endpoint.queryOptionsWithSelect(...input, options?)` | Query options with typed `select` output. |

Input is generated from the Go request contract:

| Go request shape | Direct/query input | Wire request |
|------------------|--------------------|--------------|
| `bridgeipc.NoRequest` | `linuxio.system.get_cpu_info()` | `{}` |
| one required JSON field | `linuxio.filebrowser.dir_size(path)` | `{ "path": path }` |
| multi-field or optional object | `linuxio.docker.system_prune(request)` | `request` |

React Query mutations use the full generated request object as their mutation variable. That keeps mutation variables stable and self-describing:

```typescript
linuxio.jobs.cancel.useMutation().mutate({ jobId });
linuxio.docker.start_container.useMutation().mutate({ containerId });
```

## Transport

The browser opens a stream through the multiplexer and sends one JSON envelope as the `OpStreamOpen` payload:

```json
{"route":"docker.start_container","request":{"containerId":"abc"}}
```

For no-request routes, the frontend sends `{}`:

```json
{"route":"system.get_cpu_info","request":{}}
```

The relay frame format, WebSocket/yamux framing, stream data frames, progress frames, and result frames stay separate from the API contract. Result/progress payloads are already JSON.

## Route Modes

Route metadata is generated into `frontend/src/api/generated/route-metadata.ts`.

| Mode | Frontend surface |
|------|------------------|
| `query` | `endpoint()` / `endpoint.useQuery()` |
| `job` | `endpoint()` / `endpoint.useMutation()` |
| `duplex` | Stream opener such as `openTerminalStream()` |

The endpoint factory enforces mode use: query hooks reject non-query routes, and mutation hooks reject non-job routes.

## Jobs

Job routes return `JobSnapshot`.

```typescript
const snapshot = await linuxio.packages.update(["nginx"]);

if (snapshot.state === "completed") {
  console.log(snapshot.result);
}
```

Non-terminal jobs can be followed through the stream helpers:

```typescript
import { openJobAttachStream, waitForStreamResult } from "@/api";

await waitForStreamResult(openJobAttachStream(snapshot.id), {
  onProgress: (progress) => console.log(progress),
});
```

Built-in job routes:

| Route | Use |
|-------|-----|
| `jobs.get` | Fetch one owned job snapshot. |
| `jobs.list` | List owned jobs. |
| `jobs.cancel` | Cancel one owned job. |
| `jobs.attach` | Progress/result stream. |
| `jobs.data` | Upload/download/archive data stream. |
| `jobs.events` | Lifecycle event stream. |

Feature code should start feature routes directly, for example `linuxio.filebrowser.copy(...)` or `linuxio.docker.compose(...)`.

## Streams

Streams are multiplexed over `/ws`. App code should use exported stream openers instead of constructing envelopes directly.

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

Frontend endpoint files are generated. For a normal query or job route, do not edit `frontend/src/api/generated/*`.

1. Define or reuse Go request/response structs in `backend/bridge/apischema`.
   - Put shared request structs and small shared responses in `contracts.go`.
   - Put API response/domain models in `models.go`.
   - Use exported fields with JSON tags.
2. Add a `RouteSpec` to `backend/bridge/apischema/routes.go` with `Kind`, `Route`, `Mode`, `Request`, and `Result`.
3. Implement the typed handler, runner, or duplex function in the relevant `backend/bridge/handlers/...` package.
4. Register it from that package's `RegisterHandlers` using `apischema.RegisterRoutes`, `apischema.AttachRunner`, or `apischema.AttachDuplex`.
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
{Kind: KindHandler, Route: "packages.search", Mode: bridgeipc.ModeQuery, Request: TypeOf[PackageSearchRequest](), Result: TypeOf[PackageSearchResult]()},
```

```go
func handlePackageSearch(ctx context.Context, req apischema.PackageSearchRequest, emit bridgeipc.Events) error {
    result, err := SearchPackages(ctx, req.Query)
    return bridgeipc.EmitResult(emit, result, err)
}
```

```go
apischema.RegisterRoutes(router, "packages", []bridgeipc.Command{
    {Name: "search", Mode: bridgeipc.ModeQuery, Handler: handlePackageSearch},
})
```

After `make generate`, the frontend gets:

```typescript
const result = await linuxio.packages.search(query);
```

For a stream-only route, set `NoEndpoint: true` in the route spec and add a focused stream opener in `frontend/src/api/linuxio.ts`.

## Connection Lifecycle

Authentication owns the connection lifecycle:

- `initStreamMux()` after sign-in or session restore.
- `closeStreamMux()` on sign-out.
- `useStreamMux()` for current mux state.
- `useIsUpdating()` to pause query hooks during app update.

Read-like commands retry once on connection close. Mutation/job starts do not retry automatically.
