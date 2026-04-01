# Frontend API

The LinuxIO frontend API provides a clean, type-safe interface for communicating with the backend over a multiplexed WebSocket connection.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Frontend                                  │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                  react-query.ts                            │ │
│  │  Type-safe API + React Query integration                  │ │
│  │  linuxio.handler.command.useQuery()                        │ │
│  └──────────────────────────┬─────────────────────────────────┘ │
│                             │                                    │
│  ┌──────────────────────────▼─────────────────────────────────┐ │
│  │                  linuxio-core.ts                           │ │
│  │  Core API: call() — Promise-based bridge communication     │ │
│  └──────────────────────────┬─────────────────────────────────┘ │
│                             │                                    │
│  ┌──────────────────────────▼─────────────────────────────────┐ │
│  │                  linuxio.ts                                │ │
│  │  Stream openers + hooks (useStreamMux, useIsUpdating)      │ │
│  └──────────────────────────┬─────────────────────────────────┘ │
│                             │                                    │
│  ┌──────────────────────────▼─────────────────────────────────┐ │
│  │                  StreamMultiplexer.ts                      │ │
│  │  WebSocket + yamux multiplexing (singleton)                │ │
│  └──────────────────────────┬─────────────────────────────────┘ │
│                             │                                    │
└─────────────────────────────┼────────────────────────────────────┘
                              │
                              ▼
                      WebSocket Connection
                              │
                              ▼
                    ┌─────────────────┐
                    │     Backend     │
                    └─────────────────┘
```

## API Files

| Module | Purpose | Import |
|--------|---------|--------|
| `react-query.ts` | Type-safe API + React Query hooks | Prefer `@/api` |
| `linuxio-core.ts` | Framework-agnostic `call()` function | Prefer `@/api` |
| `linuxio.ts` | Stream openers + hooks | Prefer `@/api` |
| `stream-helpers.ts` | Stream event binding + utilities | Prefer `@/api` |
| `StreamMultiplexer.ts` | WebSocket + yamux singleton | Via `@/api` exports |

---

## Type-Safe API (`linuxio`)

**Recommended for all React components.** Provides full TypeScript autocomplete, compile-time type checking, and React Query integration.

### Importing

```typescript
import { linuxio } from "@/api";
```

### Schema & Types

The type-safe surface comes from `frontend/src/api/linuxio-types.ts` (`LinuxIOSchema`).
When the backend adds/changes a handler/command, update that schema entry to keep:
- Autocomplete for `linuxio.<handler>.<command>`
- Typed `args` / `result` inference

### Query Hooks

Use `linuxio.handler.command.useQuery()` for fetching data:

```typescript
// Basic usage — no arguments
const { data, isLoading, error } = linuxio.storage.get_drive_info.useQuery();

// With string arguments
const { data } = linuxio.docker.get_container_logs.useQuery(containerId);

// Multiple string arguments
const { data } = linuxio.filebrowser.resource_get.useQuery(path, "", "true");

// With React Query options
const { data } = linuxio.system.get_cpu_info.useQuery({
  staleTime: 60000,
  refetchInterval: 5000,
});

// Options-only (no args)
const { data } = linuxio.docker.list_containers.useQuery({
  staleTime: 2000,
});
```

**Returns:** React Query `UseQueryResult<T, LinuxIOError>`

**Features:**
- Automatic caching based on handler + command + args
- Auto-refetch on window focus/reconnect
- Loading and error states
- Disabled when mux is not open
- Full TypeScript inference for result types

### Mutation Hooks

Use `linuxio.handler.command.useMutation()` for write operations:

```typescript
// Basic mutation — no arguments
const { mutate, isPending } = linuxio.control.version.useMutation();
mutate([]);

// String arguments
const { mutate } = linuxio.docker.start_container.useMutation();
mutate([containerId]);

// Complex arguments (objects, arrays — JSON-serialized automatically)
const { mutate } = linuxio.dbus.set_auto_updates.useMutation();
mutate([
  {
    enabled: true,
    frequency: "daily",
    scope: "security",
    download_only: true,
    reboot_policy: "if_needed",
    exclude_packages: [],
  },
]);

// With callbacks
const { mutate } = linuxio.docker.remove_container.useMutation({
  onSuccess: () => {
    toast.success("Container removed");
    queryClient.invalidateQueries({ queryKey: ["linuxio", "docker"] });
  },
  onError: (error) => {
    toast.error(error.message);
  },
});
mutate([containerId]);
```

**Returns:** React Query `UseMutationResult<TResult, LinuxIOError, unknown[]>`

**Note:** Mutations always expect an array of arguments. Objects and arrays are JSON-serialized automatically.

### Imperative Helpers (No Hooks)

For effects, contexts, or other non-hook code paths, every typed command also exposes:
- `.call(...args)` – Promise-based helper using the same serialization as hooks
- `.queryKey(...args)` / `.queryOptions(...)` – for `queryClient.fetchQuery()` / `ensureQueryData()`

```typescript
import { useQueryClient } from "@tanstack/react-query";
import { linuxio } from "@/api";

const queryClient = useQueryClient();

// Fetch via QueryClient (deduped + cached)
const caps = await queryClient.fetchQuery(
  linuxio.system.get_capabilities.queryOptions({ staleTime: 0 }),
);

// Or call directly (typed)
const version = await linuxio.control.version.call();
```

---

## Streaming API

For persistent or binary streams (terminals, file transfers, logs), use the typed stream openers exported from `@/api`. These are named functions that open a specific stream type and return a `Stream | null`.

```typescript
import {
  openTerminalStream,
  openContainerStream,
  openFileUploadStream,
  openFileDownloadStream,
  openDockerLogsStream,
  // ... etc
} from "@/api";
```

### Stream Openers Reference

| Function | Args | Stream Type | Description |
|----------|------|-------------|-------------|
| `openTerminalStream(cols, rows)` | `number, number` | `terminal` | PTY shell session |
| `openContainerStream(id, shell, cols, rows)` | `string, string, number, number` | `container` | Docker exec session |
| `openDockerLogsStream(id, tail?)` | `string, string?` | `docker-logs` | Live container logs |
| `openDockerComposeStream(action, project, path?)` | `string, string, string?` | `docker-compose` | Compose up/down/stop/restart |
| `openDockerIndexerStream()` | — | `docker-indexer` | Run indexer |
| `openDockerIndexerAttachStream()` | — | `docker-indexer-attach` | Attach to running indexer |
| `openPackageUpdateStream(packages)` | `string[]` | `pkg-update` | Install/update packages |
| `openAppUpdateStream(runId, version?)` | `string, string?` | `app-update` | LinuxIO self-update |
| `openServiceLogsStream(name, lines?)` | `string, string?` | `service-logs` | Systemd service logs |
| `openGeneralLogsStream(lines?, period?, priority?, id?)` | `string?, ...` | `general-logs` | Journal logs |
| `openFileUploadStream(path, size, override?)` | `string, number, boolean?` | `fb-upload` | Upload a file |
| `openFileDownloadStream(paths)` | `string[]` | `fb-download` or `fb-archive` | Download file(s) |
| `openFileCompressStream(paths, dest, format)` | `string[], string, string` | `fb-compress` | Create archive |
| `openFileExtractStream(archive, dest?)` | `string, string?` | `fb-extract` | Extract archive |
| `openFileIndexerStream(path?)` | `string?` | `fb-reindex` | Reindex filesystem |
| `openFileIndexerAttachStream()` | — | `fb-indexer-attach` | Attach to running reindex |
| `openFileCopyStream(src, dst)` | `string, string` | `fb-copy` | Copy with progress |
| `openFileMoveStream(src, dst)` | `string, string` | `fb-move` | Move with progress |
| `openSmartTestStream(device, testType)` | `string, string` | `smart-test` | S.M.A.R.T. test |

### Stream Interface

All openers return `Stream | null` (`null` if the mux is not open):

```typescript
interface Stream {
  readonly id: number;
  readonly type: StreamType;
  readonly status: StreamStatus;      // "opening" | "open" | "closing" | "closed"
  write(data: Uint8Array): void;      // Send OpStreamData
  resize(cols: number, rows: number): void; // Send OpStreamResize (terminal)
  close(): void;                      // Send FIN (OpStreamClose)
  abort(): void;                      // Send RST — immediate abort
  onData:     ((data: Uint8Array) => void) | null;
  onProgress: ((progress: ProgressFrame) => void) | null;
  onResult:   ((result: ResultFrame) => void) | null;
  onClose:    (() => void) | null;
}
```

### Usage Example (Terminal)

```typescript
import { openTerminalStream, encodeString, decodeString } from "@/api";

const stream = openTerminalStream(120, 32);
if (!stream) return; // mux not ready

stream.onData = (data) => {
  terminal.write(decodeString(data));
};

stream.onClose = () => {
  console.log("Terminal closed");
};

// Send user input
stream.write(encodeString("ls -la\n"));

// Resize
stream.resize(160, 48);

// Close
stream.close();
```

### Usage Example (File Upload with Progress)

```typescript
import { openFileUploadStream, streamWriteChunks, waitForStreamResult } from "@/api";

async function uploadFile(path: string, fileData: Uint8Array) {
  const stream = openFileUploadStream(path, fileData.length);
  if (!stream) throw new Error("Not connected");

  const resultPromise = waitForStreamResult(stream, {
    onProgress: (p) => setProgress(p.pct),
  });

  await streamWriteChunks(stream, fileData);
  return resultPromise;
}
```

---

## Utilities (`@/api`)

### `useStreamMux()`

React hook for accessing the stream multiplexer status. Polls for the mux if not available at mount (handles late initialization).

```typescript
import { useStreamMux } from "@/api";

const { status, isOpen, getStream } = useStreamMux();

if (!isOpen) {
  return <ConnectionLost />;
}

// Get an existing persistent stream (e.g., terminal)
const termStream = getStream("terminal");
```

**Returns:**
| Property | Type | Description |
|----------|------|-------------|
| `status` | `MuxStatus` | `"connecting"` \| `"open"` \| `"closed"` \| `"error"` |
| `isOpen` | `boolean` | True if connected and ready |
| `getStream` | `(type: StreamType) => Stream \| null` | Get existing stream by type |

### `useIsUpdating()`

Returns `true` while a system update is in progress. Use this to pause API queries:

```typescript
import { useIsUpdating } from "@/api";

const isUpdating = useIsUpdating();
// Queries are automatically disabled while updating
```

### Mux Lifecycle Functions

```typescript
import {
  initStreamMux,
  closeStreamMux,
  waitForStreamMux,
  getStreamMux,
} from "@/api";

// Initialize connection (called by AuthContext on login)
initStreamMux();

// Wait for connection to be ready
await waitForStreamMux(timeoutMs);

// Close connection (called on logout)
closeStreamMux();

// Get mux instance directly (advanced usage)
const mux = getStreamMux();
```

### String Encoding

```typescript
import { encodeString, decodeString } from "@/api";

const bytes = encodeString("Hello, World!"); // → Uint8Array (UTF-8)
const text  = decodeString(bytes);           // → "Hello, World!"
```

---

## Stream Helpers (`stream-helpers.ts`)

Utilities for attaching handlers to streams and managing completion:

### `bindStreamHandlers(stream, handlers)`

Attach event handlers to a stream and return a cleanup function:

```typescript
import { bindStreamHandlers } from "@/api";

const unbind = bindStreamHandlers(stream, {
  onData:     (data) => writeChunk(data),
  onProgress: (p) => setProgress(p.pct),
  onResult:   (r) => console.log("done", r),
  onClose:    () => cleanup(),
});

// Later
unbind(); // detach all handlers
```

### `waitForStreamResult(stream, options?)`

Await a stream operation that completes with an `onResult` frame:

```typescript
import { waitForStreamResult } from "@/api";

const result = await waitForStreamResult(stream, {
  onProgress: (p) => setProgress(p.pct),
  onData:     (chunk) => buffer.push(chunk),
  signal:     abortController.signal,
  closeMessage: "Upload failed: connection lost",
});
```

**Options:**
| Option | Type | Description |
|--------|------|-------------|
| `signal` | `AbortSignal` | Cancel the wait (sends abort/close to stream) |
| `closeOnAbort` | `"abort" \| "close" \| "none"` | What to send on abort (default: `"abort"`) |
| `onData` | `(data: Uint8Array) => void` | Binary data callback |
| `onProgress` | `(progress: ProgressFrame) => void` | Progress callback |
| `onClose` | `() => void` | Close callback |
| `closeMessage` | `string` | Error message if stream closes before result |
| `mapResult` | `(data, frame) => T` | Transform the result payload |

### `streamWriteChunks(stream, data, options?)`

Write binary data to a stream in chunks (with optional pacing):

```typescript
import { streamWriteChunks } from "@/api";

await streamWriteChunks(stream, fileData, {
  chunkSize: 64 * 1024, // 64 KB chunks (default)
  yieldMs: 0,           // ms to yield between chunks (default: 0)
  closeAtEnd: true,     // send FIN when done (default: true)
  signal: abortSignal,
});
```

---

## Error Handling

All API methods throw/reject with `LinuxIOError`:

```typescript
import { LinuxIOError, linuxio } from "@/api";

try {
  await linuxio.storage.get_drive_info.call();
} catch (error) {
  if (error instanceof LinuxIOError) {
    console.error(`Error ${error.code}: ${error.message}`);
  }
}
```

**Common Error Codes:**
| Code | Description |
|------|-------------|
| `"not_initialized"` | StreamMux not ready (not logged in) |
| `"timeout"` | Request timed out (default: 30 seconds) |
| `"connection_closed"` | Connection dropped before completion |
| `"stream_unavailable"` | Stream could not be opened |
| `500` | Server error |
| `403` | Permission denied |

**Default call timeout:** 30 000 ms (30 seconds). Configurable via `VITE_STREAM_DEFAULT_CALL_TIMEOUT_MS` env var or `configureStreamMultiplexer({ defaultCallTimeoutMs: ... })`.

---

## Protocol Format

All JSON request/response calls use the bridge protocol with null-separated arguments:

```
bridge\0<handler>\0<command>\0<arg1>\0<arg2>...
```

Complex types (objects, arrays) are JSON-serialized automatically when using the typed API.

---

## Available Handlers

| Handler | Description | Example Commands |
|---------|-------------|------------------|
| `system` | System information | `get_cpu_info`, `get_memory_info`, `get_host_info`, `get_health_summary` |
| `monitoring` | Time-series metrics | `get_cpu_series`, `get_memory_series`, `get_network_series` |
| `storage` | Storage management | `get_drive_info`, `list_pvs`, `list_vgs`, `list_nfs_mounts` |
| `docker` | Docker management | `list_containers`, `start_container`, `list_compose_projects`, `get_icon` |
| `filebrowser` | File operations | `resource_get`, `subfolders`, `search`, `chmod` |
| `dbus` | D-Bus / systemd | `list_services`, `get_updates_basic`, `get_network_info`, `reboot` |
| `wireguard` | WireGuard VPN | `list_interfaces`, `add_peer`, `remove_peer` |
| `accounts` | User/group management | `list_users`, `create_user`, `list_groups`, `change_password` |
| `shares` | NFS/Samba shares | `list_nfs_shares`, `create_nfs_share`, `list_samba_shares` |
| `config` | User configuration | `get`, `set` |
| `control` | App control | `version` |
| `terminal` | Terminal utilities | `list_shells` |
| `logs` | Log streaming | (stream-only, see stream openers) |

---

## Best Practices

1. **Use type-safe API** (`linuxio.handler.command.useQuery()`) for all JSON calls
2. **Use typed stream openers** (`openTerminalStream`, etc.) for streaming operations
3. **Handle errors** with try/catch or React Query's error states
4. **Set appropriate timeouts** for long-running operations via `CallOptions.timeout`
5. **Invalidate queries** after mutations to refresh cached data

```typescript
// Good: type-safe with proper invalidation
const { mutate } = linuxio.docker.remove_container.useMutation({
  onSuccess: () => {
    queryClient.invalidateQueries({ queryKey: ["linuxio", "docker"] });
  },
});
mutate([containerId]);
```
