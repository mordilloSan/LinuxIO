/**
 * LinuxIO Common Utilities
 *
 * Shared utilities for stream multiplexer access and payload helpers.
 * App code should generally import from `@/api`; this module exists to
 * implement that public surface.
 */

import { useCallback, useSyncExternalStore } from "react";

import type { JobSnapshot } from "./generated/linuxio-types";

import { isTerminalJobState } from "./job-state";
import { call as bridgeCall } from "./linuxio-core";
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

let nextJobBackedStreamID = -1;

type JobDataProgress = {
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
  const candidate = progress as JobDataProgress;
  if (candidate.type !== "data") {
    return null;
  }
  if (typeof candidate.data === "string") {
    return candidate.data;
  }
  if (candidate.data == null) {
    return "";
  }
  return String(candidate.data);
}

class JobBackedDataStream implements Stream {
  readonly id = nextJobBackedStreamID--;
  readonly type: StreamType;
  onData: ((data: Uint8Array) => void) | null = null;
  onClose: (() => void) | null = null;
  onProgress: ((progress: ProgressFrame) => void) | null = null;
  onResult: ((result: ResultFrame) => void) | null = null;

  private _status: StreamStatus = "opening";
  private attachStream: Stream | null = null;
  private jobId: string | null = null;
  private closed = false;

  constructor(
    route: string,
    private readonly args: string[],
  ) {
    this.type = route;
    void this.start(route);
  }

  get status(): StreamStatus {
    return this._status;
  }

  write(): void {
    // Job-backed data streams are receive-only.
  }

  resize(): void {
    // Not applicable to receive-only job data streams.
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
      const snapshot = await bridgeCall<JobSnapshot>(
        handler,
        command,
        this.args,
      );
      if (this.closed) {
        void this.cancelJob(snapshot.id);
        return;
      }

      this.jobId = snapshot.id;
      const attach = openJobAttachStream(snapshot.id);
      if (!attach) {
        if (isTerminalJobState(snapshot.state)) {
          this.forwardProgress(snapshot.progress);
          this.forwardTerminalSnapshot(snapshot);
          return;
        }
        this.forwardError("Failed to attach job stream", "stream_unavailable");
        return;
      }

      this.attachStream = attach;
      this._status = "open";
      attach.onData = (data) => this.onData?.(data);
      attach.onProgress = (progress) => this.forwardProgress(progress);
      attach.onResult = (result) => {
        this.onResult?.(result);
        this.markClosed();
      };
      attach.onClose = () => this.markClosed();
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to start job stream";
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

  private forwardTerminalSnapshot(snapshot: JobSnapshot): void {
    if (snapshot.state === "completed") {
      this.onResult?.({ status: "ok", data: snapshot.result });
    } else {
      this.onResult?.({
        status: "error",
        error: snapshot.error?.message ?? "Job failed",
        code: snapshot.error?.code,
      });
    }
    this.markClosed();
  }

  private forwardError(message: string, code?: string | number): void {
    this.onResult?.({
      status: "error",
      error: message,
      code: typeof code === "number" ? code : undefined,
    });
    this.markClosed();
  }

  private stop(abort: boolean): void {
    if (this.closed) return;
    if (abort) {
      this.attachStream?.abort();
    } else {
      this.attachStream?.close();
    }
    if (this.jobId) {
      void this.cancelJob(this.jobId);
    }
    this.markClosed();
  }

  private async cancelJob(jobId: string): Promise<void> {
    try {
      await bridgeCall<JobSnapshot>("jobs", "cancel", [jobId]);
    } catch (error) {
      console.debug("Failed to cancel bridge job", error);
    }
  }

  private markClosed(): void {
    if (this.closed) return;
    this.closed = true;
    this._status = "closed";
    this.onClose?.();
  }
}

function openJobBackedDataStream(route: string, args: string[]): Stream | null {
  if (!isConnected()) {
    return null;
  }
  return new JobBackedDataStream(route, args);
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
  const route = "terminal.open";
  return openMuxStream(route, encodeString([route, cols, rows].join("\0")));
}

export function openContainerStream(
  containerId: string,
  shell: string,
  cols: number,
  rows: number,
): Stream | null {
  const route = "container.open";
  return openMuxStream(
    route,
    encodeString([route, containerId, shell, cols, rows].join("\0")),
  );
}

export function openDockerLogsStream(
  containerId: string,
  tail: string = "100",
): Stream | null {
  const route = "docker.logs.follow";
  return openJobBackedDataStream(route, [containerId, tail]);
}

export function openServiceLogsStream(
  serviceName: string,
  lines: string = "100",
): Stream | null {
  const route = "logs.service.follow";
  return openJobBackedDataStream(route, [serviceName, lines]);
}

export function openGeneralLogsStream(
  lines: string = "100",
  timePeriod: string = "",
  priority: string = "",
  identifier: string = "",
  fieldFilters: string[] = [],
): Stream | null {
  const route = "logs.general.follow";
  return openJobBackedDataStream(route, [
    lines,
    timePeriod,
    priority,
    identifier,
    ...fieldFilters,
  ]);
}

export function openAppUpdateStream(
  runId: string,
  version?: string,
): Stream | null {
  const route = "control.app_update";
  const parts = [runId];
  if (version) parts.push(version);
  return openJobBackedDataStream(route, parts);
}

export function openJobAttachStream(jobId: string): Stream | null {
  const route = "jobs.attach";
  return openMuxStream(route, encodeString([route, jobId].join("\0")));
}

export function openJobDataStream(
  jobId: string,
  offset: number = 0,
): Stream | null {
  const route = "jobs.data";
  return openMuxStream(
    route,
    encodeString([route, jobId, String(offset)].join("\0")),
  );
}

export function openJobEventsStream(): Stream | null {
  return openMuxStream("jobs.events", encodeString("jobs.events"));
}
