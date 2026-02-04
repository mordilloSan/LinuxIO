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
│  │  Core API: call(), spawn(), openStream()                   │ │
│  └──────────────────────────┬─────────────────────────────────┘ │
│                             │                                    │
│  ┌──────────────────────────▼─────────────────────────────────┐ │
│  │                  linuxio.ts                                │ │
│  │  Utilities: useStreamMux(), payload helpers                │ │
│  └──────────────────────────┬─────────────────────────────────┘ │
│                             │                                    │
│  ┌──────────────────────────▼─────────────────────────────────┐ │
│  │                  StreamMultiplexer.ts                      │ │
│  │  WebSocket + yamux multiplexing                            │ │
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

## Module Summary

| Module | Purpose | Import |
|--------|---------|--------|
| `react-query.ts` | Type-safe API + React Query hooks | `@/api/react-query` |
| `linuxio-core.ts` | Framework-agnostic core API | `@/api/linuxio-core` |
| `linuxio.ts` | Shared utilities and payload helpers | `@/api/linuxio` |

---

## Type-Safe API (`@/api/react-query`)

**Recommended for all React components.** Provides full TypeScript autocomplete, compile-time type checking, and React Query integration.

### Importing

```typescript
import linuxio from "@/api/react-query";
// Also available: initStreamMux, closeStreamMux, waitForStreamMux, getStreamMux
```

### Schema & Types

The type-safe surface comes from `frontend/src/api/linuxio-types.ts` (`LinuxIOSchema`).
When the backend adds/changes a handler/command, update that schema entry to keep:
- Autocomplete for `linuxio.<handler>.<command>`
- Typed `args` / `result` inference

### Query Hooks

Use `linuxio.handler.command.useQuery()` for fetching data:

```typescript
// Basic usage - no arguments
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
// Basic mutation - no arguments
const { mutate, isPending } = linuxio.control.shutdown.useMutation();
mutate([]);

// String arguments
const { mutate } = linuxio.docker.start_container.useMutation();
mutate([containerId]);

// Complex arguments (objects, arrays)
const { mutate } = linuxio.dbus.SetAutoUpdates.useMutation();
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
import linuxio from "@/api/react-query";

const queryClient = useQueryClient();

// Fetch via QueryClient (deduped + cached)
const caps = await queryClient.fetchQuery(
  linuxio.system.get_capabilities.queryOptions({ staleTime: 0 }),
);

// Or call directly (typed)
const version = await linuxio.control.version.call();
```

---

## String-Based API (for modules)

For dynamic handlers (modules) or when handler/command names are not in the schema:

```typescript
// Query
const { data } = linuxio.useCall<WeatherData>(
  "module.weather",
  "getForecast",
  ["London"],
  { staleTime: 60000 }
);

// Mutation
const { mutate } = linuxio.useMutate<void, string[]>(
  "module.lights",
  "toggle"
);
mutate(["living-room"]);
```

---

## Core API (`@/api/linuxio-core`)

For non-React code or when you need direct control.

```typescript
import { call, spawn, openStream } from "@/api/linuxio-core";
```

For built-in handlers, prefer the type-safe imperative helper:

```typescript
import linuxio from "@/api/react-query";

await linuxio.storage.get_drive_info.call();
```

### `call<T>(handler, command, args?, options?)`

Simple request/response call. Returns a Promise that rejects on timeout or if the connection closes.

```typescript
// Basic usage
const drives = await call<DiskInfo[]>("storage", "get_drive_info");

// With arguments
const logs = await call<string>("docker", "get_container_logs", [containerId]);

// With timeout
const result = await call("dbus", "InstallPackage", [packageId], {
  timeout: 60000, // 60 second timeout (default: 30000)
});
```

**Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `handler` | `string` | - | Handler namespace |
| `command` | `string` | - | Command name |
| `args` | `string[]` | `[]` | Arguments array |
| `options.timeout` | `number` | `30000` | Timeout in milliseconds |

**Returns:** `Promise<T>`

**Throws:**
- `LinuxIOError("Request timeout", "timeout")` - Request timed out
- `LinuxIOError("Connection closed before receiving result", "connection_closed")` - Connection dropped
- `LinuxIOError(message, code)` - Backend error

### `spawn(handler, command, args?, options?)`

Streaming operation with progress and data callbacks. Returns a `SpawnedProcess` that is also a Promise.

```typescript
// Download with progress
const result = await spawn("filebrowser", "download", ["/path/to/file"])
  .onStream((chunk) => {
    // Handle binary data chunks
    writeToFile(chunk);
  })
  .progress((p) => {
    // Update progress bar
    setProgress(p.pct);
    console.log(`${p.current}/${p.total} bytes`);
  });

// Package installation with timeout
await spawn("dbus", "InstallPackage", [packageId], {
  timeout: 300000,  // 5 minutes (default: 300000)
  onProgress: (p) => setProgress(p.pct),
});

// With early cancellation
const operation = spawn("filebrowser", "compress", [paths, output, "zip"])
  .progress((p) => setProgress(p.pct));

// Later...
operation.close();  // Cancel the operation
```

**SpawnOptions:**
| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `timeout` | `number` | `300000` | Timeout in milliseconds (5 minutes) |
| `onData` | `(chunk: Uint8Array) => void` | - | Binary data callback |
| `onProgress` | `(progress: ProgressFrame) => void` | - | Progress callback |

**SpawnedProcess Methods:**
| Method | Description |
|--------|-------------|
| `.onStream(callback)` | Register handler for binary data chunks |
| `.progress(callback)` | Register handler for progress updates |
| `.input(data)` | Send data to the process (for bidirectional streams) |
| `.close()` | Abort the operation early |
| `.then()/.catch()/.finally()` | Promise methods for completion |

**Throws:**
- `LinuxIOError("Operation timeout", "timeout")` - Operation timed out
- `LinuxIOError("Connection closed before operation completed", "connection_closed")` - Connection dropped

### `openStream(handler, command, args?, streamType?)`

Opens a bidirectional stream for terminal, docker exec, or custom protocols.

```typescript
// Terminal session with persistence (reusable stream)
const stream = openStream("terminal", "bash", ["120", "32"], "terminal");

stream.onData = (data) => {
  terminal.write(decodeString(data));
};

stream.onClose = () => {
  console.log("Terminal closed");
};

// Send user input
stream.write(encodeString("ls -la\n"));

// Close when done
stream.close();

// Docker container exec (one-off stream)
const stream = openStream(
  "docker",
  "container_exec",
  [containerId, "sh", "80", "24"]
  // streamType defaults to "bridge" (one-off)
);
```

**Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `handler` | `string` | - | Handler name (e.g., "terminal", "docker") |
| `command` | `string` | - | Command name (e.g., "bash", "container_exec") |
| `args` | `string[]` | `[]` | Command arguments |
| `streamType` | `string` | `"bridge"` | Stream type for persistence ("terminal", "container", or "bridge") |

**Stream Properties:**
| Property | Type | Description |
|----------|------|-------------|
| `onData` | `(data: Uint8Array) => void` | Binary data callback |
| `onProgress` | `(progress: ProgressFrame) => void` | Progress update callback |
| `onResult` | `(result: ResultFrame) => void` | Completion callback |
| `onClose` | `() => void` | Stream closed callback |

**Stream Methods:**
| Method | Description |
|--------|-------------|
| `write(data: Uint8Array)` | Send binary data |
| `close()` | Close the stream |

---

## Utilities (`@/api/linuxio`)

Shared utilities for stream management and payload building.

### `useStreamMux()`

React hook for accessing the stream multiplexer status. Now supports late initialization (polls for mux if not available at mount).

```typescript
import { useStreamMux } from "@/api/linuxio";

const { status, isOpen, openStream, getStream } = useStreamMux();

// Check connection status
if (!isOpen) {
  return <ConnectionLost />;
}

// Open a raw stream (advanced usage)
const stream = openStream("bridge", payload);
```

**Returns:**
| Property | Type | Description |
|----------|------|-------------|
| `status` | `MuxStatus` | Current status: "connecting", "open", "closed", "error" |
| `isOpen` | `boolean` | True if connected and ready |
| `openStream` | `function` | Open a new stream |
| `getStream` | `function` | Get existing stream by type |

### Mux Lifecycle Functions

All available from `@/api/react-query`:

```typescript
import {
  initStreamMux,
  closeStreamMux,
  waitForStreamMux,
  getStreamMux
} from "@/api/react-query";

// Initialize connection (called by AuthContext on login)
initStreamMux();

// Wait for connection to be ready
await waitForStreamMux();

// Close connection (called on logout)
closeStreamMux();

// Get mux instance (advanced usage)
const mux = getStreamMux();
```

### Payload Helpers

Pre-built payload constructors for common operations:

```typescript
import {
  terminalPayload,
  containerPayload,
  uploadPayload,
  downloadPayload,
  compressPayload,
  extractPayload,
} from "@/api/linuxio";

// Terminal session
const payload = terminalPayload(120, 32);

// Docker container exec
const payload = containerPayload(containerId, "bash", 120, 32);

// File upload
const payload = uploadPayload("/destination/path", fileSize);

// File download (supports multiple files)
const payload = downloadPayload(["/file1", "/file2"]);

// Archive compression
const payload = compressPayload(["/files/..."], "/output.zip", "zip");

// Archive extraction
const payload = extractPayload("/archive.zip", "/destination/");
```

### String Encoding

```typescript
import { encodeString, decodeString } from "@/api/linuxio";

// Convert string to Uint8Array (UTF-8)
const bytes = encodeString("Hello, World!");

// Convert Uint8Array back to string
const text = decodeString(bytes);
```

---

## Error Handling

All API methods throw/reject with `LinuxIOError`:

```typescript
import { LinuxIOError } from "@/api/react-query";

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
| `"timeout"` | Request/operation timed out |
| `"connection_closed"` | Connection dropped before completion |
| `500` | Server error |
| `403` | Permission denied |

**New in v0.6:** Promises now properly reject if the connection closes before receiving a result, preventing hanging promises.

---

## Protocol Format

All requests use the bridge protocol with null-separated arguments:

```
bridge\0<handler>\0<command>\0<arg1>\0<arg2>...
```

Complex types (objects, arrays) are JSON-serialized automatically when using the typed API.

---

## Available Handlers

| Handler | Description | Example Commands |
|---------|-------------|------------------|
| `system` | System information | `get_cpu_info`, `get_memory_info`, `get_host_info` |
| `storage` | Storage management | `get_drive_info`, `list_vgs`, `list_nfs_mounts` |
| `docker` | Docker management | `list_containers`, `start_container`, `container_exec` |
| `filebrowser` | File operations | `resource_get`, `subfolders`, `upload`, `download`, `compress` |
| `dbus` | D-Bus services | `ListServices`, `GetUpdates`, `InstallPackage`, `SetAutoUpdates` |
| `wireguard` | WireGuard VPN | `list_interfaces`, `add_peer`, `remove_peer` |
| `config` | User configuration | `get`, `set` |
| `control` | System control | `version`, `shutdown`, `update` |
| `modules` | Module management | `GetModules`, `InstallModule`, `UninstallModule` |

---

## Best Practices

1. **Use type-safe API** (`linuxio.handler.command.useQuery()`) for built-in handlers
2. **Use string-based API** (`linuxio.useCall()`) for dynamic/module handlers
3. **Use explicit args** when passing objects: `useQuery({ args: ["str", obj] })`
4. **Handle errors** with try/catch or React Query's error states
5. **Set appropriate timeouts** for long-running operations
6. **Use `spawn` with progress** for file transfers and package operations
7. **Invalidate queries** after mutations to refresh cached data

```typescript
// Good: Type-safe with proper invalidation
const { mutate } = linuxio.docker.remove_container.useMutation({
  onSuccess: () => {
    queryClient.invalidateQueries({ queryKey: ["linuxio", "docker"] });
  },
});
mutate([containerId]);
```

---

## Migration from Old API

If you're migrating from the old string-based API:

```typescript
// Old (still works, but deprecated for built-in handlers)
const { data } = useCall("storage", "get_drive_info");

// New (recommended)
const { data } = linuxio.storage.get_drive_info.useQuery();

// Old mutation
const { mutate } = useMutate("docker", "start_container");
mutate(containerId);  // Single string

// New mutation
const { mutate } = linuxio.docker.start_container.useMutation();
mutate([containerId]);  // Array of args
```

Key differences:
- Mutations now expect arrays: `mutate([arg1, arg2])` instead of `mutate(arg1)`
- Complex objects need explicit args in queries: `useQuery({ args: [...] })`
- Full TypeScript autocomplete and type checking
