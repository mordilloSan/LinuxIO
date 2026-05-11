/**
 * LinuxIO Common Utilities
 *
 * Shared utilities for stream multiplexer access and payload helpers.
 * App code should generally import from `@/api`; this module exists to
 * implement that public surface.
 */

import { useCallback, useSyncExternalStore } from "react";
import {
  getStreamMux,
  subscribeMuxInstanceChanged,
  type Stream,
  type MuxStatus,
  type StreamMultiplexer,
  type StreamType,
  encodeString,
} from "./StreamMultiplexer";

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

function makeSubscribeWithRebind(
  bindToMux: (
    mux: StreamMultiplexer,
    notifyStoreChanged: () => void,
  ) => () => void,
) {
  return (notifyStoreChanged: () => void) => {
    let muxUnsub: (() => void) | null = null;

    const rebind = (notify: boolean) => {
      muxUnsub?.();
      const mux = getStreamMux();
      muxUnsub = mux ? bindToMux(mux, notifyStoreChanged) : null;
      if (notify) {
        notifyStoreChanged();
      }
    };

    rebind(false);
    const instanceUnsub = subscribeMuxInstanceChanged(() => rebind(true));

    return () => {
      muxUnsub?.();
      instanceUnsub();
    };
  };
}

const subscribeToStatus = makeSubscribeWithRebind((mux, notifyStoreChanged) =>
  mux.addStatusListener(notifyStoreChanged),
);

const subscribeToUpdating = makeSubscribeWithRebind((mux, notifyStoreChanged) =>
  mux.addUpdatingListener(notifyStoreChanged),
);

function getStatusSnapshot(): MuxStatus {
  return getStreamMux()?.status ?? "closed";
}

function getUpdatingSnapshot(): boolean {
  return getStreamMux()?.isUpdating ?? false;
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
  const status = useSyncExternalStore(subscribeToStatus, getStatusSnapshot);

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
  return useSyncExternalStore(subscribeToUpdating, getUpdatingSnapshot);
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
  fieldFilters: string[] = [],
): Stream | null {
  return openMuxStream(
    "general-logs",
    encodeString(
      [
        "general-logs",
        lines,
        timePeriod,
        priority,
        identifier,
        ...fieldFilters,
      ].join("\0"),
    ),
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

export function openJobAttachStream(jobId: string): Stream | null {
  return openMuxStream(
    "jobs-attach",
    encodeString(["jobs-attach", jobId].join("\0")),
  );
}

export function openJobDataStream(
  jobId: string,
  offset: number = 0,
): Stream | null {
  return openMuxStream(
    "jobs-data",
    encodeString(["jobs-data", jobId, String(offset)].join("\0")),
  );
}

export function openJobEventsStream(): Stream | null {
  return openMuxStream("jobs-events", encodeString("jobs-events"));
}
