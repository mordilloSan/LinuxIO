# Handler Patterns

Bridge handlers are route based. Pick the route mode first, then write the smallest handler shape that matches it.

## Mode Selection

| Need | Mode |
|------|------|
| Immediate read-only result | `Query` |
| Mutation/action | `Job` |
| Long-running read, log follow, watch feed, app update | `Job` |
| Interactive bidirectional session | `Duplex` |

When in doubt between `Query` and `Job`, ask: "can this change system state, run for a while, emit progress, or need cancellation?" If yes, use `Job`.

## File Naming

Handler packages should name files after the domain operation they implement, not after IPC mechanics.

| Location | Allowed naming | Avoid |
|----------|----------------|-------|
| `backend/common/ipc/bridge` | Framework terms such as `jobs.go`, `job_primitives.go`, `router.go` | Domain-specific handler code |
| `backend/bridge/handlers/<domain>` | Domain terms such as `package_update_operation.go`, `log_follow_operation.go`, `terminal_session.go`, `smart_test_operation.go` | Generic `jobs.go`, `stream.go`, `rpc.go`, `handler.go` |

Use `operation` for job-backed mutations or long-running work, `follow` for log/watch-style jobs, and `session` for true Duplex routes. The route mode still lives in the route table; the filename should help humans find the domain behavior without implying a second IPC subsystem.

## Query Pattern

```go
func handleList(ctx context.Context, args []string, emit bridgeipc.Events) error {
    result, err := listThings(ctx)
    return bridgeipc.EmitResult(emit, result, err)
}

router.Query("things.list", handleList)
```

Queries should be read-only and bounded. They emit one result and return.

## Job Pattern

Use `router.Job` for existing `Events` handlers and `router.JobRunner` when the implementation wants a `*bridgeipc.Job`.

```go
router.Job("things.create", handleCreate, bridgeipc.ActionDefault)
router.JobRunner("things.reindex", runReindex, bridgeipc.SingletonSystem)
```

Handlers registered with `router.Job` can report progress with `emit.Progress(...)` and set the final result with `emit.Result(...)`.

Runners registered with `router.JobRunner` report progress directly:

```go
func runReindex(ctx context.Context, job *bridgeipc.Job, args []string) (any, error) {
    job.ReportProgress(map[string]any{"phase": "scanning", "pct": 10})
    return doReindex(ctx)
}
```

Fast-complete behavior is automatic. A runner that returns quickly produces a terminal initial `JobSnapshot`; the runner does not need special code.

## Stream-Style Jobs

Logs, subscriptions, and watch feeds are Jobs with `bridgeipc.StreamDefault`.

For text data, emit a progress payload with `type: "data"`:

```go
job.ReportProgress(map[string]any{
    "type": "data",
    "data": line,
})
```

Frontend stream openers can adapt those job progress events back to `onData` bytes while the backend keeps one coherent lifecycle.

## Duplex Pattern

Only interactive bidirectional sessions should be Duplex:

```go
router.Duplex("terminal.open", func(ctx context.Context, stream net.Conn, args []string) error {
    return openTerminal(ctx, stream, args)
})
```

Use Duplex for terminals and container shells. Do not use it for one-way logs or progress.

## Arguments

Use `bridgeipc` helpers:

```go
name, err := bridgeipc.Arg(args, 0)
if err != nil {
    return err
}

req, err := bridgeipc.DecodeJSONArg[CreateRequest](args, 0)
if err != nil {
    return err
}
```

Return `bridgeipc.NewError(message, code)` for typed client errors.

## Privilege

Declare privilege in the route table:

```go
{Name: "set_hostname", Mode: bridgeipc.ModeJob, Handler: handleSetHostname, Privileged: true}
```

The dispatcher handles the check and logs the rejected start centrally.

## Frontend Contract

Frontend route metadata lives in `frontend/src/api/route-metadata.ts`.

| Backend mode | Frontend API |
|--------------|--------------|
| `Query` | `.useQuery()` or `.call()` |
| `Job` | `.useMutation()` or `.call()`, returning `JobSnapshot` |
| `Duplex` | stream opener |

`.useQuery()` rejects non-query routes and `.useMutation()` rejects non-job routes.
