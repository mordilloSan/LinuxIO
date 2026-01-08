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
 * const drives = await call<ApiDisk[]>("system", "get_drive_info");
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

  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => {
      stream.close();
      reject(new LinuxIOError("Request timeout", "timeout"));
    }, timeoutMs);

    stream.onResult = (result: ResultFrame) => {
      clearTimeout(timer);
      if (result.status === "ok") {
        resolve(result.data as T);
      } else {
        reject(new LinuxIOError(result.error || "Unknown error", result.code));
      }
    };

    // Handle errors (though onResult should catch most)
    stream.onClose = () => {
      clearTimeout(timer);
    };
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
 * @example
 * const stream = openStream("terminal", "bash", ["120", "32"]);
 * stream.onData = (data) => xterm.write(decodeString(data));
 * stream.write(encodeString("ls -la\n"));
 */
export function openStream(
  handler: string,
  command: string,
  args: string[] = [],
): Stream {
  const mux = getStreamMux();
  if (!mux) {
    throw new LinuxIOError("StreamMux not initialized", "not_initialized");
  }

  const parts = ["bridge", handler, command, ...args];
  const payload = encodeString(parts.join("\0"));

  return mux.openStream("bridge", payload);
}

/**
 * SpawnedProcess - Promise with additional streaming methods
 */
export class SpawnedProcess implements Promise<any> {
  private promise: Promise<any>;
  private resolvePromise!: (value: any) => void;
  private rejectPromise!: (error: any) => void;
  private _stream: Stream;

  // For Promise implementation
  readonly [Symbol.toStringTag] = "SpawnedProcess";

  constructor(stream: Stream, options?: SpawnOptions) {
    this._stream = stream;

    // Create underlying promise
    this.promise = new Promise((resolve, reject) => {
      this.resolvePromise = resolve;
      this.rejectPromise = reject;
    });

    // Wire up stream events
    this._stream.onResult = (result: ResultFrame) => {
      if (result.status === "ok") {
        this.resolvePromise(result.data);
      } else {
        this.rejectPromise(
          new LinuxIOError(result.error || "Unknown error", result.code),
        );
      }
    };

    // Apply initial options
    if (options?.onData) {
      this._stream.onData = options.onData;
    }
    if (options?.onProgress) {
      this._stream.onProgress = options.onProgress;
    }
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
    this._stream.close();
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
