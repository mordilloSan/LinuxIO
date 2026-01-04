/**
 * LinuxIO Unified API
 *
 * Single source of truth for all backend communication.
 * Everything flows through yamux streams over a single WebSocket connection.
 *
 * Usage:
 *   // API calls with React Query
 *   const { data, isLoading } = linuxio.useCall("system", "get_cpu_info");
 *
 *   // Direct stream access
 *   const stream = linuxio.useStream("terminal", { onData: handleData });
 *   const upload = linuxio.useStream("fb-upload", { onProgress: handleProgress });
 */

import { useEffect, useState, useCallback } from "react";
import {
  useQuery,
  useMutation,
  type UseQueryOptions,
  type UseMutationOptions,
} from "@tanstack/react-query";
import {
  getStreamMux,
  initStreamMux,
  closeStreamMux,
  waitForStreamMux,
  STREAM_CHUNK_SIZE,
  UPLOAD_WINDOW_SIZE,
  type Stream,
  type ProgressFrame,
  type ResultFrame,
  type MuxStatus,
  type StreamType,
  encodeString,
  decodeString,
} from "./StreamMultiplexer";

// ============================================================================
// Types
// ============================================================================

export class LinuxIOError extends Error {
  constructor(
    message: string,
    public code: number = 500,
  ) {
    super(message);
    this.name = "LinuxIOError";
  }
}

export interface StreamOptions {
  onData?: (data: Uint8Array) => void;
  onProgress?: (progress: ProgressFrame) => void;
  onResult?: (result: ResultFrame) => void;
  onClose?: () => void;
}

type CallOptions<T> = Omit<
  UseQueryOptions<T, LinuxIOError>,
  "queryKey" | "queryFn"
>;

// Allow custom mutationFn to be passed for complex operations
type MutateOptions<TData, TVariables> = UseMutationOptions<
  TData,
  LinuxIOError,
  TVariables
>;

// ============================================================================
// React Hook: useStreamMux
// ============================================================================

/**
 * Hook to use the singleton StreamMultiplexer.
 *
 * The multiplexer is initialized by AuthContext on login.
 * This hook provides access to it and tracks status changes.
 *
 * @example
 * const { status, isOpen, openStream } = linuxio.useStreamMux();
 */
export function useStreamMux() {
  const [status, setStatus] = useState<MuxStatus>(() => {
    const mux = getStreamMux();
    return mux?.status ?? "closed";
  });

  useEffect(() => {
    const mux = getStreamMux();
    if (!mux) return;

    // Update status immediately
    setStatus(mux.status);

    // Subscribe to status changes
    const unsubscribe = mux.addStatusListener((newStatus: MuxStatus) => {
      setStatus(newStatus);
    });

    return () => {
      unsubscribe();
    };
  }, []);

  const openStream = useCallback(
    (type: StreamType, initialPayload?: Uint8Array): Stream | null => {
      const mux = getStreamMux();
      if (!mux || mux.status !== "open") {
        console.warn("[useStreamMux] Cannot open stream - mux not ready");
        return null;
      }
      return mux.openStream(type, initialPayload);
    },
    [],
  );

  const getStream = useCallback((type: StreamType): Stream | null => {
    const mux = getStreamMux();
    if (!mux) return null;
    return mux.getStream(type);
  }, []);

  return {
    status,
    isOpen: status === "open",
    openStream,
    getStream,
  };
}

// ============================================================================
// Core: Request Function (Promise-based)
// ============================================================================

/**
 * Low-level API call function.
 *
 * ⚠️ **DO NOT USE DIRECTLY** ⚠️
 *
 * This is an internal function. Use React hooks instead:
 * - For queries (reads): `linuxio.useCall()`
 * - For mutations (writes): `linuxio.useMutate()`
 *
 * **Only use this inside:**
 * - Custom `mutationFn` for complex parallel operations
 * - Hook implementations (useCall, useMutate internals)
 *
 * @internal
 * @example
 * // ❌ BAD - Don't do this
 * await linuxio.request("docker", "start_container", [id]);
 *
 * @example
 * // ✅ GOOD - Use the hook
 * const start = linuxio.useMutate("docker", "start_container");
 * start.mutate(id);
 *
 * @example
 * // ✅ OK - Inside custom mutationFn for complex operations
 * const deleteMany = linuxio.useMutate("files", "delete", {
 *   mutationFn: async (paths: string[]) => {
 *     await Promise.all(paths.map(p => linuxio.request("files", "delete", [p])));
 *   }
 * });
 */
export async function request<T = unknown>(
  handler: string,
  command: string,
  args: string[] = [],
  timeoutMs = 30000,
): Promise<T> {
  const mux = getStreamMux();
  if (!mux || mux.status !== "open") {
    throw new LinuxIOError("Not connected to server", 503);
  }

  // Build payload: "json\0handler\0command\0arg1\0arg2..."
  const parts = ["json", handler, command, ...args];
  const payload = encodeString(parts.join("\0"));

  const stream = mux.openStream("json", payload);
  if (!stream) {
    throw new LinuxIOError("Failed to open stream", 503);
  }

  return new Promise<T>((resolve, reject) => {
    let resolved = false;

    const timeout = setTimeout(() => {
      if (!resolved) {
        resolved = true;
        stream.abort();
        reject(new LinuxIOError(`Timeout: ${handler}/${command}`, 408));
      }
    }, timeoutMs);

    stream.onResult = (result: ResultFrame) => {
      if (resolved) return;
      resolved = true;
      clearTimeout(timeout);

      if (result.status === "ok") {
        resolve(result.data as T);
      } else {
        reject(
          new LinuxIOError(result.error || "Unknown error", result.code || 500),
        );
      }
    };

    stream.onClose = () => {
      if (!resolved) {
        resolved = true;
        clearTimeout(timeout);
        reject(new LinuxIOError("Stream closed unexpectedly", 500));
      }
    };
  });
}

// ============================================================================
// React Query: useCall() Hook
// ============================================================================

/**
 * Make an API call with React Query integration.
 * Provides loading states, caching, refetching, etc.
 *
 * @example
 * const { data, isLoading, error, refetch } = linuxio.useCall("system", "get_cpu_info");
 *
 * @example
 * const { data } = linuxio.useCall("docker", "get_stats", ["container-123"], {
 *   refetchInterval: 2000,
 *   staleTime: 1000,
 * });
 */
export function useCall<T = unknown>(
  handler: string,
  command: string,
  args: string[] = [],
  options?: CallOptions<T>,
) {
  const { isOpen } = useStreamMux();
  const { enabled = true, ...queryOptions } = options || {};

  return useQuery<T, LinuxIOError>({
    queryKey: ["linuxio", handler, command, ...args],
    queryFn: () => request<T>(handler, command, args),
    enabled: isOpen && enabled,
    ...queryOptions,
  });
}

// ============================================================================
// React Query: useMutate() Hook
// ============================================================================

/**
 * Make a mutation (write operation) with React Query.
 *
 * Provides a default mutationFn that calls linuxio.request(),
 * but you can override it by passing a custom mutationFn in options.
 *
 * @example
 * // Simple usage - auto-generates mutationFn
 * const { mutate, isPending } = linuxio.useMutate("docker", "start_container");
 * mutate("container-123");
 *
 * @example
 * // With callbacks
 * const { mutate } = linuxio.useMutate("docker", "create_container", {
 *   onSuccess: () => toast.success("Container created"),
 * });
 * mutate({ name: "my-container", image: "nginx" });
 *
 * @example
 * // With custom mutationFn for complex operations
 * const deleteMutation = linuxio.useMutate("files", "delete", {
 *   mutationFn: async (paths: string[]) => {
 *     await Promise.all(paths.map(path => linuxio.request("files", "delete", [path])));
 *   },
 *   onSuccess: () => toast.success("Files deleted"),
 * });
 */
export function useMutate<TData = unknown, TVariables = unknown>(
  handler: string,
  command: string,
  options?: MutateOptions<TData, TVariables>,
) {
  // Default mutationFn - can be overridden by options.mutationFn
  const defaultMutationFn = (variables: TVariables) => {
    // Handle different variable types
    let args: string[];

    if (variables === undefined || variables === null) {
      args = [];
    } else if (Array.isArray(variables)) {
      args = variables.map(String);
    } else if (typeof variables === "string") {
      args = [variables];
    } else if (typeof variables === "object") {
      // Object - JSON stringify as last arg
      args = [JSON.stringify(variables)];
    } else {
      args = [String(variables)];
    }

    return request<TData>(handler, command, args);
  };

  return useMutation<TData, LinuxIOError, TVariables>({
    mutationFn: defaultMutationFn,
    ...options, // Custom mutationFn in options will override defaultMutationFn
  });
}

// ============================================================================
// Direct Stream Access
// ============================================================================

/**
 * Open a stream with custom callbacks.
 * Use this for terminals, file transfers, long-running operations.
 *
 * @example
 * // Terminal
 * const term = linuxio.useStream("terminal", {
 *   onData: (data) => xterm.write(decodeString(data)),
 * });
 * term.write(encodeString("ls -la\n"));
 * term.close();
 *
 * @example
 * // File upload with progress
 * const upload = linuxio.useStream("fb-upload", {
 *   onProgress: (p) => setProgress(p.pct),
 *   onResult: (r) => console.log("Done:", r),
 * });
 * upload.write(fileChunk);
 *
 * @example
 * // Package update with progress
 * const update = linuxio.useStream("pkg-update", {
 *   onProgress: (p) => setProgress(p.pct),
 *   onResult: (r) => toast.success("Updated!"),
 * });
 */
export function useStream(
  type: string,
  payloadOrOptions: Uint8Array | StreamOptions,
  options?: StreamOptions,
): Stream {
  const mux = getStreamMux();
  if (!mux || mux.status !== "open") {
    throw new LinuxIOError("Not connected to server", 503);
  }

  // Parse arguments
  let payload: Uint8Array;
  let opts: StreamOptions;

  if (payloadOrOptions instanceof Uint8Array) {
    payload = payloadOrOptions;
    opts = options || {};
  } else {
    // No payload provided - empty payload
    payload = new Uint8Array(0);
    opts = payloadOrOptions;
  }

  const s = mux.openStream(type, payload);
  if (!s) {
    throw new LinuxIOError("Failed to open stream", 503);
  }

  // Wire up callbacks
  if (opts.onData) s.onData = opts.onData;
  if (opts.onProgress) s.onProgress = opts.onProgress;
  if (opts.onResult) s.onResult = opts.onResult;
  if (opts.onClose) s.onClose = opts.onClose;

  return s;
}

// ============================================================================
// Helpers for Stream Payloads
// ============================================================================

/**
 * Build payload for terminal stream
 */
export function terminalPayload(cols: number, rows: number): Uint8Array {
  return encodeString(`terminal\0${cols}\0${rows}`);
}

/**
 * Build payload for container terminal stream
 */
export function containerPayload(
  containerId: string,
  shell: string,
  cols: number,
  rows: number,
): Uint8Array {
  return encodeString(`container\0${containerId}\0${shell}\0${cols}\0${rows}`);
}

/**
 * Build payload for file upload stream
 */
export function uploadPayload(path: string, size: number): Uint8Array {
  return encodeString(`fb-upload\0${path}\0${size}`);
}

/**
 * Build payload for file download stream
 */
export function downloadPayload(paths: string[]): Uint8Array {
  return encodeString(`fb-download\0${paths.join("\0")}`);
}

/**
 * Build payload for archive compression
 */
export function compressPayload(
  paths: string[],
  destination: string,
  format: string,
): Uint8Array {
  return encodeString(
    `fb-compress\0${paths.join("\0")}\0${destination}\0${format}`,
  );
}

/**
 * Build payload for archive extraction
 */
export function extractPayload(
  archive: string,
  destination: string,
): Uint8Array {
  return encodeString(`fb-extract\0${archive}\0${destination}`);
}

/**
 * Build payload for exec stream
 * @param command - The command to execute
 * @param args - Command arguments as a single string or array
 *
 * @example
 * execPayload('ls', '-lh /home')
 * execPayload('ls', ['-lh', '/home'])
 */
export function execPayload(
  command: string,
  args?: string | string[],
): Uint8Array {
  const parts = ["exec", command];

  if (args) {
    if (Array.isArray(args)) {
      parts.push(...args);
    } else {
      // Split string by spaces, respecting quotes
      const argArray = args.match(/(?:[^\s"]+|"[^"]*")+/g) || [];
      parts.push(...argArray.map((arg) => arg.replace(/^"|"$/g, "")));
    }
  }

  return encodeString(parts.join("\0"));
}

// ============================================================================
// Utilities
// ============================================================================

/**
 * Check if connected to server
 */
export function isConnected(): boolean {
  const mux = getStreamMux();
  return mux !== null && mux.status === "open";
}

/**
 * Get connection status
 */
export function getStatus(): "connecting" | "open" | "closed" | "error" | null {
  const mux = getStreamMux();
  return mux?.status ?? null;
}

// ============================================================================
// Re-exports from StreamMultiplexer
// ============================================================================

export {
  encodeString,
  decodeString,
  getStreamMux,
  initStreamMux,
  closeStreamMux,
  waitForStreamMux,
  STREAM_CHUNK_SIZE,
  UPLOAD_WINDOW_SIZE,
};
export type { Stream, ProgressFrame, ResultFrame, MuxStatus, StreamType };

// ============================================================================
// Default Export (Namespace)
// ============================================================================

/**
 * LinuxIO API - Single entry point for all backend communication
 */
export const linuxio = {
  // React hooks
  useStreamMux,
  useCall,
  useMutate,
  useStream,

  // Promise-based (non-React)
  request,

  // Payload builders
  terminalPayload,
  containerPayload,
  uploadPayload,
  downloadPayload,
  compressPayload,
  extractPayload,
  execPayload,

  // Utilities
  isConnected,
  getStatus,
  encodeString,
  decodeString,
};

export default linuxio;
