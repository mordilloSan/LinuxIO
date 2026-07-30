import { useCallback, useEffect, useRef, useState } from "react";

import {
  isTerminalJobState,
  linuxio,
  type JobSnapshot,
  type Stream,
} from "@/api";
import { JOB_TYPE_PACKAGE_UPDATE } from "@/constants/backgroundJobTypes";
import {
  claimTerminalFeedback,
  markTerminalFeedbackEmitted,
} from "@/hooks/backgroundJobs/terminalJobFeedback";
import { useActiveJobRecovery } from "@/hooks/backgroundJobs/useActiveJobRecovery";

export interface PackageUpdateRequest {
  packageIds: string[];
}

export interface PackageUpdateProgress {
  item_pct?: number;
  message?: string;
  package_id?: string;
  percentage?: number;
  status?: string;
  type: "item_progress" | "package" | "status" | "percentage" | "message";
}

interface ActiveTransaction {
  cancelRequested: boolean;
  job: JobSnapshot | null;
  releaseFeedback: () => void;
  request: PackageUpdateRequest;
  stream: Stream | null;
}

interface PackageUpdateTransactionOptions {
  onError: (error: unknown, request: PackageUpdateRequest) => void;
  onProgress: (
    progress: PackageUpdateProgress,
    request: PackageUpdateRequest,
  ) => void;
  onRecover: (request: PackageUpdateRequest) => void;
  onSuccess: (request: PackageUpdateRequest) => void;
}

/**
 * Owns the complete package-update transaction lifecycle. The consuming UI
 * only starts or attaches a transaction and reacts to domain events.
 */
export function usePackageUpdateTransaction({
  onError,
  onProgress,
  onRecover,
  onSuccess,
}: PackageUpdateTransactionOptions) {
  const transactionRef = useRef<ActiveTransaction | null>(null);
  const [canCancel, setCanCancel] = useState(false);

  const accepts = useCallback((request: PackageUpdateRequest) => {
    return transactionRef.current?.request === request;
  }, []);

  const begin = useCallback((request: PackageUpdateRequest) => {
    if (transactionRef.current) return false;
    transactionRef.current = {
      cancelRequested: false,
      job: null,
      releaseFeedback: claimTerminalFeedback(JOB_TYPE_PACKAGE_UPDATE),
      request,
      stream: null,
    };
    setCanCancel(false);
    return true;
  }, []);

  const settle = useCallback(
    (request: PackageUpdateRequest) => {
      if (!accepts(request)) return false;
      const transaction = transactionRef.current;
      if (!transaction) return false;
      if (transaction.job) {
        markTerminalFeedbackEmitted(transaction.job.id);
      }
      transactionRef.current = null;
      setCanCancel(false);
      transaction.releaseFeedback();
      return true;
    },
    [accepts],
  );

  const streamAction = linuxio.packages.update.useJobStreamAction<
    void,
    PackageUpdateProgress
  >({
    closeMessage: "Update stream closed unexpectedly",
    onJobStart: (job, request) => {
      if (!accepts(request)) return;
      const transaction = transactionRef.current;
      if (!transaction) return;
      transaction.job = job;
      setCanCancel(
        !transaction.cancelRequested && !isTerminalJobState(job.state),
      );
    },
    onOpen: (stream, job, request) => {
      const transaction = transactionRef.current;
      if (!accepts(request) || !transaction || transaction.job?.id !== job.id) {
        stream.close();
        return;
      }
      transaction.stream = stream;
    },
    onProgress: (progress, _job, request) => {
      if (accepts(request)) onProgress(progress, request);
    },
    success: (_result, request) => {
      if (settle(request)) onSuccess(request);
    },
    error: (error, request) => {
      if (settle(request)) onError(error, request);
    },
  });

  const start = useCallback(
    (request: PackageUpdateRequest): Promise<void> | null => {
      if (!begin(request)) return null;
      // Claim before submission so a very fast terminal job cannot be reported
      // by the global events path before this page receives its snapshot.
      return streamAction.mutateAsync(request).then(
        () => undefined,
        () => undefined,
      );
    },
    [begin, streamAction],
  );

  const attach = useCallback(
    (job: JobSnapshot): boolean => {
      const packageIds = job.metadata?.packageIds ?? [];
      const request = { packageIds };
      if (!begin(request)) return false;
      onRecover(request);
      streamAction.attach(job, request);
      return true;
    },
    [begin, onRecover, streamAction],
  );

  const { mutateAsync: requestCancel } = linuxio.jobs.cancel.useAction();
  const cancel = useCallback(() => {
    const transaction = transactionRef.current;
    const job = transaction?.job;
    if (
      !transaction ||
      !job ||
      transaction.cancelRequested ||
      isTerminalJobState(job.state)
    ) {
      return false;
    }

    // Keep watching the update stream. The backend's canceled terminal frame,
    // not this click, is the authority that settles the UI.
    transaction.cancelRequested = true;
    setCanCancel(false);
    void requestCancel({ jobId: job.id }).catch(() => {
      if (
        accepts(transaction.request) &&
        transactionRef.current?.job?.id === job.id
      ) {
        transaction.cancelRequested = false;
        setCanCancel(true);
      }
    });
    return true;
  }, [accepts, requestCancel]);

  const recovery = useActiveJobRecovery({
    type: JOB_TYPE_PACKAGE_UPDATE,
    scanKey: "package-update-controller",
    match: () => true,
    onRecover: attach,
  });

  useEffect(() => {
    return () => {
      const transaction = transactionRef.current;
      transaction?.stream?.close();
      transaction?.releaseFeedback();
      transactionRef.current = null;
    };
  }, []);

  return {
    attach,
    cancel,
    canCancel,
    isScanning: recovery.isScanning,
    start,
  };
}
