// src/hooks/usePackageUpdater.ts
import { useCallback, useEffect, useRef, useState } from "react";

import { linuxio, type Stream } from "@/api";
import { JOB_TYPE_PACKAGE_UPDATE } from "@/constants/backgroundJobTypes";
import { useActiveJobRecovery } from "@/hooks/backgroundJobs/useActiveJobRecovery";

const MIN_PROGRESS_VISIBLE_MS = 1500;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    window.setTimeout(resolve, ms);
  });
}

async function ensureMinimumVisible(startedAtMs: number): Promise<void> {
  const elapsed = Date.now() - startedAtMs;
  const remaining = MIN_PROGRESS_VISIBLE_MS - elapsed;
  if (remaining > 0) {
    await sleep(remaining);
  }
}

// Progress event types from backend
interface PkgUpdateProgress {
  info_code?: number;
  item_pct?: number;
  message?: string;
  package_id?: string;
  package_summary?: string;
  percentage?: number;
  status?: string;
  status_code?: number;
  type: "item_progress" | "package" | "status" | "percentage" | "message";
}

// Extract package name from package ID (e.g., "nginx;1.24.0-1ubuntu1;amd64;ubuntu" -> "nginx")
function extractPackageName(packageId: string): string {
  const parts = packageId.split(";");
  return parts[0] || packageId;
}

export const usePackageUpdater = () => {
  const [updatingPackage, setUpdatingPackage] = useState<string | null>(null);
  const [progress, setProgress] = useState(0);
  const [status, setStatus] = useState<string | null>(null);
  const [eventLog, setEventLog] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const streamRef = useRef<Stream | null>(null);
  const jobIdRef = useRef<string | null>(null);
  const cancelledRef = useRef(false);
  const detachedRef = useRef(false);
  const recoveryAttachedRef = useRef(false);
  // Cancels are fire-and-forget; a plain job action reports nothing.
  const { mutate: cancelJob } = linuxio.jobs.cancel.useJobAction();

  const appendEvent = useCallback((message: string) => {
    const trimmed = message.trim();
    if (!trimmed) {
      return;
    }

    setEventLog((previous) => {
      if (previous[previous.length - 1] === trimmed) {
        return previous;
      }
      const next = [...previous, trimmed];
      return next.slice(-8);
    });
  }, []);

  // Drive the overall bar from the global transaction percentage only,
  // clamped to [0,100] and kept monotonic so interleaved frames can't pull
  // it backwards. Per-package item_pct is intentionally NOT used here — it
  // resets every package and every download/install phase.
  const bumpProgress = useCallback((pct?: number) => {
    if (pct === undefined || pct > 100) return;
    setProgress((prev) => Math.max(prev, pct));
  }, []);

  const [recoveryPending, setRecoveryPending] = useState(true);

  const finishSuccess = useCallback(async () => {
    if (cancelledRef.current) return;
    if (detachedRef.current) {
      return;
    }
    setProgress(100);
    setStatus("Finished");
    appendEvent("Finished");
    await ensureMinimumVisible(Date.now());
    if (detachedRef.current) return;
    setUpdatingPackage(null);
    setStatus(null);
    setRecoveryPending(false);
  }, [appendEvent]);
  const finishError = useCallback((err: unknown, request: unknown) => {
    if (detachedRef.current || cancelledRef.current) return;
    const packageIds = (request as { packageIds?: string[] }).packageIds ?? [];
    const errorMsg = err instanceof Error ? err.message : "Update failed";
    setError(
      packageIds.length === 1
        ? `Failed to update ${extractPackageName(packageIds[0])}: ${errorMsg}`
        : errorMsg,
    );
    setUpdatingPackage(null);
    setStatus(null);
    setRecoveryPending(false);
  }, []);

  const { mutateAsync: startUpdateJob, attach: attachUpdateJob } =
    linuxio.packages.update.useJobStreamAction<void, PkgUpdateProgress>({
      // The global jobs owner receives terminal events even after this page's
      // stream closes, and owns cache invalidation through the manifest.
      invalidates: [],
      markHandled: false,
      closeOnAbort: "none",
      closeMessage: "Update stream closed unexpectedly",
      onJobStart: (job) => {
        jobIdRef.current = job.id;
      },
      onOpen: (stream) => {
        if (detachedRef.current) {
          stream.close();
          return;
        }
        streamRef.current = stream;
        setRecoveryPending(false);
      },
      onProgress: (data) => {
        if (detachedRef.current) return;
        switch (data.type) {
          case "item_progress":
            // item_pct is a per-package / per-phase sub-percentage, not a
            // global value — use it only to track the current package and
            // status, never to set the overall bar.
            if (data.package_id) {
              setUpdatingPackage(extractPackageName(data.package_id));
            }
            if (data.status) {
              setStatus(data.status);
            }
            break;
          case "package":
            if (data.package_id) {
              const packageName = extractPackageName(data.package_id);
              setUpdatingPackage(packageName);
              if (data.status) {
                appendEvent(`${data.status}: ${packageName}`);
              }
            }
            if (data.status) {
              setStatus(data.status);
            }
            break;
          case "status":
            if (data.status) {
              setStatus(data.status);
              appendEvent(data.status);
            }
            bumpProgress(data.percentage);
            break;
          case "percentage":
            bumpProgress(data.percentage);
            break;
          case "message":
            if (data.message) {
              setStatus(data.message);
              appendEvent(data.message);
            } else if (data.status) {
              setStatus(data.status);
              appendEvent(data.status);
            }
            break;
        }
      },
      success: () => finishSuccess(),
      error: (err, request) => finishError(err, request),
      options: {
        onSettled: () => {
          streamRef.current = null;
          jobIdRef.current = null;
          cancelledRef.current = false;
        },
      },
    });

  useEffect(() => {
    detachedRef.current = false;
    return () => {
      detachedRef.current = true;
      // Closing only detaches this page's stream; the backend job continues
      // and is safely adopted by a later controller instance.
      streamRef.current?.close();
      streamRef.current = null;
    };
  }, []);

  useActiveJobRecovery({
    type: JOB_TYPE_PACKAGE_UPDATE,
    scanKey: "package-update-controller",
    match: () => true,
    onRecover: (job) => {
      if (recoveryAttachedRef.current) return;
      recoveryAttachedRef.current = true;
      const request = job.request as { packageIds?: string[] } | undefined;
      const packageIds = request?.packageIds ?? [];
      setUpdatingPackage(
        packageIds.length === 1
          ? extractPackageName(packageIds[0])
          : "Resuming updates...",
      );
      setStatus("Resuming update transaction");
      setEventLog(["Resuming update transaction"]);
      attachUpdateJob(job, { packageIds });
    },
    onMiss: () => setRecoveryPending(false),
  });

  const runUpdate = useCallback(
    async (packages: string[], initialLabel: string) => {
      if (packages.length === 0) {
        console.log("No packages to update");
        return;
      }

      setProgress(0);
      setEventLog([]);
      setError(null);
      setStatus("Initializing");
      setUpdatingPackage(initialLabel);
      appendEvent("Initializing update transaction");
      cancelledRef.current = false;

      await startUpdateJob({ packageIds: packages }).catch(() => undefined);
    },
    [appendEvent, startUpdateJob],
  );

  const updateOne = useCallback(
    (pkg: string) => runUpdate([pkg], extractPackageName(pkg)),
    [runUpdate],
  );

  const updateAll = useCallback(
    (packages: string[]) => runUpdate(packages, "Preparing updates..."),
    [runUpdate],
  );

  const cancelUpdate = useCallback(() => {
    if (streamRef.current || jobIdRef.current) {
      cancelledRef.current = true;
      streamRef.current?.abort();
      streamRef.current = null;
      if (jobIdRef.current) {
        cancelJob({ jobId: jobIdRef.current });
        jobIdRef.current = null;
      }
      setUpdatingPackage(null);
      setStatus(null);
      setError("Update cancelled");
    }
  }, [cancelJob]);

  const clearError = useCallback(() => setError(null), []);

  return {
    updatingPackage,
    updateOne,
    updateAll,
    cancelUpdate,
    progress,
    status,
    eventLog,
    error,
    clearError,
    recoveryPending,
  };
};
