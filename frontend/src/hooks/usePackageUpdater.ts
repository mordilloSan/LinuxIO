// src/hooks/usePackageUpdater.ts
import { useCallback, useEffect, useRef, useState } from "react";

import { isJobCancellationError, linuxio, type Stream } from "@/api";
import { JOB_TYPE_PACKAGE_UPDATE } from "@/constants/backgroundJobTypes";
import {
  claimTerminalFeedback,
  markTerminalFeedbackEmitted,
} from "@/hooks/backgroundJobs/terminalJobFeedback";
import { useActiveJobRecovery } from "@/hooks/backgroundJobs/useActiveJobRecovery";
import { getMutationErrorMessage } from "@/utils/mutations";

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
  // When this transaction became visible, so the 1.5 s minimum only tops up
  // updates that finish faster than that instead of padding every one of them.
  const startedAtRef = useRef<number | null>(null);
  const detachedRef = useRef(false);
  const recoveryAttachedRef = useRef(false);
  // While this page tracks a live transaction it owns packages.update terminal
  // feedback: the inline alert is the report, so the global handler suppresses
  // its failure toast. Claim and release are both synchronous — ownership
  // starts when a run starts (or is adopted) and ends the moment it settles or
  // the page unmounts, with no trailing window that could swallow a failure
  // arriving right after navigation (see terminalJobFeedback).
  const releaseFeedbackClaimRef = useRef<(() => void) | null>(null);
  const claimFeedbackOwnership = useCallback(() => {
    releaseFeedbackClaimRef.current ??= claimTerminalFeedback(
      JOB_TYPE_PACKAGE_UPDATE,
    );
  }, []);
  const releaseFeedbackOwnership = useCallback(() => {
    releaseFeedbackClaimRef.current?.();
    releaseFeedbackClaimRef.current = null;
  }, []);
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
    // This surface has reported the outcome; keep the global fallback silent
    // even if its copy of the terminal event lands after the claim is released.
    if (jobIdRef.current) markTerminalFeedbackEmitted(jobIdRef.current);
    // The transaction is over: drop the handles the cancel affordance keys off
    // so a click during the minimum-visible hold cannot cancel a finished job.
    streamRef.current = null;
    jobIdRef.current = null;
    await ensureMinimumVisible(startedAtRef.current ?? Date.now());
    if (detachedRef.current || cancelledRef.current) return;
    setUpdatingPackage(null);
    setStatus(null);
    setRecoveryPending(false);
  }, [appendEvent]);
  const finishError = useCallback((err: unknown, request: unknown) => {
    if (detachedRef.current || cancelledRef.current) return;
    // This surface is painting the outcome; keep the global fallback silent
    // even if its copy of the terminal event lands after the claim is released.
    if (jobIdRef.current) markTerminalFeedbackEmitted(jobIdRef.current);
    if (isJobCancellationError(err)) {
      // Canceled elsewhere (navbar chip, another session): report it the way a
      // local cancel does instead of blaming the backend for a failure.
      setUpdatingPackage(null);
      setStatus(null);
      setRecoveryPending(false);
      setError("Update cancelled");
      return;
    }
    const packageIds = (request as { packageIds?: string[] }).packageIds ?? [];
    const errorMsg = getMutationErrorMessage(err, "Update failed");
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
      // Invalidate through the manifest while this page owns the stream, so the
      // list refreshes even when the global job-events stream is reconnecting.
      // Ownership (markHandled) keeps the global handler from invalidating the
      // same terminal event twice; once this stream ends it takes over again.
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
        // Batch-level progress can accompany any event type. In particular,
        // the backend reports the completed share of a failed package on the
        // continuation message before moving to the next package.
        bumpProgress(data.percentage);
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
            break;
          case "percentage":
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
          startedAtRef.current = null;
          // finishSuccess/finishError have painted by now (React Query settles
          // after the success/error callbacks), so ownership can go back to
          // the global handler.
          releaseFeedbackOwnership();
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
      // The page can no longer paint feedback — hand ownership back to the
      // global handler immediately so a failure after navigation still toasts.
      releaseFeedbackOwnership();
    };
  }, [releaseFeedbackOwnership]);

  useActiveJobRecovery({
    type: JOB_TYPE_PACKAGE_UPDATE,
    scanKey: "package-update-controller",
    match: () => true,
    onRecover: (job) => {
      if (recoveryAttachedRef.current) return;
      recoveryAttachedRef.current = true;
      // Claim before attaching: if the job goes terminal in the attach window,
      // the short-circuit routes it to finishError/finishSuccess on this page.
      claimFeedbackOwnership();
      startedAtRef.current = Date.now();
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
      startedAtRef.current = Date.now();
      // Claim before submitting so even a failure that beats the submit
      // round trip to the global events stream is owned by this page.
      claimFeedbackOwnership();

      await startUpdateJob({ packageIds: packages }).catch(() => undefined);
    },
    [appendEvent, claimFeedbackOwnership, startUpdateJob],
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
