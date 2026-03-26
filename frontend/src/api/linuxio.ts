/**
 * LinuxIO Common Utilities
 *
 * Shared utilities for stream multiplexer access and payload helpers.
 * App code should generally import from `@/api`; this module exists to
 * implement that public surface.
 */

import { useEffect, useState, useCallback } from "react";
import {
  getStreamMux,
  type Stream,
  type MuxStatus,
  type StreamType,
  encodeString,
} from "./StreamMultiplexer";

const STREAM_TYPE_TERMINAL = "terminal";
const STREAM_TYPE_CONTAINER = "container";
const STREAM_TYPE_DOCKER_LOGS = "docker-logs";
const STREAM_TYPE_SERVICE_LOGS = "service-logs";
const STREAM_TYPE_GENERAL_LOGS = "general-logs";
const STREAM_TYPE_DOCKER_COMPOSE = "docker-compose";
const STREAM_TYPE_DOCKER_INDEXER = "docker-indexer";
const STREAM_TYPE_DOCKER_INDEXER_ATTACH = "docker-indexer-attach";
const STREAM_TYPE_EXEC = "exec";
const STREAM_TYPE_PKG_UPDATE = "pkg-update";
const STREAM_TYPE_SMART_TEST = "smart-test";
const STREAM_TYPE_FB_DOWNLOAD = "fb-download";
const STREAM_TYPE_FB_ARCHIVE = "fb-archive";
const STREAM_TYPE_FB_UPLOAD = "fb-upload";
const STREAM_TYPE_FB_COMPRESS = "fb-compress";
const STREAM_TYPE_FB_EXTRACT = "fb-extract";
const STREAM_TYPE_FB_REINDEX = "fb-reindex";
const STREAM_TYPE_FB_INDEXER_ATTACH = "fb-indexer-attach";
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
 * const { status, isOpen, getStream } = useStreamMux();
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

  const getStream = useCallback((type: StreamType): Stream | null => {
    const mux = getStreamMux();
    if (!mux) return null;
    return mux.getStream(type);
  }, []);

  return {
    status,
    isOpen: status === "open",
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

function terminalPayload(cols: number, rows: number): Uint8Array {
  return encodeString([STREAM_TYPE_TERMINAL, cols, rows].join("\0"));
}

function dockerLogsPayload(
  containerId: string,
  tail: string = "100",
): Uint8Array {
  return encodeString([STREAM_TYPE_DOCKER_LOGS, containerId, tail].join("\0"));
}

function serviceLogsPayload(
  serviceName: string,
  lines: string = "100",
): Uint8Array {
  return encodeString(
    [STREAM_TYPE_SERVICE_LOGS, serviceName, lines].join("\0"),
  );
}

function generalLogsPayload(
  lines: string = "100",
  timePeriod: string = "",
  priority: string = "",
  identifier: string = "",
): Uint8Array {
  return encodeString(
    [STREAM_TYPE_GENERAL_LOGS, lines, timePeriod, priority, identifier].join(
      "\0",
    ),
  );
}

function containerPayload(
  containerId: string,
  shell: string,
  cols: number,
  rows: number,
): Uint8Array {
  return encodeString(
    [STREAM_TYPE_CONTAINER, containerId, shell, cols, rows].join("\0"),
  );
}

function uploadPayload(
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

function downloadPayload(paths: string[]): Uint8Array {
  if (isSingleFileDownload(paths)) {
    return encodeString([STREAM_TYPE_FB_DOWNLOAD, paths[0]].join("\0"));
  }
  return encodeString([STREAM_TYPE_FB_ARCHIVE, "zip", ...paths].join("\0"));
}

function compressPayload(
  paths: string[],
  destination: string,
  format: string,
): Uint8Array {
  return encodeString(
    [STREAM_TYPE_FB_COMPRESS, format, destination, ...paths].join("\0"),
  );
}

function extractPayload(archive: string, destination?: string): Uint8Array {
  const parts = [STREAM_TYPE_FB_EXTRACT, archive];
  if (destination) {
    parts.push(destination);
  }
  return encodeString(parts.join("\0"));
}

function packageUpdatePayload(packages: string[]): Uint8Array {
  return encodeString([STREAM_TYPE_PKG_UPDATE, ...packages].join("\0"));
}

function execPayload(program: string, args: string[] = []): Uint8Array {
  return encodeString([STREAM_TYPE_EXEC, program, ...args].join("\0"));
}

function smartTestPayload(device: string, testType: string): Uint8Array {
  return encodeString([STREAM_TYPE_SMART_TEST, device, testType].join("\0"));
}

function dockerComposePayload(
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

function dockerIndexerPayload(): Uint8Array {
  return encodeString(STREAM_TYPE_DOCKER_INDEXER);
}

function dockerIndexerAttachPayload(): Uint8Array {
  return encodeString(STREAM_TYPE_DOCKER_INDEXER_ATTACH);
}

function fileIndexerPayload(path?: string): Uint8Array {
  const parts = [STREAM_TYPE_FB_REINDEX];
  if (path && path !== "/") {
    parts.push(path);
  }
  return encodeString(parts.join("\0"));
}

function fileIndexerAttachPayload(): Uint8Array {
  return encodeString(STREAM_TYPE_FB_INDEXER_ATTACH);
}

function fileCopyPayload(source: string, destination: string): Uint8Array {
  return encodeString([STREAM_TYPE_FB_COPY, source, destination].join("\0"));
}

function fileMovePayload(source: string, destination: string): Uint8Array {
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

export function openDockerIndexerStream(): Stream | null {
  return openMuxStream(STREAM_TYPE_DOCKER_INDEXER, dockerIndexerPayload());
}

export function openDockerIndexerAttachStream(): Stream | null {
  return openMuxStream(
    STREAM_TYPE_DOCKER_INDEXER_ATTACH,
    dockerIndexerAttachPayload(),
  );
}

export function openExecStream(
  program: string,
  args: string[] = [],
): Stream | null {
  return openMuxStream(STREAM_TYPE_EXEC, execPayload(program, args));
}

export function openPackageUpdateStream(packages: string[]): Stream | null {
  if (packages.length === 0) return null;
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
  if (paths.length === 0) return null;
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

export function openFileIndexerAttachStream(): Stream | null {
  return openMuxStream(STREAM_TYPE_FB_INDEXER_ATTACH, fileIndexerAttachPayload());
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
