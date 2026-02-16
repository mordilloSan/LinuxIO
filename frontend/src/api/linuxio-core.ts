/**
 * LinuxIO Core API - Promise-based bridge communication
 *
 * - Simple calls: await call()
 * - Streaming: spawn().onStream().progress().then()
 * - Bidirectional: openStream()
 *
 */

import type { Stream, ProgressFrame, ResultFrame } from "./StreamMultiplexer";
import { getStreamMux, encodeString, decodeString } from "./StreamMultiplexer";
import { awaitStreamResult, bindStreamHandlers } from "./stream-helpers";

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

/**
 * SpawnOptions for streaming operations
 */
export interface SpawnOptions {
  timeout?: number;
  onData?: (chunk: Uint8Array) => void;
  onProgress?: (progress: ProgressFrame) => void;
}

/**
 * Simple request/response call
 * Returns a Promise that resolves with the result
 *
 * @example
 * const drives = await call<ApiDisk[]>("storage", "get_drive_info");
 */
export async function call<T = unknown>(
  handler: string,
  command: string,
  args: string[] = [],
  options?: CallOptions,
): Promise<T> {
  const mux = getStreamMux();
  if (!mux) {
    throw new LinuxIOError("StreamMux not initialized", "not_initialized");
  }

  const timeoutMs = options?.timeout ?? 30000;

  // Build payload: "bridge\0handlerType\0command\0arg1\0arg2..."
  const parts = ["bridge", handler, command, ...args];
  const payload = encodeString(parts.join("\0"));

  const stream = mux.openStream("bridge", payload);
  const operation = awaitStreamResult<T>(stream, {
    closeMessage: "Connection closed before receiving result",
  });

  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => {
      stream.close();
      reject(new LinuxIOError("Request timeout", "timeout"));
    }, timeoutMs);

    operation
      .then((result) => {
        clearTimeout(timer);
        resolve(result);
      })
      .catch((error) => {
        clearTimeout(timer);
        reject(error);
      });
  });
}

/**
 * Spawns a streaming operation
 * Returns a SpawnedProcess that is also a Promise
 *
 * @example
 * const result = await spawn("filebrowser", "download", ["/path/to/file"])
 *   .onStream(chunk => saveToFile(chunk))
 *   .progress(p => setProgress(p.pct));
 */
export function spawn(
  handler: string,
  command: string,
  args: string[] = [],
  options?: SpawnOptions,
): SpawnedProcess {
  const mux = getStreamMux();
  if (!mux) {
    throw new LinuxIOError("StreamMux not initialized", "not_initialized");
  }

  const parts = ["bridge", handler, command, ...args];
  const payload = encodeString(parts.join("\0"));

  const stream = mux.openStream("bridge", payload);

  return new SpawnedProcess(stream, options);
}

/**
 * Opens a bidirectional stream for manual control
 * Use for terminal, docker attach, or custom protocols
 *
 * @param handler - Handler name (e.g., "terminal", "docker")
 * @param command - Command name (e.g., "bash", "container_exec")
 * @param args - Command arguments
 * @param streamType - Stream type for persistence/reuse (default: "bridge")
 *
 * @example
 * // Terminal stream (reusable via "terminal" type)
 * const stream = openStream("terminal", "bash", ["120", "32"], "terminal");
 * stream.onData = (data) => xterm.write(decodeString(data));
 * stream.write(encodeString("ls -la\n"));
 *
 * @example
 * // One-off stream (bridge type)
 * const stream = openStream("docker", "container_exec", ["abc123", "sh", "80", "24"]);
 */
export function openStream(
  handler: string,
  command: string,
  args: string[] = [],
  streamType: string = "bridge",
): Stream {
  const mux = getStreamMux();
  if (!mux) {
    throw new LinuxIOError("StreamMux not initialized", "not_initialized");
  }

  const parts = ["bridge", handler, command, ...args];
  const payload = encodeString(parts.join("\0"));

  return mux.openStream(streamType, payload);
}

/**
 * SpawnedProcess - Promise with additional streaming methods
 */
export class SpawnedProcess implements Promise<any> {
  private promise: Promise<any>;
  private resolvePromise!: (value: any) => void;
  private rejectPromise!: (error: any) => void;
  private _stream: Stream;
  private unbindHandlers: (() => void) | null = null;

  // For Promise implementation
  readonly [Symbol.toStringTag] = "SpawnedProcess";

  constructor(stream: Stream, options?: SpawnOptions) {
    this._stream = stream;

    // Create underlying promise
    this.promise = new Promise((resolve, reject) => {
      this.resolvePromise = resolve;
      this.rejectPromise = reject;
    });

    let settled = false;
    const timeoutMs = options?.timeout ?? 300000; // Default 5 minutes for long operations

    const timeoutTimer = setTimeout(() => {
      if (settled) {
        return;
      }
      settled = true;
      this.unbindHandlers?.();
      this.unbindHandlers = null;
      this._stream.abort();
      this.rejectPromise(new LinuxIOError("Operation timeout", "timeout"));
    }, timeoutMs);

    this.unbindHandlers = bindStreamHandlers(this._stream, {
      onData: options?.onData,
      onProgress: options?.onProgress,
      onResult: (result) => {
        if (settled) {
          return;
        }
        settled = true;
        clearTimeout(timeoutTimer);
        this.unbindHandlers?.();
        this.unbindHandlers = null;

        if (result.status === "ok") {
          this.resolvePromise(result.data);
        } else {
          this.rejectPromise(
            new LinuxIOError(result.error || "Unknown error", result.code),
          );
        }
      },
      onClose: () => {
        if (settled) {
          return;
        }
        settled = true;
        clearTimeout(timeoutTimer);
        this.unbindHandlers?.();
        this.unbindHandlers = null;
        this.rejectPromise(
          new LinuxIOError(
            "Connection closed before operation completed",
            "connection_closed",
          ),
        );
      },
    });
  }

  /**
   * Register callback for incremental data chunks
   * Returns this for chaining
   */
  onStream(callback: (chunk: Uint8Array) => void): this {
    this._stream.onData = callback;
    return this;
  }

  /**
   * Register callback for progress updates
   * Returns this for chaining
   */
  progress(callback: (progress: ProgressFrame) => void): this {
    this._stream.onProgress = callback;
    return this;
  }

  /**
   * Send data to the process (for bidirectional streams)
   */
  input(data: Uint8Array | string): void {
    if (typeof data === "string") {
      data = encodeString(data);
    }
    this._stream.write(data);
  }

  /**
   * Close/abort the process early
   */
  close(): void {
    this._stream.abort();
  }

  /**
   * Abort the process immediately.
   */
  abort(): void {
    this._stream.abort();
  }

  // Promise implementation
  then<TResult1 = any, TResult2 = never>(
    onfulfilled?:
      | ((value: any) => TResult1 | PromiseLike<TResult1>)
      | null
      | undefined,
    onrejected?:
      | ((reason: any) => TResult2 | PromiseLike<TResult2>)
      | null
      | undefined,
  ): Promise<TResult1 | TResult2> {
    return this.promise.then(onfulfilled, onrejected);
  }

  catch<TResult = never>(
    onrejected?:
      | ((reason: any) => TResult | PromiseLike<TResult>)
      | null
      | undefined,
  ): Promise<any | TResult> {
    return this.promise.catch(onrejected);
  }

  finally(onfinally?: (() => void) | null | undefined): Promise<any> {
    return this.promise.finally(onfinally);
  }
}

// Re-export types
export type { Stream, ProgressFrame, ResultFrame };
export { encodeString, decodeString };
