import { useCallback } from "react";

import {
  LinuxIOError,
  type ProgressFrame,
  type Stream,
  type WaitForStreamResultOptions,
} from "@/api";
import { streamWriteChunks, waitForStreamResult } from "@/api";

export interface RunStreamResultOptions<
  TResult = unknown,
  TProgress = ProgressFrame,
> extends Omit<WaitForStreamResultOptions<TResult, TProgress>, "signal"> {
  onError?: (error: unknown) => void;
  onFinally?: () => void;
  onOpen?: (stream: Stream) => void;
  onSuccess?: (result: TResult) => void;
  open: () => Stream | null;
  openErrorCode?: string;
  openErrorMessage?: string;
  signal?: AbortSignal;
  throwOnError?: boolean;
}

type RunStreamResultFn = {
  <TResult = unknown, TProgress = ProgressFrame>(
    options: RunStreamResultOptions<TResult, TProgress> & {
      throwOnError: false;
    },
  ): Promise<TResult | undefined>;
  <TResult = unknown, TProgress = ProgressFrame>(
    options: RunStreamResultOptions<TResult, TProgress>,
  ): Promise<TResult>;
};

export interface RunChunkedStreamResultOptions<
  TResult = unknown,
  TProgress = ProgressFrame,
> extends RunStreamResultOptions<TResult, TProgress> {
  chunkSize?: number;
  closeAtEnd?: boolean;
  data: Uint8Array;
  yieldMs?: number;
}

type RunChunkedStreamResultFn = {
  <TResult = unknown, TProgress = ProgressFrame>(
    options: RunChunkedStreamResultOptions<TResult, TProgress> & {
      throwOnError: false;
    },
  ): Promise<TResult | undefined>;
  <TResult = unknown, TProgress = ProgressFrame>(
    options: RunChunkedStreamResultOptions<TResult, TProgress>,
  ): Promise<TResult>;
};

export interface UseStreamResultReturn {
  run: RunStreamResultFn;
  runChunked: RunChunkedStreamResultFn;
}

/**
 * Runs a result-oriented stream operation with a single lifecycle:
 * open -> bind/await -> success|error -> finally cleanup.
 */
export function useStreamResult(): UseStreamResultReturn {
  const run = useCallback(
    async <TResult = unknown, TProgress = ProgressFrame>(
      options: RunStreamResultOptions<TResult, TProgress>,
    ): Promise<TResult | undefined> => {
      const {
        open,
        signal,
        onOpen,
        onSuccess,
        onError,
        onFinally,
        throwOnError,
        openErrorMessage = "Failed to open stream",
        openErrorCode = "stream_unavailable",
        ...awaitOptions
      } = options;
      const shouldThrow = throwOnError ?? !onError;

      const stream = open();
      if (!stream) {
        const error = new LinuxIOError(openErrorMessage, openErrorCode);
        onError?.(error);
        onFinally?.();
        if (shouldThrow) {
          throw error;
        }
        return undefined;
      }

      onOpen?.(stream);

      try {
        const result = await waitForStreamResult<TResult, TProgress>(stream, {
          ...awaitOptions,
          signal,
        });
        onSuccess?.(result);
        return result;
      } catch (error) {
        onError?.(error);
        if (shouldThrow) {
          throw error;
        }
        return undefined;
      } finally {
        onFinally?.();
      }
    },
    [],
  ) as RunStreamResultFn;

  const runChunked = useCallback(
    async <TResult = unknown, TProgress = ProgressFrame>(
      options: RunChunkedStreamResultOptions<TResult, TProgress>,
    ): Promise<TResult | undefined> => {
      const {
        data,
        chunkSize,
        yieldMs,
        closeAtEnd,
        open,
        signal,
        onOpen,
        onSuccess,
        onError,
        onFinally,
        throwOnError,
        openErrorMessage = "Failed to open stream",
        openErrorCode = "stream_unavailable",
        ...awaitOptions
      } = options;
      const shouldThrow = throwOnError ?? !onError;

      const stream = open();
      if (!stream) {
        const error = new LinuxIOError(openErrorMessage, openErrorCode);
        onError?.(error);
        onFinally?.();
        if (shouldThrow) {
          throw error;
        }
        return undefined;
      }

      onOpen?.(stream);

      try {
        const completion = waitForStreamResult<TResult, TProgress>(stream, {
          ...awaitOptions,
          signal,
        });

        try {
          await streamWriteChunks(stream, data, {
            chunkSize,
            yieldMs,
            closeAtEnd,
            signal,
          });
        } catch (writeError) {
          if (stream.status === "open" || stream.status === "opening") {
            stream.abort();
          }
          await completion.catch(() => undefined);
          throw writeError;
        }

        const result = await completion;
        onSuccess?.(result);
        return result;
      } catch (error) {
        onError?.(error);
        if (shouldThrow) {
          throw error;
        }
        return undefined;
      } finally {
        onFinally?.();
      }
    },
    [],
  ) as RunChunkedStreamResultFn;

  return { run, runChunked };
}
