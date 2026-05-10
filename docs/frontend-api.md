# Frontend API

The LinuxIO frontend API is exported from `frontend/src/api/index.ts` and should be consumed through `@/api`. It has two surfaces:

1. Typed JSON request/response calls through `linuxio.<handler>.<command>`.
2. Multiplexed binary streams for terminals, logs, jobs, and data transfer.

## Architecture

```text
React components/hooks
        |
        v
@/api
        |
        +-- react-query.ts     typed linuxio proxy and React Query hooks
        +-- linuxio-core.ts    Promise request/response bridge call
        +-- linuxio.ts         stream openers and mux hooks
        +-- stream-helpers.ts  stream lifecycle helpers
        +-- StreamMultiplexer  singleton WebSocket stream mux
        +-- linuxio-types.ts   command schema and domain response types
        |
        v
/ws WebSocket -> bridge streams -> backend handlers/jobs
```

Authentication owns the connection lifecycle. `AuthContext` calls `initStreamMux()` after sign-in or session restore and `closeStreamMux()` on sign-out. The mux connects to `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}/ws`.

## Public Exports

Import from `@/api` unless you are editing API internals.

| Export | Source | Purpose |
|--------|--------|---------|
| `linuxio` | `react-query.ts` | Typed request/response endpoint proxy |
| `CACHE_TTL_MS` | `react-query.ts` | Common React Query TTL constants |
| `LinuxIOError` | `linuxio-core.ts` | Structured API error |
| `useStreamMux`, `useIsUpdating` | `linuxio.ts` | React hooks for mux state |
| `isConnected`, `getStatus` | `linuxio.ts` | Non-hook connection checks |
| `open*Stream` functions | `linuxio.ts` | Typed stream openers |
| `initStreamMux`, `closeStreamMux`, `waitForStreamMux`, `getStreamMux` | `StreamMultiplexer.ts` | Connection management |
| `encodeString`, `decodeString` | `StreamMultiplexer.ts` | UTF-8 helpers |
| `STREAM_MULTIPLEXER_CONFIG`, `configureStreamMultiplexer` | `StreamMultiplexer.ts` | Runtime mux configuration |
| `bindStreamHandlers`, `waitForStreamResult`, `streamWriteChunks` | `stream-helpers.ts` | Stream lifecycle utilities |
| `type *` | `StreamMultiplexer.ts`, `stream-helpers.ts`, `linuxio-types.ts` | Stream, helper, schema, and domain types |

## Typed JSON API

Use the typed proxy for normal request/response operations:

```typescript
import { linuxio, CACHE_TTL_MS } from "@/api";

const { data, isLoading, error } = linuxio.system.get_cpu_info.useQuery();
```

The available handlers and commands come from `frontend/src/api/linuxio-types.ts` (`LinuxIOSchema`). Adding or changing a backend command should be reflected there so `linuxio.<handler>.<command>` has correct args and result types.

### Endpoint Shape

Every command endpoint exposes:

| Member | Use |
|--------|-----|
| `.useQuery(...params)` | React Query hook for reads |
| `.useQueryWithSelect(...params)` | React Query hook with typed `select` output |
| `.useMutation(options?)` | React Query hook for writes |
| `.call(...args)` | Promise call for effects, contexts, and non-hook code |
| `.queryKey(...args)` | Stable React Query key |
| `.queryOptions(...params)` | Options object for `queryClient.fetchQuery()` / `ensureQueryData()` |
| `.queryOptionsWithSelect(...params)` | Query options with typed `select` output |

There is intentionally no `linuxio.call()` alias. Use the command endpoint's `.call()`.

### Query Arguments

`useQuery` accepts string arguments followed by an optional React Query options object:

```typescript
linuxio.filebrowser.resource_get.useQuery("/var/log");

linuxio.filebrowser.resource_get.useQuery("/var/log", "", "true", {
  staleTime: CACHE_TTL_MS.ONE_MINUTE,
});

linuxio.docker.list_containers.useQuery({
  staleTime: CACHE_TTL_MS.TWO_SECONDS,
});
```

For object or array arguments in a query-style endpoint, use the explicit `args` form. A plain object passed to `useQuery` is treated as options unless it has an `args` property.

```typescript
endpoint.useQuery({
  args: ["literal-arg", { complex: true }],
  staleTime: CACHE_TTL_MS.ONE_MINUTE,
});
```

Internally, all args are serialized before being sent to the bridge:

| Value | Serialized As |
|-------|---------------|
| `undefined` | `""` |
| `string` | unchanged |
| `object` / array | `JSON.stringify(value)` |
| other primitives | `String(value)` |

React Query keys use the serialized args:

```typescript
["linuxio", handler, command, ...serializedArgs]
```

### Mutations

Mutations always receive an argument array:

```typescript
const start = linuxio.docker.start_container.useMutation();
start.mutate([containerId]);

const setAutoUpdates = linuxio.dbus.set_auto_updates.useMutation();
setAutoUpdates.mutate([
  {
    enabled: true,
    frequency: "daily",
    scope: "security",
    download_only: true,
    reboot_policy: "if_needed",
    exclude_packages: [],
  },
]);
```

Invalidate or update matching query keys after successful writes:

```typescript
const queryClient = useQueryClient();

const remove = linuxio.docker.remove_container.useMutation({
  onSuccess: () => {
    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_containers.queryKey(),
    });
  },
});
```

### Imperative Calls

Use `.call()` from contexts, event handlers, or utilities:

```typescript
const caps = await linuxio.system.get_capabilities.call();
const job = await linuxio.jobs.start.call("file.indexer", "/srv/data");
```

For deduped cached reads outside a component, use endpoint query options:

```typescript
const config = await queryClient.fetchQuery(
  linuxio.config.get.queryOptions({ staleTime: CACHE_TTL_MS.NONE }),
);
```

### Query Enablement And Retries

`useQuery` and `useQueryWithSelect` are automatically disabled unless:

- The stream mux status is `"open"`.
- `useIsUpdating()` is `false`.
- The endpoint options do not set `enabled: false`.

Read-like commands retry once on `connection_closed`. The retry policy is applied to commands that start with `get_`, `list_`, or `validate_`, plus these explicit commands:

- `control.version`
- `filebrowser.dir_size`
- `filebrowser.indexer_status`
- `filebrowser.resource_get`
- `filebrowser.resource_stat`
- `filebrowser.search`
- `filebrowser.subfolders`
- `filebrowser.users_groups`
- `wireguard.peer_config_download`
- `wireguard.peer_qrcode`

## Streams

Streams are multiplexed over the same `/ws` WebSocket. Each stream has a transport frame (`streamID`, flags, payload) and an inner bridge frame (`opcode`, stream ID, payload length, payload). App code should use the exported stream openers instead of building payloads directly.

### Stream Interface

```typescript
interface Stream {
  readonly id: number;
  readonly type: StreamType;
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

`close()` sends a graceful close frame. `abort()` sends an abort frame and resets the stream, which is the right choice for user cancellation.

Only the `terminal` stream type is persistent and reused by `getStream("terminal")`. Other stream openers create a fresh stream each time.

### Stream Openers

| Function | Payload Type | Use |
|----------|--------------|-----|
| `openTerminalStream(cols, rows)` | `terminal` | Interactive host shell |
| `openContainerStream(containerId, shell, cols, rows)` | `container` | Docker exec terminal |
| `openDockerLogsStream(containerId, tail = "100")` | `docker-logs` | Live container logs |
| `openServiceLogsStream(serviceName, lines = "100")` | `service-logs` | Systemd unit logs |
| `openGeneralLogsStream(lines = "100", timePeriod = "", priority = "", identifier = "", fieldFilters = [])` | `general-logs` | Journal log stream |
| `openAppUpdateStream(runId, version?)` | `app-update` | LinuxIO self-update stream |
| `openJobAttachStream(jobId)` | `jobs-attach` | Attach to job progress/result stream |
| `openJobDataStream(jobId, offset = 0)` | `jobs-data` | Send or receive job data bytes |
| `openJobEventsStream()` | `jobs-events` | Subscribe to job lifecycle events |

The older direct file, compose, package update, and SMART-test stream openers are no longer exported. Those flows use the job API plus `openJobAttachStream` or `openJobDataStream`.

### Terminal Example

```typescript
import { openTerminalStream, encodeString, decodeString } from "@/api";

const stream = openTerminalStream(120, 32);
if (!stream) return;

stream.onData = (data) => terminal.write(decodeString(data));
stream.write(encodeString("ls -la\n"));
stream.resize(160, 48);
stream.close();
```

### Job Stream Example

```typescript
import { linuxio, openJobAttachStream, waitForStreamResult } from "@/api";

const job = await linuxio.jobs.start.call("file.indexer", "/srv/data");

await waitForStreamResult(openJobAttachStream(job.id), {
  onProgress: (progress) => {
    console.log(progress.phase, progress.pct);
  },
  closeMessage: "Indexer stream closed unexpectedly",
});
```

### Data Stream Example

```typescript
import {
  linuxio,
  openJobDataStream,
  streamWriteChunks,
  waitForStreamResult,
} from "@/api";

const job = await linuxio.jobs.start.call(
  "file.upload",
  "/srv/data/example.bin",
  String(fileBytes.length),
);

const stream = openJobDataStream(job.id, 0);
if (!stream) throw new Error("Stream connection not ready");

const result = waitForStreamResult(stream);
await streamWriteChunks(stream, fileBytes);
await result;
```

## Jobs

Long-running operations are modeled as bridge jobs. Start them with `linuxio.jobs.start.call(jobType, ...args)`, then attach to progress/results or data streams.

| Command | Result |
|---------|--------|
| `linuxio.jobs.start.call(type, ...args)` | Starts a job and returns `JobSnapshot` |
| `linuxio.jobs.recover.call(type)` | Recovers an active recoverable job by type |
| `linuxio.jobs.list.call(status?)` | Lists jobs |
| `linuxio.jobs.get.call(jobId)` | Gets a job snapshot |
| `linuxio.jobs.cancel.call(jobId)` | Cancels a job |

Current job types used by the frontend:

| Job Type | Used For |
|----------|----------|
| `file.compress` | Archive creation |
| `file.extract` | Archive extraction |
| `file.copy` | File/folder copy |
| `file.move` | File/folder move |
| `file.indexer` | File indexer |
| `file.upload` | Uploads through `jobs-data` |
| `file.download` | Single-file downloads through `jobs-data` |
| `file.archive` | Multi-file archive downloads through `jobs-data` |
| `file.chmod` | Permission changes |
| `docker.compose` | Compose up/down/stop/restart |
| `docker.indexer` | Docker stack indexer |
| `package.update` | Package update transactions |
| `storage.smart_test` | SMART tests |

`FileTransferContext` centralizes most file job behavior, recovery, cancellation, event subscription, and notification state. For result-oriented streams in feature code, use `useStreamResult()` from `@/hooks/useStreamResult`.

## Stream Helpers

### `bindStreamHandlers(stream, handlers)`

Attaches handlers and returns a cleanup function that clears them:

```typescript
const cleanup = bindStreamHandlers(stream, {
  onData: (chunk) => append(chunk),
  onProgress: (progress) => setProgress(progress.pct),
  onResult: (result) => console.log(result),
  onClose: () => setClosed(true),
});

cleanup();
```

### `waitForStreamResult(stream, options?)`

Resolves on an `"ok"` result frame and rejects on an error result, premature close, unavailable stream, or abort signal.

| Option | Meaning |
|--------|---------|
| `signal` | Abort signal |
| `closeOnAbort` | `"abort"` default, `"close"`, or `"none"` |
| `onData` | Data callback |
| `onProgress` | Progress callback |
| `onClose` | Close callback |
| `closeMessage` | Error message if the stream closes before a result |
| `mapResult` | Transform result payload before resolving |

### `streamWriteChunks(stream, data, options?)`

Writes bytes in chunks. Defaults are `chunkSize: 64 * 1024`, `yieldMs: 0`, and `closeAtEnd: true`.

## Connection Utilities

### `useStreamMux()`

```typescript
const { status, isOpen, getStream } = useStreamMux();
const terminal = getStream("terminal");
```

| Property | Type |
|----------|------|
| `status` | `"connecting"` \| `"open"` \| `"closed"` \| `"error"` |
| `isOpen` | `boolean` |
| `getStream` | `(type: StreamType) => Stream \| null` |

### `useIsUpdating()`

Returns the mux-level update flag. Typed query hooks pause automatically while this is true.

### Configuration

Mux config is read from Vite env vars and can also be changed with `configureStreamMultiplexer()`.

| Setting | Env Var | Default |
|---------|---------|---------|
| `scrollbackBytes` | `VITE_STREAM_SCROLLBACK_BYTES` | `65536` |
| `detachedBufferBytes` | `VITE_STREAM_DETACHED_BUFFER_BYTES` | `4194304` |
| `uploadChunkSize` | `VITE_STREAM_UPLOAD_CHUNK_SIZE` | `1048576` |
| `uploadWindowChunks` | `VITE_STREAM_UPLOAD_WINDOW_CHUNKS` | `4` |
| `defaultCallTimeoutMs` | `VITE_STREAM_DEFAULT_CALL_TIMEOUT_MS` | `30000` |

The JSON API call timeout uses `defaultCallTimeoutMs`. `waitForStreamMux()` defaults to 10 seconds.

## Error Handling

API failures use `LinuxIOError`:

```typescript
try {
  await linuxio.storage.get_drive_info.call();
} catch (error) {
  if (error instanceof LinuxIOError) {
    console.error(error.code, error.message);
  }
}
```

Common frontend-generated codes:

| Code | Meaning |
|------|---------|
| `not_initialized` | Mux has not been created |
| `timeout` | Request exceeded `defaultCallTimeoutMs` |
| `connection_closed` | Connection or stream closed before a result |
| `stream_unavailable` | A stream opener returned `null` |

Backend result frames may provide numeric error codes such as `401`, `403`, or `500`.

## Bridge Protocol

Typed JSON calls are sent as a `bridge` stream with a null-separated payload:

```text
bridge\0<handler>\0<command>\0<arg1>\0<arg2>...
```

Stream openers use the same null-separated convention for their initial stream payload:

```text
<stream-type>\0<arg1>\0<arg2>...
```

The frontend wraps stream payloads in bridge opcodes:

| Opcode | Hex | Meaning |
|--------|-----|---------|
| `StreamOpen` | `0x80` | Open stream |
| `StreamData` | `0x81` | Data frame |
| `StreamClose` | `0x82` | Graceful close |
| `StreamResize` | `0x83` | Terminal resize |
| `StreamProgress` | `0x84` | JSON progress frame |
| `StreamResult` | `0x85` | JSON result frame |
| `StreamAbort` | `0x86` | Cancel operation |

## Available Handlers

The current typed handlers are defined in `LinuxIOSchema`:

| Handler | Example Commands |
|---------|------------------|
| `jobs` | `start`, `recover`, `list`, `get`, `cancel` |
| `system` | `get_capabilities`, `get_cpu_info`, `get_health_summary`, `get_server_time` |
| `docker` | `list_containers`, `start_container`, `list_compose_projects`, `get_icon_uri`, `system_prune` |
| `dbus` | `list_services`, `get_updates_basic`, `get_network_info`, `set_timezone` |
| `filebrowser` | `resource_get`, `resource_stat`, `subfolders`, `search`, `chmod` |
| `config` | `get`, `set` |
| `indexer` | `get_config`, `get_status`, `set_config` |
| `control` | `version` |
| `power` | `get_status`, `start`, `set_profile`, `disable` |
| `wireguard` | `list_interfaces`, `add_peer`, `peer_qrcode`, `peer_config_download` |
| `terminal` | `list_shells` |
| `accounts` | `list_users`, `get_user_details`, `create_user`, `change_password` |
| `shares` | `list_nfs_shares`, `create_samba_share`, `delete_samba_share` |
| `storage` | `get_drive_info`, `run_smart_test`, `list_nfs_mounts`, `create_lv` |

## Best Practices

1. Import from `@/api`.
2. Use `linuxio.<handler>.<command>.useQuery()` for reads and `.useMutation()` for writes in React components.
3. Use `.call()` or `.queryOptions()` from contexts, event handlers, and non-hook code.
4. Use explicit `{ args: [...] }` when query arguments include objects or arrays.
5. Invalidate specific endpoint query keys after mutations.
6. Use jobs for long-running operations and `openJobAttachStream` / `openJobDataStream` for progress or bytes.
7. Use `abort()` for user cancellation and `close()` for graceful completion.
8. Update `linuxio-types.ts` whenever backend handler args or results change.
