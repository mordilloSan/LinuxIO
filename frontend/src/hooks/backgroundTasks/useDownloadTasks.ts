import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import {
  type FileProgress,
  isConnected,
  type TaskProgress,
  linuxio,
  openTaskWatchStream,
} from "@/api";
import * as TaskTypes from "@/constants/backgroundTaskTypes";
import { useLatestRef } from "@/hooks/useLatestRef";
import { useStreamResult } from "@/hooks/useStreamResult";
import type { Download } from "@/types/backgroundTasks";
import {
  createProgressSpeedCalculator,
  taskIdentityKey,
} from "@/utils/backgroundTasks";
import {
  triggerNativeArchiveDownload,
  triggerNativeFileDownload,
} from "@/utils/nativeDownload";
import { isDirectoryPath } from "@/utils/path";

import type { BackgroundTaskRuntime } from "./useBackgroundTaskRuntime";

export function useDownloadTasks(runtime: BackgroundTaskRuntime) {
  const [downloads, setDownloads] = useState<Download[]>([]);
  const downloadsRef = useLatestRef(downloads);
  const { run: runStreamResult } = useStreamResult();
  const {
    activeFileTransferTaskIdsRef,
    pendingLocalTaskKeysRef,
    streamRefsRef,
    transferRatesRef,
    cancelBridgeTask,
    recordTransferRate,
    primeTransferRate,
    allocateDownloadLabelBase,
    releaseDownloadLabelBase,
  } = runtime;

  // Progress frames arrive far faster than the UI needs to repaint (a fast
  // download can push hundreds a second); coalesce them into one setState
  // per animation frame instead of one per frame received.
  const pendingProgressRef = useRef<
    Map<string, Partial<Omit<Download, "id" | "abortController">>>
  >(new Map());
  const progressFrameRef = useRef<number | null>(null);

  const flushDownloadProgress = useCallback(() => {
    if (progressFrameRef.current !== null) {
      window.cancelAnimationFrame(progressFrameRef.current);
      progressFrameRef.current = null;
    }
    const pending = pendingProgressRef.current;
    if (pending.size === 0) return;
    pendingProgressRef.current = new Map();
    setDownloads((prev) =>
      prev.map((d) => {
        const patch = pending.get(d.id);
        return patch ? { ...d, ...patch } : d;
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

  const hideDownload = useCallback(
    (id: string) => {
      setDownloads((prev) => prev.filter((d) => d.id !== id));
      releaseDownloadLabelBase(id);
      transferRatesRef.current.delete(id);
    },
    [releaseDownloadLabelBase, transferRatesRef],
  );

  const removeDownload = useCallback(
    (id: string) => {
      hideDownload(id);
      activeFileTransferTaskIdsRef.current.delete(id);
      streamRefsRef.current.delete(id);
    },
    [activeFileTransferTaskIdsRef, hideDownload, streamRefsRef],
  );

  const startDownload = useCallback(
    async (paths: string[]) => {
      if (!paths.length) return;

      const isSingleFile = paths.length === 1 && !isDirectoryPath(paths[0]);

      const sanitizeLabelBase = (path: string) => {
        const trimmed = path.replace(/\/+$/, "");
        if (!trimmed) {
          return "download";
        }
        const segments = trimmed.split("/");
        return segments[segments.length - 1] || "download";
      };
      if (isSingleFile) {
        triggerNativeFileDownload(paths[0]);
        return;
      }

      const selectedName =
        paths.length === 1 ? sanitizeLabelBase(paths[0]) : "download";
      let reqId: string = crypto.randomUUID();
      const abortController = new AbortController();
      const candidateLabelBase = `${selectedName}.zip`;
      let downloadLabelBase = allocateDownloadLabelBase(
        candidateLabelBase,
        reqId,
      );

      const formatDownloadLabel = (
        stage: string,
        options: { percent?: number; name?: string } = {},
      ) => {
        const targetName = options.name ?? downloadLabelBase;
        const base = `${stage} ${targetName}`;
        if (options.percent !== undefined) {
          return `${base} (${options.percent}%)`;
        }
        return base;
      };

      if (!isConnected()) {
        toast.error("Stream connection not ready");
        removeDownload(reqId);
        return;
      }

      const pendingKey = taskIdentityKey(TaskTypes.TASK_TYPE_FILE_ARCHIVE, [
        "zip",
        ...paths,
      ]);
      pendingLocalTaskKeysRef.current.add(pendingKey);
      let pendingKeyHeld = true;

      try {
        const activeDownloadTask = await linuxio.filebrowser.archive({
          format: "zip",
          paths,
        });
        activeFileTransferTaskIdsRef.current.add(activeDownloadTask.id);
        pendingLocalTaskKeysRef.current.delete(pendingKey);
        pendingKeyHeld = false;
        releaseDownloadLabelBase(reqId);
        reqId = activeDownloadTask.id;
        downloadLabelBase = allocateDownloadLabelBase(
          candidateLabelBase,
          reqId,
        );

        const download: Download = {
          id: reqId,
          type: "download",
          taskId: activeDownloadTask.id,
          paths,
          progress: 0,
          label: formatDownloadLabel("Preparing", { percent: 0 }),
          speed: undefined,
          abortController,
        };

        setDownloads((prev) => [...prev, download]);
        // The watch stream covers archive preparation and tells us when the
        // server is ready for the browser-managed HTTP download.
        let browserDownloadStarted = false;
        const handoffToBrowser = () => {
          if (browserDownloadStarted || abortController.signal.aborted) return;
          browserDownloadStarted = true;
          triggerNativeArchiveDownload(activeDownloadTask.id);
          // Compression is LinuxIO's concern; once the browser owns the bytes,
          // keep only the invisible watch that prevents task recovery and
          // releases server-side bookkeeping at terminal completion.
          hideDownload(reqId);
        };
        const getTaskSpeed = createProgressSpeedCalculator();
        primeTransferRate(reqId, 0);
        void runStreamResult<void, TaskProgress<FileProgress>>({
          open: () => openTaskWatchStream(activeDownloadTask.id),
          signal: abortController.signal,
          closeOnAbort: "none",
          openErrorMessage: "Failed to watch download task",
          closeMessage: "Download task stream closed unexpectedly",
          onOpen: (stream) => {
            streamRefsRef.current.set(reqId, stream);
          },
          onProgress: (progress) => {
            const detail = progress.detail;
            const phase = progress.phase ?? detail?.phase;
            if (phase === "waiting_for_client") handoffToBrowser();
            if (browserDownloadStarted) {
              // The item is about to be (or already was) hidden; drop any
              // queued frame instead of flushing a render for a row that's
              // gone, and let the flush before terminal handling below cover
              // any completion event still racing in.
              pendingProgressRef.current.delete(reqId);
              return;
            }
            if (!detail) return;
            const speed = getTaskSpeed(detail.bytes);
            const phaseLabel =
              phase === "preparing" ? "Preparing" : "Compressing";
            const percentage = progress.percentage ?? detail.pct;
            const indeterminate = detail.indeterminate === true;
            pendingProgressRef.current.set(reqId, {
              progress: percentage,
              label: formatDownloadLabel(
                phaseLabel,
                indeterminate ? {} : { percent: percentage },
              ),
              indeterminate,
              bytes: detail.bytes,
              total: detail.total,
              ...(speed !== undefined && { speed }),
            });
            if (progressFrameRef.current === null) {
              progressFrameRef.current = window.requestAnimationFrame(
                flushDownloadProgress,
              );
            }
          },
          onSuccess: () => {
            // Flush any queued progress before the terminal update so a task
            // never briefly shows stale progress (or none) after completion,
            // and so a late rAF can't fire after removal and re-add it.
            flushDownloadProgress();
            recordTransferRate(reqId, undefined);
            removeDownload(reqId);
          },
          onError: (error) => {
            if (!abortController.signal.aborted) {
              flushDownloadProgress();
              console.debug("Download task watch failed", error);
              if (!browserDownloadStarted) {
                toast.error(
                  error instanceof Error ? error.message : "Download failed",
                );
              }
              recordTransferRate(reqId, undefined);
              removeDownload(reqId);
            }
          },
        });
        // A task can already be ready when its creation response arrives. Do
        // this after the watch is attached so no waiting_for_client event can
        // race past us, while still avoiding any wait for task data here.
        const initialPhase =
          activeDownloadTask.progress?.phase ??
          (activeDownloadTask.progress?.detail as FileProgress | undefined)
            ?.phase;
        if (initialPhase === "waiting_for_client") handoffToBrowser();
      } catch (err: any) {
        if (pendingKeyHeld) {
          pendingLocalTaskKeysRef.current.delete(pendingKey);
        }
        if (err?.name === "AbortError" || err?.name === "CanceledError") {
          console.log("Download cancelled by user");
        } else {
          console.error("Download failed", err);
          const message = err?.message || "Download failed";
          toast.error(message);
        }
        recordTransferRate(reqId, undefined);
        removeDownload(reqId);
      }
    },
    [
      activeFileTransferTaskIdsRef,
      allocateDownloadLabelBase,
      flushDownloadProgress,
      pendingLocalTaskKeysRef,
      primeTransferRate,
      recordTransferRate,
      releaseDownloadLabelBase,
      hideDownload,
      removeDownload,
      runStreamResult,
      streamRefsRef,
    ],
  );

  const cancelDownload = useCallback(
    (id: string) => {
      const download = downloadsRef.current.find((d) => d.id === id);
      if (download) {
        download.abortController.abort();
        // Abort the task watch immediately; tasks.cancel below owns the actual
        // bridge-task cancellation and closes any active HTTP data stream.
        const stream = streamRefsRef.current.get(id) || download.stream;
        if (stream) {
          stream.abort();
          streamRefsRef.current.delete(id);
        }
        if (download.taskId) {
          cancelBridgeTask(download.taskId);
        }
        toast.info("Download cancelled");
        removeDownload(id);
      }
    },
    [downloadsRef, cancelBridgeTask, removeDownload, streamRefsRef],
  );

  return {
    downloads,
    startDownload,
    cancelDownload,
  };
}
