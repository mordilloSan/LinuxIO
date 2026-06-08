import { useQueryClient } from "@tanstack/react-query";
import {
  type Dispatch,
  type SetStateAction,
  useCallback,
  useEffect,
  useRef,
} from "react";
import { toast } from "sonner";

import type {
  ActiveIndexer,
  BackgroundJob,
  Compression,
  Copy,
  Extraction,
  Indexer,
  Move,
} from "@/types/backgroundJobs";

import {
  bindStreamHandlers,
  CAPABILITIES,
  type CapabilityDef,
  type InstallCapabilityResult,
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
import { INVALIDATIONS_BY_JOB_TYPE } from "@/constants/backgroundJobQueryInvalidations";
import * as JobTypes from "@/constants/backgroundJobTypes";
import useAuth from "@/hooks/useAuth";
import { useStreamResult } from "@/hooks/useStreamResult";
import {
  createProgressSpeedCalculator,
  jobIdentityKey,
} from "@/utils/backgroundJobs";

import type { BackgroundJobRuntime } from "./useBackgroundJobRuntime";

function requestObject(request: unknown): Record<string, unknown> {
  return request && typeof request === "object"
    ? (request as Record<string, unknown>)
    : {};
}

function requestString(
  request: Record<string, unknown>,
  key: string,
): string | undefined {
  const value = request[key];
  return typeof value === "string" ? value : undefined;
}

function requestStringArray(
  request: Record<string, unknown>,
  key: string,
): string[] {
  const value = request[key];
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];
}

interface RecoveredJobControls {
  archives: {
    setCompressions: Dispatch<SetStateAction<Compression[]>>;
    setExtractions: Dispatch<SetStateAction<Extraction[]>>;
    removeCompression: (id: string) => void;
    removeExtraction: (id: string) => void;
  };
  copyMove: {
    setCopies: Dispatch<SetStateAction<Copy[]>>;
    setMoves: Dispatch<SetStateAction<Move[]>>;
    removeCopy: (id: string) => void;
    removeMove: (id: string) => void;
  };
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

  // De-dupes capability-install completion toasts so the attach path and the
  // terminal fallback (events stream) can never both fire for one job.
  const installToastedRef = useRef(new Set<string>());

  // Single source of truth for capability-install completion feedback. Owned by
  // this global handler (not CapabilityManagerSection) so the toast still fires
  // when the Settings dialog has been closed mid-install.
  const emitCapabilityCompletion = useCallback(
    (
      jobId: string,
      wire: string,
      result?: InstallCapabilityResult,
      errorMessage?: string,
    ) => {
      if (installToastedRef.current.has(jobId)) return;
      installToastedRef.current.add(jobId);

      const def = CAPABILITIES.find((c) => c.wire === wire) as
        | CapabilityDef
        | undefined;
      const label = def?.label ?? wire;
      // Surface an "Open …" action link on the notification for capabilities
      // that have a dedicated page (omitted for ones that don't).
      const opts = def?.route ? { meta: def.route } : undefined;

      if (errorMessage !== undefined) {
        toast.error(errorMessage || `Failed to install ${label}`, opts);
        return;
      }

      // Any successful job result (available or not) refreshes app-wide state.
      void refreshCapabilities();
      if (result?.available) {
        toast.success(`${label} installed`, opts);
      } else {
        const reason = result?.error ? `: ${result.error}` : ".";
        toast.warning(
          `${label} installed but is still unavailable${reason}`,
          opts,
        );
      }
    },
    [refreshCapabilities],
  );
  const {
    activeCompressionIdsRef,
    activeExtractionIdsRef,
    activeIndexerIdsRef,
    activeCopyIdsRef,
    activeMoveIdsRef,
    activeBackgroundJobIdsRef,
    activeFileTransferJobIdsRef,
    recoveringJobIdsRef,
    pendingLocalJobKeysRef,
    streamRefsRef,
    allocateDownloadLabelBase,
  } = runtime;
  const {
    archives: {
      setCompressions,
      setExtractions,
      removeCompression,
      removeExtraction,
    },
    copyMove: { setCopies, setMoves, removeCopy, removeMove },
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
          jobIdentityKey(job.type, job.request),
        )
      ) {
        return;
      }

      const request = requestObject(job.request);
      const progress = job.progress as ProgressFrame | undefined;
      const initialPct = Math.min(99, progress?.pct ?? 0);
      const getName = (path: string | undefined, fallback: string) => {
        const trimmed = (path ?? "").replace(/\/+$/, "");
        if (!trimmed) return fallback;
        const parts = trimmed.split("/");
        return parts[parts.length - 1] || fallback;
      };
      const getSpeed = createProgressSpeedCalculator();
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
              phase?: string;
              pct?: number;
              processed?: number;
              indeterminate?: boolean;
            }
          | undefined;
        switch (job.type) {
          case JobTypes.JOB_TYPE_FILE_UPLOAD: {
            const name = getName(requestString(request, "targetPath"), "file");
            return data?.phase === "waiting_for_client"
              ? `Upload waiting: ${name}`
              : `Uploading ${name}${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          }
          case JobTypes.JOB_TYPE_FILE_DOWNLOAD: {
            const name = getName(requestString(request, "path"), "file");
            return data?.phase === "waiting_for_client"
              ? `Download waiting: ${name}`
              : `Downloading ${name}${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          }
          case JobTypes.JOB_TYPE_FILE_ARCHIVE:
            return data?.phase === "waiting_for_client"
              ? "Archive download waiting"
              : `Preparing archive${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          case JobTypes.JOB_TYPE_FILE_CHMOD:
            return `${data?.phase === "chown" ? "Changing ownership" : "Changing permissions"}${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          case JobTypes.JOB_TYPE_FILE_DELETE: {
            const name = getName(requestString(request, "path"), "item");
            if (data?.indeterminate) {
              const processed = data.processed ?? 0;
              return `Deleting ${name} (${processed} item${processed === 1 ? "" : "s"})`;
            }
            return `Deleting ${name}${data?.pct !== undefined ? ` (${data.pct}%)` : ""}`;
          }
          case JobTypes.JOB_TYPE_DOCKER_COMPOSE:
            return (
              data?.message ??
              `Docker compose ${requestString(request, "action") ?? "operation"}`
            );
          case JobTypes.JOB_TYPE_DOCKER_INDEXER:
            return data?.files_indexed !== undefined ||
              data?.dirs_indexed !== undefined
              ? `Indexing Docker folders: ${data.files_indexed ?? 0} files, ${data.dirs_indexed ?? 0} dirs`
              : "Indexing Docker folders";
          case JobTypes.JOB_TYPE_PACKAGE_UPDATE:
            return data?.package_id
              ? `Updating ${String(data.package_id).split(";")[0]}`
              : data?.status
                ? `Updating packages: ${data.status}`
                : "Updating packages";
          case JobTypes.JOB_TYPE_STORAGE_SMART_TEST:
            return data?.message ?? "Running SMART self-test";
          case JobTypes.JOB_TYPE_SYSTEM_INSTALL_CAPABILITY: {
            const cap = requestString(request, "capability") ?? "capability";
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
        case JobTypes.JOB_TYPE_FILE_COMPRESS: {
          if (activeCompressionIdsRef.current.has(job.id)) return;
          const destination = requestString(request, "targetPath") ?? "";
          const labelBase = allocateDownloadLabelBase(
            getName(destination, "archive"),
            job.id,
          );
          activeCompressionIdsRef.current.add(job.id);
          setCompressions((prev) => [
            ...prev,
            {
              id: job.id,
              type: "compression",
              archiveName: labelBase,
              destination,
              paths: requestStringArray(request, "paths"),
              progress: initialPct,
              label: `Compressing ${labelBase} (${initialPct}%)`,
              bytes: progress?.bytes,
              total: progress?.total,
              abortController,
            },
          ]);
          attach({
            onProgress: (nextProgress) => {
              const speed = getSpeed(nextProgress.bytes);
              const pct = Math.min(99, nextProgress.pct);
              setCompressions((prev) =>
                prev.map((item) =>
                  item.id === job.id
                    ? {
                        ...item,
                        progress: Math.max(item.progress, pct),
                        label: `Compressing ${labelBase} (${Math.max(item.progress, pct)}%)`,
                        bytes: nextProgress.bytes,
                        total: nextProgress.total,
                        ...(speed !== undefined && { speed }),
                      }
                    : item,
                ),
              );
            },
            onSuccess: () => toast.success(`Created ${labelBase}`),
            onError: (error) => {
              if (!abortController.signal.aborted) {
                toast.error(
                  error instanceof Error ? error.message : "Compression failed",
                );
              }
            },
            onFinally: () => removeCompression(job.id),
          });
          break;
        }
        case JobTypes.JOB_TYPE_FILE_EXTRACT: {
          if (activeExtractionIdsRef.current.has(job.id)) return;
          const archivePath = requestString(request, "archivePath") ?? "";
          const labelBase = allocateDownloadLabelBase(
            getName(archivePath, "archive"),
            job.id,
          );
          activeExtractionIdsRef.current.add(job.id);
          setExtractions((prev) => [
            ...prev,
            {
              id: job.id,
              type: "extraction",
              archivePath,
              destination: requestString(request, "destination") ?? "",
              progress: initialPct,
              label: `Extracting ${labelBase} (${initialPct}%)`,
              bytes: progress?.bytes,
              total: progress?.total,
              abortController,
            },
          ]);
          attach({
            onProgress: (nextProgress) => {
              const speed = getSpeed(nextProgress.bytes);
              const pct = Math.min(99, nextProgress.pct);
              setExtractions((prev) =>
                prev.map((item) =>
                  item.id === job.id
                    ? {
                        ...item,
                        progress: Math.max(item.progress, pct),
                        label: `Extracting ${labelBase} (${Math.max(item.progress, pct)}%)`,
                        bytes: nextProgress.bytes,
                        total: nextProgress.total,
                        ...(speed !== undefined && { speed }),
                      }
                    : item,
                ),
              );
            },
            onSuccess: () => toast.success(`Extracted ${labelBase}`),
            onError: (error) => {
              if (!abortController.signal.aborted) {
                toast.error(
                  error instanceof Error ? error.message : "Extraction failed",
                );
              }
            },
            onFinally: () => removeExtraction(job.id),
          });
          break;
        }
        case JobTypes.JOB_TYPE_FILE_COPY:
        case JobTypes.JOB_TYPE_FILE_MOVE: {
          const isMove = job.type === JobTypes.JOB_TYPE_FILE_MOVE;
          const activeIds = isMove ? activeMoveIdsRef : activeCopyIdsRef;
          if (activeIds.current.has(job.id)) return;
          const source = requestString(request, "source") ?? "";
          const destination = requestString(request, "destination") ?? "";
          const labelBase = getName(source, "item");
          activeIds.current.add(job.id);
          if (isMove) {
            setMoves((prev) => [
              ...prev,
              {
                id: job.id,
                type: "move",
                source,
                destination,
                progress: initialPct,
                label: `Moving ${labelBase} (${initialPct}%)`,
                bytes: progress?.bytes,
                total: progress?.total,
                abortController,
              },
            ]);
          } else {
            setCopies((prev) => [
              ...prev,
              {
                id: job.id,
                type: "copy",
                source,
                destination,
                progress: initialPct,
                label: `Copying ${labelBase} (${initialPct}%)`,
                bytes: progress?.bytes,
                total: progress?.total,
                abortController,
              },
            ]);
          }
          attach({
            onProgress: (nextProgress) => {
              const speed = getSpeed(nextProgress.bytes);
              const pct = Math.min(99, nextProgress.pct);
              const update = (item: Copy | Move) => ({
                ...item,
                progress: Math.max(item.progress, pct),
                label: `${isMove ? "Moving" : "Copying"} ${labelBase} (${Math.max(item.progress, pct)}%)`,
                bytes: nextProgress.bytes,
                total: nextProgress.total,
                ...(speed !== undefined && { speed }),
              });
              if (isMove) {
                setMoves((prev) =>
                  prev.map((item) =>
                    item.id === job.id ? (update(item) as Move) : item,
                  ),
                );
              } else {
                setCopies((prev) =>
                  prev.map((item) =>
                    item.id === job.id ? (update(item) as Copy) : item,
                  ),
                );
              }
            },
            onSuccess: () =>
              toast.success(`${isMove ? "Moved" : "Copied"} ${labelBase}`),
            onError: (error) => {
              if (!abortController.signal.aborted) {
                toast.error(
                  error instanceof Error
                    ? error.message
                    : `${isMove ? "Move" : "Copy"} failed`,
                );
              }
            },
            onFinally: () => (isMove ? removeMove(job.id) : removeCopy(job.id)),
          });
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
              type: "indexer",
              path: requestString(request, "path") ?? "/",
              filesIndexed: 0,
              dirsIndexed: 0,
              totalSize: 0,
              durationMs: 0,
              currentPath: "",
              phase: "connecting",
              progress: 0,
              label: "Indexing in progress...",
              abortController,
            },
          ]);
          attach({
            onProgress: (nextProgress) => {
              const indexProgress = nextProgress as ProgressFrame & {
                files_indexed?: number;
                dirs_indexed?: number;
                current_path?: string;
                phase?: string;
              };
              setIndexers((prev) =>
                prev.map((item) => {
                  if (item.id !== job.id) return item;
                  const filesIndexed =
                    indexProgress.files_indexed ?? item.filesIndexed;
                  const dirsIndexed =
                    indexProgress.dirs_indexed ?? item.dirsIndexed;
                  const phase = indexProgress.phase ?? item.phase;
                  return {
                    ...item,
                    filesIndexed,
                    dirsIndexed,
                    currentPath: indexProgress.current_path ?? item.currentPath,
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
              const summaryResult = result as
                | {
                    files_indexed?: number;
                    dirs_indexed?: number;
                    total_size?: number;
                    duration_ms?: number;
                  }
                | undefined;
              setLastIndexerResult({
                path: requestString(request, "path") ?? "/",
                filesIndexed: summaryResult?.files_indexed ?? 0,
                dirsIndexed: summaryResult?.dirs_indexed ?? 0,
                totalSize: summaryResult?.total_size ?? 0,
                durationMs: summaryResult?.duration_ms ?? 0,
              });
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
        case JobTypes.JOB_TYPE_DOCKER_INDEXER:
        case JobTypes.JOB_TYPE_PACKAGE_UPDATE:
        case JobTypes.JOB_TYPE_STORAGE_SMART_TEST:
        case JobTypes.JOB_TYPE_SYSTEM_INSTALL_CAPABILITY:
        case JobTypes.JOB_TYPE_FILE_UPLOAD:
        case JobTypes.JOB_TYPE_FILE_DOWNLOAD:
        case JobTypes.JOB_TYPE_FILE_ARCHIVE:
        case JobTypes.JOB_TYPE_FILE_CHMOD:
        case JobTypes.JOB_TYPE_FILE_DELETE: {
          if (activeFileTransferJobIdsRef.current.has(job.id)) {
            return;
          }
          if (activeBackgroundJobIdsRef.current.has(job.id)) return;
          const initialProgress = genericProgressPct(job.progress);
          const initialMeta = genericProgressMeta(job.progress);
          activeBackgroundJobIdsRef.current.add(job.id);
          setBackgroundJobs((prev) => [
            ...prev,
            {
              id: job.id,
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
              if (job.type === JobTypes.JOB_TYPE_SYSTEM_INSTALL_CAPABILITY) {
                const cap =
                  requestString(request, "capability") ?? "capability";
                emitCapabilityCompletion(
                  job.id,
                  cap,
                  result as InstallCapabilityResult,
                );
              }
            },
            onError: (error) => {
              if (abortController.signal.aborted) return;
              // Capability install is now surfaced here (survives the Settings
              // dialog closing). storage.run_smart_test is still owned by a
              // specific page (DiskOverview) that fires its own scoped toast, so
              // skip the generic one to avoid duplicates.
              if (job.type === JobTypes.JOB_TYPE_STORAGE_SMART_TEST) {
                return;
              }
              if (job.type === JobTypes.JOB_TYPE_SYSTEM_INSTALL_CAPABILITY) {
                const cap =
                  requestString(request, "capability") ?? "capability";
                emitCapabilityCompletion(
                  job.id,
                  cap,
                  undefined,
                  error instanceof Error ? error.message : "",
                );
                return;
              }
              toast.error(
                error instanceof Error ? error.message : "Job failed",
              );
            },
            onFinally: () => removeBackgroundJob(job.id),
          });
          break;
        }
      }
    },
    [
      allocateDownloadLabelBase,
      removeCompression,
      removeExtraction,
      removeCopy,
      removeMove,
      removeIndexer,
      removeBackgroundJob,
      runStreamResult,
      emitCapabilityCompletion,
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

          // Airtight fallback: attachRecoveredJob() bails on already-terminal
          // jobs, so a capability install first observed in a terminal state
          // would never toast via the attach path. emitCapabilityCompletion is
          // de-duped, so this is a no-op when the attach path already fired.
          if (job.type === JobTypes.JOB_TYPE_SYSTEM_INSTALL_CAPABILITY) {
            const cap =
              requestString(requestObject(job.request), "capability") ??
              "capability";
            if (job.state === "failed" || job.state === "canceled") {
              emitCapabilityCompletion(
                job.id,
                cap,
                undefined,
                job.error?.message ?? "",
              );
            } else {
              emitCapabilityCompletion(
                job.id,
                cap,
                job.result as InstallCapabilityResult,
              );
            }
          }

          if (isJobLocallyHandled(job.id)) return;
          const keysFn = INVALIDATIONS_BY_JOB_TYPE[job.type];
          if (!keysFn) return;
          for (const queryKey of keysFn()) {
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
  }, [
    attachRecoveredJob,
    emitCapabilityCompletion,
    queryClient,
    streamMuxStatus,
  ]);
}
