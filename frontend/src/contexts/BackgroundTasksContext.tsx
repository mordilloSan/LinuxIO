import { useState, type ReactNode } from "react";

import {
  BackgroundTasksActionsContext,
  type BackgroundTasksActionsContextValue,
} from "@/contexts/TasksActionsContext";
import { useBackgroundTaskRuntime } from "@/hooks/backgroundTasks/useBackgroundTaskRuntime";
import { useDownloadTasks } from "@/hooks/backgroundTasks/useDownloadTasks";
import { useGenericBackgroundTasks } from "@/hooks/backgroundTasks/useGenericBackgroundTasks";
import { useIndexerTasks } from "@/hooks/backgroundTasks/useIndexerTasks";
import { useRecoveredTasks } from "@/hooks/backgroundTasks/useRecoveredTasks";
import { useTransferTasks } from "@/hooks/backgroundTasks/useTransferTasks";
import { useUploadTasks } from "@/hooks/backgroundTasks/useUploadTasks";
import { useUploadChunkSizeGetter } from "@/hooks/useUploadChunkSize";
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

  const [actionsValue] = useState<BackgroundTasksActionsContextValue>(() => ({
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
