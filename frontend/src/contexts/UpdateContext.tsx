import React, {
  createContext,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useBeforeUnload, useLocation, useNavigate } from "react-router-dom";

import {
  bindStreamHandlers,
  decodeString,
  getStreamMux,
  openAppUpdateStream,
  type Stream,
} from "@/api";

const UPDATE_TIMEOUT_MS = 20 * 60 * 1000;
const POLL_START_DELAY_MS = 2000;
const POLL_INTERVAL_MS = 2000;
const VERIFY_TIMEOUT_MS = 10 * 60 * 1000;

const buildUpdateRunId = () => {
  if (
    typeof crypto !== "undefined" &&
    typeof crypto.randomUUID === "function"
  ) {
    return crypto.randomUUID();
  }
  return `update-${Date.now()}-${Math.random().toString(16).slice(2)}`;
};

export type UpdatePhase =
  | "idle"
  | "running"
  | "restarting"
  | "verifying"
  | "done"
  | "failed";

export interface UpdateContextValue {
  phase: UpdatePhase;
  status: string;
  progress: number;
  output: string[];
  error: string | null;
  targetVersion: string | null;
  isUpdating: boolean;
  updateComplete: boolean;
  updateSuccess: boolean;
  canNavigate: boolean;
  startUpdate: (targetVersion?: string) => void;
  resetUpdate: () => void;
}

interface UpdateStatusResponse {
  status: "unknown" | "running" | "ok" | "error";
  id?: string;
  exit_code?: number;
  started_at?: number;
  finished_at?: number;
  message?: string;
}

export const UpdateContext = createContext<UpdateContextValue | null>(null);
UpdateContext.displayName = "UpdateContext";

export const UpdateProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const value = useUpdateController();
  useUpdateNavigationGuard(value.isUpdating);

  return (
    <UpdateContext.Provider value={value}>{children}</UpdateContext.Provider>
  );
};

const useUpdateController = (): UpdateContextValue => {
  const [phase, setPhase] = useState<UpdatePhase>("idle");
  const [status, setStatus] = useState<string>("");
  const [progress, setProgress] = useState(0);
  const [output, setOutput] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [targetVersion, setTargetVersion] = useState<string | null>(null);

  const streamRef = useRef<Stream | null>(null);
  const unbindStreamHandlersRef = useRef<(() => void) | null>(null);
  const updateStartedRef = useRef(false);
  const updateRunIdRef = useRef<string | null>(null);
  const targetVersionRef = useRef<string | null>(null);
  const timersRef = useRef<Set<number>>(new Set());

  const isUpdating =
    phase === "running" || phase === "restarting" || phase === "verifying";
  const updateComplete = phase === "done" || phase === "failed";
  const updateSuccess = phase === "done";
  const canNavigate = !isUpdating;

  const trackTimeout = useCallback((fn: () => void, delayMs: number) => {
    const timerId = window.setTimeout(() => {
      timersRef.current.delete(timerId);
      fn();
    }, delayMs);
    timersRef.current.add(timerId);
    return timerId;
  }, []);

  const trackInterval = useCallback((fn: () => void, delayMs: number) => {
    const timerId = window.setInterval(fn, delayMs);
    timersRef.current.add(timerId);
    return timerId;
  }, []);

  const clearTimers = useCallback(() => {
    timersRef.current.forEach((timerId) => clearTimeout(timerId));
    timersRef.current.clear();
  }, []);

  const detachStreamHandlers = useCallback(() => {
    if (unbindStreamHandlersRef.current) {
      unbindStreamHandlersRef.current();
      unbindStreamHandlersRef.current = null;
    }
  }, []);

  const resetUpdate = useCallback(() => {
    clearTimers();
    detachStreamHandlers();
    if (streamRef.current) {
      streamRef.current.close();
    }
    streamRef.current = null;
    updateStartedRef.current = false;
    updateRunIdRef.current = null;
    targetVersionRef.current = null;
    setPhase("idle");
    setStatus("");
    setProgress(0);
    setOutput([]);
    setError(null);
    setTargetVersion(null);
    // Re-enable API requests
    getStreamMux()?.setUpdating(false);
  }, [clearTimers, detachStreamHandlers]);

  const failUpdate = useCallback(
    (message: string) => {
      clearTimers();
      detachStreamHandlers();
      if (streamRef.current) {
        streamRef.current.close();
      }
      streamRef.current = null;
      updateRunIdRef.current = null;
      setPhase("failed");
      setError(message);
      setStatus("Update failed");
      setProgress(100);
      // Re-enable API requests
      getStreamMux()?.setUpdating(false);
    },
    [clearTimers, detachStreamHandlers],
  );

  const markUpdateStarted = useCallback(() => {
    if (updateStartedRef.current) return;
    updateStartedRef.current = true;
    setProgress((prev) => Math.max(prev, 30));
  }, []);

  const markUpdateStartedFromStatus = useCallback(
    (status?: UpdateStatusResponse | null) => {
      if (!status) return;
      if (status.status === "running" || status.status === "ok") {
        markUpdateStarted();
      }
      if (status.status === "error") {
        markUpdateStarted();
      }
    },
    [markUpdateStarted],
  );

  const fetchUpdateStatus = useCallback(async () => {
    const runId = updateRunIdRef.current;
    if (!runId) return null;
    const url = `/api/update-status?id=${encodeURIComponent(runId)}`;
    try {
      const res = await fetch(url, {
        method: "GET",
        headers: { "Content-Type": "application/json" },
        cache: "no-store",
      });
      if (!res.ok) return null;
      return (await res.json()) as UpdateStatusResponse;
    } catch {
      return null;
    }
  }, []);

  const beginVerification = useCallback(() => {
    const runId = updateRunIdRef.current;
    if (!runId) {
      failUpdate("Update verification missing run id");
      return;
    }

    setPhase("verifying");
    clearTimers();
    setStatus("Waiting for server to come back...");
    setProgress((prev) => Math.max(prev, 90));

    const poll = async () => {
      if (updateRunIdRef.current !== runId) return;
      const target = targetVersionRef.current;
      const statusUrl = `/api/update-status?id=${encodeURIComponent(runId)}`;
      const [versionResult, statusResult] = await Promise.allSettled([
        fetch("/api/version", {
          method: "GET",
          headers: { "Content-Type": "application/json" },
          cache: "no-store",
        }),
        fetch(statusUrl, {
          method: "GET",
          headers: { "Content-Type": "application/json" },
          cache: "no-store",
        }),
      ]);

      if (updateRunIdRef.current !== runId) return;

      let versionMatch = false;
      let serverResponding = false;
      if (versionResult.status === "fulfilled" && versionResult.value.ok) {
        serverResponding = true;
        const versions = await versionResult.value.json();
        versionMatch = target
          ? Object.values(versions).some((value) => value === target)
          : true;
      }

      let updateStatus: UpdateStatusResponse | null = null;
      if (statusResult.status === "fulfilled" && statusResult.value.ok) {
        updateStatus = await statusResult.value.json();
      }

      if (updateRunIdRef.current !== runId) return;

      if (updateStatus?.status === "error") {
        const exitCode = updateStatus.exit_code;
        const message =
          exitCode !== undefined
            ? `Update failed (exit code ${exitCode})`
            : "Update failed";
        failUpdate(message);
        return;
      }

      if (updateStatus) {
        markUpdateStartedFromStatus(updateStatus);
      }

      // Complete if update status is OK and either:
      // 1. Version matches the target, or
      // 2. Server is responding (even if we can't verify version yet)
      if (updateStatus?.status === "ok" && (versionMatch || serverResponding)) {
        clearTimers();
        updateRunIdRef.current = null;
        setPhase("done");
        setStatus("Update complete");
        setProgress(100);
        // Re-enable API requests
        getStreamMux()?.setUpdating(false);
      }
    };

    trackTimeout(() => {
      void poll();
      trackInterval(() => {
        void poll();
      }, POLL_INTERVAL_MS);
    }, POLL_START_DELAY_MS);

    trackTimeout(() => {
      if (updateRunIdRef.current === runId) {
        failUpdate("Update verification timed out");
      }
    }, VERIFY_TIMEOUT_MS);
  }, [
    clearTimers,
    failUpdate,
    markUpdateStartedFromStatus,
    trackInterval,
    trackTimeout,
  ]);

  const handleStreamFinished = useCallback(
    (fallbackError?: string) => {
      const finalize = async () => {
        if (!updateRunIdRef.current) {
          return;
        }
        const updateStatus = await fetchUpdateStatus();

        if (!updateStatus || updateStatus.status === "unknown") {
          // If we know the update started (received output), the server is likely just
          // restarting. Don't fail immediately - proceed to verification and keep polling.
          if (updateStartedRef.current) {
            setStatus("Update in progress - service restarting...");
            setProgress((prev) => Math.max(prev, 60));
            beginVerification();
            return;
          }
          failUpdate(fallbackError || "Stream closed before update started");
          return;
        }

        markUpdateStartedFromStatus(updateStatus);

        if (updateStatus.status === "error") {
          const exitCode = updateStatus.exit_code;
          const message =
            exitCode !== undefined
              ? `Update failed (exit code ${exitCode})`
              : "Update failed";
          failUpdate(message);
          return;
        }

        // Don't set phase here - let beginVerification handle it to avoid state update race conditions
        setStatus("Update in progress - service restarting...");
        setProgress((prev) => Math.max(prev, 60));
        beginVerification();
      };

      void finalize();
    },
    [
      beginVerification,
      failUpdate,
      fetchUpdateStatus,
      markUpdateStartedFromStatus,
    ],
  );

  const startUpdate = useCallback(
    (version?: string) => {
      if (phase !== "idle") return;

      const target = version ?? null;
      const runId = buildUpdateRunId();
      targetVersionRef.current = target;
      updateStartedRef.current = false;
      updateRunIdRef.current = runId;
      clearTimers();

      setPhase("running");
      setStatus("Starting update...");
      setProgress(10);
      setError(null);
      setOutput([]);
      setTargetVersion(target);

      const mux = getStreamMux();
      if (!mux || mux.status !== "open") {
        failUpdate("Stream connection not ready");
        return;
      }

      // Disable all API requests during update
      mux.setUpdating(true);

      const stream = openAppUpdateStream(runId, target ?? undefined);
      if (!stream) {
        failUpdate("Failed to open update stream");
        return;
      }

      streamRef.current = stream;

      trackTimeout(() => {
        if (updateRunIdRef.current === runId) {
          failUpdate("Update timed out");
        }
      }, UPDATE_TIMEOUT_MS);

      unbindStreamHandlersRef.current = bindStreamHandlers(stream, {
        onData: (data: Uint8Array) => {
          const text = decodeString(data);
          const lines = text
            .split("\n")
            .filter((line) => line.trim().length > 0);
          if (lines.length === 0) return;
          markUpdateStarted();

          for (const line of lines) {
            setOutput((prev) => [...prev, line]);
            setStatus(line);

            // Update progress based on installation steps
            if (
              line.includes("Step 1/5:") ||
              line.includes("Downloading binaries")
            ) {
              setProgress(20);
            } else if (
              line.includes("Step 2/5:") ||
              line.includes("Verifying checksums")
            ) {
              setProgress(35);
            } else if (
              line.includes("Step 3/5:") ||
              line.includes("Installing binaries")
            ) {
              setProgress(50);
            } else if (
              line.includes("Step 4/5:") ||
              line.includes("Installing configuration")
            ) {
              setProgress(65);
            } else if (
              line.includes("Step 5/5:") ||
              line.includes("Installing systemd")
            ) {
              setProgress(75);
            } else if (line.includes("Installation complete")) {
              setProgress(85);
            }
          }
        },
        onResult: (result) => {
          detachStreamHandlers();
          streamRef.current = null;
          const fallbackError =
            result.status === "error"
              ? result.error || "Update failed"
              : undefined;
          handleStreamFinished(fallbackError);
        },
        onClose: () => {
          detachStreamHandlers();
          streamRef.current = null;
          handleStreamFinished();
        },
      });
    },
    [
      clearTimers,
      detachStreamHandlers,
      failUpdate,
      handleStreamFinished,
      markUpdateStarted,
      phase,
      trackTimeout,
    ],
  );

  useEffect(() => {
    return () => {
      clearTimers();
      detachStreamHandlers();
      if (streamRef.current) {
        streamRef.current.close();
        streamRef.current = null;
      }
    };
  }, [clearTimers, detachStreamHandlers]);

  return useMemo(
    () => ({
      phase,
      status,
      progress,
      output,
      error,
      targetVersion,
      isUpdating,
      updateComplete,
      updateSuccess,
      canNavigate,
      startUpdate,
      resetUpdate,
    }),
    [
      canNavigate,
      error,
      isUpdating,
      output,
      phase,
      progress,
      resetUpdate,
      startUpdate,
      status,
      targetVersion,
      updateComplete,
      updateSuccess,
    ],
  );
};

const useUpdateNavigationGuard = (isUpdating: boolean) => {
  const navigate = useNavigate();
  const location = useLocation();
  const lastSafePathRef = useRef(
    `${location.pathname}${location.search}${location.hash}`,
  );

  useEffect(() => {
    if (!isUpdating) {
      lastSafePathRef.current = `${location.pathname}${location.search}${location.hash}`;
    }
  }, [isUpdating, location.hash, location.pathname, location.search]);

  useEffect(() => {
    if (!isUpdating) return;
    const currentPath = `${location.pathname}${location.search}${location.hash}`;
    if (currentPath !== lastSafePathRef.current) {
      navigate(lastSafePathRef.current, { replace: true });
    }
  }, [isUpdating, location.hash, location.pathname, location.search, navigate]);

  const handleBeforeUnload = useCallback(
    (event: BeforeUnloadEvent) => {
      if (!isUpdating) return;
      event.preventDefault();
      const legacyEvent: { returnValue?: string } = event;
      legacyEvent.returnValue = "";
    },
    [isUpdating],
  );

  useBeforeUnload(handleBeforeUnload);
};
