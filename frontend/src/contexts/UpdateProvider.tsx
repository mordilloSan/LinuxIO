import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

import {
  bindStreamHandlers,
  decodeString,
  getStreamMux,
  openAppUpdateStream,
  type Stream,
} from "@/api";
import {
  publishLiveUpdateBlocked,
  UpdateContext,
  UpdateNavigationContext,
  type UpdateContextValue,
  type UpdatePhase,
} from "@/contexts/UpdateContext";
import { useUpdateNavigationGuard } from "@/contexts/useUpdateNavigationGuard";
import { useLatestRef } from "@/hooks/useLatestRef";

const UPDATE_TIMEOUT_MS = 20 * 60 * 1000;
const POLL_START_DELAY_MS = 2000;
const POLL_INTERVAL_MS = 2000;
const VERIFY_TIMEOUT_MS = 10 * 60 * 1000;
const LEGACY_UPDATE_STORAGE_KEY = "linuxio.active-app-update";

const buildUpdateRunId = () => {
  if (
    typeof crypto !== "undefined" &&
    typeof crypto.randomUUID === "function"
  ) {
    return crypto.randomUUID();
  }
  throw new Error("This browser cannot create a secure update operation ID");
};

interface UpdateStatusResponse {
  exit_code?: number;
  finished_at?: number;
  id?: string;
  message?: string;
  started_at?: number;
  status: "unknown" | "running" | "ok" | "error";
}

export const UpdateProvider = ({ children }: { children: ReactNode }) => {
  const value = useUpdateController();
  const ownerRef = useRef<object>({});
  useUpdateNavigationGuard(value.isUpdating);

  useLayoutEffect(
    () => publishLiveUpdateBlocked(ownerRef.current, value.isUpdating),
    [value.isUpdating],
  );

  return (
    <UpdateNavigationContext.Provider value={value.canNavigate}>
      <UpdateContext.Provider value={value}>{children}</UpdateContext.Provider>
    </UpdateNavigationContext.Provider>
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
  // startUpdate reads the phase through a ref so its identity stays stable
  // and the memoized context value only changes when real state changes.
  const phaseRef = useLatestRef(phase);

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

  const completeUpdate = useCallback(() => {
    clearTimers();
    updateRunIdRef.current = null;
    setPhase("done");
    setStatus("Update complete");
    setProgress(100);
    getStreamMux()?.setUpdating(false);
  }, [clearTimers]);

  const beginVerification = useCallback(
    (installerCompleted = false) => {
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
          const versions = (await versionResult.value.json()) as Record<
            string,
            unknown
          >;
          versionMatch =
            target !== null && versions["LinuxIO Web Server"] === target;
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

        // The webserver restart invalidates its in-memory login session, so the
        // authenticated status endpoint can return 401 after a successful update.
        // A targeted update completes only when the restarted webserver reports
        // that exact target. For an untargeted update, the successful Task result
        // or a pre-restart status=ok supplies the missing install proof.
        const completionConfirmed = target
          ? versionMatch
          : installerCompleted || updateStatus?.status === "ok";
        if (serverResponding && completionConfirmed) {
          completeUpdate();
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
    },
    [
      clearTimers,
      completeUpdate,
      failUpdate,
      markUpdateStartedFromStatus,
      trackInterval,
      trackTimeout,
    ],
  );

  const handleStreamFinished = useCallback(
    (fallbackError?: string, installerCompleted = false) => {
      const finalize = async () => {
        if (!updateRunIdRef.current) {
          return;
        }

        if (installerCompleted) {
          setStatus("Update installed - restarting services...");
          setProgress((prev) => Math.max(prev, 85));
          beginVerification(true);
          return;
        }

        const updateStatus = await fetchUpdateStatus();

        if (fallbackError) {
          const exitCode = updateStatus?.exit_code;
          const detail = updateStatus?.message?.trim() || fallbackError.trim();
          const prefix =
            exitCode !== undefined
              ? `Update failed (exit code ${exitCode})`
              : "Update failed";
          failUpdate(detail ? `${prefix}: ${detail}` : prefix);
          return;
        }

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
          const detail = updateStatus.message?.trim();
          const prefix =
            exitCode !== undefined
              ? `Update failed (exit code ${exitCode})`
              : "Update failed";
          const message = detail ? `${prefix}: ${detail}` : prefix;
          failUpdate(message);
          return;
        }

        // Don't set phase here - let beginVerification handle it to avoid state update race conditions
        setStatus("Update in progress - service restarting...");
        setProgress((prev) => Math.max(prev, 60));
        beginVerification(updateStatus.status === "ok");
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
      if (phaseRef.current !== "idle") return;

      const target = version ?? null;
      let runId: string;
      try {
        runId = buildUpdateRunId();
      } catch (cause) {
        failUpdate(
          cause instanceof Error ? cause.message : "Failed to create update ID",
        );
        return;
      }
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
          handleStreamFinished(fallbackError, result.status === "ok");
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
      phaseRef,
      trackTimeout,
    ],
  );

  useEffect(() => {
    // v0.18-v0.24 persisted this marker and could permanently disable
    // navigation after the restart invalidated the old server session.
    try {
      window.localStorage.removeItem(LEGACY_UPDATE_STORAGE_KEY);
    } catch {
      // The updater no longer depends on browser storage.
    }
  }, []);

  // Unmount-only cleanup. Self-contained over refs so the effect stays
  // mount-only and can never close the stream mid-update.
  useEffect(() => {
    // The Set itself is created once and never reassigned, so capturing it
    // here still clears whatever timers are pending at unmount.
    const timers = timersRef.current;
    return () => {
      timers.forEach((timerId) => clearTimeout(timerId));
      timers.clear();
      if (unbindStreamHandlersRef.current) {
        unbindStreamHandlersRef.current();
        unbindStreamHandlersRef.current = null;
      }
      if (streamRef.current) {
        streamRef.current.close();
        streamRef.current = null;
      }
    };
  }, []);

  return useMemo<UpdateContextValue>(() => {
    const isUpdating =
      phase === "running" || phase === "restarting" || phase === "verifying";
    return {
      phase,
      status,
      progress,
      output,
      error,
      targetVersion,
      isUpdating,
      updateComplete: phase === "done" || phase === "failed",
      updateSuccess: phase === "done",
      canNavigate: !isUpdating,
      startUpdate,
      resetUpdate,
    };
  }, [
    error,
    output,
    phase,
    progress,
    resetUpdate,
    startUpdate,
    status,
    targetVersion,
  ]);
};
