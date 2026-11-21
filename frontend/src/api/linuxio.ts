/**
 * LinuxIO Common Utilities
 *
 * Shared utilities for stream multiplexer access and payload helpers.
 * App code should generally import from `@/api`; this module exists to
 * implement that public surface.
 */

import { useCallback, useSyncExternalStore } from "react";

import type { AssistantAgentId } from "./acp/agents";
import type {
  LinuxIOStreamSchema,
  StreamRouteName,
} from "./generated/linuxio-types";
import { streamOpenPayload } from "./linuxio-core";
import {
  getStreamMux,
  type MuxStatus,
  type Stream,
  type StreamMultiplexer,
  type StreamType,
  subscribeMuxInstanceChanged,
} from "./StreamMultiplexer";

function openMuxStream(
  type: StreamType,
  initialPayload: Uint8Array,
  options?: { cache?: boolean },
): Stream | null {
  const mux = getStreamMux();
  if (!mux || mux.status !== "open") {
    return null;
  }
  return mux.openStream(type, initialPayload, options);
}

/**
 * Argument tuple for opening a stream route: nothing when the route takes no
 * request, otherwise the Go-generated wire request — so route names and
 * request shapes are checked against the backend contract.
 */
type StreamRouteArgs<R extends StreamRouteName> =
  LinuxIOStreamSchema[R] extends void ? [] : [request: LinuxIOStreamSchema[R]];

export function openChannel<R extends StreamRouteName>(
  route: R,
  ...args: StreamRouteArgs<R>
): Stream | null {
  return openMuxStream(route, streamOpenPayload(route, args[0]));
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

/** Subscribe to changes that determine whether API requests may run. */
export function subscribeRequestAvailability(
  notifyStoreChanged: () => void,
): () => void {
  return subscribeToStatus(notifyStoreChanged);
}

/** Whether the shared request transport is open. */
export function isRequestAvailable(): boolean {
  return getStreamMux()?.status === "open";
}

function getStatusSnapshot(): MuxStatus {
  return getStreamMux()?.status ?? "closed";
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
  return openChannel("terminal.open", { cols, rows });
}

/** An agent login PTY: never cached, so it runs beside the user's shell. */
export function openTerminalLoginStream(
  cols: number,
  rows: number,
  login: { agent: AssistantAgentId; args: string[] },
): Stream | null {
  return openMuxStream(
    "terminal.open",
    streamOpenPayload("terminal.open", {
      cols,
      rows,
      agent: login.agent,
      args: login.args,
    }),
    { cache: false },
  );
}

export function openContainerStream(
  containerId: string,
  shell: string,
  cols: number,
  rows: number,
): Stream | null {
  return openChannel("container.open", { containerId, shell, cols, rows });
}

export function openTaskWatchStream(taskId: string): Stream | null {
  return openChannel("tasks.watch", { taskId });
}

export function openTaskDataStream(
  taskId: string,
  offset: number = 0,
): Stream | null {
  return openChannel("tasks.data", { taskId, offset: String(offset) });
}

export function openVMConsoleStream(name: string): Stream | null {
  return openChannel("virt.console_open", { name });
}

export function openTaskEventsStream(): Stream | null {
  return openChannel("tasks.events");
}
