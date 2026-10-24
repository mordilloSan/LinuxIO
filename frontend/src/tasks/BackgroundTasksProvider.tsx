import { useState, type ReactNode } from "react";

import { useUploadChunkSizeGetter } from "@/hooks/useUploadChunkSize";

import { BackgroundTasksActionsContext } from "./BackgroundTasksActionsContext";
import { useBackgroundTaskRuntime } from "./engine/useBackgroundTaskRuntime";
import { useDownloadTasks } from "./engine/useDownloadTasks";
import { useGenericBackgroundTasks } from "./engine/useGenericBackgroundTasks";
import { useIndexerTasks } from "./engine/useIndexerTasks";
import { useRecoveredTasks } from "./engine/useRecoveredTasks";
import { useTransferTasks } from "./engine/useTransferTasks";
import { useUploadTasks } from "./engine/useUploadTasks";
import type { BackgroundTaskActions } from "./types";

export const BackgroundTasksProvider = ({
  children,
}: {
  children: ReactNode;
}) => {
  const getUploadChunkSize = useUploadChunkSizeGetter();

  const runtime = useBackgroundTaskRuntime();
  const { startDownload, cancelDownload } = useDownloadTasks(runtime);
  const { startUpload, cancelUpload } = useUploadTasks(
    runtime,
    getUploadChunkSize,
  );
  const {
    startCompression,
    startExtraction,
    startCopy,
    startMove,
    cancelTransfer,
    recoverTransfer,
  } = useTransferTasks(runtime);
  const {
    startIndexer,
    openIndexerDialog,
    closeIndexerDialog,
    recoveryControls: indexerRecoveryControls,
  } = useIndexerTasks(runtime);
  const { cancelTask, recoveryControls: genericTaskRecoveryControls } =
    useGenericBackgroundTasks(runtime);

  useRecoveredTasks(runtime, {
    recoverTransfer,
    indexers: indexerRecoveryControls,
    genericTasks: genericTaskRecoveryControls,
  });

  const [actionsValue] = useState<BackgroundTaskActions>(() => ({
    startDownload,
    startCompression,
    startExtraction,
    startIndexer,
    openIndexerDialog,
    closeIndexerDialog,
    startCopy,
    startMove,
    startUpload,
    cancelDownload,
    cancelUpload,
    cancelCompression: cancelTransfer,
    cancelExtraction: cancelTransfer,
    cancelCopy: cancelTransfer,
    cancelMove: cancelTransfer,
    cancelTask,
  }));

  return (
    <BackgroundTasksActionsContext.Provider value={actionsValue}>
      {children}
    </BackgroundTasksActionsContext.Provider>
  );
};
