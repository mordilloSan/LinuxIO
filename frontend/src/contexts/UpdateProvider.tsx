import {
  useEffect,
  useEffectEvent,
  useLayoutEffect,
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

const UPDATE_TIMEOUT_MS = 20 * 60 * 1000;
const POLL_START_DELAY_MS = 2000;
const POLL_INTERVAL_MS = 2000;
const VERIFY_TIMEOUT_MS = 10 * 60 * 1000;
const UPDATE_STORAGE_KEY = "linuxio.active-app-update";
const CANONICAL_UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

interface StoredUpdate {
  runId: string;
  targetVersion: string | null;
}

const buildUpdateRunId = () => {
  if (
    typeof crypto !== "undefined" &&
    typeof crypto.randomUUID === "function"
  ) {
    return crypto.randomUUID();
  }
  throw new Error("This browser cannot create a secure update operation ID");
};

const readStoredUpdate = (): StoredUpdate | null => {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(UPDATE_STORAGE_KEY);
    if (!raw) return null;
    const value = JSON.parse(raw) as Partial<StoredUpdate>;
    if (
      typeof value.runId !== "string" ||
      !CANONICAL_UUID_RE.test(value.runId) ||
      (value.targetVersion !== null && typeof value.targetVersion !== "string")
    ) {
      window.localStorage.removeItem(UPDATE_STORAGE_KEY);
      return null;
    }
    return { runId: value.runId, targetVersion: value.targetVersion ?? null };
  } catch {
    window.localStorage.removeItem(UPDATE_STORAGE_KEY);
    return null;
  }
};

const writeStoredUpdate = (value: StoredUpdate) => {
  try {
    window.localStorage.setItem(UPDATE_STORAGE_KEY, JSON.stringify(value));
  } catch {
    // The durable backend record remains authoritative if browser storage is unavailable.
  }
};

const clearStoredUpdate = () => {
  try {
    window.localStorage.removeItem(UPDATE_STORAGE_KEY);
  } catch {
    // Nothing else is required; terminal state is already persisted by the backend.
  }
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
  const [storedUpdate] = useState(readStoredUpdate);
  const [phase, setPhase] = useState<UpdatePhase>(
    storedUpdate ? "verifying" : "idle",
  );
  const [status, setStatus] = useState<string>(
    storedUpdate ? "Recovering update status..." : "",
  );
  const [progress, setProgress] = useState(storedUpdate ? 60 : 0);
  const [output, setOutput] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [targetVersion, setTargetVersion] = useState<string | null>(
    storedUpdate?.targetVersion ?? null,
  );

  const streamRef = useRef<Stream | null>(null);
  const unbindStreamHandlersRef = useRef<(() => void) | null>(null);
  const updateStartedRef = useRef(storedUpdate !== null);
  const updateRunIdRef = useRef<string | null>(storedUpdate?.runId ?? null);
  const targetVersionRef = useRef<string | null>(
    storedUpdate?.targetVersion ?? null,
  );
  const timersRef = useRef<Set<number>>(new Set());

  const isUpdating =
    phase === "running" || phase === "restarting" || phase === "verifying";
  const updateComplete = phase === "done" || phase === "failed";
  const updateSuccess = phase === "done";
  const canNavigate = !isUpdating;

  const trackTimeout = (fn: () => void, delayMs: number) => {
    const timerId = window.setTimeout(() => {
      timersRef.current.delete(timerId);
      fn();
    }, delayMs);
    timersRef.current.add(timerId);
    return timerId;
  };

  const trackInterval = (fn: () => void, delayMs: number) => {
    const timerId = window.setInterval(fn, delayMs);
    timersRef.current.add(timerId);
    return timerId;
  };

  const clearTimers = () => {
    timersRef.current.forEach((timerId) => clearTimeout(timerId));
    timersRef.current.clear();
  };

  const detachStreamHandlers = () => {
    if (unbindStreamHandlersRef.current) {
      unbindStreamHandlersRef.current();
      unbindStreamHandlersRef.current = null;
    }
  };

  const resetUpdate = () => {
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
    clearStoredUpdate();
    // Re-enable API requests
    getStreamMux()?.setUpdating(false);
  };

  const failUpdate = (message: string) => {
    clearTimers();
    detachStreamHandlers();
    if (streamRef.current) {
      streamRef.current.close();
    }
    streamRef.current = null;
    updateRunIdRef.current = null;
    clearStoredUpdate();
    setPhase("failed");
    setError(message);
    setStatus("Update failed");
    setProgress(100);
    // Re-enable API requests
    getStreamMux()?.setUpdating(false);
  };

  const markUpdateStarted = () => {
    if (updateStartedRef.current) return;
    updateStartedRef.current = true;
    setProgress((prev) => Math.max(prev, 30));
  };

  const markUpdateStartedFromStatus = (
    status?: UpdateStatusResponse | null,
  ) => {
    if (!status) return;
    if (status.status === "running" || status.status === "ok") {
      markUpdateStarted();
    }
    if (status.status === "error") {
      markUpdateStarted();
    }
  };

  const fetchUpdateStatus = async () => {
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
  };

  const beginVerification = () => {
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
        clearStoredUpdate();
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
  };

  const handleStreamFinished = (fallbackError?: string) => {
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
        const detail = updateStatus.message?.trim() || fallbackError?.trim();
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
      beginVerification();
    };

    void finalize();
  };

  const startUpdate = (version?: string) => {
    if (phase !== "idle") return;

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
    writeStoredUpdate({ runId, targetVersion: target });

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
        const lines = text.split("\n").filter((line) => line.trim().length > 0);
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
  };

  const resumeStoredUpdate = useEffectEvent(() => {
    if (!storedUpdate || updateRunIdRef.current !== storedUpdate.runId) return;
    getStreamMux()?.setUpdating(true);
    beginVerification();
  });

  useEffect(() => {
    resumeStoredUpdate();
  }, []);

  // Unmount-only cleanup. Self-contained over refs (instead of calling
  // clearTimers/detachStreamHandlers) so the dependency array can stay empty:
  // with unstable function identities in dev, depending on them would re-run
  // this cleanup every render and close the stream mid-update.
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

  return {
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
  };
};
