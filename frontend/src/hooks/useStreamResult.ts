import { useCallback } from "react";

import {
  awaitStreamResult,
  LinuxIOError,
  type AwaitStreamResultOptions,
  type ProgressFrame,
  type Stream,
} from "@/api";

export interface RunStreamResultOptions<
  TResult = unknown,
  TProgress = ProgressFrame,
> extends Omit<AwaitStreamResultOptions<TResult, TProgress>, "signal"> {
  open: () => Stream | null;
  signal?: AbortSignal;
  onOpen?: (stream: Stream) => void;
  onSuccess?: (result: TResult) => void;
  onError?: (error: unknown) => void;
  onFinally?: () => void;
  throwOnError?: boolean;
  openErrorMessage?: string;
  openErrorCode?: string;
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

export interface UseStreamResultReturn {
  run: RunStreamResultFn;
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
        const result = await awaitStreamResult<TResult, TProgress>(stream, {
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

  return { run };
}
