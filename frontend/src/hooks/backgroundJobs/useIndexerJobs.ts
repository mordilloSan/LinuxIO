import { useCallback, useState } from "react";
import { toast } from "sonner";

import {
  isConnected,
  type JobSnapshot,
  linuxio,
  openJobAttachStream,
} from "@/api";
import * as JobTypes from "@/constants/backgroundJobTypes";
import { useStreamResult } from "@/hooks/useStreamResult";
import type { ActiveIndexer, Indexer } from "@/types/backgroundJobs";
import { jobIdentityKey } from "@/utils/backgroundJobs";

import {
  indexerResultFromFrame,
  mergeIndexerProgress,
  type IndexerProgressFrame,
  type IndexerResultFrame,
} from "./indexerProgress";
import type { BackgroundJobRuntime } from "./useBackgroundJobRuntime";

export function useIndexerJobs(runtime: BackgroundJobRuntime) {
  const [indexers, setIndexers] = useState<ActiveIndexer[]>([]);
  const [isIndexerDialogOpen, setIsIndexerDialogOpen] = useState(false);
  const [lastIndexerResult, setLastIndexerResult] = useState<Indexer | null>(
    null,
  );
  const [lastIndexerError, setLastIndexerError] = useState<string | null>(null);
  const { run: runStreamResult } = useStreamResult();
  const { activeIndexerIdsRef, pendingLocalJobKeysRef, streamRefsRef } =
    runtime;

  const isIndexing = indexers.length > 0;

  const openIndexerDialog = useCallback(() => {
    setIsIndexerDialogOpen(true);
  }, []);

  const closeIndexerDialog = useCallback(() => {
    setIsIndexerDialogOpen(false);
  }, []);

  const removeIndexer = useCallback(
    (id: string) => {
      if (!activeIndexerIdsRef.current.has(id)) {
        return;
      }
      activeIndexerIdsRef.current.delete(id);
      setIndexers((prev) => prev.filter((r) => r.id !== id));
      streamRefsRef.current.delete(id);
    },
    [activeIndexerIdsRef, streamRefsRef],
  );

  const startIndexer = useCallback(
    async ({
      path = "/",
      onComplete,
    }: {
      path?: string;
      onComplete?: (result: Indexer) => void;
    }) => {
      // Only allow one indexer task at a time
      if (activeIndexerIdsRef.current.size > 0) {
        setIsIndexerDialogOpen(true);
        return;
      }

      setIsIndexerDialogOpen(true);

      if (!isConnected()) {
        setLastIndexerError("Stream connection not ready");
        toast.error("Stream connection not ready");
        return;
      }

      setLastIndexerResult(null);
      setLastIndexerError(null);

      const pendingKey = jobIdentityKey(JobTypes.JOB_TYPE_FILE_INDEXER, [
        path && path !== "/" ? path : "",
      ]);
      pendingLocalJobKeysRef.current.add(pendingKey);

      let job: JobSnapshot;
      try {
        job =
          path && path !== "/"
            ? await linuxio.filebrowser.index({ path })
            : await linuxio.filebrowser.index({});
      } catch (error) {
        pendingLocalJobKeysRef.current.delete(pendingKey);
        const message =
          error instanceof Error ? error.message : "Failed to start indexer";
        setLastIndexerError(message);
        toast.error(message);
        return;
      }

      const id = job.id;
      const abortController = new AbortController();

      const indexerTask: ActiveIndexer = {
        id,
        jobId: id,
        type: "indexer",
        path,
        bytesIndexed: 0,
        filesIndexed: 0,
        dirsIndexed: 0,
        totalSize: 0,
        durationMs: 0,
        currentPath: "",
        operation: path && path !== "/" ? "reindex" : "index",
        phase: "connecting",
        progress: 0,
        label: "Starting indexer...",
        state: "connecting",
        abortController,
      };

      setIndexers((prev) => [...prev, indexerTask]);
      activeIndexerIdsRef.current.add(id);
      pendingLocalJobKeysRef.current.delete(pendingKey);

      void runStreamResult<
        IndexerResultFrame | undefined,
        IndexerProgressFrame
      >({
        open: () => openJobAttachStream(id),
        signal: abortController.signal,
        closeOnAbort: "none",
        openErrorMessage: "Failed to open indexer stream",
        closeMessage: "Indexer stream closed unexpectedly",
        onOpen: (stream) => {
          streamRefsRef.current.set(id, stream);
          setIndexers((prev) =>
            prev.map((r) => (r.id === id ? { ...r, stream } : r)),
          );
        },
        onProgress: (progress) => {
          setIndexers((prev) =>
            prev.map((item) =>
              item.id === id ? mergeIndexerProgress(item, progress) : item,
            ),
          );
        },
        onSuccess: (result) => {
          const summary = indexerResultFromFrame(path, result);
          setLastIndexerResult(summary);
          setLastIndexerError(null);
          toast.success(
            `Indexing complete: ${summary.filesIndexed} files, ${summary.dirsIndexed} dirs`,
          );
          onComplete?.(summary);
        },
        onError: (error: unknown) => {
          if (abortController.signal.aborted) {
            return;
          }
          const message =
            error instanceof Error ? error.message : "Indexing failed";
          setLastIndexerError(message);
          setLastIndexerResult(null);
          toast.error(message);
        },
        onFinally: () => {
          streamRefsRef.current.delete(id);
          setIndexers((prev) =>
            prev.map((r) => (r.id === id ? { ...r, stream: null } : r)),
          );
          removeIndexer(id);
        },
      });
    },
    [
      activeIndexerIdsRef,
      pendingLocalJobKeysRef,
      removeIndexer,
      runStreamResult,
      streamRefsRef,
    ],
  );

  return {
    indexers,
    startIndexer,
    isIndexing,
    isIndexerDialogOpen,
    openIndexerDialog,
    closeIndexerDialog,
    lastIndexerResult,
    lastIndexerError,
    recoveryControls: {
      setIndexers,
      setIsIndexerDialogOpen,
      setLastIndexerResult,
      setLastIndexerError,
      removeIndexer,
    },
  };
}
