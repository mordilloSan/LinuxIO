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
import { LinuxIOError } from "./linuxio-core";

const STREAM_TYPE_TERMINAL = "terminal";
const STREAM_TYPE_CONTAINER = "container";
const STREAM_TYPE_DOCKER_LOGS = "docker-logs";
const STREAM_TYPE_SERVICE_LOGS = "service-logs";
const STREAM_TYPE_GENERAL_LOGS = "general-logs";
const STREAM_TYPE_DOCKER_COMPOSE = "docker-compose";
const STREAM_TYPE_DOCKER_REINDEX = "docker-reindex";
const STREAM_TYPE_EXEC = "exec";
const STREAM_TYPE_PKG_UPDATE = "pkg-update";
const STREAM_TYPE_SMART_TEST = "smart-test";
const STREAM_TYPE_FB_DOWNLOAD = "fb-download";
const STREAM_TYPE_FB_ARCHIVE = "fb-archive";
const STREAM_TYPE_FB_UPLOAD = "fb-upload";
const STREAM_TYPE_FB_COMPRESS = "fb-compress";
const STREAM_TYPE_FB_EXTRACT = "fb-extract";
const STREAM_TYPE_FB_REINDEX = "fb-reindex";
const STREAM_TYPE_FB_COPY = "fb-copy";
const STREAM_TYPE_FB_MOVE = "fb-move";

function isSingleFileDownload(paths: string[]): boolean {
  return paths.length === 1 && !paths[0].endsWith("/");
}

function openMuxStream(
  type: StreamType,
  initialPayload: Uint8Array,
): Stream | null {
  const mux = getStreamMux();
  if (!mux || mux.status !== "open") {
    return null;
  }
  return mux.openStream(type, initialPayload);
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

/**
 * Hook to track system update status.
 * Returns true when a system update is in progress and all API queries should be paused.
 */
export function useIsUpdating(): boolean {
  const [isUpdating, setIsUpdating] = useState<boolean>(() => {
    const mux = getStreamMux();
    return mux?.isUpdating ?? false;
  });

  useEffect(() => {
    const mux = getStreamMux();
    if (!mux) return;

    // Update immediately
    setIsUpdating(mux.isUpdating);

    // Subscribe to changes
    const unsubscribe = mux.addUpdatingListener((value: boolean) => {
      setIsUpdating(value);
    });

    return unsubscribe;
  }, []);

  return isUpdating;
}

// ============================================================================
// Payload Helpers (stream handler protocol)
// ============================================================================

/**
 * Build payload for terminal stream
 */
export function terminalPayload(cols: number, rows: number): Uint8Array {
  return encodeString(`${STREAM_TYPE_TERMINAL}\0${cols}\0${rows}`);
}

/**
 * Build payload for docker logs stream
 */
export function dockerLogsPayload(
  containerId: string,
  tail: string = "100",
): Uint8Array {
  return encodeString(`${STREAM_TYPE_DOCKER_LOGS}\0${containerId}\0${tail}`);
}

/**
 * Build payload for service logs stream (journalctl)
 */
export function serviceLogsPayload(
  serviceName: string,
  lines: string = "100",
): Uint8Array {
  return encodeString(`${STREAM_TYPE_SERVICE_LOGS}\0${serviceName}\0${lines}`);
}

/**
 * Build payload for general logs stream (journalctl)
 * @param lines - Number of initial lines to show (default "100")
 * @param timePeriod - Time range like "1h", "24h", "7d" (optional)
 * @param priority - Max priority level 0-7 (optional, empty = all)
 * @param identifier - Filter by SYSLOG_IDENTIFIER (optional, empty = all)
 */
export function generalLogsPayload(
  lines: string = "100",
  timePeriod: string = "",
  priority: string = "",
  identifier: string = "",
): Uint8Array {
  return encodeString(
    `${STREAM_TYPE_GENERAL_LOGS}\0${lines}\0${timePeriod}\0${priority}\0${identifier}`,
  );
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
    `${STREAM_TYPE_CONTAINER}\0${containerId}\0${shell}\0${cols}\0${rows}`,
  );
}

/**
 * Build payload for file upload stream
 */
export function uploadPayload(
  path: string,
  size: number,
  override: boolean = false,
): Uint8Array {
  const parts = [STREAM_TYPE_FB_UPLOAD, path, String(size)];
  if (override) {
    parts.push("true");
  }
  return encodeString(parts.join("\0"));
}

/**
 * Build payload for file download stream
 */
export function downloadPayload(paths: string[]): Uint8Array {
  if (paths.length === 0) {
    throw new Error("downloadPayload requires at least one path");
  }
  if (isSingleFileDownload(paths)) {
    return encodeString(`${STREAM_TYPE_FB_DOWNLOAD}\0${paths[0]}`);
  }
  return encodeString(`${STREAM_TYPE_FB_ARCHIVE}\0zip\0${paths.join("\0")}`);
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
    `${STREAM_TYPE_FB_COMPRESS}\0${format}\0${destination}\0${paths.join("\0")}`,
  );
}

/**
 * Build payload for archive extraction
 */
export function extractPayload(
  archive: string,
  destination?: string,
): Uint8Array {
  const parts = [STREAM_TYPE_FB_EXTRACT, archive];
  if (destination) {
    parts.push(destination);
  }
  return encodeString(parts.join("\0"));
}

/**
 * Build payload for package update stream
 */
export function packageUpdatePayload(packages: string[]): Uint8Array {
  if (packages.length === 0) {
    throw new Error("packageUpdatePayload requires at least one package");
  }
  return encodeString([STREAM_TYPE_PKG_UPDATE, ...packages].join("\0"));
}

/**
 * Build payload for exec stream
 */
export function execPayload(program: string, args: string[] = []): Uint8Array {
  return encodeString([STREAM_TYPE_EXEC, program, ...args].join("\0"));
}

/**
 * Build payload for SMART test stream
 */
export function smartTestPayload(device: string, testType: string): Uint8Array {
  return encodeString(`${STREAM_TYPE_SMART_TEST}\0${device}\0${testType}`);
}

/**
 * Build payload for docker-compose stream
 */
export function dockerComposePayload(
  action: "up" | "down" | "stop" | "restart",
  projectName: string,
  composePath?: string,
): Uint8Array {
  const parts = [STREAM_TYPE_DOCKER_COMPOSE, action, projectName];
  if (composePath) {
    parts.push(composePath);
  }
  return encodeString(parts.join("\0"));
}

/**
 * Build payload for docker reindex stream
 */
export function dockerReindexPayload(): Uint8Array {
  return encodeString(STREAM_TYPE_DOCKER_REINDEX);
}

/**
 * Build payload for file indexer stream
 */
export function fileIndexerPayload(path?: string): Uint8Array {
  const parts = [STREAM_TYPE_FB_REINDEX];
  if (path && path !== "/") {
    parts.push(path);
  }
  return encodeString(parts.join("\0"));
}

/**
 * Build payload for file copy stream
 */
export function fileCopyPayload(
  source: string,
  destination: string,
): Uint8Array {
  return encodeString([STREAM_TYPE_FB_COPY, source, destination].join("\0"));
}

/**
 * Build payload for file move stream
 */
export function fileMovePayload(
  source: string,
  destination: string,
): Uint8Array {
  return encodeString([STREAM_TYPE_FB_MOVE, source, destination].join("\0"));
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
// Stream Open Helpers
// ============================================================================

export function openTerminalStream(cols: number, rows: number): Stream | null {
  return openMuxStream(STREAM_TYPE_TERMINAL, terminalPayload(cols, rows));
}

export function openContainerStream(
  containerId: string,
  shell: string,
  cols: number,
  rows: number,
): Stream | null {
  return openMuxStream(
    STREAM_TYPE_CONTAINER,
    containerPayload(containerId, shell, cols, rows),
  );
}

export function openDockerLogsStream(
  containerId: string,
  tail: string = "100",
): Stream | null {
  return openMuxStream(
    STREAM_TYPE_DOCKER_LOGS,
    dockerLogsPayload(containerId, tail),
  );
}

export function openServiceLogsStream(
  serviceName: string,
  lines: string = "100",
): Stream | null {
  return openMuxStream(
    STREAM_TYPE_SERVICE_LOGS,
    serviceLogsPayload(serviceName, lines),
  );
}

export function openGeneralLogsStream(
  lines: string = "100",
  timePeriod: string = "",
  priority: string = "",
  identifier: string = "",
): Stream | null {
  return openMuxStream(
    STREAM_TYPE_GENERAL_LOGS,
    generalLogsPayload(lines, timePeriod, priority, identifier),
  );
}

export function openDockerComposeStream(
  action: "up" | "down" | "stop" | "restart",
  projectName: string,
  composePath?: string,
): Stream | null {
  return openMuxStream(
    STREAM_TYPE_DOCKER_COMPOSE,
    dockerComposePayload(action, projectName, composePath),
  );
}

export function openDockerReindexStream(): Stream | null {
  return openMuxStream(STREAM_TYPE_DOCKER_REINDEX, dockerReindexPayload());
}

export function openExecStream(
  program: string,
  args: string[] = [],
): Stream | null {
  return openMuxStream(STREAM_TYPE_EXEC, execPayload(program, args));
}

export function openPackageUpdateStream(packages: string[]): Stream | null {
  return openMuxStream(STREAM_TYPE_PKG_UPDATE, packageUpdatePayload(packages));
}

export function openSmartTestStream(
  device: string,
  testType: string,
): Stream | null {
  return openMuxStream(
    STREAM_TYPE_SMART_TEST,
    smartTestPayload(device, testType),
  );
}

export function openFileUploadStream(
  path: string,
  size: number,
  override: boolean = false,
): Stream | null {
  return openMuxStream(
    STREAM_TYPE_FB_UPLOAD,
    uploadPayload(path, size, override),
  );
}

export function openFileDownloadStream(paths: string[]): Stream | null {
  const streamType = isSingleFileDownload(paths)
    ? STREAM_TYPE_FB_DOWNLOAD
    : STREAM_TYPE_FB_ARCHIVE;
  return openMuxStream(streamType, downloadPayload(paths));
}

export function openFileCompressStream(
  paths: string[],
  destination: string,
  format: string,
): Stream | null {
  return openMuxStream(
    STREAM_TYPE_FB_COMPRESS,
    compressPayload(paths, destination, format),
  );
}

export function openFileExtractStream(
  archive: string,
  destination?: string,
): Stream | null {
  return openMuxStream(
    STREAM_TYPE_FB_EXTRACT,
    extractPayload(archive, destination),
  );
}

export function openFileIndexerStream(path?: string): Stream | null {
  return openMuxStream(STREAM_TYPE_FB_REINDEX, fileIndexerPayload(path));
}

export function openFileCopyStream(
  source: string,
  destination: string,
): Stream | null {
  return openMuxStream(
    STREAM_TYPE_FB_COPY,
    fileCopyPayload(source, destination),
  );
}

export function openFileMoveStream(
  source: string,
  destination: string,
): Stream | null {
  return openMuxStream(
    STREAM_TYPE_FB_MOVE,
    fileMovePayload(source, destination),
  );
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
