/**
 * LinuxIO Unified API
 *
 * Single source of truth for all backend communication.
 * Everything flows through yamux streams over a single WebSocket connection.
 *
 * Usage:
 *   // API calls with React Query
 *   const { data, isLoading } = linuxio.call("system", "get_cpu_info");
 *
 *   // Direct stream access
 *   const stream = linuxio.stream("terminal", { onData: handleData });
 *   const upload = linuxio.stream("fb-upload", { onProgress: handleProgress });
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
 * Make an API call (non-React).
 * Opens stream, waits for result, returns Promise.
 *
 * @example
 * const cpuInfo = await linuxio.request("system", "get_cpu_info");
 * const containers = await linuxio.request("docker", "list_containers");
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

  // Build payload: "api\0handler\0command\0arg1\0arg2..."
  const parts = ["api", handler, command, ...args];
  const payload = encodeString(parts.join("\0"));

  const stream = mux.openStream("api", payload);
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
// React Query: call() Hook
// ============================================================================

type CallOptions<T> = Omit<
  UseQueryOptions<T, LinuxIOError>,
  "queryKey" | "queryFn"
>;

/**
 * Make an API call with React Query integration.
 * Provides loading states, caching, refetching, etc.
 *
 * @example
 * const { data, isLoading, error, refetch } = linuxio.call("system", "get_cpu_info");
 *
 * @example
 * const { data } = linuxio.call("docker", "get_stats", ["container-123"], {
 *   refetchInterval: 2000,
 *   staleTime: 1000,
 * });
 */
export function call<T = unknown>(
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
// React Query: mutate() Hook
// ============================================================================

type MutateOptions<TData, TVariables> = Omit<
  UseMutationOptions<TData, LinuxIOError, TVariables>,
  "mutationFn"
>;

/**
 * Make a mutation (write operation) with React Query.
 *
 * @example
 * const { mutate, isPending } = linuxio.mutate("docker", "start_container");
 * mutate("container-123");
 *
 * @example
 * const { mutate } = linuxio.mutate("docker", "create_container", {
 *   onSuccess: () => toast.success("Container created"),
 * });
 * mutate({ name: "my-container", image: "nginx" });
 */
export function mutate<TData = unknown, TVariables = unknown>(
  handler: string,
  command: string,
  options?: MutateOptions<TData, TVariables>,
) {
  return useMutation<TData, LinuxIOError, TVariables>({
    mutationFn: (variables: TVariables) => {
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
    },
    ...options,
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
 * const term = linuxio.stream("terminal", {
 *   onData: (data) => xterm.write(decodeString(data)),
 * });
 * term.write(encodeString("ls -la\n"));
 * term.close();
 *
 * @example
 * // File upload with progress
 * const upload = linuxio.stream("fb-upload", {
 *   onProgress: (p) => setProgress(p.pct),
 *   onResult: (r) => console.log("Done:", r),
 * });
 * upload.write(fileChunk);
 *
 * @example
 * // Package update with progress
 * const update = linuxio.stream("pkg-update", {
 *   onProgress: (p) => setProgress(p.pct),
 *   onResult: (r) => toast.success("Updated!"),
 * });
 */
export function stream(
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
  call,
  mutate,

  // Direct stream access
  stream,

  // Promise-based (non-React)
  request,

  // Payload builders
  terminalPayload,
  containerPayload,
  uploadPayload,
  downloadPayload,
  compressPayload,
  extractPayload,

  // Utilities
  isConnected,
  getStatus,
  encodeString,
  decodeString,
};

export default linuxio;
