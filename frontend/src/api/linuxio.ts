/**
 * LinuxIO Common Utilities
 *
 * Shared utilities for stream multiplexer access.
 * For API calls, use:
 * - @/api/react-query for React Query hooks (useCall, useMutate)
 * - @/api/linuxio-core for direct calls (call, spawn, openStream)
 */

import { useEffect, useState, useCallback } from "react";
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
import { LinuxIOError, spawn } from "./linuxio-core";
import { useCall, useMutate } from "./react-query";

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
 * const { status, isOpen, openStream } = useStreamMux();
 */
export function useStreamMux() {
  const [status, setStatus] = useState<MuxStatus>(() => {
    const mux = getStreamMux();
    return mux?.status ?? "closed";
  });

  useEffect(() => {
    let unsubscribe: (() => void) | null = null;
    let checkInterval: ReturnType<typeof setInterval> | null = null;

    const setupListener = () => {
      const mux = getStreamMux();
      if (!mux) {
        setStatus("closed");
        return false;
      }

      // Update status immediately
      setStatus(mux.status);

      // Subscribe to status changes
      unsubscribe = mux.addStatusListener((newStatus: MuxStatus) => {
        setStatus(newStatus);
      });

      return true;
    };

    // Try to set up listener immediately
    if (!setupListener()) {
      // If mux doesn't exist yet, poll for it (handles late initialization)
      checkInterval = setInterval(() => {
        if (setupListener()) {
          // Successfully set up, stop polling
          if (checkInterval) {
            clearInterval(checkInterval);
            checkInterval = null;
          }
        }
      }, 100);
    }

    return () => {
      if (unsubscribe) {
        unsubscribe();
      }
      if (checkInterval) {
        clearInterval(checkInterval);
      }
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
// Payload Helpers (using new bridge protocol)
// ============================================================================

/**
 * Build payload for terminal stream
 */
export function terminalPayload(cols: number, rows: number): Uint8Array {
  return encodeString(`bridge\0terminal\0bash\0${cols}\0${rows}`);
}

/**
 * Build payload for docker logs stream
 */
export function dockerLogsPayload(
  containerId: string,
  tail: string = "100",
): Uint8Array {
  return encodeString(`docker-logs\0${containerId}\0${tail}`);
}

/**
 * Build payload for service logs stream (journalctl)
 */
export function serviceLogsPayload(
  serviceName: string,
  lines: string = "100",
): Uint8Array {
  return encodeString(`service-logs\0${serviceName}\0${lines}`);
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
  return encodeString(
    `bridge\0docker\0container_exec\0${containerId}\0${shell}\0${cols}\0${rows}`,
  );
}

/**
 * Build payload for file upload stream
 */
export function uploadPayload(path: string, size: number): Uint8Array {
  return encodeString(`bridge\0filebrowser\0upload\0${path}\0${size}`);
}

/**
 * Build payload for file download stream
 */
export function downloadPayload(paths: string[]): Uint8Array {
  return encodeString(`bridge\0filebrowser\0download\0${paths.join("\0")}`);
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
    `bridge\0filebrowser\0compress\0${paths.join("\0")}\0${destination}\0${format}`,
  );
}

/**
 * Build payload for archive extraction
 */
export function extractPayload(
  archive: string,
  destination: string,
): Uint8Array {
  return encodeString(
    `bridge\0filebrowser\0extract\0${archive}\0${destination}`,
  );
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
// Re-exports
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
export { LinuxIOError };

// ============================================================================
// useStream - Callback-based streaming for modules
// ============================================================================

export interface UseStreamOptions {
  onData?: (data: Uint8Array) => void;
  onResult?: (result: ResultFrame) => void;
  onProgress?: (progress: ProgressFrame) => void;
  onClose?: () => void;
  onError?: (error: Error) => void;
}

/**
 * Opens a stream with callback-style API for module use.
 *
 * @param handler - Handler name (e.g., "exec", "system")
 * @param command - Command name (e.g., "ls", "get_cpu_info")
 * @param args - Command arguments
 * @param options - Callback options
 * @returns The stream or SpawnedProcess, or null on error
 *
 * @example
 * // Execute a command (exec stream)
 * linuxio.useStream('exec', 'ls', ['-lh', '/home'], {
 *   onData: (data) => console.log(decodeString(data)),
 *   onResult: (result) => console.log('Exit code:', result.data?.exitCode),
 *   onClose: () => console.log('Stream closed')
 * });
 *
 * @example
 * // Call a bridge handler (RPC-style)
 * linuxio.useStream('system', 'get_cpu_info', [], {
 *   onResult: (result) => console.log(result.data)
 * });
 */
export function useStream(
  handler: string,
  command: string,
  args: string[] = [],
  options?: UseStreamOptions,
) {
  try {
    const mux = getStreamMux();
    if (!mux) {
      throw new Error("StreamMux not initialized");
    }

    // Special handling for exec streams - they use a different protocol
    if (handler === "exec") {
      const payloadParts = ["exec", command, ...args];
      const payload = encodeString(payloadParts.join("\0"));
      const stream = mux.openStream("exec", payload);

      if (options?.onData) {
        stream.onData = options.onData;
      }
      if (options?.onProgress) {
        stream.onProgress = options.onProgress;
      }
      if (options?.onResult) {
        stream.onResult = (result) => {
          options.onResult?.(result);
          options?.onClose?.();
        };
      }
      stream.onClose = () => {
        options?.onClose?.();
      };

      return stream;
    }

    // For bridge handlers, use spawn
    const process = spawn(handler, command, args, {
      onData: options?.onData,
      onProgress: options?.onProgress,
    });

    // Handle result and completion via the promise
    process
      .then((result) => {
        options?.onResult?.({ status: "ok", data: result });
        options?.onClose?.();
      })
      .catch((error: Error) => {
        options?.onError?.(error);
        options?.onClose?.();
      });

    return process;
  } catch (error) {
    options?.onError?.(
      error instanceof Error ? error : new Error(String(error)),
    );
    return null;
  }
}

// ============================================================================
// Default Export - Unified API for modules
// ============================================================================

const linuxio = {
  useCall,
  useMutate,
  useStream,
  decodeString,
  encodeString,
};

export default linuxio;
