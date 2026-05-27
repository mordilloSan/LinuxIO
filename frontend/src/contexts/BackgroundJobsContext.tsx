import React, { createContext, useContext, useMemo } from "react";

import type {
  BackgroundJobItem,
  BackgroundJobsContextValue,
} from "@/types/backgroundJobs";

import { STREAM_MULTIPLEXER_CONFIG } from "@/api";
import { ConfigContext } from "@/contexts/ConfigContext";
import { useArchiveJobs } from "@/hooks/backgroundJobs/useArchiveJobs";
import { useBackgroundJobRuntime } from "@/hooks/backgroundJobs/useBackgroundJobRuntime";
import { useCopyMoveJobs } from "@/hooks/backgroundJobs/useCopyMoveJobs";
import { useDownloadJobs } from "@/hooks/backgroundJobs/useDownloadJobs";
import { useGenericBackgroundJobs } from "@/hooks/backgroundJobs/useGenericBackgroundJobs";
import { useIndexerJobs } from "@/hooks/backgroundJobs/useIndexerJobs";
import { useRecoveredJobs } from "@/hooks/backgroundJobs/useRecoveredJobs";
import { useUploadJobs } from "@/hooks/backgroundJobs/useUploadJobs";

export type { BackgroundJobsContextValue } from "@/types/backgroundJobs";

export const BackgroundJobsContext =
  createContext<BackgroundJobsContextValue | null>(null);

export const BackgroundJobsProvider: React.FC<{
  children: React.ReactNode;
}> = ({ children }) => {
  const configCtx = useContext(ConfigContext);
  const chunkSize =
    (configCtx?.config.appSettings.chunkSizeMB ?? 0) > 0
      ? (configCtx!.config.appSettings.chunkSizeMB as number) * 1024 * 1024
      : STREAM_MULTIPLEXER_CONFIG.uploadChunkSize;
  const uploadWindowSize =
    chunkSize * STREAM_MULTIPLEXER_CONFIG.uploadWindowChunks;

  const runtime = useBackgroundJobRuntime();
  const { downloads, startDownload, cancelDownload } = useDownloadJobs(runtime);
  const { uploads, startUpload, cancelUpload } = useUploadJobs(runtime, {
    chunkSize,
    uploadWindowSize,
  });
  const {
    compressions,
    extractions,
    startCompression,
    cancelCompression,
    startExtraction,
    cancelExtraction,
    recoveryControls: archiveRecoveryControls,
  } = useArchiveJobs(runtime);
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
  const {
    copies,
    moves,
    startCopy,
    cancelCopy,
    startMove,
    cancelMove,
    recoveryControls: copyMoveRecoveryControls,
  } = useCopyMoveJobs(runtime);

  useRecoveredJobs(runtime, {
    archives: archiveRecoveryControls,
    copyMove: copyMoveRecoveryControls,
    indexers: indexerRecoveryControls,
    genericJobs: genericJobRecoveryControls,
  });

  const transfers = useMemo<BackgroundJobItem[]>(() => {
    const addIds = (ids: Set<string>, ...values: (string | undefined)[]) => {
      for (const v of values) if (v) ids.add(v);
    };
    const localTransferIds = new Set<string>();
    for (const d of downloads) addIds(localTransferIds, d.id);
    for (const u of uploads) addIds(localTransferIds, u.id, u.jobId);
    for (const c of compressions) addIds(localTransferIds, c.id);
    for (const e of extractions) addIds(localTransferIds, e.id);
    for (const i of indexers) addIds(localTransferIds, i.id);
    for (const c of copies) addIds(localTransferIds, c.id);
    for (const m of moves) addIds(localTransferIds, m.id);
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

  const contextValue = useMemo(
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
      startDownload,
      cancelDownload,
      startCompression,
      cancelCompression,
      startExtraction,
      cancelExtraction,
      startIndexer,
      isIndexing,
      isIndexerDialogOpen,
      openIndexerDialog,
      closeIndexerDialog,
      lastIndexerResult,
      lastIndexerError,
      startCopy,
      cancelCopy,
      startMove,
      cancelMove,
      cancelJob,
      startUpload,
      cancelUpload,
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
      startDownload,
      cancelDownload,
      startCompression,
      cancelCompression,
      startExtraction,
      cancelExtraction,
      startIndexer,
      isIndexing,
      isIndexerDialogOpen,
      openIndexerDialog,
      closeIndexerDialog,
      lastIndexerResult,
      lastIndexerError,
      startCopy,
      cancelCopy,
      startMove,
      cancelMove,
      cancelJob,
      startUpload,
      cancelUpload,
    ],
  );

  return (
    <BackgroundJobsContext.Provider value={contextValue}>
      {children}
    </BackgroundJobsContext.Provider>
  );
};
