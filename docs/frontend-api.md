# Frontend API

The LinuxIO frontend API provides a clean, type-safe interface for communicating with the backend over a multiplexed WebSocket connection.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Frontend                                  │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                  react-query.ts                            │ │
│  │  React Query integration: useCall(), useMutate()           │ │
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
| `react-query.ts` | React Query hooks for data fetching | `@/api/react-query` |
| `linuxio-core.ts` | Framework-agnostic core API | `@/api/linuxio-core` |
| `linuxio.ts` | Shared utilities and payload helpers | `@/api/linuxio` |

---

## React Query API (`@/api/react-query`)

For most React components, use these hooks for automatic caching, refetching, and state management.

### `useCall<T>(handler, command, args?, options?)`

Query hook for fetching data. Automatically handles loading states, caching, and refetching.

```typescript
import { useCall } from "@/api/react-query";

// Basic usage
const { data, isLoading, error, refetch } = useCall<DiskInfo[]>(
  "system",
  "get_drive_info"
);

// With arguments
const { data } = useCall<ContainerStats>(
  "docker",
  "get_container_stats",
  [containerId]
);

// With options
const { data } = useCall<ServiceList>(
  "dbus",
  "ListServices",
  [],
  {
    refetchInterval: 5000,  // Auto-refresh every 5 seconds
    staleTime: 2000,        // Consider data fresh for 2 seconds
    enabled: isReady,       // Conditional fetching
  }
);
```

**Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `handler` | `string` | Handler namespace (e.g., "system", "docker", "dbus") |
| `command` | `string` | Command name (e.g., "get_drive_info", "ListServices") |
| `args` | `string[]` | Optional arguments array |
| `options` | `UseQueryOptions` | React Query options |

**Returns:** React Query `UseQueryResult<T, LinuxIOError>`

### `useMutate<TData, TVariables>(handler, command, options?)`

Mutation hook for write operations. Handles loading states and error handling.

```typescript
import { useMutate } from "@/api/react-query";

// Basic usage
const { mutate, isPending } = useMutate("docker", "start_container");
mutate(containerId);

// With callbacks
const { mutate } = useMutate("docker", "remove_container", {
  onSuccess: () => {
    toast.success("Container removed");
    queryClient.invalidateQueries(["linuxio", "docker"]);
  },
  onError: (error) => {
    toast.error(error.message);
  },
});

// With object variables (auto-converted to args)
const { mutate } = useMutate("config", "theme_set");
mutate({ theme: "dark", accent: "blue" });
```

**Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `handler` | `string` | Handler namespace |
| `command` | `string` | Command name |
| `options` | `UseMutationOptions` | React Query mutation options |

**Returns:** React Query `UseMutationResult<TData, LinuxIOError, TVariables>`

---

## Core API (`@/api/linuxio-core`)

For non-React code or when you need direct control over requests.

### `call<T>(handler, command, args?, options?)`

Simple request/response call. Returns a Promise.

```typescript
import { call } from "@/api/linuxio-core";

// Basic usage
const drives = await call<DiskInfo[]>("system", "get_drive_info");

// With arguments
const stats = await call<ContainerStats>(
  "docker",
  "get_container_stats",
  [containerId]
);

// With timeout
const result = await call("dbus", "InstallPackage", [packageId], {
  timeout: 60000, // 60 second timeout
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

**Throws:** `LinuxIOError` on failure or timeout

### `spawn(handler, command, args?, options?)`

Streaming operation with progress and data callbacks. Returns a `SpawnedProcess` that is also a Promise.

```typescript
import { spawn } from "@/api/linuxio-core";

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

// Package installation with progress
await spawn("dbus", "InstallPackage", [packageId])
  .progress((p) => setProgress(p.pct));
```

**SpawnedProcess Methods:**

| Method | Description |
|--------|-------------|
| `.onStream(callback)` | Register handler for binary data chunks |
| `.progress(callback)` | Register handler for progress updates |
| `.input(data)` | Send data to the process (for bidirectional streams) |
| `.close()` | Abort the operation early |
| `.then()/.catch()/.finally()` | Promise methods for completion |

### `openStream(handler, command, args?)`

Opens a bidirectional stream for terminal, docker exec, or custom protocols.

```typescript
import { openStream, encodeString, decodeString } from "@/api/linuxio-core";

// Terminal session
const stream = openStream("terminal", "bash", ["120", "32"]);

stream.onData = (data) => {
  // Receive terminal output
  terminal.write(decodeString(data));
};

stream.onResult = (result) => {
  // Stream completed
  console.log("Exit code:", result.data.exit_code);
};

// Send user input
stream.write(encodeString("ls -la\n"));

// Resize terminal
stream.write(encodeString("\x1b[8;40;120t")); // ANSI resize

// Close when done
stream.close();
```

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

React hook for accessing the stream multiplexer status.

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

### Connection Utilities

```typescript
import { isConnected, getStatus } from "@/api/linuxio";

// Check if connected
if (isConnected()) {
  // Safe to make API calls
}

// Get detailed status
const status = getStatus(); // "connecting" | "open" | "closed" | "error" | null
```

---

## Error Handling

All API methods throw `LinuxIOError` on failure:

```typescript
import { LinuxIOError } from "@/api/linuxio-core";

try {
  await call("system", "get_drive_info");
} catch (error) {
  if (error instanceof LinuxIOError) {
    console.error(`Error ${error.code}: ${error.message}`);
  }
}
```

**Common Error Codes:**

| Code | Description |
|------|-------------|
| `not_initialized` | StreamMux not ready (not logged in) |
| `timeout` | Request timed out |
| `500` | Server error |
| `403` | Permission denied |

---

## Protocol Format

All requests use the bridge protocol with null-separated arguments:

```
bridge\0<handler>\0<command>\0<arg1>\0<arg2>...
```

Example:
```
bridge\0docker\0start_container\0abc123
```

The backend dispatches to the appropriate handler based on the handler name and command.

---

## Available Handlers

| Handler | Description | Example Commands |
|---------|-------------|------------------|
| `system` | System information | `get_drive_info`, `get_cpu_info`, `get_memory_info` |
| `docker` | Docker management | `list_containers`, `start_container`, `container_exec` |
| `filebrowser` | File operations | `list_directory`, `upload`, `download`, `compress` |
| `dbus` | D-Bus services | `ListServices`, `GetUpdates`, `InstallPackage` |
| `terminal` | Terminal sessions | `bash`, `sh` |
| `wireguard` | WireGuard VPN | `list_interfaces`, `add_peer` |
| `config` | User configuration | `theme_get`, `theme_set` |
| `control` | System control | `version`, `shutdown`, `update` |
| `modules` | Module management | `GetModules`, `InstallModule` |

---

## Best Practices

1. **Use React Query hooks** (`useCall`, `useMutate`) for React components
2. **Use core API** (`call`, `spawn`) for non-React code or complex operations
3. **Handle errors** with try/catch or React Query's error states
4. **Set appropriate timeouts** for long-running operations
5. **Use `spawn` with progress** for file transfers and package operations
6. **Invalidate queries** after mutations to refresh cached data

```typescript
// Good: Invalidate related queries after mutation
const { mutate } = useMutate("docker", "remove_container", {
  onSuccess: () => {
    queryClient.invalidateQueries({ queryKey: ["linuxio", "docker"] });
  },
});
```
