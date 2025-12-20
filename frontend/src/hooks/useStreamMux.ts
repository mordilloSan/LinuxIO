import { useEffect, useState, useCallback } from "react";
import {
  getStreamMux,
  initStreamMux,
  Stream,
  MuxStatus,
  StreamType,
} from "@/services/StreamMultiplexer";

/**
 * Hook to use the singleton StreamMultiplexer.
 *
 * The multiplexer persists across component mounts - streams stay alive
 * when navigating away and can be reattached when returning.
 */
export function useStreamMux() {
  const [status, setStatus] = useState<MuxStatus>(() => {
    const mux = getStreamMux();
    return mux?.status ?? "closed";
  });

  useEffect(() => {
    // Initialize singleton if not already done
    const mux = initStreamMux();

    // Update status immediately
    setStatus(mux.status);

    // Subscribe to status changes
    const unsubscribe = mux.addStatusListener((newStatus: MuxStatus) => {
      setStatus(newStatus);
    });

    // Don't close on unmount - singleton persists
    return () => {
      unsubscribe();
    };
  }, []);

  const openStream = useCallback(
    (type: StreamType, initialPayload?: Uint8Array): Stream | null => {
      const mux = getStreamMux();
      if (!mux || mux.status !== "open") {
        console.warn("[useStreamMux] Cannot open stream - mux not ready");
        return null;
      }
      return mux.openStream(type, initialPayload);
    },
    [],
  );

  const getStream = useCallback((type: StreamType): Stream | null => {
    const mux = getStreamMux();
    if (!mux) return null;
    return mux.getStream(type);
  }, []);

  return {
    status,
    isOpen: status === "open",
    openStream,
    getStream,
  };
}

export default useStreamMux;
