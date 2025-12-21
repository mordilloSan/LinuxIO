import { useEffect, useState, useCallback } from "react";
import {
  getStreamMux,
  Stream,
  MuxStatus,
  StreamType,
} from "@/utils/StreamMultiplexer";

/**
 * Hook to use the singleton StreamMultiplexer.
 *
 * The multiplexer is initialized by AuthContext on login.
 * This hook provides access to it and tracks status changes.
 */
export function useStreamMux() {
  const [status, setStatus] = useState<MuxStatus>(() => {
    const mux = getStreamMux();
    return mux?.status ?? "closed";
  });

  useEffect(() => {
    const mux = getStreamMux();
    if (!mux) return;

    // Update status immediately
    setStatus(mux.status);

    // Subscribe to status changes
    const unsubscribe = mux.addStatusListener((newStatus: MuxStatus) => {
      setStatus(newStatus);
    });

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
