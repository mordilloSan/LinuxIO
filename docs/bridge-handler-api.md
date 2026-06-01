# Bridge Handler API

The bridge owns LinuxIO application semantics. The webserver authenticates the browser and relays WebSocket/yamux bytes; the bridge parses the first relay frame as a JSON route invocation and dispatches to typed Go handlers.

## Package Split

| Package | Purpose |
|---------|---------|
| `backend/common/ipc/auth` | Auth/bootstrap protocol. |
| `backend/common/ipc/relay` | Relay frames, stream-open JSON envelope, yamux helpers, progress/result frames. |
| `backend/common/ipc/bridge` | Route registry, dispatch, typed errors, privileges, jobs, lifecycle primitives. |
| `backend/bridge/apischema` | Single source of truth for API routes, request/response contracts, registration adapters, and generated frontend types. |

Feature handlers usually import:

```go
import (
    "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
    bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)
```

## Core Flow

```text
browser /ws
    |
    v
webserver yamux relay
    |
    v
bridge accepts yamux stream
    |
    v
OpStreamOpen payload: {"route":"handler.command","request":{...}}
    |
    v
bridgeipc.Router decodes request through the apischema RouteSpec
    |
    v
typed handler / runner / duplex function
```

The route name is the protocol name. There is no separate request/response transport namespace.

## API Contract Ownership

`backend/bridge/apischema` is the contract source:

| File | Role |
|------|------|
| `routes.go` | One `RouteSpec` per route: route name, mode, kind, request type, result type, policy, privilege, and `NoEndpoint`. |
| `contracts.go` | Shared request structs and small shared responses. |
| `models.go` | API response/domain model structs reflected into TypeScript. |
| `schema.go` | Contract helpers, request decoders, and typed registration adapters. |

The generator at `backend/common/tools/linuxio-api-gen` reflects those contracts into:

- `frontend/src/api/generated/client.ts`
- `frontend/src/api/generated/linuxio-types.ts`
- `frontend/src/api/generated/route-metadata.ts`

Run `make generate` after contract changes.

## Route Modes And Kinds

Every route has one mode:

| Mode | Contract |
|------|----------|
| `bridgeipc.ModeQuery` | Read-only, bounded, one result, dispatcher closes. |
| `bridgeipc.ModeJob` | Mutations, cancellable work, long-running reads, logs, subscriptions. |
| `bridgeipc.ModeDuplex` | Interactive bidirectional sessions such as terminals. |

Every route also has one schema kind:

| Kind | Go signature |
|------|--------------|
| `KindHandler` | `func(context.Context, TRequest, bridgeipc.Events) error` |
| `KindRunner` | `func(context.Context, *bridgeipc.Job, TRequest) (any, error)` |
| `KindDuplex` | `func(context.Context, net.Conn, TRequest) error` |

Use `bridgeipc.NoRequest` / `apischema.NoRequest()` for no request payload and `bridgeipc.NoResponse` / `apischema.NoResponse()` for no result payload.

## Registering Handler Routes

Add the route spec:

```go
{Kind: KindHandler, Route: "systemd.get_unit_info", Mode: bridgeipc.ModeQuery, Request: TypeOf[UnitNameRequest](), Result: TypeOf[UnitInfo]()},
```

Implement the typed handler:

```go
func handleGetUnitInfo(ctx context.Context, req apischema.UnitNameRequest, emit bridgeipc.Events) error {
    result, err := GetUnitInfo(ctx, req.UnitName)
    return bridgeipc.EmitResult(emit, result, err)
}
```

Register it from the domain package:

```go
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
    apischema.RegisterRoutes(router, "systemd", []bridgeipc.Command{
        {Name: "get_unit_info", Mode: bridgeipc.ModeQuery, Handler: handleGetUnitInfo},
    })
}
```

`apischema.RegisterRoutes` looks up the route in `routes.go`, installs the generated JSON request decoder, validates the function signature, applies schema privilege/policy, and registers the route with `bridgeipc.Router`.

## Registering Job Runners

Use a runner when the implementation needs direct access to `*bridgeipc.Job`, for example progress reporting, job data, or lower-level cancellation behavior.

```go
func runPackageUpdateJob(ctx context.Context, job *bridgeipc.Job, req apischema.PackageUpdateRequest) (any, error) {
    job.ReportProgress(map[string]any{"phase": "starting"})
    return nil, UpdatePackages(ctx, req.PackageIDs)
}
```

```go
policy := bridgeipc.SingletonSystem
policy.Timeout = 2 * time.Hour

apischema.AttachRunner(router, apischema.RunnerBinding{
    Route:  "packages.update",
    Runner: runPackageUpdateJob,
    Policy: policy,
})
```

The route spec still owns the route name, mode, kind, request type, result type, privilege, and default policy. The binding can override policy when a route needs a tuned timeout or limit.

## Registering Duplex Routes

Duplex routes receive the raw `net.Conn` and a typed request:

```go
apischema.AttachDuplex(router, apischema.DuplexBinding{
    Route: "terminal.open",
    Handle: func(ctx context.Context, stream net.Conn, req apischema.TerminalOpenRequest) error {
        return HandleTerminalSession(ctx, rt, stream, req)
    },
})
```

Stream-only routes usually have `NoEndpoint: true` in the route spec. They appear in generated route metadata, but no React Query endpoint is generated for them.

## Emitting Results

Handlers should use `bridgeipc.EmitResult` for ordinary query/job responses:

```go
return bridgeipc.EmitResult(emit, result, err)
```

For progress:

```go
if err := emit.Progress(progress); err != nil {
    return err
}
```

Runner implementations can use `job.ReportProgress(progress)`.

## Jobs

All actions are jobs, including fast atomic mutations. If a job completes before the initial response is written, the initial `JobSnapshot` is already terminal. Otherwise the frontend can attach to shared job lifecycle primitives.

Terminal job state is committed under the job lock before observers are notified, so clients cannot see `completed` without a result or `failed` without an error.

## Job Primitives

Job lifecycle routes are built into `bridgeipc` and are handled before feature route lookup:

| Route | Purpose |
|-------|---------|
| `jobs.get` | Get one owned job snapshot. |
| `jobs.list` | List owned jobs. |
| `jobs.cancel` | Cancel one owned job. |
| `jobs.attach` | Attach to progress/result. |
| `jobs.data` | Attach binary job data. |
| `jobs.events` | Subscribe to owned job events. |

The `jobs.*` namespace is reserved. Feature handlers cannot register routes there.

## Job Policies

Every job route has a `JobPolicy`.

| Preset | Use |
|--------|-----|
| `ActionDefault` | Normal frontend mutations. |
| `SingletonSystem` | Package/app updates and system-wide mutations. |
| `StreamDefault` | Logs, subscriptions, watch-style jobs. |

The dispatcher centrally handles rate limits, queue limits, duplicate singleton starts, invalid requests, forbidden routes, logging, and typed start failures.

## Errors

Use typed bridge errors where status matters:

```go
return bridgeipc.NewError("missing service name", 400)
```

Request JSON is decoded before the handler runs. Validate semantic requirements in the handler:

```go
if req.ServiceName == "" {
    return bridgeipc.NewError("missing service name", 400)
}
```

## Privilege

Prefer declaring privilege in the route spec:

```go
{Kind: KindHandler, Route: "control.reboot", Privileged: true, Mode: bridgeipc.ModeJob, Request: NoRequest(), Result: NoResponse()},
```

The dispatcher checks the authenticated session before running the route.

## Relay Frames

Relay frames live in `backend/common/ipc/relay`:

```text
[opcode:1][streamID:4 big-endian][length:4 big-endian][payload:N]
```

| Opcode | Meaning |
|--------|---------|
| `OpStreamOpen` | Initial route payload: JSON `{"route":"...","request":...}`. |
| `OpStreamData` | Binary data. |
| `OpStreamClose` | Graceful close. |
| `OpStreamResize` | Terminal resize. |
| `OpStreamProgress` | JSON progress payload. |
| `OpStreamResult` | JSON result payload. |
| `OpStreamAbort` | Client cancellation. |

## Adding An Endpoint

1. Define or reuse exported Go request/response structs in `backend/bridge/apischema`.
2. Add one `RouteSpec` to `backend/bridge/apischema/routes.go`.
3. Implement the typed handler, runner, or duplex function.
4. Register it from the relevant `RegisterHandlers` function.
5. Run `make generate`.
6. Run at least `cd backend && go test ./...` and `make tsc-only`.

No generated frontend file should be edited by hand.
