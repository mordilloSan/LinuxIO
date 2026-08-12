/**
 * LinuxIO Core API - internal JSON request bridge.
 *
 * App code should use generated Call descriptors with TanStack Query,
 * or the endpoint action/fetch/cache hooks; see docs/api-contract.md.
 */

import { waitForStreamResult } from "./stream-helpers";
import {
  encodeString,
  getStreamMux,
  initStreamMux,
  STREAM_MULTIPLEXER_CONFIG,
  waitForStreamMux,
} from "./StreamMultiplexer";

/**
 * LinuxIOError - structured error with code
 */
export class LinuxIOError extends Error {
  code?: string | number;

  constructor(message: string, code?: string | number) {
    super(message);
    this.code = code;
    this.name = "LinuxIOError";
  }
}

/**
 * RequestOptions for simple request/response calls
 */
export type ConnectionLossCode = "connection_unavailable" | "outcome_unknown";
export type RequestRetryPolicy = "connection_loss" | "none";

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

export function isConnectionLossError(error: unknown): boolean {
  return (
    error instanceof LinuxIOError &&
    (error.code === "connection_unavailable" ||
      error.code === "outcome_unknown")
  );
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
  const payload = encodeString(
    JSON.stringify({
      route,
      request: request ?? {},
    }),
  );

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
