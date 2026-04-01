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
// Stream Openers
// ============================================================================

export function openTerminalStream(cols: number, rows: number): Stream | null {
  return openMuxStream(
    "terminal",
    encodeString(["terminal", cols, rows].join("\0")),
  );
}

export function openContainerStream(
  containerId: string,
  shell: string,
  cols: number,
  rows: number,
): Stream | null {
  return openMuxStream(
    "container",
    encodeString(["container", containerId, shell, cols, rows].join("\0")),
  );
}

export function openDockerLogsStream(
  containerId: string,
  tail: string = "100",
): Stream | null {
  return openMuxStream(
    "docker-logs",
    encodeString(["docker-logs", containerId, tail].join("\0")),
  );
}

export function openServiceLogsStream(
  serviceName: string,
  lines: string = "100",
): Stream | null {
  return openMuxStream(
    "service-logs",
    encodeString(["service-logs", serviceName, lines].join("\0")),
  );
}

export function openGeneralLogsStream(
  lines: string = "100",
  timePeriod: string = "",
  priority: string = "",
  identifier: string = "",
): Stream | null {
  return openMuxStream(
    "general-logs",
    encodeString(
      ["general-logs", lines, timePeriod, priority, identifier].join("\0"),
    ),
  );
}

export function openDockerComposeStream(
  action: "up" | "down" | "stop" | "restart",
  projectName: string,
  composePath?: string,
): Stream | null {
  const parts = ["docker-compose", action, projectName];
  if (composePath) parts.push(composePath);
  return openMuxStream("docker-compose", encodeString(parts.join("\0")));
}

export function openDockerIndexerStream(): Stream | null {
  return openMuxStream("docker-indexer", encodeString("docker-indexer"));
}

export function openDockerIndexerAttachStream(): Stream | null {
  return openMuxStream(
    "docker-indexer-attach",
    encodeString("docker-indexer-attach"),
  );
}

export function openAppUpdateStream(
  runId: string,
  version?: string,
): Stream | null {
  const parts = ["app-update", runId];
  if (version) parts.push(version);
  return openMuxStream("app-update", encodeString(parts.join("\0")));
}

export function openPackageUpdateStream(packages: string[]): Stream | null {
  if (packages.length === 0) return null;
  return openMuxStream(
    "pkg-update",
    encodeString(["pkg-update", ...packages].join("\0")),
  );
}

export function openSmartTestStream(
  device: string,
  testType: string,
): Stream | null {
  return openMuxStream(
    "smart-test",
    encodeString(["smart-test", device, testType].join("\0")),
  );
}

export function openFileUploadStream(
  path: string,
  size: number,
  override: boolean = false,
): Stream | null {
  const parts = ["fb-upload", path, String(size)];
  if (override) parts.push("true");
  return openMuxStream("fb-upload", encodeString(parts.join("\0")));
}

export function openFileDownloadStream(paths: string[]): Stream | null {
  if (paths.length === 0) return null;
  if (isSingleFileDownload(paths)) {
    return openMuxStream(
      "fb-download",
      encodeString(["fb-download", paths[0]].join("\0")),
    );
  }
  return openMuxStream(
    "fb-archive",
    encodeString(["fb-archive", "zip", ...paths].join("\0")),
  );
}

export function openFileCompressStream(
  paths: string[],
  destination: string,
  format: string,
): Stream | null {
  return openMuxStream(
    "fb-compress",
    encodeString(["fb-compress", format, destination, ...paths].join("\0")),
  );
}

export function openFileExtractStream(
  archive: string,
  destination?: string,
): Stream | null {
  const parts = ["fb-extract", archive];
  if (destination) parts.push(destination);
  return openMuxStream("fb-extract", encodeString(parts.join("\0")));
}

export function openFileIndexerStream(path?: string): Stream | null {
  const parts = ["fb-reindex"];
  if (path && path !== "/") parts.push(path);
  return openMuxStream("fb-reindex", encodeString(parts.join("\0")));
}

export function openFileIndexerAttachStream(): Stream | null {
  return openMuxStream("fb-indexer-attach", encodeString("fb-indexer-attach"));
}

export function openFileCopyStream(
  source: string,
  destination: string,
): Stream | null {
  return openMuxStream(
    "fb-copy",
    encodeString(["fb-copy", source, destination].join("\0")),
  );
}

export function openFileMoveStream(
  source: string,
  destination: string,
): Stream | null {
  return openMuxStream(
    "fb-move",
    encodeString(["fb-move", source, destination].join("\0")),
  );
}
