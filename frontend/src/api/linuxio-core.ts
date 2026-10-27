/**
 * LinuxIO Core API - internal JSON request bridge.
 *
 * App code should use generated Call descriptors with TanStack Query,
 * or the endpoint action/fetch/cache hooks; see docs/api-contract.md.
 */

import { isConnectionLossError, LinuxIOError } from "./errors";
import type { TaskSnapshot } from "./generated/linuxio-types";
import { waitForStreamResult } from "./stream-helpers";
import {
  encodeString,
  getStreamMux,
  initStreamMux,
  type ProgressFrame,
  STREAM_MULTIPLEXER_CONFIG,
  waitForStreamMux,
} from "./StreamMultiplexer";

/**
 * RequestOptions for simple request/response calls
 */
type RequestRetryPolicy = "connection_loss" | "none";

export interface RequestOptions {
  retryPolicy?: RequestRetryPolicy;
  signal?: AbortSignal;
  timeout?: number; // Timeout in milliseconds (default: 30000)
}

const MAX_REQUEST_ATTEMPTS = 2;

function connectionUnavailableError(): LinuxIOError {
  return new LinuxIOError(
    "Connection unavailable before request was sent",
    "connection_unavailable",
  );
}

function outcomeUnknownError(): LinuxIOError {
  return new LinuxIOError(
    "Connection closed before the server confirmed the outcome",
    "outcome_unknown",
  );
}

function isConnectionClosedError(error: unknown): boolean {
  return error instanceof LinuxIOError && error.code === "connection_closed";
}

/** The stream-open envelope; `id` is the client-chosen job id of a mutation. */
export function streamOpenPayload(
  route: string,
  request: unknown = {},
  id?: string,
): Uint8Array {
  return encodeString(JSON.stringify({ route, request: request ?? {}, id }));
}

/**
 * Ensure the singleton transport can accept a request.
 *
 * AuthContext remains responsible for the authenticated mux lifecycle. This
 * small imperative seam only makes a request safe when a router loader runs
 * before a component has mounted: initialization is idempotent and a closed
 * singleton is reconnected by initStreamMux().
 */
export async function ensureLoaderRequestReady(
  timeoutMs = STREAM_MULTIPLEXER_CONFIG.defaultRequestTimeoutMs,
  signal?: AbortSignal,
) {
  throwIfAborted(signal);
  const existingMux = getStreamMux();
  if (!existingMux || existingMux.status === "closed") {
    try {
      initStreamMux();
    } catch {
      throw connectionUnavailableError();
    }
  }

  let ready: boolean;
  try {
    ready = signal
      ? await waitForStreamMux(timeoutMs, signal)
      : await waitForStreamMux(timeoutMs);
  } catch {
    if (signal?.aborted) throw abortSignalError(signal);
    throw connectionUnavailableError();
  }
  throwIfAborted(signal);
  if (!ready) {
    throw connectionUnavailableError();
  }

  const mux = getStreamMux();
  if (!mux || mux.status !== "open") {
    throw connectionUnavailableError();
  }

  return mux;
}

function abortSignalError(signal?: AbortSignal): Error {
  if (signal?.reason instanceof Error) {
    return signal.reason;
  }
  const error = new Error("Operation cancelled");
  error.name = "AbortError";
  return error;
}

function throwIfAborted(signal?: AbortSignal): void {
  if (signal?.aborted) {
    throw abortSignalError(signal);
  }
}

async function ensureRequestMuxReady(timeoutMs: number, signal?: AbortSignal) {
  throwIfAborted(signal);
  const existingMux = getStreamMux();
  if (!existingMux) {
    throw connectionUnavailableError();
  }

  if (existingMux.status === "closed") {
    try {
      initStreamMux();
    } catch {
      throw connectionUnavailableError();
    }
  }

  let ready: boolean;
  try {
    ready = signal
      ? await waitForStreamMux(timeoutMs, signal)
      : await waitForStreamMux(timeoutMs);
  } catch {
    if (signal?.aborted) throw abortSignalError(signal);
    throw connectionUnavailableError();
  }
  throwIfAborted(signal);
  if (!ready) {
    throw connectionUnavailableError();
  }

  const mux = getStreamMux();
  if (!mux || mux.status !== "open") {
    throw connectionUnavailableError();
  }

  return mux;
}

async function executeRequestAttempt<T>(
  handler: string,
  command: string,
  request: unknown,
  timeoutMs: number,
  callerSignal?: AbortSignal,
): Promise<T> {
  throwIfAborted(callerSignal);
  const startedAt = Date.now();
  const route = `${handler}.${command}`;
  const payload = streamOpenPayload(route, request);

  const mux = await ensureRequestMuxReady(timeoutMs, callerSignal);
  throwIfAborted(callerSignal);

  const remainingMs = timeoutMs - (Date.now() - startedAt);
  if (remainingMs <= 0) {
    throw new LinuxIOError("Request timeout", "timeout");
  }
  const stream = mux.openStream(route, payload);
  if (!stream) {
    throw connectionUnavailableError();
  }

  const controller = new AbortController();
  let abortSource: "caller" | "timeout" | null = null;
  const abortAttempt = (
    source: "caller" | "timeout",
    reason?: unknown,
  ): void => {
    if (abortSource !== null) return;
    abortSource = source;
    controller.abort(reason);
  };
  const handleCallerAbort = () => abortAttempt("caller", callerSignal?.reason);
  const timer = setTimeout(() => abortAttempt("timeout"), remainingMs);
  if (callerSignal?.aborted) {
    handleCallerAbort();
  } else {
    callerSignal?.addEventListener("abort", handleCallerAbort, { once: true });
  }

  try {
    return await waitForStreamResult<T>(stream, {
      closeMessage: "Connection closed before receiving result",
      signal: controller.signal,
    });
  } catch (error) {
    if (
      error instanceof Error &&
      error.name === "AbortError" &&
      abortSource === "timeout"
    ) {
      throw new LinuxIOError("Request timeout", "timeout");
    }
    if (isConnectionClosedError(error)) {
      throw outcomeUnknownError();
    }
    throw error;
  } finally {
    clearTimeout(timer);
    callerSignal?.removeEventListener("abort", handleCallerAbort);
  }
}

/**
 * Simple request/response call (internal — use linuxio.*() for typed access)
 */
export async function request<T = unknown>(
  handler: string,
  command: string,
  payload: unknown = {},
  options?: RequestOptions,
): Promise<T> {
  const timeoutMs =
    options?.timeout ?? STREAM_MULTIPLEXER_CONFIG.defaultRequestTimeoutMs;
  const retryPolicy = options?.retryPolicy ?? "none";
  const deadline = Date.now() + timeoutMs;
  let lastError: unknown = null;

  for (let attempt = 1; attempt <= MAX_REQUEST_ATTEMPTS; attempt += 1) {
    throwIfAborted(options?.signal);
    const remainingMs = deadline - Date.now();
    if (remainingMs <= 0) {
      throw new LinuxIOError("Request timeout", "timeout");
    }

    try {
      return await executeRequestAttempt<T>(
        handler,
        command,
        payload,
        remainingMs,
        options?.signal,
      );
    } catch (error) {
      lastError = error;

      const canRetry =
        retryPolicy === "connection_loss" &&
        attempt < MAX_REQUEST_ATTEMPTS &&
        isConnectionLossError(error);
      if (!canRetry) {
        throw error;
      }
    }
  }

  if (lastError) {
    throw lastError;
  }
  throw new LinuxIOError("Request timeout", "timeout");
}

export interface MutationHandlers<TProgress> {
  onSnapshot?(snapshot: TaskSnapshot): void;
  onProgress?(progress: TProgress): void;
  signal?: AbortSignal;
}

function isTaskSnapshotFrame(
  frame: unknown,
): frame is { type: "task.snapshot"; task: TaskSnapshot } {
  return (
    typeof frame === "object" &&
    frame !== null &&
    (frame as { type?: unknown }).type === "task.snapshot"
  );
}

function isMutationStreamLoss(error: unknown): boolean {
  return (
    error instanceof LinuxIOError &&
    (error.code === "connection_closed" ||
      error.code === "connection_unavailable")
  );
}

/**
 * Run a mutation on one stream with no client deadline. The envelope carries a
 * client-chosen job id, so a connection lost before the job's snapshot resends
 * the same envelope once, and one lost after it reattaches through
 * tasks.watch once. A further loss leaves the outcome unknown. Aborting
 * `signal` aborts the stream, which cancels the job.
 */
export function mutationRequest<TResult, TProgress = ProgressFrame>(
  handler: string,
  command: string,
  request: unknown,
  handlers: MutationHandlers<TProgress> = {},
): Promise<TResult> {
  const route = `${handler}.${command}`;
  const id = crypto.randomUUID();
  return followMutation<TResult, TProgress>(
    id,
    { route, payload: streamOpenPayload(route, request, id) },
    handlers,
  );
}

/** Adopt a running mutation job: report its snapshot, then follow it. */
export async function watchMutation<TResult, TProgress = ProgressFrame>(
  taskId: string,
  handlers: MutationHandlers<TProgress> = {},
): Promise<TResult> {
  const snapshot = await request<TaskSnapshot>(
    "tasks",
    "get",
    { taskId },
    { retryPolicy: "connection_loss", signal: handlers.signal },
  );
  handlers.onSnapshot?.(snapshot);
  return followMutation<TResult, TProgress>(taskId, null, handlers);
}

async function followMutation<TResult, TProgress>(
  id: string,
  start: { route: string; payload: Uint8Array } | null,
  handlers: MutationHandlers<TProgress>,
): Promise<TResult> {
  let started = start === null;
  let sent = false;
  let resent = false;
  let reattached = false;
  for (;;) {
    try {
      // Waiting for the transport is bounded; the job itself is not.
      const mux = await ensureRequestMuxReady(
        STREAM_MULTIPLEXER_CONFIG.defaultRequestTimeoutMs,
        handlers.signal,
      );
      const stream =
        start && !started
          ? mux.openStream(start.route, start.payload)
          : mux.openStream(
              "tasks.watch",
              streamOpenPayload("tasks.watch", { taskId: id }),
            );
      if (!stream) throw connectionUnavailableError();
      sent = true;
      return await waitForStreamResult<TResult, unknown>(stream, {
        closeMessage: "Connection closed before the mutation finished",
        onProgress: (frame) => {
          if (isTaskSnapshotFrame(frame)) {
            started = true;
            handlers.onSnapshot?.(frame.task);
          } else {
            handlers.onProgress?.(frame as TProgress);
          }
        },
        signal: handlers.signal,
      });
    } catch (error) {
      if (!isMutationStreamLoss(error)) throw error;
      if (started ? reattached : resent) {
        throw sent ? outcomeUnknownError() : error;
      }
      if (started) reattached = true;
      else resent = true;
    }
  }
}
