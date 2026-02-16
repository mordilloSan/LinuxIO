import { useCallback, useEffect, useRef, type RefObject } from "react";

import {
  bindStreamHandlers,
  type StreamEventHandlers,
  type Stream,
} from "@/api";

export interface OpenLiveStreamOptions<
  TProgress = unknown,
> extends StreamEventHandlers<TProgress> {
  open: () => Stream | null;
  onOpen?: (stream: Stream) => void;
  onOpenError?: () => void;
}

export interface UseLiveStreamOptions {
  closeOnUnmount?: boolean;
}

export interface UseLiveStreamReturn {
  streamRef: RefObject<Stream | null>;
  // Returns true when a stream is active (existing or newly opened), false when opening failed.
  openStream: <TProgress = unknown>(
    options: OpenLiveStreamOptions<TProgress>,
  ) => boolean;
  closeStream: () => void;
}

/**
 * Manages lifecycle for long-lived stream consumers (logs/terminal-style).
 */
export function useLiveStream(
  options: UseLiveStreamOptions = {},
): UseLiveStreamReturn {
  const closeOnUnmount = options.closeOnUnmount ?? true;
  const streamRef = useRef<Stream | null>(null);
  const unbindRef = useRef<(() => void) | null>(null);

  const closeStream = useCallback(() => {
    if (unbindRef.current) {
      unbindRef.current();
      unbindRef.current = null;
    }

    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
  }, []);

  const openStream = useCallback(
    <TProgress = unknown>(
      options: OpenLiveStreamOptions<TProgress>,
    ): boolean => {
      if (streamRef.current) {
        return true;
      }

      const stream = options.open();
      if (!stream) {
        options.onOpenError?.();
        return false;
      }

      streamRef.current = stream;
      options.onOpen?.(stream);

      unbindRef.current = bindStreamHandlers<TProgress>(stream, {
        onData: options.onData,
        onProgress: options.onProgress,
        onResult: options.onResult,
        onClose: () => {
          options.onClose?.();
          unbindRef.current = null;
          streamRef.current = null;
        },
      });

      return true;
    },
    [],
  );

  useEffect(() => {
    if (!closeOnUnmount) {
      return;
    }

    return () => {
      closeStream();
    };
  }, [closeOnUnmount, closeStream]);

  return {
    streamRef,
    openStream,
    closeStream,
  };
}
