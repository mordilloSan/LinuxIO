# Frontend API

The LinuxIO frontend API is exported from `frontend/src/api/index.ts` and should be consumed through `@/api`.

## Route Modes

Route metadata lives in `frontend/src/api/route-metadata.ts`.

| Mode | Frontend surface |
|------|------------------|
| `query` | `.useQuery()` / `.call()` for read-only operations |
| `job` | `.useMutation()` / `.call()` returning `JobSnapshot` |
| `duplex` | stream opener for interactive bidirectional sessions |

The hooks enforce this contract: query hooks reject non-query routes, and mutation hooks reject non-job routes.

## Typed API

```typescript
import { linuxio } from "@/api";

const { data } = linuxio.system.get_cpu_info.useQuery();

const start = linuxio.docker.start_container.useMutation();
start.mutate([containerId]);
```

Every endpoint exposes:

| Member | Use |
|--------|-----|
| `.useQuery(...params)` | React Query hook for query routes |
| `.useMutation(options?)` | React Query hook for job routes |
| `.call(...args)` | Promise call for imperative code |
| `.queryKey(...args)` | stable React Query key |
| `.queryOptions(...params)` | options for `queryClient.fetchQuery()` |

Arguments are serialized before transport:

| Value | Serialized As |
|-------|---------------|
| `undefined` | `""` |
| `string` | unchanged |
| object / array | `JSON.stringify(value)` |
| other primitives | `String(value)` |

## Jobs

All frontend mutations return a `JobSnapshot`.

```typescript
const job = await linuxio.packages.update.call("nginx");

if (job.state === "completed") {
  console.log(job.result);
}
```

Fast jobs may be terminal in the first response. Non-terminal jobs use built-in job helpers:

```typescript
import { openJobAttachStream, waitForStreamResult } from "@/api";

await waitForStreamResult(openJobAttachStream(job.id), {
  onProgress: (progress) => console.log(progress),
});
```

Built-in job routes:

| Route | Use |
|-------|-----|
| `jobs.get` | fetch one job |
| `jobs.list` | list jobs |
| `jobs.cancel` | cancel a job |
| `jobs.attach` | progress/result stream |
| `jobs.data` | upload/download/archive data stream |
| `jobs.events` | lifecycle event stream |

Feature code should start feature routes directly, for example `linuxio.filebrowser.copy.call(...)` or `linuxio.docker.compose.call(...)`.

## Streams

Streams are multiplexed over `/ws`. App code should use exported stream openers instead of constructing payloads.

| Function | Route | Use |
|----------|-------|-----|
| `openTerminalStream(cols, rows)` | `terminal.open` | host shell |
| `openContainerStream(containerId, shell, cols, rows)` | `container.open` | container shell |
| `openDockerLogsStream(containerId, tail)` | `docker.logs.follow` | job-backed container logs |
| `openServiceLogsStream(serviceName, lines)` | `logs.service.follow` | job-backed unit logs |
| `openGeneralLogsStream(...)` | `logs.general.follow` | job-backed journal logs |
| `openAppUpdateStream(runId, version?)` | `control.app_update` | job-backed app update output |
| `openJobAttachStream(jobId)` | `jobs.attach` | job progress/result |
| `openJobDataStream(jobId, offset?)` | `jobs.data` | binary job data |
| `openJobEventsStream()` | `jobs.events` | job events |

Terminal and container streams are true Duplex streams. Logs and app update expose a stream-shaped frontend helper, but the backend lifecycle is a Job.

## Stream Interface

```typescript
interface Stream {
  readonly id: number;
  readonly type: string;
  readonly status: "opening" | "open" | "closing" | "closed";
  write(data: Uint8Array): void;
  resize(cols: number, rows: number): void;
  close(): void;
  abort(): void;
  onData: ((data: Uint8Array) => void) | null;
  onClose: (() => void) | null;
  onProgress: ((progress: ProgressFrame) => void) | null;
  onResult: ((result: ResultFrame) => void) | null;
}
```

Use `close()` for graceful detaching and `abort()` for user cancellation. Job-backed stream helpers cancel their underlying Job when closed.

## Connection Lifecycle

Authentication owns the connection lifecycle:

- `initStreamMux()` after sign-in or session restore
- `closeStreamMux()` on sign-out
- `useStreamMux()` for current mux state
- `useIsUpdating()` to pause query hooks during app update

Read-like commands retry once on connection close. Mutation/job starts do not retry automatically.
