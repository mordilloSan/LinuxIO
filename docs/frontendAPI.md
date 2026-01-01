# LinuxIO Frontend API

## Core Truth

**Everything is a yamux stream.** There is no fundamental difference between "API calls", "terminals", and "file transfers" at the protocol level. All communication flows through multiplexed binary streams over a single WebSocket connection.

The `linuxio` API provides a unified interface for all backend communication:
- **React Query integration** for API calls with caching, loading states, auto-refetch
- **Direct stream access** for terminals, file transfers, real-time updates
- **Promise-based API** for non-React contexts

## Quick Start

```typescript
import { linuxio } from "@/api/linuxio";

// 1. API calls with React Query
const { data, isLoading, refetch } = linuxio.call("system", "get_cpu_info");

// 2. Direct stream access (terminals)
const term = linuxio.stream("terminal", {
  onData: (data) => xterm.write(linuxio.decodeString(data)),
});

// 3. File upload with progress
const upload = linuxio.stream("fb-upload", {
  onProgress: (p) => setProgress(p.pct),
  onResult: (r) => toast.success("Upload complete!"),
});

// 4. Mutations (write operations)
const { mutate, isPending } = linuxio.mutate("docker", "start_container");
mutate("container-id");
```

## Protocol Layers (Browser → Bridge)

```
┌─────────────────────────────────────────────────────┐
│ Application Layer (What you write)                  │
│ - linuxio.call("system", "get_cpu_info")           │
│ - linuxio.stream("terminal", { onData })           │
└────────────────────┬────────────────────────────────┘
                     │
┌────────────────────┴────────────────────────────────┐
│ linuxio API (/frontend/src/api/linuxio.ts)         │
│ - call() - React Query wrapper                      │
│ - stream() - Direct stream access                   │
│ - request() - Promise-based                         │
└────────────────────┬────────────────────────────────┘
                     │
┌────────────────────┴────────────────────────────────┐
│ StreamMultiplexer (Binary protocol)                 │
│ - openStream(type, payload) → Stream                │
│ - Frame routing: [streamID][flags][payload]         │
│ - WebSocket: Binary messages only                   │
└────────────────────┬────────────────────────────────┘
                     │
                 WebSocket
                     │
┌────────────────────┴────────────────────────────────┐
│ Server (Pure byte relay)                            │
│ - No JSON parsing                                   │
│ - Just routes frames by streamID                    │
└────────────────────┬────────────────────────────────┘
                     │
                  Yamux
                     │
┌────────────────────┴────────────────────────────────┐
│ Bridge (Frame handling)                             │
│ - Parses StreamFrame opcode                         │
│ - Routes to handlers based on stream type           │
│ - Sends responses as frames                         │
└─────────────────────────────────────────────────────┘
```

## API Reference

### linuxio.call() - React Query Hook

Make API calls with full React Query integration.

```typescript
function call<T>(
  handler: string,
  command: string,
  args?: string[],
  options?: UseQueryOptions<T>
): UseQueryResult<T, LinuxIOError>
```

**Features:**
- Automatic loading states (`isLoading`, `isPending`)
- Error handling (`error`)
- Caching and deduplication
- Auto-refetch with intervals
- Manual refetch
- Stale-while-revalidate
- Only runs when WebSocket is connected

**Examples:**

```typescript
// Basic usage
const { data, isLoading, error } = linuxio.call("system", "get_cpu_info");

// With arguments
const { data } = linuxio.call("docker", "get_container", ["container-123"]);

// With React Query options
const { data, refetch } = linuxio.call("system", "get_memory", [], {
  refetchInterval: 2000,    // Auto-refresh every 2s
  staleTime: 1000,          // Consider fresh for 1s
  enabled: isVisible,       // Conditional fetching
});

// Manual refetch
<button onClick={() => refetch()}>Refresh</button>
```

### linuxio.mutate() - React Query Mutations

Execute write operations with React Query mutations.

```typescript
function mutate<TData, TVariables>(
  handler: string,
  command: string,
  options?: UseMutationOptions<TData, TVariables>
): UseMutationResult<TData, LinuxIOError, TVariables>
```

**Examples:**

```typescript
// Simple mutation with string argument
const { mutate, isPending } = linuxio.mutate("docker", "start_container");
mutate("container-123");

// With object body (auto-JSON stringified)
const { mutate } = linuxio.mutate("docker", "create_container", {
  onSuccess: () => toast.success("Container created!"),
  onError: (error) => toast.error(error.message),
});
mutate({ name: "my-app", image: "nginx:latest" });

// With array arguments
const { mutate } = linuxio.mutate("filebrowser", "delete_file");
mutate(["file1.txt", "file2.txt"]);
```

### linuxio.stream() - Direct Stream Access

Open a bidirectional stream for terminals, file transfers, or long-running operations.

```typescript
function stream(
  type: string,
  options: StreamOptions
): Stream

interface StreamOptions {
  onData?: (data: Uint8Array) => void;
  onProgress?: (progress: ProgressFrame) => void;
  onResult?: (result: ResultFrame) => void;
  onClose?: () => void;
}
```

**Examples:**

```typescript
// Terminal
const term = linuxio.stream("terminal", {
  onData: (data) => {
    // OpStreamData (0x81) - raw bytes
    xterm.write(linuxio.decodeString(data));
  },
  onClose: () => console.log("Terminal closed"),
});

// Send user input
xterm.onData((input) => {
  term.write(linuxio.encodeString(input));
});

// Close when done
term.close();

// File upload with progress
const upload = linuxio.stream("fb-upload", {
  onProgress: (progress) => {
    // OpStreamProgress (0x84) - JSON
    setProgress(progress.pct);
    console.log(`${progress.pct}% (${progress.bytes}/${progress.total})`);
  },
  onResult: (result) => {
    // OpStreamResult (0x85) - JSON
    if (result.status === "ok") {
      toast.success("Upload complete!");
    } else {
      toast.error(result.error);
    }
  },
});

// Write file chunks
upload.write(chunk1);
upload.write(chunk2);

// Package update with progress
const update = linuxio.stream("pkg-update", {
  onProgress: (p) => {
    setProgress(p.pct);
    setStatus(p.msg);
  },
  onResult: (r) => {
    if (r.status === "ok") {
      queryClient.invalidateQueries(["packages"]);
    }
  },
});
```

### linuxio.request() - Promise-based

Make API calls outside React components (promise-based).

```typescript
async function request<T>(
  handler: string,
  command: string,
  args?: string[],
  timeoutMs?: number
): Promise<T>
```

**Examples:**

```typescript
// Simple request
const cpuInfo = await linuxio.request("system", "get_cpu_info");

// With arguments
const container = await linuxio.request("docker", "get_container", ["id-123"]);

// With custom timeout
const result = await linuxio.request(
  "system",
  "long_operation",
  [],
  60000  // 60s timeout
);

// Error handling
try {
  await linuxio.request("docker", "start_container", ["invalid-id"]);
} catch (error) {
  if (error instanceof LinuxIOError) {
    console.error(`Error ${error.code}: ${error.message}`);
  }
}
```

## Stream Types

### Terminal Streams

```typescript
// Host terminal
const term = linuxio.stream("terminal", {
  onData: (data) => xterm.write(linuxio.decodeString(data)),
});

// With payload builder
const payload = linuxio.terminalPayload(cols, rows);
const term = linuxio.stream("terminal", { onData });
```

### Container Streams

```typescript
// Docker container terminal
const payload = linuxio.containerPayload(containerId, shell, cols, rows);
const term = linuxio.stream("container", {
  onData: (data) => xterm.write(linuxio.decodeString(data)),
});
```

### File Browser Streams

```typescript
// Upload
const payload = linuxio.uploadPayload(path, fileSize);
const upload = linuxio.stream("fb-upload", {
  onProgress: (p) => setProgress(p.pct),
  onResult: (r) => handleComplete(r),
});

// Download
const payload = linuxio.downloadPayload(["/path/to/file"]);
const download = linuxio.stream("fb-download", {
  onData: (chunk) => saveChunk(chunk),
  onResult: () => console.log("Download complete"),
});

// Compress
const payload = linuxio.compressPayload(
  ["/file1", "/file2"],
  "/output.zip",
  "zip"
);
const compress = linuxio.stream("fb-compress", {
  onProgress: (p) => setProgress(p.pct),
});

// Extract
const payload = linuxio.extractPayload("/archive.zip", "/dest");
const extract = linuxio.stream("fb-extract", {
  onProgress: (p) => setProgress(p.pct),
});
```

## Frame Flow Examples

### Terminal Session (Persistent Stream)

```
Browser                     Server                  Bridge
  │                           │                       │
  │─ SYN+DATA ───────────────►│──────────────────────►│
  │  payload: "terminal\080\024"                     │
  │                           │                       │── spawn PTY
  │                           │                       │
  │◄──────────── DATA ────────│◄──────────────────────│ (bash prompt)
  │  opcode: 0x81             │                       │
  │                           │                       │
  │─ DATA ───────────────────►│──────────────────────►│ (user input)
  │  "ls -la\n"               │                       │
  │                           │                       │
  │◄──────────── DATA ────────│◄──────────────────────│ (command output)
  │  opcode: 0x81             │                       │
  │                           │                       │
  ... stream stays open indefinitely ...
```

### API Call (Ephemeral Stream)

```
Browser                     Server                  Bridge
  │                           │                       │
  │─ SYN+DATA ───────────────►│──────────────────────►│
  │  payload: "api\0system\0get_cpu_info"           │
  │                           │                       │── execute handler
  │                           │                       │
  │◄───── RESULT ─────────────│◄──────────────────────│
  │  opcode: 0x85             │  {status:"ok",        │
  │  {model:"Intel i7",...}   │   data:{...}}         │
  │                           │                       │
  │◄──────── FIN ─────────────│◄──────────────────────│
  │                           │                       │
  Stream closed automatically
```

### File Upload (Progress Stream)

```
Browser                     Server                  Bridge
  │                           │                       │
  │─ SYN+DATA ───────────────►│──────────────────────►│
  │  payload: "fb-upload\0/path\01024000"           │
  │                           │                       │── create file
  │─ DATA (chunk 1) ─────────►│──────────────────────►│
  │─ DATA (chunk 2) ─────────►│──────────────────────►│
  │                           │                       │
  │◄────── PROGRESS ──────────│◄──────────────────────│
  │  opcode: 0x84             │  {bytes:512000,       │
  │  {pct:50,bytes:512000}    │   pct:50}             │
  │                           │                       │
  │─ DATA (chunk 3) ─────────►│──────────────────────►│
  │─ DATA (chunk 4) ─────────►│──────────────────────►│
  │                           │                       │
  │◄────── RESULT ────────────│◄──────────────────────│
  │  opcode: 0x85             │  {status:"ok"}        │
  │  {status:"ok"}            │                       │
  │                           │                       │
  │◄──────── FIN ─────────────│◄──────────────────────│
```

## Utilities

### Encoding/Decoding

```typescript
// String to bytes
const bytes = linuxio.encodeString("hello world");

// Bytes to string
const str = linuxio.decodeString(bytes);
```

### Connection Status

```typescript
// Check if connected
if (linuxio.isConnected()) {
  // WebSocket is open and ready
}

// Get current status
const status = linuxio.getStatus();
// Returns: "connecting" | "open" | "closed" | "error" | null
```

### Payload Builders

```typescript
// Terminal
linuxio.terminalPayload(cols, rows);

// Container
linuxio.containerPayload(containerId, shell, cols, rows);

// File upload
linuxio.uploadPayload(path, size);

// File download
linuxio.downloadPayload([path1, path2]);

// Compress
linuxio.compressPayload(paths, destination, format);

// Extract
linuxio.extractPayload(archive, destination);
```

## Error Handling

### LinuxIOError

```typescript
try {
  await linuxio.request("system", "invalid_command");
} catch (error) {
  if (error instanceof LinuxIOError) {
    console.error(`Error ${error.code}: ${error.message}`);
    // error.code: HTTP-like status code (400, 404, 500, etc.)
    // error.message: Human-readable error message
  }
}
```

### React Query Error Handling

```typescript
// Per-query error handling
const { error } = linuxio.call("system", "cpu");
if (error) {
  console.error(error.message);
}

// Global error handling (via QueryClient)
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      onError: (error) => {
        if (error instanceof LinuxIOError) {
          toast.error(error.message);
        }
      },
    },
  },
});
```

## TypeScript Types

```typescript
// Import types
import {
  linuxio,
  LinuxIOError,
  Stream,
  ProgressFrame,
  ResultFrame,
  StreamOptions,
} from "@/api/linuxio";

// Stream interface
interface Stream {
  id: number;
  type: string;
  status: "opening" | "open" | "closing" | "closed";
  write(data: Uint8Array): void;
  close(): void;
  abort(): void;
  onData: ((data: Uint8Array) => void) | null;
  onProgress: ((progress: ProgressFrame) => void) | null;
  onResult: ((result: ResultFrame) => void) | null;
  onClose: (() => void) | null;
}

// Progress frame
interface ProgressFrame {
  pct: number;        // Percentage (0-100)
  msg?: string;       // Optional status message
  bytes?: number;     // Bytes transferred
  total?: number;     // Total bytes
}

// Result frame
interface ResultFrame {
  status: "ok" | "error";
  data?: unknown;     // Result data (if ok)
  error?: string;     // Error message (if error)
  code?: number;      // HTTP-like status code
}
```

## React Query Integration

The `linuxio` API is fully integrated with your React Query client:

```typescript
// Global settings from ReactQueryProvider apply automatically
const { data } = linuxio.call("system", "cpu");

// Benefits:
// ✓ Automatic error toasts (via QueryCache.onError)
// ✓ Default retry: 1 attempt
// ✓ Default staleTime: 2000ms
// ✓ refetchOnWindowFocus: false
// ✓ refetchIntervalInBackground: true
// ✓ Shows in React Query DevTools

// Override per-query
const { data } = linuxio.call("system", "cpu", [], {
  staleTime: 5000,  // Override default
  retry: 3,         // Override default
});
```

## Frame Types Reference

| Opcode | Name | Payload | Usage |
|--------|------|---------|-------|
| `0x80` | OpStreamOpen | `type\0arg1\0arg2` | Open new stream (SYN flag) |
| `0x81` | OpStreamData | Raw bytes | Terminal output, file chunks |
| `0x82` | OpStreamClose | Empty | Close stream (FIN flag) |
| `0x83` | OpStreamResize | `[cols:2][rows:2]` | Terminal resize |
| `0x84` | OpStreamProgress | JSON bytes | Progress updates |
| `0x85` | OpStreamResult | JSON bytes | Final result |

**Note:** Opcodes are **bridge conventions**, not yamux protocol. At yamux level, everything is just DATA frames.

## Design Principles

### 1. Everything is a Stream
All communication uses yamux streams. The distinction between "API calls" and "streaming data" is just client-side convenience.

### 2. Single Source of Truth
The `linuxio` namespace is the only API you need. All imports come from `/api/linuxio`.

### 3. React Query First
Use `linuxio.call()` and `linuxio.mutate()` for all API operations. Get loading states, caching, and error handling for free.

### 4. Direct Access When Needed
Use `linuxio.stream()` for bidirectional communication (terminals, file transfers).

### 5. Type Safety
LinuxIOError provides structured errors with HTTP-like status codes.

### 6. WebSocket is Singleton
One WebSocket connection per session. All streams multiplexed over it. Managed automatically.

## Migration from streamApi

```typescript
// Old (streamApi)
import { streamApi } from "@/utils/streamApi";
import { useStreamQuery } from "@/hooks/useStreamApi";

const result = await streamApi.get("system", "cpu");
const { data } = useStreamQuery({ handlerType: "system", command: "cpu" });

// New (linuxio)
import { linuxio } from "@/api/linuxio";

const result = await linuxio.request("system", "cpu");
const { data } = linuxio.call("system", "cpu");
```

## File Location

- **Implementation**: `/frontend/src/api/linuxio.ts`
- **Documentation**: `/docs/frontendAPI.md` (this file)

## See Also

- [Server Yamux Protocol](./server-yamux-protocol.md) - Server implementation
- [Bridge Handler API](./bridge-handler-api.md) - How bridge handles streams
- [yamux-relay.md](./yamux-relay.md) - Complete architecture overview
