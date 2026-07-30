import { useCallback, useReducer, useRef } from "react";

import { isJobCancellationError, linuxio } from "@/api";
import { JOB_TYPE_PACKAGE_UPDATE } from "@/constants/backgroundJobTypes";
import { useActiveJobRecovery } from "@/hooks/backgroundJobs/useActiveJobRecovery";
import { useManagedJobStreamLifecycle } from "@/hooks/backgroundJobs/useManagedJobStreamLifecycle";
import { useTerminalFeedbackOwnership } from "@/hooks/backgroundJobs/useTerminalFeedbackOwnership";
import { getMutationErrorMessage } from "@/utils/mutations";

import {
  initialPackageUpdateState,
  packageUpdateReducer,
} from "./packageUpdateState";
import { useMountedGuard } from "./useMountedGuard";

const MIN_PROGRESS_VISIBLE_MS = 1500;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

async function ensureMinimumVisible(startedAtMs: number): Promise<void> {
  const remaining = MIN_PROGRESS_VISIBLE_MS - (Date.now() - startedAtMs);
  if (remaining > 0) await sleep(remaining);
}

interface PkgUpdateProgress {
  item_pct?: number;
  message?: string;
  package_id?: string;
  percentage?: number;
  status?: string;
  type: "item_progress" | "package" | "status" | "percentage" | "message";
}

function extractPackageName(packageId: string): string {
  return packageId.split(";")[0] || packageId;
}

export const usePackageUpdater = () => {
  const [state, dispatch] = useReducer(
    packageUpdateReducer,
    initialPackageUpdateState,
  );
  const startedAtRef = useRef<number | null>(null);
  const isMounted = useMountedGuard();
  const feedback = useTerminalFeedbackOwnership(JOB_TYPE_PACKAGE_UPDATE);

  // Cancels are deliberately fire-and-forget; the stream lifecycle owns local
  // detach/abort while this route-specific action stops the backend job.
  const { mutate: cancelJob } = linuxio.jobs.cancel.useJobAction();
  const lifecycle = useManagedJobStreamLifecycle((job) => {
    feedback.release();
    startedAtRef.current = null;
    cancelJob({ jobId: job.id });
  });

  const finishSuccess = useCallback(
    async (_result: void, request: { packageIds: string[] }) => {
      if (!lifecycle.accepts(request)) return;
      const startedAt = startedAtRef.current ?? Date.now();
      feedback.mark();
      feedback.release();
      dispatch({ type: "finishing" });
      await ensureMinimumVisible(startedAt);
      if (isMounted() && lifecycle.settle(request)) {
        dispatch({ type: "complete" });
        startedAtRef.current = null;
      }
    },
    [feedback, isMounted, lifecycle],
  );

  const finishError = useCallback(
    (err: unknown, request: unknown) => {
      if (!lifecycle.accepts(request as { packageIds: string[] })) return;
      feedback.mark();
      if (isJobCancellationError(err)) {
        dispatch({ type: "canceled" });
        lifecycle.settle(request as { packageIds: string[] });
        startedAtRef.current = null;
        feedback.release();
        return;
      }
      const packageIds =
        (request as { packageIds?: string[] }).packageIds ?? [];
      const message = getMutationErrorMessage(err, "Update failed");
      dispatch({
        type: "failed",
        error:
          packageIds.length === 1
            ? `Failed to update ${extractPackageName(packageIds[0])}: ${message}`
            : message,
      });
      lifecycle.settle(request as { packageIds: string[] });
      startedAtRef.current = null;
      feedback.release();
    },
    [feedback, lifecycle],
  );

  const streamAction = linuxio.packages.update.useJobStreamAction<
    void,
    PkgUpdateProgress
  >({
    // Keep manifest invalidation and local-handled ownership as the previous
    // controller did; recovery/global feedback remain its fallback.
    closeOnAbort: "none",
    closeMessage: "Update stream closed unexpectedly",
    onJobStart: (job, request) => {
      if (lifecycle.onJobStart(job, request)) feedback.claim(job.id);
    },
    onOpen: (stream, job, request) => {
      lifecycle.onOpen(stream, job, request);
    },
    onProgress: (data, _job, request) => {
      if (!lifecycle.accepts(request)) return;
      dispatch({ type: "progress", percentage: data.percentage });
      switch (data.type) {
        case "item_progress":
          if (data.package_id) {
            dispatch({
              type: "package",
              packageName: extractPackageName(data.package_id),
              status: data.status,
            });
          } else if (data.status) {
            dispatch({ type: "status", status: data.status });
          }
          break;
        case "package":
          if (data.package_id) {
            const packageName = extractPackageName(data.package_id);
            dispatch({
              type: "package",
              packageName,
              status: data.status,
              event: data.status ? `${data.status}: ${packageName}` : undefined,
            });
          } else if (data.status) {
            dispatch({ type: "status", status: data.status });
          }
          break;
        case "status":
          if (data.status) {
            dispatch({
              type: "status",
              status: data.status,
              event: data.status,
            });
          }
          break;
        case "message": {
          const message = data.message || data.status;
          if (message)
            dispatch({ type: "status", status: message, event: message });
          break;
        }
        case "percentage":
          break;
      }
    },
    success: finishSuccess,
    error: finishError,
  });

  const recovery = useActiveJobRecovery({
    type: JOB_TYPE_PACKAGE_UPDATE,
    scanKey: "package-update-controller",
    match: () => true,
    onRecover: (job) => {
      const packageIds =
        (job.request as { packageIds?: string[] } | undefined)?.packageIds ??
        [];
      const request = { packageIds };
      if (!lifecycle.begin(request)) return;
      dispatch({
        type: "start",
        packageName:
          packageIds.length === 1
            ? extractPackageName(packageIds[0])
            : "Resuming updates...",
        status: "Resuming update transaction",
        event: "Resuming update transaction",
      });
      // Claim before attach so a terminal event in its opening window remains
      // page-owned. onJobStart records the concrete id for deduplication.
      feedback.claim();
      startedAtRef.current = Date.now();
      streamAction.attach(job, request);
    },
  });

  const runUpdate = useCallback(
    async (packages: string[], initialLabel: string) => {
      if (packages.length === 0) return;
      const request = { packageIds: packages };
      if (!lifecycle.begin(request)) return;
      dispatch({
        type: "start",
        packageName: initialLabel,
        status: "Initializing",
        event: "Initializing update transaction",
      });
      startedAtRef.current = Date.now();
      // Claim before submission: the job-events fallback may otherwise win a
      // very fast failed job before its attach stream is open.
      feedback.claim();
      await streamAction.mutateAsync(request).catch(() => undefined);
    },
    [feedback, lifecycle, streamAction],
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
    if (state.phase === "running" && lifecycle.cancel()) {
      dispatch({ type: "canceled" });
    }
  }, [lifecycle, state.phase]);
  const clearError = useCallback(() => dispatch({ type: "clearError" }), []);

  const isUpdating = state.phase !== "idle";
  const canCancel = state.phase === "running" && lifecycle.isActive;

  return {
    ...state,
    canCancel,
    cancelUpdate,
    clearError,
    error: state.error,
    isUpdating,
    recoveryPending: recovery.isScanning,
    updateAll,
    updateOne,
  };
};
