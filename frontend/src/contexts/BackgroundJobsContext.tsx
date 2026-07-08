import React, { useMemo } from "react";

import {
  BackgroundJobsIndexerContext,
  type BackgroundJobsIndexerContextValue,
} from "@/contexts/IndexerContext";
import {
  BackgroundJobsActionsContext,
  type BackgroundJobsActionsContextValue,
} from "@/contexts/JobsActionsContext";
import {
  BackgroundJobsStateContext,
  type BackgroundJobsStateContextValue,
} from "@/contexts/JobsStateContext";
import { useBackgroundJobRuntime } from "@/hooks/backgroundJobs/useBackgroundJobRuntime";
import { useDownloadJobs } from "@/hooks/backgroundJobs/useDownloadJobs";
import { useGenericBackgroundJobs } from "@/hooks/backgroundJobs/useGenericBackgroundJobs";
import { useIndexerJobs } from "@/hooks/backgroundJobs/useIndexerJobs";
import { useRecoveredJobs } from "@/hooks/backgroundJobs/useRecoveredJobs";
import { useTransferJobs } from "@/hooks/backgroundJobs/useTransferJobs";
import { useUploadJobs } from "@/hooks/backgroundJobs/useUploadJobs";
import { useUploadChunkSizeGetter } from "@/hooks/useUploadChunkSize";
import type { BackgroundJobItem } from "@/types/backgroundJobs";

export const BackgroundJobsProvider: React.FC<{
  children: React.ReactNode;
}> = ({ children }) => {
  const getUploadChunkSize = useUploadChunkSizeGetter();

  const runtime = useBackgroundJobRuntime();
  const { downloads, startDownload, cancelDownload } = useDownloadJobs(runtime);
  const { uploads, startUpload, cancelUpload } = useUploadJobs(
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
  } = useTransferJobs(runtime);
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
  } = useIndexerJobs(runtime);
  const {
    backgroundJobs,
    cancelJob,
    recoveryControls: genericJobRecoveryControls,
  } = useGenericBackgroundJobs(runtime);

  useRecoveredJobs(runtime, {
    recoverTransfer,
    indexers: indexerRecoveryControls,
    genericJobs: genericJobRecoveryControls,
  });

  const transfers = useMemo<BackgroundJobItem[]>(() => {
    const addIds = (ids: Set<string>, ...values: (string | undefined)[]) => {
      for (const v of values) if (v) ids.add(v);
    };
    const localTransferIds = new Set<string>();
    const localItems: { id: string; jobId?: string }[] = [
      ...downloads,
      ...uploads,
      ...compressions,
      ...extractions,
      ...indexers,
      ...copies,
      ...moves,
    ];
    for (const item of localItems) {
      addIds(localTransferIds, item.id, item.jobId);
    }
    return [
      ...downloads,
      ...uploads,
      ...compressions,
      ...extractions,
      ...indexers,
      ...copies,
      ...moves,
      ...backgroundJobs.filter((j) => !localTransferIds.has(j.id)),
    ];
  }, [
    downloads,
    uploads,
    compressions,
    extractions,
    indexers,
    copies,
    moves,
    backgroundJobs,
  ]);

  const actionsValue = useMemo<BackgroundJobsActionsContextValue>(
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
      cancelJob,
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
      cancelJob,
    ],
  );

  const indexerValue = useMemo<BackgroundJobsIndexerContextValue>(
    () => ({
      indexers,
      isIndexing,
      isIndexerDialogOpen,
      lastIndexerResult,
      lastIndexerError,
    }),
    [
      indexers,
      isIndexing,
      isIndexerDialogOpen,
      lastIndexerResult,
      lastIndexerError,
    ],
  );

  const stateValue = useMemo<BackgroundJobsStateContextValue>(
    () => ({
      downloads,
      uploads,
      compressions,
      extractions,
      indexers,
      copies,
      moves,
      backgroundJobs,
      transfers,
      isIndexing,
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
      backgroundJobs,
      transfers,
      isIndexing,
      isIndexerDialogOpen,
      lastIndexerResult,
      lastIndexerError,
    ],
  );

  return (
    <BackgroundJobsActionsContext.Provider value={actionsValue}>
      <BackgroundJobsIndexerContext.Provider value={indexerValue}>
        <BackgroundJobsStateContext.Provider value={stateValue}>
          {children}
        </BackgroundJobsStateContext.Provider>
      </BackgroundJobsIndexerContext.Provider>
    </BackgroundJobsActionsContext.Provider>
  );
};
