import { useMemo, type ReactNode } from "react";

import {
  BackgroundTasksIndexerContext,
  BackgroundTasksIsIndexingContext,
  type BackgroundTasksIndexerContextValue,
} from "@/contexts/IndexerContext";
import {
  BackgroundTasksActionsContext,
  type BackgroundTasksActionsContextValue,
} from "@/contexts/TasksActionsContext";
import {
  BackgroundTasksStateContext,
  type BackgroundTasksStateContextValue,
} from "@/contexts/TasksStateContext";
import { useBackgroundTaskRuntime } from "@/hooks/backgroundTasks/useBackgroundTaskRuntime";
import { useDownloadTasks } from "@/hooks/backgroundTasks/useDownloadTasks";
import { useGenericBackgroundTasks } from "@/hooks/backgroundTasks/useGenericBackgroundTasks";
import { useIndexerTasks } from "@/hooks/backgroundTasks/useIndexerTasks";
import { useRecoveredTasks } from "@/hooks/backgroundTasks/useRecoveredTasks";
import { useTransferTasks } from "@/hooks/backgroundTasks/useTransferTasks";
import { useUploadTasks } from "@/hooks/backgroundTasks/useUploadTasks";
import { useUploadChunkSizeGetter } from "@/hooks/useUploadChunkSize";
import type { BackgroundTaskItem } from "@/types/backgroundTasks";

// Module scope on purpose: declared inside the transfers useMemo, this
// rest-params closure trips an invariant in the React Compiler (oxc port)
// and the whole file is left unmemoized.
const addIds = (ids: Set<string>, ...values: (string | undefined)[]) => {
  for (const v of values) if (v) ids.add(v);
};

export const BackgroundTasksProvider = ({
  children,
}: {
  children: ReactNode;
}) => {
  const getUploadChunkSize = useUploadChunkSizeGetter();

  const runtime = useBackgroundTaskRuntime();
  const { downloads, startDownload, cancelDownload } =
    useDownloadTasks(runtime);
  const { uploads, startUpload, cancelUpload } = useUploadTasks(
    runtime,
    getUploadChunkSize,
  );
  const {
    compressions,
    extractions,
    copies,
    moves,
    startCompression,
    startExtraction,
    startCopy,
    startMove,
    cancelTransfer,
    recoverTransfer,
  } = useTransferTasks(runtime);
  const {
    indexers,
    startIndexer,
    isIndexing,
    isIndexerDialogOpen,
    openIndexerDialog,
    closeIndexerDialog,
    lastIndexerResult,
    lastIndexerError,
    recoveryControls: indexerRecoveryControls,
  } = useIndexerTasks(runtime);
  const {
    backgroundTasks,
    cancelTask,
    recoveryControls: genericTaskRecoveryControls,
  } = useGenericBackgroundTasks(runtime);

  useRecoveredTasks(runtime, {
    recoverTransfer,
    indexers: indexerRecoveryControls,
    genericTasks: genericTaskRecoveryControls,
  });

  const transfers = useMemo<BackgroundTaskItem[]>(() => {
    const localTransferIds = new Set<string>();
    const localItems: { id: string; taskId?: string }[] = [
      ...downloads,
      ...uploads,
      ...compressions,
      ...extractions,
      ...indexers,
      ...copies,
      ...moves,
    ];
    for (const item of localItems) {
      addIds(localTransferIds, item.id, item.taskId);
    }
    return [
      ...downloads,
      ...uploads,
      ...compressions,
      ...extractions,
      ...indexers,
      ...copies,
      ...moves,
      ...backgroundTasks.filter((task) => !localTransferIds.has(task.id)),
    ];
  }, [
    downloads,
    uploads,
    compressions,
    extractions,
    indexers,
    copies,
    moves,
    backgroundTasks,
  ]);

  const actionsValue = useMemo<BackgroundTasksActionsContextValue>(
    () => ({
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
    }),
    [
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
      cancelTransfer,
      cancelTask,
    ],
  );

  const indexerValue = useMemo<BackgroundTasksIndexerContextValue>(
    () => ({
      indexers,
      isIndexerDialogOpen,
      lastIndexerResult,
      lastIndexerError,
    }),
    [indexers, isIndexerDialogOpen, lastIndexerResult, lastIndexerError],
  );

  const stateValue = useMemo<BackgroundTasksStateContextValue>(
    () => ({
      downloads,
      uploads,
      compressions,
      extractions,
      indexers,
      copies,
      moves,
      backgroundTasks,
      transfers,
      isIndexerDialogOpen,
      lastIndexerResult,
      lastIndexerError,
    }),
    [
      downloads,
      uploads,
      compressions,
      extractions,
      indexers,
      copies,
      moves,
      backgroundTasks,
      transfers,
      isIndexerDialogOpen,
      lastIndexerResult,
      lastIndexerError,
    ],
  );

  return (
    <BackgroundTasksActionsContext.Provider value={actionsValue}>
      <BackgroundTasksIsIndexingContext.Provider value={isIndexing}>
        <BackgroundTasksIndexerContext.Provider value={indexerValue}>
          <BackgroundTasksStateContext.Provider value={stateValue}>
            {children}
          </BackgroundTasksStateContext.Provider>
        </BackgroundTasksIndexerContext.Provider>
      </BackgroundTasksIsIndexingContext.Provider>
    </BackgroundTasksActionsContext.Provider>
  );
};
