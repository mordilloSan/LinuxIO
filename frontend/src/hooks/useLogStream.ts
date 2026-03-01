import { useState, useEffect, useRef, useCallback } from "react";

import { useStreamMux, decodeString, type Stream } from "@/api";
import { useLiveStream } from "@/hooks/useLiveStream";

export interface UseLogStreamOptions {
  open: boolean;
  /** Returns the stream handle; called with tail line count as a string. */
  createStream: (tail: string) => Stream | null;
  /** Number of tail lines to fetch on initial open. Default: "200". */
  initialTail?: string;
  /** Number of tail lines when re-enabling live mode. Default: "0". */
  liveTail?: string;
}

export interface UseLogStreamResult {
  logs: string;
  isLoading: boolean;
  error: string | null;
  liveMode: boolean;
  setLiveMode: React.Dispatch<React.SetStateAction<boolean>>;
  logsBoxRef: React.RefObject<HTMLDivElement | null>;
  resetState: () => void;
}

/**
 * Manages a live log stream: opens/closes based on dialog state and live mode,
 * accumulates log text, and handles loading/error state.
 *
 * `createStream` does not need to be memoized — a ref is used internally.
 */
export function useLogStream({
  open,
  createStream,
  initialTail = "200",
  liveTail = "0",
}: UseLogStreamOptions): UseLogStreamResult {
  const [liveMode, setLiveMode] = useState(true);
  const [logs, setLogs] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const logsBoxRef = useRef<HTMLDivElement>(null);
  const hasReceivedData = useRef(false);

  // Stable ref so effects don't need createStream in their dep arrays.
  const createStreamRef = useRef(createStream);
  createStreamRef.current = createStream;

  const { streamRef, openStream, closeStream } = useLiveStream();
  const { isOpen: muxIsOpen } = useStreamMux();

  // Scroll to bottom whenever new logs arrive.
  useEffect(() => {
    if (open && logsBoxRef.current) {
      logsBoxRef.current.scrollTop = logsBoxRef.current.scrollHeight;
    }
  }, [logs, open]);

  const resetState = useCallback(() => {
    closeStream();
    setLogs("");
    setError(null);
    setLiveMode(true);
    setIsLoading(true);
    hasReceivedData.current = false;
  }, [closeStream]);

  // Open stream when the dialog opens and the mux is ready.
  useEffect(() => {
    if (!open || !muxIsOpen) return;
    if (streamRef.current) return;

    hasReceivedData.current = false;

    openStream({
      open: () => createStreamRef.current(initialTail),
      onOpenError: () => {
        queueMicrotask(() => {
          setError("Failed to connect to log stream");
          setIsLoading(false);
        });
      },
      onData: (data: Uint8Array) => {
        const text = decodeString(data);
        if (!hasReceivedData.current) {
          hasReceivedData.current = true;
          setIsLoading(false);
        }
        setLogs((prev) => prev + text);
      },
      onClose: () => {
        if (!hasReceivedData.current) {
          setIsLoading(false);
        }
      },
    });
  }, [open, muxIsOpen, openStream, streamRef, initialTail]);

  // Handle live mode toggle.
  useEffect(() => {
    if (!liveMode && streamRef.current) {
      closeStream();
      if (!hasReceivedData.current) {
        queueMicrotask(() => setIsLoading(false));
      }
    } else if (liveMode && !streamRef.current && open && muxIsOpen) {
      openStream({
        open: () => createStreamRef.current(liveTail),
        onData: (data: Uint8Array) => {
          setLogs((prev) => prev + decodeString(data));
        },
      });
    }
  }, [liveMode, open, muxIsOpen, closeStream, openStream, streamRef, liveTail]);

  // Close stream when the dialog closes (state is reset separately via onExited).
  useEffect(() => {
    if (!open) closeStream();
  }, [open, closeStream]);

  return {
    logs,
    isLoading,
    error,
    liveMode,
    setLiveMode,
    logsBoxRef,
    resetState,
  };
}
