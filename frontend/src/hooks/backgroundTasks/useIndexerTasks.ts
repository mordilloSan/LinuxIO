import { useCallback, useState } from "react";
import { toast } from "sonner";

import {
  isConnected,
  type TaskSnapshot,
  type TaskProgress,
  linuxio,
  openTaskWatchStream,
} from "@/api";
import * as TaskTypes from "@/constants/backgroundTaskTypes";
import { useStreamResult } from "@/hooks/useStreamResult";
import type { ActiveIndexer, Indexer } from "@/types/backgroundTasks";
import { taskIdentityKey } from "@/utils/backgroundTasks";

import {
  indexerResultFromFrame,
  mergeIndexerProgress,
  type IndexerProgressFrame,
  type IndexerResultFrame,
} from "./indexerProgress";
import type { BackgroundTaskRuntime } from "./useBackgroundTaskRuntime";

export function useIndexerTasks(runtime: BackgroundTaskRuntime) {
  const [indexers, setIndexers] = useState<ActiveIndexer[]>([]);
  const [isIndexerDialogOpen, setIsIndexerDialogOpen] = useState(false);
  const [lastIndexerResult, setLastIndexerResult] = useState<Indexer | null>(
    null,
  );
  const [lastIndexerError, setLastIndexerError] = useState<string | null>(null);
  const { run: runStreamResult } = useStreamResult();
  const { activeIndexerIdsRef, pendingLocalTaskKeysRef, streamRefsRef } =
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

      const pendingKey = taskIdentityKey(TaskTypes.TASK_TYPE_FILE_INDEXER, [
        path && path !== "/" ? path : "",
      ]);
      pendingLocalTaskKeysRef.current.add(pendingKey);

      let task: TaskSnapshot;
      try {
        task =
          path && path !== "/"
            ? await linuxio.filebrowser.index({ path })
            : await linuxio.filebrowser.index({});
      } catch (error) {
        pendingLocalTaskKeysRef.current.delete(pendingKey);
        const message =
          error instanceof Error ? error.message : "Failed to start indexer";
        setLastIndexerError(message);
        toast.error(message);
        return;
      }

      const id = task.id;
      const abortController = new AbortController();

      const indexerTask: ActiveIndexer = {
        id,
        taskId: id,
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
      pendingLocalTaskKeysRef.current.delete(pendingKey);

      void runStreamResult<
        IndexerResultFrame | undefined,
        TaskProgress<IndexerProgressFrame>
      >({
        open: () => openTaskWatchStream(id),
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
          const detail = progress.detail;
          if (!detail) return;
          setIndexers((prev) =>
            prev.map((item) =>
              item.id === id ? mergeIndexerProgress(item, detail) : item,
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
      pendingLocalTaskKeysRef,
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
