import { useCallback, useState } from "react";
import { toast } from "sonner";

import type { ActiveIndexer, Indexer } from "@/types/backgroundJobs";

import {
  isConnected,
  type JobSnapshot,
  linuxio,
  openJobAttachStream,
  type ProgressFrame,
} from "@/api";
import * as JobTypes from "@/constants/backgroundJobTypes";
import { useStreamResult } from "@/hooks/useStreamResult";
import { jobIdentityKey } from "@/utils/backgroundJobs";

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

      const jobArgs = path && path !== "/" ? [path] : [];
      const pendingKey = jobIdentityKey(
        JobTypes.JOB_TYPE_FILE_INDEXER,
        jobArgs,
      );
      pendingLocalJobKeysRef.current.add(pendingKey);

      let job: JobSnapshot;
      try {
        job =
          path && path !== "/"
            ? await linuxio.filebrowser.index.call(path)
            : await linuxio.filebrowser.index.call();
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
        type: "indexer",
        path,
        filesIndexed: 0,
        dirsIndexed: 0,
        totalSize: 0,
        durationMs: 0,
        currentPath: "",
        phase: "connecting",
        progress: 0,
        label: "Starting indexer...",
        abortController,
      };

      setIndexers((prev) => [...prev, indexerTask]);
      activeIndexerIdsRef.current.add(id);
      pendingLocalJobKeysRef.current.delete(pendingKey);

      void runStreamResult<
        | {
            files_indexed?: number;
            dirs_indexed?: number;
            total_size?: number;
            duration_ms?: number;
          }
        | undefined,
        ProgressFrame
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
          const progressData = progress as ProgressFrame & {
            files_indexed?: number;
            dirs_indexed?: number;
            current_path?: string;
            phase?: string;
          };

          setIndexers((prev) =>
            prev.map((r) => {
              if (r.id !== id) return r;
              const filesIndexed = progressData.files_indexed ?? r.filesIndexed;
              const dirsIndexed = progressData.dirs_indexed ?? r.dirsIndexed;
              const currentPath = progressData.current_path ?? r.currentPath;
              const phase = progressData.phase ?? "indexing";

              return {
                ...r,
                filesIndexed,
                dirsIndexed,
                currentPath,
                phase,
                label:
                  phase === "connecting"
                    ? "Connecting to indexer..."
                    : `Indexing: ${filesIndexed} files, ${dirsIndexed} dirs`,
              };
            }),
          );
        },
        onSuccess: (result) => {
          const summary = {
            path,
            filesIndexed: result?.files_indexed ?? 0,
            dirsIndexed: result?.dirs_indexed ?? 0,
            totalSize: result?.total_size ?? 0,
            durationMs: result?.duration_ms ?? 0,
          };
          setLastIndexerResult(summary);
          setLastIndexerError(null);
          toast.success(
            `Indexing complete: ${result?.files_indexed ?? 0} files, ${result?.dirs_indexed ?? 0} dirs`,
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
