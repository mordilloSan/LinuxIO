import { useCallback, useEffect, useRef, useState } from "react";

import {
  isTerminalTaskState,
  linuxio,
  type PackageUpdateRequest,
  type PackageUpdateResult,
  type TaskSnapshot,
  type Stream,
  useCallMutation,
} from "@/api";
import { TASK_TYPE_PACKAGE_UPDATE } from "@/constants/backgroundTaskTypes";
import {
  claimTerminalFeedback,
  markTerminalFeedbackEmitted,
} from "@/hooks/backgroundTasks/terminalTaskFeedback";
import { useActiveTaskRecovery } from "@/hooks/backgroundTasks/useActiveTaskRecovery";

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
  task: TaskSnapshot | null;
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
      task: null,
      releaseFeedback: claimTerminalFeedback(TASK_TYPE_PACKAGE_UPDATE),
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
      if (transaction.task) {
        markTerminalFeedbackEmitted(transaction.task.id);
      }
      transactionRef.current = null;
      setCanCancel(false);
      transaction.releaseFeedback();
      return true;
    },
    [accepts],
  );

  const streamAction = linuxio.packages.update.useTaskStreamAction<
    PackageUpdateResult,
    PackageUpdateProgress
  >({
    closeMessage: "Update stream closed unexpectedly",
    onTaskStart: (task, request) => {
      if (!accepts(request)) return;
      const transaction = transactionRef.current;
      if (!transaction) return;
      transaction.task = task;
      setCanCancel(
        !transaction.cancelRequested && !isTerminalTaskState(task.state),
      );
    },
    onOpen: (stream, task, request) => {
      const transaction = transactionRef.current;
      if (
        !accepts(request) ||
        !transaction ||
        transaction.task?.id !== task.id
      ) {
        stream.close();
        return;
      }
      transaction.stream = stream;
    },
    onProgress: (progress, _task, request) => {
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
      // Claim before submission so a very fast terminal task cannot be reported
      // by the global events path before this page receives its snapshot.
      return streamAction.mutateAsync(request).then(
        () => undefined,
        () => undefined,
      );
    },
    [begin, streamAction],
  );

  const watch = useCallback(
    (task: TaskSnapshot): boolean => {
      const packageIds = task.metadata?.packageIds ?? [];
      const request = { packageIds };
      if (!begin(request)) return false;
      onRecover(request);
      streamAction.watch(task, request);
      return true;
    },
    [begin, onRecover, streamAction],
  );

  const { mutateAsync: requestCancel } = useCallMutation(linuxio.tasks.cancel);
  const cancel = useCallback(() => {
    const transaction = transactionRef.current;
    const task = transaction?.task;
    if (
      !transaction ||
      !task ||
      transaction.cancelRequested ||
      isTerminalTaskState(task.state)
    ) {
      return false;
    }

    // Keep watching the update stream. The backend's canceled terminal frame,
    // not this click, is the authority that settles the UI.
    transaction.cancelRequested = true;
    setCanCancel(false);
    void requestCancel({ taskId: task.id }).catch(() => {
      if (
        accepts(transaction.request) &&
        transactionRef.current?.task?.id === task.id
      ) {
        transaction.cancelRequested = false;
        setCanCancel(true);
      }
    });
    return true;
  }, [accepts, requestCancel]);

  const recovery = useActiveTaskRecovery({
    type: TASK_TYPE_PACKAGE_UPDATE,
    scanKey: "package-update-controller",
    match: () => true,
    onRecover: watch,
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
    watch,
    cancel,
    canCancel,
    isScanning: recovery.isScanning,
    start,
  };
}
