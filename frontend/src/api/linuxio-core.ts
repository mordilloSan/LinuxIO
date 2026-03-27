/**
 * LinuxIO Core API - Promise-based bridge communication
 *
 * - Typed calls: linuxio.*.call()
 *
 */

import {
  getStreamMux,
  initStreamMux,
  waitForStreamMux,
  encodeString,
} from "./StreamMultiplexer";
import { waitForStreamResult } from "./stream-helpers";

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
 * CallOptions for simple request/response calls
 */
export interface CallOptions {
  timeout?: number; // Timeout in milliseconds (default: 30000)
}

const DEFAULT_CALL_TIMEOUT_MS = 30000;
const MAX_CALL_ATTEMPTS = 2;

function isConnectionClosedError(error: unknown): boolean {
  return error instanceof LinuxIOError && error.code === "connection_closed";
}

async function ensureCallMuxReady(timeoutMs: number) {
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

async function executeCallAttempt<T>(
  handler: string,
  command: string,
  args: string[],
  timeoutMs: number,
): Promise<T> {
  // Build payload: "bridge\0handlerType\0command\0arg1\0arg2..."
  const parts = ["bridge", handler, command, ...args];
  const payload = encodeString(parts.join("\0"));

  const mux = await ensureCallMuxReady(timeoutMs);
  const stream = mux.openStream("bridge", payload);

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
 * Simple request/response call (internal — use linuxio.*.call() for typed access)
 */
export async function call<T = unknown>(
  handler: string,
  command: string,
  args: string[] = [],
  options?: CallOptions,
): Promise<T> {
  const timeoutMs = options?.timeout ?? DEFAULT_CALL_TIMEOUT_MS;
  const deadline = Date.now() + timeoutMs;
  let lastError: unknown = null;

  for (let attempt = 1; attempt <= MAX_CALL_ATTEMPTS; attempt += 1) {
    const remainingMs = deadline - Date.now();
    if (remainingMs <= 0) {
      throw new LinuxIOError("Request timeout", "timeout");
    }

    try {
      return await executeCallAttempt<T>(handler, command, args, remainingMs);
    } catch (error) {
      lastError = error;

      const canRetry =
        attempt < MAX_CALL_ATTEMPTS && isConnectionClosedError(error);
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
