import { useCallback, useEffect, useRef, useState } from "react";
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

// Backend progress frames are partial: a frame may report byte/file counts
// without a path, or vice versa. Folding frames the same way they'd be
// merged onto the live item (defined fields win, undefined falls back to
// what's already queued) means coalescing frames into one render per
// animation frame never drops a field an intermediate frame carried.
function stripUndefined<T extends object>(obj: T): Partial<T> {
  const result: Partial<T> = {};
  for (const key of Object.keys(obj) as (keyof T)[]) {
    if (obj[key] !== undefined) result[key] = obj[key];
  }
  return result;
}

function foldIndexerFrame(
  prev: IndexerProgressFrame | undefined,
  next: IndexerProgressFrame,
): IndexerProgressFrame {
  return prev ? { ...prev, ...stripUndefined(next) } : next;
}

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

  // Progress frames arrive far faster than the UI needs to repaint (a fast
  // indexer can push hundreds a second); coalesce them into one setState per
  // animation frame instead of one per frame received.
  const pendingProgressRef = useRef<Map<string, IndexerProgressFrame>>(
    new Map(),
  );
  const progressFrameRef = useRef<number | null>(null);

  const flushIndexerProgress = useCallback(() => {
    if (progressFrameRef.current !== null) {
      window.cancelAnimationFrame(progressFrameRef.current);
      progressFrameRef.current = null;
    }
    const pending = pendingProgressRef.current;
    if (pending.size === 0) return;
    pendingProgressRef.current = new Map();
    setIndexers((prev) =>
      prev.map((item) => {
        const detail = pending.get(item.id);
        return detail ? mergeIndexerProgress(item, detail) : item;
      }),
    );
  }, []);

  useEffect(() => {
    return () => {
      if (progressFrameRef.current !== null) {
        window.cancelAnimationFrame(progressFrameRef.current);
        progressFrameRef.current = null;
      }
      pendingProgressRef.current.clear();
    };
  }, []);

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
          const pending = pendingProgressRef.current;
          pending.set(id, foldIndexerFrame(pending.get(id), detail));
          if (progressFrameRef.current === null) {
            progressFrameRef.current =
              window.requestAnimationFrame(flushIndexerProgress);
          }
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
          // Flush any queued progress before the terminal update so a task
          // never briefly shows stale progress (or none) after completion,
          // and so a late rAF can't fire after removal and re-add it.
          flushIndexerProgress();
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
      flushIndexerProgress,
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
