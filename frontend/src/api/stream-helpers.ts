import { LinuxIOError } from "./linuxio-core";
import type { ProgressFrame, ResultFrame, Stream } from "./StreamMultiplexer";

export interface StreamEventHandlers<TProgress = ProgressFrame> {
  onData?: (data: Uint8Array) => void;
  onProgress?: (progress: TProgress) => void;
  onResult?: (result: ResultFrame) => void;
  onClose?: () => void;
}

/**
 * Attach stream handlers and return a cleanup function that detaches all handlers.
 */
export function bindStreamHandlers<TProgress = ProgressFrame>(
  stream: Stream,
  handlers: StreamEventHandlers<TProgress>,
): () => void {
  stream.onData = handlers.onData ?? null;
  stream.onProgress = handlers.onProgress
    ? (progress: ProgressFrame) => {
        handlers.onProgress?.(progress as TProgress);
      }
    : null;
  stream.onResult = handlers.onResult ?? null;
  stream.onClose = handlers.onClose ?? null;

  return () => {
    stream.onData = null;
    stream.onProgress = null;
    stream.onResult = null;
    stream.onClose = null;
  };
}

export interface AwaitStreamResultOptions<
  TResult = unknown,
  TProgress = ProgressFrame,
> extends Omit<StreamEventHandlers<TProgress>, "onResult"> {
  /**
   * Optional abort signal. By default, aborting triggers stream.abort().
   */
  signal?: AbortSignal;
  /**
   * Action to perform on abort signal.
   * - "abort": send RST (default)
   * - "close": send FIN
   * - "none": do not send close/abort frame
   */
  closeOnAbort?: "abort" | "close" | "none";
  /**
   * Custom message for close-before-result failures.
   */
  closeMessage?: string;
  /**
   * Transform result payload before resolving.
   */
  mapResult?: (data: unknown, frame: ResultFrame) => TResult;
}

/**
 * Await a stream operation that must complete with an onResult frame.
 */
export function awaitStreamResult<TResult = unknown, TProgress = ProgressFrame>(
  stream: Stream | null,
  options: AwaitStreamResultOptions<TResult, TProgress> = {},
): Promise<TResult> {
  if (!stream) {
    return Promise.reject(
      new LinuxIOError("Stream connection not ready", "stream_unavailable"),
    );
  }
  const activeStream: Stream = stream;

  return new Promise<TResult>((resolve, reject) => {
    let settled = false;
    const signal = options.signal;

    const cleanupAbortListener = () => {
      if (signal) {
        signal.removeEventListener("abort", handleAbort);
      }
    };

    const cleanup = bindStreamHandlers(activeStream, {
      onData: options.onData,
      onProgress: options.onProgress,
      onClose: () => {
        options.onClose?.();
        if (settled) return;
        settled = true;
        cleanupAbortListener();
        cleanup();
        reject(
          new LinuxIOError(
            options.closeMessage ?? "Stream closed before operation completed",
            "connection_closed",
          ),
        );
      },
      onResult: (result) => {
        if (settled) return;
        settled = true;
        cleanupAbortListener();
        cleanup();
        if (result.status === "ok") {
          const mapped = options.mapResult
            ? options.mapResult(result.data, result)
            : (result.data as TResult);
          resolve(mapped);
          return;
        }
        reject(
          new LinuxIOError(
            result.error || "Stream operation failed",
            result.code,
          ),
        );
      },
    });

    const rejectAbort = () => {
      if (settled) return;
      settled = true;
      cleanupAbortListener();
      cleanup();
      const error = new Error("Operation cancelled");
      error.name = "AbortError";
      reject(error);
    };

    function handleAbort() {
      if (options.closeOnAbort === "close") {
        activeStream.close();
      } else if (options.closeOnAbort !== "none") {
        activeStream.abort();
      }
      rejectAbort();
    }

    if (signal?.aborted) {
      handleAbort();
      return;
    }

    if (signal) {
      signal.addEventListener("abort", handleAbort, { once: true });
    }
  });
}

export interface WriteStreamChunksOptions {
  chunkSize?: number;
  yieldMs?: number;
  closeAtEnd?: boolean;
  signal?: AbortSignal;
}

/**
 * Write bytes to a stream in chunks, optionally yielding between writes.
 */
export async function writeStreamChunks(
  stream: Stream,
  data: Uint8Array,
  options: WriteStreamChunksOptions = {},
): Promise<void> {
  const chunkSize = options.chunkSize ?? 64 * 1024;
  const yieldMs = options.yieldMs ?? 0;
  const closeAtEnd = options.closeAtEnd ?? true;

  let offset = 0;

  while (offset < data.length) {
    if (options.signal?.aborted) {
      const error = new Error("Operation cancelled");
      error.name = "AbortError";
      throw error;
    }

    if (stream.status !== "open" && stream.status !== "opening") {
      throw new LinuxIOError("Stream is not open", "connection_closed");
    }

    const chunk = data.slice(offset, offset + chunkSize);
    stream.write(chunk);
    offset += chunk.length;

    if (offset < data.length) {
      await new Promise<void>((resolve) => {
        window.setTimeout(resolve, yieldMs);
      });
    }
  }

  if (closeAtEnd) {
    stream.close();
  }
}
