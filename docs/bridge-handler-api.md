# Bridge Handler API

The bridge owns LinuxIO application semantics. The webserver authenticates the browser, relays opaque WebSocket/yamux bytes, and the bridge parses the first relay frame as a canonical route invocation.

## Package Split

Bridge IPC is split by responsibility:

| Package | Purpose |
|---------|---------|
| `backend/common/ipc/auth` | auth/bootstrap binary protocol |
| `backend/common/ipc/relay` | relay frames and yamux helpers |
| `backend/common/ipc/bridge` | route registry, dispatch, typed errors, privileges, jobs, lifecycle primitives |

Feature handlers should import `bridgeipc` from `backend/common/ipc/bridge`.

## Route Modes

Every backend route declares one mode:

| Mode | Contract |
|------|----------|
| `Query` | read-only, bounded, one result, dispatcher closes |
| `Job` | every mutation/action plus long-running reads, logs, subscriptions, and cancellable work |
| `Duplex` | interactive bidirectional sessions such as terminals |

The frontend mirrors this in `frontend/src/api/route-metadata.ts`.

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
first relay frame payload: <route>\0<arg1>\0...
    |
    v
bridgeipc.Dispatch(route, args, session)
```

The route name is the protocol name. There is no separate request/response transport namespace.

## Registering Routes

Use the router directly for explicit routes:

```go
router.Query("packages.list_installed", handleListInstalled)
router.JobRunner("packages.update", runPackageUpdate, bridgeipc.SingletonSystem)
router.Job("docker.start_container", handleStartContainer, bridgeipc.ActionDefault)
router.Duplex("terminal.open", openTerminal)
```

For command tables inside one component, `bridgeipc.RegisterRoutes` is a route-table helper:

```go
bridgeipc.RegisterRoutes(router, "docker", []bridgeipc.Command{
    {Name: "list_containers", Mode: bridgeipc.ModeQuery, Handler: h.handleListContainers},
    {Name: "start_container", Mode: bridgeipc.ModeJob, Handler: h.handleStartContainer},
})
```

The mode must be declared. Missing mode panics at registration time.

## Jobs

All actions are Jobs, including fast atomic mutations. Runner authors do not choose a fast or long path:

```go
func runStartContainer(ctx context.Context, job *bridgeipc.Job, args []string) (any, error) {
    id, err := bridgeipc.Arg(args, 0)
    if err != nil {
        return nil, err
    }
    return map[string]string{"id": id}, startContainer(ctx, id)
}
```

If the runner completes before the initial response is written, the initial `JobSnapshot` is already terminal. Otherwise the frontend can attach to shared job lifecycle primitives.

Terminal job state is committed under the job lock before observers are notified, so clients cannot see `completed` without a result or `failed` without an error.

## Job Primitives

Job lifecycle routes are built into `bridgeipc` and are handled before feature route lookup:

| Route | Purpose |
|-------|---------|
| `jobs.get` | get one owned job snapshot |
| `jobs.list` | list owned jobs |
| `jobs.cancel` | cancel one owned job |
| `jobs.attach` | attach to progress/result |
| `jobs.data` | attach binary job data |
| `jobs.events` | subscribe to owned job events |

The `jobs.*` namespace is reserved. Feature handlers cannot register routes there.

## Job Policies

Every Job route has a `JobPolicy`.

| Preset | Use |
|--------|-----|
| `ActionDefault` | normal frontend mutations |
| `SingletonSystem` | package/app updates and system-wide mutations |
| `StreamDefault` | logs, subscriptions, watch-style jobs |

The dispatcher centrally handles rate limits, queue limits, duplicate singleton starts, invalid arguments, forbidden routes, logging, and typed start failures.

## Errors

Use typed bridge errors where status matters:

```go
return bridgeipc.NewError("missing service name", 400)
```

Common helpers:

```go
value, err := bridgeipc.Arg(args, 0)
err := bridgeipc.RequireArgs(args, 2)
payload, err := bridgeipc.DecodeJSONArg[CreateRequest](args, 0)
return bridgeipc.EmitResult(emit, result, err)
```

## Privilege

Privileged routes declare it at registration:

```go
router.Job("control.reboot", handleReboot, bridgeipc.SingletonSystem, bridgeipc.Privileged)
```

or in command tables:

```go
{Name: "reboot", Mode: bridgeipc.ModeJob, Handler: handleReboot, Privileged: true}
```

The dispatcher checks the authenticated session before running the route.

## Relay Frames

Relay frames live in `backend/common/ipc/relay`:

```text
[opcode:1][streamID:4 big-endian][length:4 big-endian][payload:N]
```

| Opcode | Meaning |
|--------|---------|
| `OpStreamOpen` | initial route payload |
| `OpStreamData` | binary data |
| `OpStreamClose` | graceful close |
| `OpStreamResize` | terminal resize |
| `OpStreamProgress` | JSON progress payload |
| `OpStreamResult` | JSON result payload |
| `OpStreamAbort` | client cancellation |

JSON remains the control/result format for this refactor. Binary optimization is deferred until there is evidence it matters.
