import { type RefObject, useCallback, useEffect, useRef } from "react";

import {
  bindStreamHandlers,
  decodeString,
  type Stream,
  type StreamEventHandlers,
} from "@/api";

export interface OpenLiveStreamOptions<
  TProgress = unknown,
> extends StreamEventHandlers<TProgress> {
  onOpen?: (stream: Stream) => void;
  onOpenError?: () => void;
  /**
   * Like `onData`, but with the frame decoded to text. Page-level consumers
   * take this so byte decoding stays inside the lifecycle hook.
   */
  onText?: (text: string) => void;
  open: () => Stream | null;
}

export interface UseLiveStreamOptions {
  closeOnUnmount?: boolean;
}

export interface UseLiveStreamReturn {
  closeStream: () => void;
  /**
   * Unbind handlers and forget the stream without closing it — for streams
   * that persist server-side across consumer unmounts (the terminal PTY).
   */
  detachStream: () => void;
  // Returns true when a stream is active (existing or newly opened), false when opening failed.
  openStream: <TProgress = unknown>(
    options: OpenLiveStreamOptions<TProgress>,
  ) => boolean;
  streamRef: RefObject<Stream | null>;
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

  const detachStream = useCallback(() => {
    if (unbindRef.current) {
      unbindRef.current();
      unbindRef.current = null;
    }
    streamRef.current = null;
  }, []);

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
        onData: (data) => {
          options.onData?.(data);
          if (options.onText) {
            options.onText(decodeString(data));
          }
        },
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
    detachStream,
  };
}
