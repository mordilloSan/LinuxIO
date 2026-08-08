import {
  useCallback,
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useRef,
  useState,
  type Dispatch,
  type RefObject,
  type SetStateAction,
} from "react";

import { type Stream, useStreamMux } from "@/api";
import { useLiveStream } from "@/hooks/useLiveStream";

export interface UseLogStreamOptions {
  /** Returns the stream handle; called with tail line count as a string. */
  createStream: (tail: string) => Stream | null;
  /** Number of tail lines to fetch on initial open. Default: "200". */
  initialTail?: string;
  /** Number of tail lines when re-enabling live mode. Default: "0". */
  liveTail?: string;
  open: boolean;
}

export interface UseLogStreamResult {
  error: string | null;
  isLoading: boolean;
  liveMode: boolean;
  logs: string;
  logsBoxRef: RefObject<HTMLDivElement | null>;
  resetState: () => void;
  setLiveMode: Dispatch<SetStateAction<boolean>>;
}

const INITIAL_LOG_SILENCE_TIMEOUT_MS = 1500;

// A live stream is unbounded; without a cap the buffer grows forever and
// every appended frame pays an O(buffer) string copy. Oldest lines fall off
// the top, trimmed to the next newline so the buffer starts on a whole line.
const MAX_LOG_BUFFER_CHARS = 512 * 1024;

function appendLogs(prev: string, text: string): string {
  const next = prev + text;
  if (next.length <= MAX_LOG_BUFFER_CHARS) {
    return next;
  }
  const trimmed = next.slice(next.length - MAX_LOG_BUFFER_CHARS);
  const newlineIndex = trimmed.indexOf("\n");
  return newlineIndex === -1 ? trimmed : trimmed.slice(newlineIndex + 1);
}

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
  const pendingLogsRef = useRef<string[]>([]);
  const logFlushFrameRef = useRef<number | null>(null);
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

  const flushPendingLogs = useCallback(() => {
    if (logFlushFrameRef.current !== null) {
      window.cancelAnimationFrame(logFlushFrameRef.current);
      logFlushFrameRef.current = null;
    }
    const pendingLogs = pendingLogsRef.current;
    pendingLogsRef.current = [];
    if (pendingLogs.length > 0) {
      const nextLogs = pendingLogs.join("");
      setLogs((previous) => appendLogs(previous, nextLogs));
    }
  }, []);

  const discardPendingLogs = useCallback(() => {
    if (logFlushFrameRef.current !== null) {
      window.cancelAnimationFrame(logFlushFrameRef.current);
      logFlushFrameRef.current = null;
    }
    pendingLogsRef.current = [];
  }, []);

  const handleStreamOpenError = useEffectEvent(() => {
    clearInitialLoadTimeout();
    queueMicrotask(() => {
      setError("Failed to connect to log stream");
      setIsLoading(false);
    });
  });

  const handleStreamText = useEffectEvent((text: string) => {
    if (!hasReceivedData.current) {
      hasReceivedData.current = true;
      clearInitialLoadTimeout();
      setIsLoading(false);
    }
    pendingLogsRef.current.push(text);
    if (logFlushFrameRef.current === null) {
      logFlushFrameRef.current = window.requestAnimationFrame(flushPendingLogs);
    }
  });

  const handleStreamResult = useEffectEvent(
    (result: { status: "ok" | "error"; error?: string }) => {
      clearInitialLoadTimeout();
      flushPendingLogs();
      if (result.status === "error") {
        setError(result.error || "Failed to load logs");
        setIsLoading(false);
      }
    },
  );

  const handleStreamClose = useEffectEvent(() => {
    clearInitialLoadTimeout();
    flushPendingLogs();
    if (!hasReceivedData.current) {
      setIsLoading(false);
    }
  });

  // Effect event so the opening effects don't depend on `createStream`, which
  // callers pass as a fresh closure every render (see the hook doc comment) —
  // depending on it would close and reopen the stream on every caller render.
  const startStream = useEffectEvent((tail: string) => {
    openStream({
      open: () => createStream(tail),
      onOpenError: handleStreamOpenError,
      onText: handleStreamText,
      onResult: handleStreamResult,
      onClose: handleStreamClose,
    });
  });

  // Scroll to bottom whenever new logs arrive (before paint to avoid a flash).
  useLayoutEffect(() => {
    if (open && logsBoxRef.current) {
      logsBoxRef.current.scrollTop = logsBoxRef.current.scrollHeight;
    }
  }, [logs, open]);

  const resetState = useCallback(() => {
    clearInitialLoadTimeout();
    discardPendingLogs();
    closeStream();
    setLogs("");
    setError(null);
    setLiveMode(true);
    setIsLoading(true);
    hasReceivedData.current = false;
  }, [clearInitialLoadTimeout, closeStream, discardPendingLogs]);

  // Open stream when the dialog opens and the mux is ready.
  useEffect(() => {
    if (!open || !muxIsOpen) return;
    if (streamRef.current) return;

    hasReceivedData.current = false;
    scheduleInitialLoadTimeout();
    startStream(initialTail);
  }, [initialTail, open, muxIsOpen, streamRef, scheduleInitialLoadTimeout]);

  // Handle live mode toggle.
  useEffect(() => {
    if (!liveMode && streamRef.current) {
      closeStream();
      clearInitialLoadTimeout();
      if (!hasReceivedData.current) {
        queueMicrotask(() => setIsLoading(false));
      }
    } else if (liveMode && !streamRef.current && open && muxIsOpen) {
      startStream(liveTail);
    }
  }, [
    liveMode,
    liveTail,
    open,
    muxIsOpen,
    streamRef,
    closeStream,
    clearInitialLoadTimeout,
  ]);

  // Close stream when the dialog closes (state is reset separately via onExited).
  useEffect(() => {
    if (!open) closeStream();
  }, [open, closeStream]);

  useEffect(
    () => () => {
      clearInitialLoadTimeout();
      discardPendingLogs();
    },
    [clearInitialLoadTimeout, discardPendingLogs],
  );

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
