import { useQueryClient } from "@tanstack/react-query";
import {
  type Dispatch,
  type SetStateAction,
  useCallback,
  useEffect,
  useMemo,
} from "react";

import {
  bindStreamHandlers,
  isTaskCancellationError,
  isTerminalTaskState,
  type TaskEvent,
  type TaskSnapshot,
  openTaskWatchStream,
  openTaskEventsStream,
  type ProgressFrame,
  type Stream,
  useStreamMux,
} from "@/api";
import { OPERATION_QUERY_INVALIDATIONS } from "@/api/operation-query-invalidations";
import * as TaskTypes from "@/constants/backgroundTaskTypes";
import useAuth from "@/hooks/useAuth";
import { useStreamResult } from "@/hooks/useStreamResult";
import type {
  ActiveIndexer,
  BackgroundTask,
  Indexer,
} from "@/types/backgroundTasks";
import {
  taskIdentityKey,
  taskMetadataIdentity,
  taskMetadataObject,
  requestString,
} from "@/utils/backgroundTasks";

import {
  indexerResultFromFrame,
  mergeIndexerProgress,
  type IndexerProgressFrame,
  type IndexerResultFrame,
} from "./indexerProgress";
import {
  emitTerminalTaskFeedback,
  GENERIC_TASK_FEEDBACK,
  TERMINAL_TASK_FEEDBACK,
  terminalSnapshotOutcome,
} from "./terminalTaskFeedback";
import type { BackgroundTaskRuntime } from "./useBackgroundTaskRuntime";

interface RecoveredTaskControls {
  /**
   * Adopt a running transfer task (compress/extract/copy/move) into the
   * navbar via the transfer engine; returns false for other task types.
   */
  recoverTransfer: (task: TaskSnapshot) => boolean;
  genericTasks: {
    setBackgroundTasks: Dispatch<SetStateAction<BackgroundTask[]>>;
    removeBackgroundTask: (id: string) => void;
  };
  indexers: {
    setIndexers: Dispatch<SetStateAction<ActiveIndexer[]>>;
    setIsIndexerDialogOpen: Dispatch<SetStateAction<boolean>>;
    setLastIndexerResult: Dispatch<SetStateAction<Indexer | null>>;
    setLastIndexerError: Dispatch<SetStateAction<string | null>>;
    removeIndexer: (id: string) => void;
  };
}

export function useRecoveredTasks(
  runtime: BackgroundTaskRuntime,
  controls: RecoveredTaskControls,
) {
  const queryClient = useQueryClient();
  const { status: streamMuxStatus } = useStreamMux();
  const { run: runStreamResult } = useStreamResult();
  const { refreshCapabilities } = useAuth();

  // Per-type feedback (which terminal states toast, and how) lives in the
  // terminalTaskFeedback registry; this hook only reports outcomes to it.
  const feedbackDeps = useMemo(
    () => ({ refreshCapabilities }),
    [refreshCapabilities],
  );
  const {
    activeIndexerIdsRef,
    activeBackgroundTaskIdsRef,
    activeFileTransferTaskIdsRef,
    recoveringTaskIdsRef,
    pendingLocalTaskKeysRef,
    streamRefsRef,
  } = runtime;
  const {
    recoverTransfer,
    indexers: {
      setIndexers,
      setIsIndexerDialogOpen,
      setLastIndexerResult,
      setLastIndexerError,
      removeIndexer,
    },
    genericTasks: { setBackgroundTasks, removeBackgroundTask },
  } = controls;

  const watchRecoveredTask = useCallback(
    (task: TaskSnapshot) => {
      if (recoveringTaskIdsRef.current.has(task.id)) {
        return;
      }
      if (isTerminalTaskState(task.state)) {
        return;
      }
      if (
        pendingLocalTaskKeysRef.current.has(
          taskIdentityKey(task.type, taskMetadataIdentity(task.metadata)),
        )
      ) {
        return;
      }

      const metadata = taskMetadataObject(task.metadata);
      const getName = (path: string | undefined, fallback: string) => {
        const trimmed = (path ?? "").replace(/\/+$/, "");
        if (!trimmed) return fallback;
        const parts = trimmed.split("/");
        return parts[parts.length - 1] || fallback;
      };
      const abortController = new AbortController();
      const genericProgressPct = (value: unknown) => {
        const data = value as
          | {
              pct?: number;
              percentage?: number;
              item_pct?: number;
              indeterminate?: boolean;
            }
          | undefined;
        if (data?.indeterminate) return 0;
        return Math.min(
          99,
          data?.pct ?? data?.percentage ?? data?.item_pct ?? 0,
        );
      };
      const genericProgressMeta = (value: unknown) => {
        const data = value as
          | { indeterminate?: boolean; processed?: number }
          | undefined;
        return {
          indeterminate: data?.indeterminate,
          processed: data?.processed,
        };
      };
      const genericLabel = (value: unknown) => {
        const data = value as
          | {
              type?: string;
              message?: string;
              status?: string;
              package_id?: string;
              files_indexed?: number;
              dirs_indexed?: number;
              filesDone?: number;
              filesTotal?: number;
              phase?: string;
              pct?: number;
              processed?: number;
              indeterminate?: boolean;
            }
          | undefined;
        switch (task.type) {
          case TaskTypes.TASK_TYPE_FILE_UPLOAD: {
            const name = getName(
              requestString(metadata, "path") ??
                requestString(metadata, "label"),
              "file",
            );
            return data?.phase === "waiting_for_client"
              ? `Upload waiting: ${name}`
              : `Uploading ${name}${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          }
          case TaskTypes.TASK_TYPE_FILE_UPLOAD_BATCH: {
            const filesTotal = data?.filesTotal ?? 0;
            return data?.phase === "waiting_for_client"
              ? `Upload waiting: ${filesTotal} file${filesTotal === 1 ? "" : "s"}`
              : `Uploading ${data?.filesDone ?? 0}/${filesTotal} files${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          }
          case TaskTypes.TASK_TYPE_FILE_DOWNLOAD: {
            const name = getName(
              requestString(metadata, "path") ??
                requestString(metadata, "label"),
              "file",
            );
            return data?.phase === "waiting_for_client"
              ? `Download waiting: ${name}`
              : `Downloading ${name}${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          }
          case TaskTypes.TASK_TYPE_FILE_ARCHIVE:
            return data?.phase === "waiting_for_client"
              ? "Archive download waiting"
              : `Preparing archive${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          case TaskTypes.TASK_TYPE_FILE_CHMOD_BATCH: {
            const processed = data?.processed ?? 0;
            return `${data?.phase === "chown" ? "Changing ownership" : "Changing permissions"}: ${processed} item${processed === 1 ? "" : "s"}`;
          }
          case TaskTypes.TASK_TYPE_FILE_DELETE_BATCH: {
            const processed = data?.processed ?? 0;
            return `Deleting ${processed} item${processed === 1 ? "" : "s"}`;
          }
          case TaskTypes.TASK_TYPE_DOCKER_COMPOSE:
            return (
              data?.message ??
              `Docker compose ${requestString(metadata, "action") ?? "operation"}`
            );
          case TaskTypes.TASK_TYPE_PACKAGE_UPDATE:
            return data?.package_id
              ? `Updating ${String(data.package_id).split(";")[0]}`
              : data?.status
                ? `Updating packages: ${data.status}`
                : "Updating packages";
          case TaskTypes.TASK_TYPE_STORAGE_SMART_TEST:
            return data?.message ?? "Running SMART self-test";
          case TaskTypes.TASK_TYPE_SYSTEM_INSTALL_CAPABILITY: {
            const cap = requestString(metadata, "capability") ?? "capability";
            return data?.message ?? `Installing ${cap}`;
          }
          default:
            return "Running task";
        }
      };

      const watch = ({
        onProgress,
        onSuccess,
        onError,
        onFinally,
      }: {
        onProgress: (progress: ProgressFrame) => void;
        onSuccess: (result: unknown) => void;
        onError: (error: unknown) => void;
        onFinally: () => void;
      }) => {
        recoveringTaskIdsRef.current.add(task.id);
        void runStreamResult<unknown>({
          open: () => openTaskWatchStream(task.id),
          signal: abortController.signal,
          closeOnAbort: "none",
          openErrorMessage: "Failed to watch running task",
          closeMessage: "Task stream closed unexpectedly",
          onOpen: (stream) => {
            streamRefsRef.current.set(task.id, stream);
          },
          onProgress,
          onSuccess,
          onError,
          onFinally: () => {
            streamRefsRef.current.delete(task.id);
            recoveringTaskIdsRef.current.delete(task.id);
            onFinally();
          },
        });
      };

      switch (task.type) {
        case TaskTypes.TASK_TYPE_FILE_COMPRESS:
        case TaskTypes.TASK_TYPE_FILE_EXTRACT:
        case TaskTypes.TASK_TYPE_FILE_COPY_BATCH:
        case TaskTypes.TASK_TYPE_FILE_MOVE_BATCH: {
          // The transfer engine rebuilds the navbar item and resumes watching with
          // the same lifecycle used for fresh starts.
          recoverTransfer(task);
          break;
        }
        case TaskTypes.TASK_TYPE_FILE_INDEXER: {
          if (activeIndexerIdsRef.current.has(task.id)) return;
          activeIndexerIdsRef.current.add(task.id);
          setIsIndexerDialogOpen(true);
          setIndexers((prev) => [
            ...prev,
            {
              id: task.id,
              taskId: task.id,
              type: "indexer",
              path: requestString(metadata, "path") ?? "/",
              bytesIndexed: 0,
              filesIndexed: 0,
              dirsIndexed: 0,
              totalSize: 0,
              durationMs: 0,
              currentPath: "",
              phase: "connecting",
              progress: 0,
              label: "Connecting to indexer...",
              state: "connecting",
              abortController,
            },
          ]);
          watch({
            onProgress: (nextProgress) => {
              setIndexers((prev) =>
                prev.map((item) =>
                  item.id === task.id
                    ? mergeIndexerProgress(
                        item,
                        nextProgress as IndexerProgressFrame,
                      )
                    : item,
                ),
              );
            },
            onSuccess: (result) => {
              setLastIndexerResult(
                indexerResultFromFrame(
                  requestString(metadata, "path") ?? "/",
                  result as IndexerResultFrame | undefined,
                ),
              );
              setLastIndexerError(null);
            },
            onError: (error) => {
              if (!abortController.signal.aborted) {
                setLastIndexerError(
                  error instanceof Error ? error.message : "Indexing failed",
                );
              }
            },
            onFinally: () => removeIndexer(task.id),
          });
          break;
        }
        case TaskTypes.TASK_TYPE_DOCKER_COMPOSE:
        case TaskTypes.TASK_TYPE_PACKAGE_UPDATE:
        case TaskTypes.TASK_TYPE_STORAGE_SMART_TEST:
        case TaskTypes.TASK_TYPE_SYSTEM_INSTALL_CAPABILITY:
        case TaskTypes.TASK_TYPE_FILE_UPLOAD:
        case TaskTypes.TASK_TYPE_FILE_UPLOAD_BATCH:
        case TaskTypes.TASK_TYPE_FILE_DOWNLOAD:
        case TaskTypes.TASK_TYPE_FILE_ARCHIVE:
        case TaskTypes.TASK_TYPE_FILE_CHMOD_BATCH:
        case TaskTypes.TASK_TYPE_FILE_DELETE_BATCH: {
          if (activeFileTransferTaskIdsRef.current.has(task.id)) {
            return;
          }
          if (activeBackgroundTaskIdsRef.current.has(task.id)) return;
          const feedbackTask = { id: task.id, type: task.type, metadata };
          const feedbackEntry =
            TERMINAL_TASK_FEEDBACK[task.type] ?? GENERIC_TASK_FEEDBACK;
          const initialProgress = genericProgressPct(task.progress);
          const initialMeta = genericProgressMeta(task.progress);
          activeBackgroundTaskIdsRef.current.add(task.id);
          setBackgroundTasks((prev) => [
            ...prev,
            {
              id: task.id,
              taskId: task.id,
              type: "task",
              taskType: task.type,
              progress: initialProgress,
              label: genericLabel(task.progress),
              indeterminate: initialMeta.indeterminate,
              processed: initialMeta.processed,
              abortController,
            },
          ]);
          watch({
            onProgress: (nextProgress) => {
              setBackgroundTasks((prev) =>
                prev.map((item) =>
                  item.id === task.id
                    ? {
                        ...item,
                        progress: Math.max(
                          item.progress,
                          genericProgressPct(nextProgress),
                        ),
                        label: genericLabel(nextProgress),
                        ...genericProgressMeta(nextProgress),
                      }
                    : item,
                ),
              );
            },
            onSuccess: (result) => {
              setBackgroundTasks((prev) =>
                prev.map((item) =>
                  item.id === task.id ? { ...item, progress: 100 } : item,
                ),
              );
              emitTerminalTaskFeedback(
                feedbackTask,
                { kind: "completed", result },
                feedbackDeps,
                feedbackEntry,
              );
            },
            onError: (error) => {
              if (abortController.signal.aborted) return;
              // Only the navbar cancel aborts the controller above; a cancel
              // from an owning page or another session arrives here as an
              // ordinary 499 stream error and must be classified so the
              // registry can tell it apart from a failure.
              emitTerminalTaskFeedback(
                feedbackTask,
                {
                  kind: isTaskCancellationError(error) ? "canceled" : "failed",
                  error,
                },
                feedbackDeps,
                feedbackEntry,
              );
            },
            onFinally: () => removeBackgroundTask(task.id),
          });
          break;
        }
      }
    },
    [
      recoverTransfer,
      removeIndexer,
      removeBackgroundTask,
      runStreamResult,
      feedbackDeps,
      // Stable runtime refs and setters: they arrive as plain function
      // params, so neither the compiler nor the lint rule can prove them
      // stable without listing them.
      activeBackgroundTaskIdsRef,
      activeFileTransferTaskIdsRef,
      activeIndexerIdsRef,
      pendingLocalTaskKeysRef,
      recoveringTaskIdsRef,
      streamRefsRef,
      setBackgroundTasks,
      setIndexers,
      setIsIndexerDialogOpen,
      setLastIndexerError,
      setLastIndexerResult,
    ],
  );

  useEffect(() => {
    if (streamMuxStatus !== "open") {
      return;
    }

    let cancelled = false;
    let cleanupEvents: (() => void) | undefined;
    let eventStream: Stream | null = null;

    eventStream = openTaskEventsStream();
    if (eventStream) {
      cleanupEvents = bindStreamHandlers<TaskEvent>(eventStream, {
        onProgress: (event) => {
          if (!event?.task || cancelled) return;
          const task = event.task;

          // 1) Attach progress trackers to tasks that don't have a local handler.
          watchRecoveredTask(task);

          // 2) On terminal events, always invalidate mapped query caches. A
          //    local handler may already have done the same, but a duplicate
          //    invalidation is safe; suppressing this fallback can leave stale
          //    data when that handler detaches before completion.
          if (!isTerminalTaskState(task.state)) return;

          // Airtight feedback fallback: watchRecoveredTask() bails on
          // already-terminal tasks, so a task first observed in a terminal state
          // would never report via the watch path. The emit is de-duped by
          // task id and registry-only here (no generic fallback), so it is a
          // no-op when the watch path or an owning page already fired.
          const outcome = terminalSnapshotOutcome(task);
          if (outcome) {
            emitTerminalTaskFeedback(
              {
                id: task.id,
                type: task.type,
                metadata: taskMetadataObject(task.metadata),
              },
              outcome,
              feedbackDeps,
            );
          }

          const keys = OPERATION_QUERY_INVALIDATIONS[task.type];
          if (!keys) return;
          for (const queryKey of keys) {
            void queryClient.invalidateQueries({ queryKey });
          }
        },
        onClose: () => {
          if (!cancelled) {
            console.debug("Task events stream closed");
          }
        },
      });
    } else {
      console.debug("Failed to open task events stream");
    }

    return () => {
      cancelled = true;
      cleanupEvents?.();
      eventStream?.close();
    };
  }, [watchRecoveredTask, feedbackDeps, queryClient, streamMuxStatus]);
}
