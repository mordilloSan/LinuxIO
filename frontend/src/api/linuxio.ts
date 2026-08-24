/**
 * LinuxIO Common Utilities
 *
 * Shared utilities for stream multiplexer access and payload helpers.
 * App code should generally import from `@/api`; this module exists to
 * implement that public surface.
 */

import { useCallback, useSyncExternalStore } from "react";

import type {
  LinuxIOStreamSchema,
  StreamRouteName,
  TaskSnapshot,
} from "./generated/linuxio-types";
import { request as bridgeRequest } from "./linuxio-core";
import {
  encodeString,
  getStreamMux,
  type MuxStatus,
  type ProgressFrame,
  type ResultFrame,
  type Stream,
  type StreamMultiplexer,
  type StreamStatus,
  type StreamType,
  subscribeMuxInstanceChanged,
} from "./StreamMultiplexer";
import { isTerminalTaskState } from "./task-state";

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

function streamOpenPayload(route: string, request: unknown = {}): Uint8Array {
  return encodeString(
    JSON.stringify({
      route,
      request: request ?? {},
    }),
  );
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

let nextTaskDataStreamID = -1;

type TaskDataProgress = {
  type?: unknown;
  data?: unknown;
};

function routeParts(route: string): [string, string] {
  const dot = route.indexOf(".");
  if (dot <= 0 || dot === route.length - 1) {
    throw new Error(`Invalid bridge route: ${route}`);
  }
  return [route.slice(0, dot), route.slice(dot + 1)];
}

function dataProgressValue(progress: unknown): string | null {
  if (!progress || typeof progress !== "object") {
    return null;
  }
  const candidate = progress as TaskDataProgress;
  if (candidate.type !== "data") {
    return null;
  }
  if (typeof candidate.data === "string") {
    return candidate.data;
  }
  if (candidate.data == null) {
    return "";
  }
  if (
    typeof candidate.data === "number" ||
    typeof candidate.data === "boolean"
  ) {
    return String(candidate.data);
  }
  // Objects and arrays would stringify to "[object Object]"; emit JSON instead.
  return JSON.stringify(candidate.data) ?? "";
}

class TaskDataStream implements Stream {
  readonly id = nextTaskDataStreamID--;
  readonly type: StreamType;
  onData: ((data: Uint8Array) => void) | null = null;
  onClose: (() => void) | null = null;
  onProgress: ((progress: ProgressFrame) => void) | null = null;
  onResult: ((result: ResultFrame) => void) | null = null;

  private _status: StreamStatus = "opening";
  private watchStream: Stream | null = null;
  private taskId: string | null = null;
  private closed = false;
  private readonly request: unknown;
  private readonly cancelOnClose: boolean;

  constructor(route: string, request: unknown, cancelOnClose: boolean) {
    this.request = request;
    this.type = route;
    this.cancelOnClose = cancelOnClose;
    void this.start(route);
  }

  get status(): StreamStatus {
    return this._status;
  }

  write(): void {
    // Task data streams are receive-only.
  }

  resize(): void {
    // Not applicable to receive-only Task data streams.
  }

  close(): void {
    this.stop(false);
  }

  abort(): void {
    this.stop(true);
  }

  private async start(route: string): Promise<void> {
    try {
      const [handler, command] = routeParts(route);
      const snapshot = await bridgeRequest<TaskSnapshot>(
        handler,
        command,
        this.request,
      );
      if (this.closed) {
        if (this.cancelOnClose) {
          void this.cancelTask(snapshot.id);
        }
        return;
      }

      this.taskId = snapshot.id;
      const watch = openTaskWatchStream(snapshot.id);
      if (!watch) {
        if (isTerminalTaskState(snapshot.state)) {
          this.forwardProgress(snapshot.progress);
          this.forwardTerminalSnapshot(snapshot);
          return;
        }
        this.forwardError("Failed to watch task", "stream_unavailable");
        return;
      }

      this.watchStream = watch;
      this._status = "open";
      watch.onData = (data) => this.onData?.(data);
      watch.onProgress = (progress) => this.forwardProgress(progress);
      watch.onResult = (result) => {
        this.onResult?.(result);
        this.markClosed();
      };
      watch.onClose = () => this.markClosed();
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to start task";
      const code =
        typeof error === "object" && error !== null && "code" in error
          ? (error as { code?: string | number }).code
          : undefined;
      this.forwardError(message, code);
    }
  }

  private forwardProgress(progress: unknown): void {
    const data = dataProgressValue(progress);
    if (data !== null) {
      this.onData?.(encodeString(data));
      return;
    }
    if (progress !== undefined && progress !== null) {
      this.onProgress?.(progress as ProgressFrame);
    }
  }

  private forwardTerminalSnapshot(snapshot: TaskSnapshot): void {
    if (snapshot.state === "completed") {
      this.onResult?.({ status: "ok", data: snapshot.result });
    } else {
      this.onResult?.({
        status: "error",
        error: snapshot.error?.message ?? "Task failed",
        code: snapshot.error?.code,
      });
    }
    this.markClosed();
  }

  private forwardError(message: string, code?: string | number): void {
    this.onResult?.({
      status: "error",
      error: message,
      code,
    });
    this.markClosed();
  }

  private stop(abort: boolean): void {
    if (this.closed) return;
    if (abort) {
      this.watchStream?.abort();
    } else {
      this.watchStream?.close();
    }
    if (this.taskId && (abort || this.cancelOnClose)) {
      void this.cancelTask(this.taskId);
    }
    this.markClosed();
  }

  private async cancelTask(taskId: string): Promise<void> {
    try {
      await bridgeRequest<TaskSnapshot>("tasks", "cancel", { taskId });
    } catch (error) {
      console.debug("Failed to cancel bridge task", error);
    }
  }

  private markClosed(): void {
    if (this.closed) return;
    this.closed = true;
    this._status = "closed";
    this.onClose?.();
  }
}

function openTaskOutputStream<R extends StreamRouteName>(
  route: R,
  request: LinuxIOStreamSchema[R],
  cancelOnClose = true,
): Stream | null {
  if (!isConnected()) {
    return null;
  }
  return new TaskDataStream(route, request, cancelOnClose);
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

/** Subscribe to changes that determine whether API requests may run. */
export function subscribeRequestAvailability(
  notifyStoreChanged: () => void,
): () => void {
  const unsubscribeStatus = subscribeToStatus(notifyStoreChanged);
  const unsubscribeUpdating = subscribeToUpdating(notifyStoreChanged);
  return () => {
    unsubscribeStatus();
    unsubscribeUpdating();
  };
}

/** Whether the shared request transport is open and not paused by an update. */
export function isRequestAvailable(): boolean {
  const mux = getStreamMux();
  return mux?.status === "open" && !mux.isUpdating;
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

export function openContainerStream(
  containerId: string,
  shell: string,
  cols: number,
  rows: number,
): Stream | null {
  return openChannel("container.open", { containerId, shell, cols, rows });
}

export function openAppUpdateStream(
  runId: string,
  version?: string,
): Stream | null {
  // The updater is session-bound. Closing this watch only detaches observation;
  // abort()/tasks.cancel explicitly cancels the running Task.
  return openTaskOutputStream("control.app_update", { runId, version }, false);
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
