import {
  useState,
  useEffect,
  useRef,
  useCallback,
  useEffectEvent,
} from "react";

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

const INITIAL_LOG_SILENCE_TIMEOUT_MS = 1500;

/**
 * Manages a live log stream: opens/closes based on dialog state and live mode,
 * accumulates log text, and handles loading/error state.
 *
 * `createStream` does not need to be memoized.
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
  const initialLoadTimeoutRef = useRef<number | null>(null);

  const { streamRef, openStream, closeStream } = useLiveStream();
  const { isOpen: muxIsOpen } = useStreamMux();

  const clearInitialLoadTimeout = useCallback(() => {
    if (initialLoadTimeoutRef.current !== null) {
      window.clearTimeout(initialLoadTimeoutRef.current);
      initialLoadTimeoutRef.current = null;
    }
  }, []);

  const scheduleInitialLoadTimeout = useCallback(() => {
    clearInitialLoadTimeout();
    initialLoadTimeoutRef.current = window.setTimeout(() => {
      if (!hasReceivedData.current) {
        setIsLoading(false);
      }
    }, INITIAL_LOG_SILENCE_TIMEOUT_MS);
  }, [clearInitialLoadTimeout]);

  const handleStreamOpenError = useEffectEvent(() => {
    clearInitialLoadTimeout();
    queueMicrotask(() => {
      setError("Failed to connect to log stream");
      setIsLoading(false);
    });
  });

  const handleStreamData = useEffectEvent((data: Uint8Array) => {
    const text = decodeString(data);
    if (!hasReceivedData.current) {
      hasReceivedData.current = true;
      clearInitialLoadTimeout();
      setIsLoading(false);
    }
    setLogs((prev) => prev + text);
  });

  const handleStreamResult = useEffectEvent(
    (result: { status: "ok" | "error"; error?: string }) => {
      clearInitialLoadTimeout();
      if (result.status === "error") {
        setError(result.error || "Failed to load logs");
        setIsLoading(false);
      }
    },
  );

  const handleStreamClose = useEffectEvent(() => {
    clearInitialLoadTimeout();
    if (!hasReceivedData.current) {
      setIsLoading(false);
    }
  });

  // Scroll to bottom whenever new logs arrive.
  useEffect(() => {
    if (open && logsBoxRef.current) {
      logsBoxRef.current.scrollTop = logsBoxRef.current.scrollHeight;
    }
  }, [logs, open]);

  const resetState = useCallback(() => {
    clearInitialLoadTimeout();
    closeStream();
    setLogs("");
    setError(null);
    setLiveMode(true);
    setIsLoading(true);
    hasReceivedData.current = false;
  }, [clearInitialLoadTimeout, closeStream]);

  // Open stream when the dialog opens and the mux is ready.
  useEffect(() => {
    if (!open || !muxIsOpen) return;
    if (streamRef.current) return;

    hasReceivedData.current = false;
    scheduleInitialLoadTimeout();

    openStream({
      open: () => createStream(initialTail),
      onOpenError: handleStreamOpenError,
      onData: handleStreamData,
      onResult: handleStreamResult,
      onClose: handleStreamClose,
    });
  }, [open, muxIsOpen, streamRef]);

  // Handle live mode toggle.
  useEffect(() => {
    if (!liveMode && streamRef.current) {
      closeStream();
      clearInitialLoadTimeout();
      if (!hasReceivedData.current) {
        queueMicrotask(() => setIsLoading(false));
      }
    } else if (liveMode && !streamRef.current && open && muxIsOpen) {
      openStream({
        open: () => createStream(liveTail),
        onOpenError: handleStreamOpenError,
        onData: handleStreamData,
        onResult: handleStreamResult,
        onClose: handleStreamClose,
      });
    }
  }, [liveMode, open, muxIsOpen, streamRef]);

  // Close stream when the dialog closes (state is reset separately via onExited).
  useEffect(() => {
    if (!open) closeStream();
  }, [open, closeStream]);

  useEffect(() => clearInitialLoadTimeout, [clearInitialLoadTimeout]);

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
