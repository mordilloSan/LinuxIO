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
  decodeString,
  encodeString,
  getStreamMux,
  type ResultFrame,
  type Stream,
} from "@/api/linuxio";

// Stream type for command execution (must match backend generic.StreamTypeExec)
const STREAM_TYPE_EXEC = "exec";

// In dev mode, use local test script; in production, use GitHub hosted script
const INSTALL_SCRIPT_URL = import.meta.env.DEV
  ? "http://localhost:9999/dev-test-update.sh"
  : "https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-linuxio-binaries.sh";
const UPDATE_STATUS_FILE = "/run/linuxio/update-status.json";

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
  const updateStartedRef = useRef(false);
  const updateRunIdRef = useRef<string | null>(null);
  const targetVersionRef = useRef<string | null>(null);
  const timersRef = useRef<Set<ReturnType<typeof setTimeout>>>(new Set());

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

  const resetUpdate = useCallback(() => {
    clearTimers();
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
  }, [clearTimers]);

  const failUpdate = useCallback(
    (message: string) => {
      clearTimers();
      updateRunIdRef.current = null;
      setPhase("failed");
      setError(message);
      setStatus("Update failed");
      setProgress(100);
      // Re-enable API requests
      getStreamMux()?.setUpdating(false);
    },
    [clearTimers],
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

  const buildUpdateCommand = useCallback(
    (runId: string, version?: string | null) => {
      const updateCmd = version
        ? `curl -fsSL ${INSTALL_SCRIPT_URL} | bash -s -- ${version}`
        : `curl -fsSL ${INSTALL_SCRIPT_URL} | bash`;

      const unitScript = [
        "set +e",
        `run_id="${runId}"`,
        "started=$(date +%s)",
        "mkdir -p /run/linuxio",
        `printf "{\\"id\\":\\"%s\\",\\"status\\":\\"running\\",\\"started_at\\":%s}\\\\n" "$run_id" "$started" > ${UPDATE_STATUS_FILE}`,
        updateCmd,
        "code=$?",
        "finished=$(date +%s)",
        "status=ok",
        "if [ $code -ne 0 ]; then status=error; fi",
        `printf "{\\"id\\":\\"%s\\",\\"status\\":\\"%s\\",\\"exit_code\\":%s,\\"started_at\\":%s,\\"finished_at\\":%s}\\\\n" "$run_id" "$status" "$code" "$started" "$finished" > ${UPDATE_STATUS_FILE}`,
        "exit $code",
      ].join("; ");

      const unitCmd = `bash -c '${unitScript}'`;
      const commandParts = [
        `rm -f ${UPDATE_STATUS_FILE}`,
        `systemd-run --no-block --unit=linuxio-update -p StandardOutput=journal -p StandardError=journal ${unitCmd}`,
        "sleep 0.5",
        "journalctl -f -u linuxio-update --lines=0 --no-pager -o cat",
      ];

      return commandParts.join(" && ");
    },
    [],
  );

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

      const cmd = buildUpdateCommand(runId, target);
      const payload = encodeString(
        [STREAM_TYPE_EXEC, "bash", "-c", cmd].join("\0"),
      );

      const stream = mux.openStream(STREAM_TYPE_EXEC, payload);
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

      // Poll update status to detect completion and close stream
      // The journalctl -f stream never closes on its own, so we monitor the status file
      const pollStatusAndCloseStream = async () => {
        if (updateRunIdRef.current !== runId) {
          return;
        }
        const status = await fetchUpdateStatus();
        if (status && (status.status === "ok" || status.status === "error")) {
          // Update completed, close the stream and trigger verification
          if (streamRef.current) {
            const currentStream = streamRef.current;
            streamRef.current = null;

            // Clear stream handlers to prevent double-calling handleStreamFinished
            currentStream.onClose = () => {};
            currentStream.onResult = () => {};
            currentStream.close();

            // Stop polling - clear all timers before starting verification
            clearTimers();

            // Manually trigger the finish handler to start verification
            const fallbackError =
              status.status === "error" ? "Update failed" : undefined;
            handleStreamFinished(fallbackError);
          }
        }
      };

      // Start polling after a delay (give update time to start)
      trackTimeout(() => {
        const intervalId = trackInterval(() => {
          void pollStatusAndCloseStream();
        }, POLL_INTERVAL_MS);
        // Clean up interval on timeout
        trackTimeout(() => clearInterval(intervalId), UPDATE_TIMEOUT_MS);
      }, POLL_START_DELAY_MS);

      stream.onData = (data: Uint8Array) => {
        const text = decodeString(data);
        const lines = text
          .split("\n")
          .map((line) => {
            const trimmed = line.trim();
            // Filter out systemd journal metadata lines
            if (!trimmed) return null;
            if (trimmed.startsWith("Running as unit:")) return null;
            // Truncate the verbose "Started linuxio-update.service - ..." line
            if (trimmed.startsWith("Started linuxio-update.service - ")) {
              return "Started linuxio-update.service";
            }
            return line;
          })
          .filter((line): line is string => line !== null);
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
      };

      stream.onResult = (result: ResultFrame) => {
        streamRef.current = null;
        const fallbackError =
          result.status === "error"
            ? result.error || "Update failed"
            : undefined;
        handleStreamFinished(fallbackError);
      };

      stream.onClose = () => {
        streamRef.current = null;
        handleStreamFinished();
      };
    },
    [
      buildUpdateCommand,
      clearTimers,
      failUpdate,
      fetchUpdateStatus,
      handleStreamFinished,
      markUpdateStarted,
      phase,
      trackInterval,
      trackTimeout,
    ],
  );

  useEffect(() => {
    return () => {
      clearTimers();
    };
  }, [clearTimers]);

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
