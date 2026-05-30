/**
 * LinuxIO Core API - internal JSON request bridge.
 *
 * App code should use generated endpoints, such as `linuxio.system.get_cpu_info()`.
 *
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
  constructor(
    message: string,
    public code?: string | number,
  ) {
    super(message);
    this.name = "LinuxIOError";
  }
}

/**
 * RequestOptions for simple request/response calls
 */
export interface RequestOptions {
  retryPolicy?: "connection_closed" | "none";
  timeout?: number; // Timeout in milliseconds (default: 30000)
}

const MAX_REQUEST_ATTEMPTS = 2;

function isConnectionClosedError(error: unknown): boolean {
  return error instanceof LinuxIOError && error.code === "connection_closed";
}

async function ensureRequestMuxReady(timeoutMs: number) {
  const existingMux = getStreamMux();
  if (!existingMux) {
    throw new LinuxIOError("StreamMux not initialized", "not_initialized");
  }

  if (existingMux.status === "closed") {
    initStreamMux();
  }

  const ready = await waitForStreamMux(timeoutMs);
  if (!ready) {
    throw new LinuxIOError(
      "Connection closed before receiving result",
      "connection_closed",
    );
  }

  const mux = getStreamMux();
  if (!mux || mux.status !== "open") {
    throw new LinuxIOError(
      "Connection closed before receiving result",
      "connection_closed",
    );
  }

  return mux;
}

async function executeRequestAttempt<T>(
  handler: string,
  command: string,
  request: unknown,
  timeoutMs: number,
): Promise<T> {
  const route = `${handler}.${command}`;
  const payload = encodeString(
    JSON.stringify({
      route,
      request: request ?? {},
    }),
  );

  const mux = await ensureRequestMuxReady(timeoutMs);
  const stream = mux.openStream(route, payload);

  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  try {
    return await waitForStreamResult<T>(stream, {
      closeMessage: "Connection closed before receiving result",
      signal: controller.signal,
    });
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      stream.close();
      throw new LinuxIOError("Request timeout", "timeout");
    }
    throw error;
  } finally {
    clearTimeout(timer);
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
      );
    } catch (error) {
      lastError = error;

      const canRetry =
        retryPolicy === "connection_closed" &&
        attempt < MAX_REQUEST_ATTEMPTS &&
        isConnectionClosedError(error);
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
