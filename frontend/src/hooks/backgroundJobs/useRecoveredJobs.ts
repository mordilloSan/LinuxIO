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
  isJobCancellationError,
  isJobLocallyHandled,
  isTerminalJobState,
  type JobEvent,
  type JobSnapshot,
  openJobAttachStream,
  openJobEventsStream,
  type ProgressFrame,
  type Stream,
  useStreamMux,
} from "@/api";
import { OPERATION_QUERY_INVALIDATIONS } from "@/api/operation-query-invalidations";
import * as JobTypes from "@/constants/backgroundJobTypes";
import useAuth from "@/hooks/useAuth";
import { useStreamResult } from "@/hooks/useStreamResult";
import type {
  ActiveIndexer,
  BackgroundJob,
  Indexer,
} from "@/types/backgroundJobs";
import {
  jobIdentityKey,
  jobMetadataIdentity,
  jobMetadataObject,
  requestString,
} from "@/utils/backgroundJobs";

import {
  indexerResultFromFrame,
  mergeIndexerProgress,
  type IndexerProgressFrame,
  type IndexerResultFrame,
} from "./indexerProgress";
import {
  emitTerminalJobFeedback,
  GENERIC_JOB_FEEDBACK,
  TERMINAL_JOB_FEEDBACK,
  terminalSnapshotOutcome,
} from "./terminalJobFeedback";
import type { BackgroundJobRuntime } from "./useBackgroundJobRuntime";

interface RecoveredJobControls {
  /**
   * Adopt a running transfer job (compress/extract/copy/move) into the
   * navbar via the transfer engine; returns false for other job types.
   */
  recoverTransfer: (job: JobSnapshot) => boolean;
  genericJobs: {
    setBackgroundJobs: Dispatch<SetStateAction<BackgroundJob[]>>;
    removeBackgroundJob: (id: string) => void;
  };
  indexers: {
    setIndexers: Dispatch<SetStateAction<ActiveIndexer[]>>;
    setIsIndexerDialogOpen: Dispatch<SetStateAction<boolean>>;
    setLastIndexerResult: Dispatch<SetStateAction<Indexer | null>>;
    setLastIndexerError: Dispatch<SetStateAction<string | null>>;
    removeIndexer: (id: string) => void;
  };
}

export function useRecoveredJobs(
  runtime: BackgroundJobRuntime,
  controls: RecoveredJobControls,
) {
  const queryClient = useQueryClient();
  const { status: streamMuxStatus } = useStreamMux();
  const { run: runStreamResult } = useStreamResult();
  const { refreshCapabilities } = useAuth();

  // Per-type feedback (which terminal states toast, and how) lives in the
  // terminalJobFeedback registry; this hook only reports outcomes to it.
  const feedbackDeps = useMemo(
    () => ({ refreshCapabilities }),
    [refreshCapabilities],
  );
  const {
    activeIndexerIdsRef,
    activeBackgroundJobIdsRef,
    activeFileTransferJobIdsRef,
    recoveringJobIdsRef,
    pendingLocalJobKeysRef,
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
    genericJobs: { setBackgroundJobs, removeBackgroundJob },
  } = controls;

  const attachRecoveredJob = useCallback(
    (job: JobSnapshot) => {
      if (recoveringJobIdsRef.current.has(job.id)) {
        return;
      }
      if (isTerminalJobState(job.state)) {
        return;
      }
      if (
        pendingLocalJobKeysRef.current.has(
          jobIdentityKey(job.type, jobMetadataIdentity(job.metadata)),
        )
      ) {
        return;
      }

      const metadata = jobMetadataObject(job.metadata);
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
        switch (job.type) {
          case JobTypes.JOB_TYPE_FILE_UPLOAD: {
            const name = getName(
              requestString(metadata, "path") ??
                requestString(metadata, "label"),
              "file",
            );
            return data?.phase === "waiting_for_client"
              ? `Upload waiting: ${name}`
              : `Uploading ${name}${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          }
          case JobTypes.JOB_TYPE_FILE_UPLOAD_BATCH: {
            const filesTotal = data?.filesTotal ?? 0;
            return data?.phase === "waiting_for_client"
              ? `Upload waiting: ${filesTotal} file${filesTotal === 1 ? "" : "s"}`
              : `Uploading ${data?.filesDone ?? 0}/${filesTotal} files${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          }
          case JobTypes.JOB_TYPE_FILE_DOWNLOAD: {
            const name = getName(
              requestString(metadata, "path") ??
                requestString(metadata, "label"),
              "file",
            );
            return data?.phase === "waiting_for_client"
              ? `Download waiting: ${name}`
              : `Downloading ${name}${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          }
          case JobTypes.JOB_TYPE_FILE_ARCHIVE:
            return data?.phase === "waiting_for_client"
              ? "Archive download waiting"
              : `Preparing archive${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          case JobTypes.JOB_TYPE_FILE_CHMOD_BATCH: {
            const processed = data?.processed ?? 0;
            return `${data?.phase === "chown" ? "Changing ownership" : "Changing permissions"}: ${processed} item${processed === 1 ? "" : "s"}`;
          }
          case JobTypes.JOB_TYPE_FILE_DELETE_BATCH: {
            const processed = data?.processed ?? 0;
            return `Deleting ${processed} item${processed === 1 ? "" : "s"}`;
          }
          case JobTypes.JOB_TYPE_DOCKER_COMPOSE:
            return (
              data?.message ??
              `Docker compose ${requestString(metadata, "action") ?? "operation"}`
            );
          case JobTypes.JOB_TYPE_PACKAGE_UPDATE:
            return data?.package_id
              ? `Updating ${String(data.package_id).split(";")[0]}`
              : data?.status
                ? `Updating packages: ${data.status}`
                : "Updating packages";
          case JobTypes.JOB_TYPE_STORAGE_SMART_TEST:
            return data?.message ?? "Running SMART self-test";
          case JobTypes.JOB_TYPE_SYSTEM_INSTALL_CAPABILITY: {
            const cap = requestString(metadata, "capability") ?? "capability";
            return data?.message ?? `Installing ${cap}`;
          }
          default:
            return "Running job";
        }
      };

      const attach = ({
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
        recoveringJobIdsRef.current.add(job.id);
        void runStreamResult<unknown, ProgressFrame>({
          open: () => openJobAttachStream(job.id),
          signal: abortController.signal,
          closeOnAbort: "none",
          openErrorMessage: "Failed to attach to running job",
          closeMessage: "Job stream closed unexpectedly",
          onOpen: (stream) => {
            streamRefsRef.current.set(job.id, stream);
          },
          onProgress,
          onSuccess,
          onError,
          onFinally: () => {
            streamRefsRef.current.delete(job.id);
            recoveringJobIdsRef.current.delete(job.id);
            onFinally();
          },
        });
      };

      switch (job.type) {
        case JobTypes.JOB_TYPE_FILE_COMPRESS:
        case JobTypes.JOB_TYPE_FILE_EXTRACT:
        case JobTypes.JOB_TYPE_FILE_COPY_BATCH:
        case JobTypes.JOB_TYPE_FILE_MOVE_BATCH: {
          // The transfer engine rebuilds the navbar item and re-attaches with
          // the same lifecycle used for fresh starts.
          recoverTransfer(job);
          break;
        }
        case JobTypes.JOB_TYPE_FILE_INDEXER: {
          if (activeIndexerIdsRef.current.has(job.id)) return;
          activeIndexerIdsRef.current.add(job.id);
          setIsIndexerDialogOpen(true);
          setIndexers((prev) => [
            ...prev,
            {
              id: job.id,
              jobId: job.id,
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
          attach({
            onProgress: (nextProgress) => {
              setIndexers((prev) =>
                prev.map((item) =>
                  item.id === job.id
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
            onFinally: () => removeIndexer(job.id),
          });
          break;
        }
        case JobTypes.JOB_TYPE_DOCKER_COMPOSE:
        case JobTypes.JOB_TYPE_PACKAGE_UPDATE:
        case JobTypes.JOB_TYPE_STORAGE_SMART_TEST:
        case JobTypes.JOB_TYPE_SYSTEM_INSTALL_CAPABILITY:
        case JobTypes.JOB_TYPE_FILE_UPLOAD:
        case JobTypes.JOB_TYPE_FILE_UPLOAD_BATCH:
        case JobTypes.JOB_TYPE_FILE_DOWNLOAD:
        case JobTypes.JOB_TYPE_FILE_ARCHIVE:
        case JobTypes.JOB_TYPE_FILE_CHMOD_BATCH:
        case JobTypes.JOB_TYPE_FILE_DELETE_BATCH: {
          if (activeFileTransferJobIdsRef.current.has(job.id)) {
            return;
          }
          if (activeBackgroundJobIdsRef.current.has(job.id)) return;
          const feedbackJob = { id: job.id, type: job.type, metadata };
          const feedbackEntry =
            TERMINAL_JOB_FEEDBACK[job.type] ?? GENERIC_JOB_FEEDBACK;
          const initialProgress = genericProgressPct(job.progress);
          const initialMeta = genericProgressMeta(job.progress);
          activeBackgroundJobIdsRef.current.add(job.id);
          setBackgroundJobs((prev) => [
            ...prev,
            {
              id: job.id,
              jobId: job.id,
              type: "job",
              jobType: job.type,
              progress: initialProgress,
              label: genericLabel(job.progress),
              indeterminate: initialMeta.indeterminate,
              processed: initialMeta.processed,
              abortController,
            },
          ]);
          attach({
            onProgress: (nextProgress) => {
              setBackgroundJobs((prev) =>
                prev.map((item) =>
                  item.id === job.id
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
              setBackgroundJobs((prev) =>
                prev.map((item) =>
                  item.id === job.id ? { ...item, progress: 100 } : item,
                ),
              );
              emitTerminalJobFeedback(
                feedbackJob,
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
              emitTerminalJobFeedback(
                feedbackJob,
                {
                  kind: isJobCancellationError(error) ? "canceled" : "failed",
                  error,
                },
                feedbackDeps,
                feedbackEntry,
              );
            },
            onFinally: () => removeBackgroundJob(job.id),
          });
          break;
        }
      }
    },
    [
      recoverTransfer,
      removeIndexer,
      removeBackgroundJob,
      runStreamResult,
      feedbackDeps,
      // Stable runtime refs and setters: they arrive as plain function
      // params, so neither the compiler nor the lint rule can prove them
      // stable without listing them.
      activeBackgroundJobIdsRef,
      activeFileTransferJobIdsRef,
      activeIndexerIdsRef,
      pendingLocalJobKeysRef,
      recoveringJobIdsRef,
      streamRefsRef,
      setBackgroundJobs,
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

    eventStream = openJobEventsStream();
    if (eventStream) {
      cleanupEvents = bindStreamHandlers<JobEvent>(eventStream, {
        onProgress: (event) => {
          if (!event?.job || cancelled) return;
          const job = event.job;

          // 1) Attach progress trackers to jobs that don't have a local handler.
          attachRecoveredJob(job);

          // 2) On terminal events, invalidate query caches for jobs whose type
          //    has a mapping above and that aren't being tracked by a local
          //    handler (those handlers are responsible for their own
          //    invalidations).
          if (!isTerminalJobState(job.state)) return;

          // Airtight feedback fallback: attachRecoveredJob() bails on
          // already-terminal jobs, so a job first observed in a terminal state
          // would never report via the attach path. The emit is de-duped by
          // job id and registry-only here (no generic fallback), so it is a
          // no-op when the attach path or an owning page already fired.
          const outcome = terminalSnapshotOutcome(job);
          if (outcome) {
            emitTerminalJobFeedback(
              {
                id: job.id,
                type: job.type,
                metadata: jobMetadataObject(job.metadata),
              },
              outcome,
              feedbackDeps,
            );
          }

          if (isJobLocallyHandled(job.id)) return;
          const keys = OPERATION_QUERY_INVALIDATIONS[job.type];
          if (!keys) return;
          for (const queryKey of keys) {
            void queryClient.invalidateQueries({ queryKey });
          }
        },
        onClose: () => {
          if (!cancelled) {
            console.debug("Job events stream closed");
          }
        },
      });
    } else {
      console.debug("Failed to open job events stream");
    }

    return () => {
      cancelled = true;
      cleanupEvents?.();
      eventStream?.close();
    };
  }, [attachRecoveredJob, feedbackDeps, queryClient, streamMuxStatus]);
}
