import { useCallback, useReducer } from "react";

import { isTaskCancellationError } from "@/api";
import { getMutationErrorMessage } from "@/utils/mutations";

import {
  initialPackageUpdateState,
  packageUpdateReducer,
} from "./packageUpdateState";
import {
  type PackageUpdateProgress,
  type PackageUpdateRequest,
  usePackageUpdateTransaction,
} from "./usePackageUpdateTransaction";

function extractPackageName(packageId: string): string {
  return packageId.split(";")[0] || packageId;
}

export const usePackageUpdater = () => {
  const [state, dispatch] = useReducer(
    packageUpdateReducer,
    initialPackageUpdateState,
  );

  const finishSuccess = useCallback(
    (_request: PackageUpdateRequest) => dispatch({ type: "complete" }),
    [],
  );

  const finishError = useCallback(
    (error: unknown, request: PackageUpdateRequest) => {
      if (isTaskCancellationError(error)) {
        dispatch({ type: "canceled" });
        return;
      }
      const message = getMutationErrorMessage(error, "Update failed");
      dispatch({
        type: "failed",
        error:
          request.packageIds.length === 1
            ? `Failed to update ${extractPackageName(request.packageIds[0])}: ${message}`
            : message,
      });
    },
    [],
  );

  const handleProgress = useCallback((data: PackageUpdateProgress) => {
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
        if (message) {
          dispatch({ type: "status", status: message, event: message });
        }
        break;
      }
      case "percentage":
        break;
    }
  }, []);

  const handleRecover = useCallback((request: PackageUpdateRequest) => {
    dispatch({
      type: "start",
      packageName:
        request.packageIds.length === 1
          ? extractPackageName(request.packageIds[0])
          : "Resuming updates...",
      status: "Resuming update transaction",
      event: "Resuming update transaction",
    });
  }, []);

  const transaction = usePackageUpdateTransaction({
    onError: finishError,
    onProgress: handleProgress,
    onRecover: handleRecover,
    onSuccess: finishSuccess,
  });

  const runUpdate = useCallback(
    async (packages: string[], initialLabel: string) => {
      if (packages.length === 0) return;
      const pending = transaction.start({ packageIds: packages });
      if (!pending) return;
      dispatch({
        type: "start",
        packageName: initialLabel,
        status: "Initializing",
        event: "Initializing update transaction",
      });
      await pending;
    },
    [transaction],
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
    if (state.phase === "running") transaction.cancel();
  }, [state.phase, transaction]);
  const clearError = useCallback(() => dispatch({ type: "clearError" }), []);

  const isUpdating = state.phase === "running";

  return {
    ...state,
    canCancel: isUpdating && transaction.canCancel,
    cancelUpdate,
    clearError,
    isUpdating,
    recoveryPending: transaction.isScanning,
    updateAll,
    updateOne,
  };
};
