/**
 * Stream-based API client
 *
 * Provides a Promise-based interface for making API calls over yamux streams
 * instead of HTTP. Each API call opens a new stream, waits for the result,
 * and closes.
 *
 * Usage:
 *   const data = await streamApi.get("system", "get_cpu_info");
 *   const result = await streamApi.post("docker", "start_container", { id: "abc" });
 */

import {
  getStreamMux,
  encodeString,
  type ResultFrame,
} from "./StreamMultiplexer";

// Stream type for API calls
const STREAM_TYPE_API = "api";

// Default timeout for API calls (30 seconds)
const DEFAULT_TIMEOUT_MS = 30000;

/**
 * Error class for stream API errors
 */
export class StreamApiError extends Error {
  constructor(
    message: string,
    public code: number = 500,
  ) {
    super(message);
    this.name = "StreamApiError";
  }
}

/**
 * Make an API call over a stream.
 *
 * @param handlerType - Handler group (e.g., "system", "docker", "filebrowser")
 * @param command - Handler command (e.g., "get_cpu_info", "list_containers")
 * @param args - Additional arguments to pass to the handler
 * @param timeoutMs - Timeout in milliseconds (default: 30000)
 * @returns Promise resolving to the response data
 */
export async function streamFetch<T = unknown>(
  handlerType: string,
  command: string,
  args: string[] = [],
  timeoutMs: number = DEFAULT_TIMEOUT_MS,
): Promise<T> {
  const mux = getStreamMux();
  if (!mux || mux.status !== "open") {
    throw new StreamApiError("Stream multiplexer not available", 503);
  }

  // Build payload: streamType\0type\0command\0arg1\0arg2...
  // Note: The first part (STREAM_TYPE_API) is the stream type
  // The rest are passed as args to HandleAPIStream
  const payloadParts = [STREAM_TYPE_API, handlerType, command, ...args];
  const payload = encodeString(payloadParts.join("\0"));

  // Open a new stream for this request
  const stream = mux.openStream(STREAM_TYPE_API, payload);
  if (!stream) {
    throw new StreamApiError("Failed to open API stream", 503);
  }

  return new Promise<T>((resolve, reject) => {
    let resolved = false;

    // Set up timeout
    const timeoutId = setTimeout(() => {
      if (!resolved) {
        resolved = true;
        stream.abort();
        reject(
          new StreamApiError(`Request timeout: ${handlerType}/${command}`, 408),
        );
      }
    }, timeoutMs);

    // Handle result
    stream.onResult = (result: ResultFrame) => {
      if (resolved) return;
      resolved = true;
      clearTimeout(timeoutId);

      if (result.status === "ok") {
        resolve(result.data as T);
      } else {
        reject(
          new StreamApiError(
            result.error || "Unknown error",
            result.code || 500,
          ),
        );
      }
    };

    // Handle unexpected close
    stream.onClose = () => {
      if (!resolved) {
        resolved = true;
        clearTimeout(timeoutId);
        reject(new StreamApiError("Stream closed unexpectedly", 500));
      }
    };
  });
}

/**
 * Convenience wrapper for API calls.
 * Provides familiar HTTP-like methods.
 */
export const streamApi = {
  /**
   * Make a GET-style API call (no body)
   */
  async get<T = unknown>(
    handlerType: string,
    command: string,
    args?: string[],
  ): Promise<T> {
    return streamFetch<T>(handlerType, command, args);
  },

  /**
   * Make a POST-style API call with optional body.
   * Body is JSON-stringified and passed as the last arg.
   */
  async post<T = unknown>(
    handlerType: string,
    command: string,
    body?: unknown,
    args: string[] = [],
  ): Promise<T> {
    const allArgs = body !== undefined ? [...args, JSON.stringify(body)] : args;
    return streamFetch<T>(handlerType, command, allArgs);
  },

  /**
   * Make a PUT-style API call with optional body
   */
  async put<T = unknown>(
    handlerType: string,
    command: string,
    body?: unknown,
    args: string[] = [],
  ): Promise<T> {
    const allArgs = body !== undefined ? [...args, JSON.stringify(body)] : args;
    return streamFetch<T>(handlerType, command, allArgs);
  },

  /**
   * Make a DELETE-style API call
   */
  async delete<T = unknown>(
    handlerType: string,
    command: string,
    args?: string[],
  ): Promise<T> {
    return streamFetch<T>(handlerType, command, args);
  },

  /**
   * Make a PATCH-style API call with optional body
   */
  async patch<T = unknown>(
    handlerType: string,
    command: string,
    body?: unknown,
    args: string[] = [],
  ): Promise<T> {
    const allArgs = body !== undefined ? [...args, JSON.stringify(body)] : args;
    return streamFetch<T>(handlerType, command, allArgs);
  },
};
