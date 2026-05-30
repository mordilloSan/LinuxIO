import { useCallback, useState } from "react";
import { toast } from "sonner";

import type { Download } from "@/types/backgroundJobs";

import {
  isConnected,
  type JobSnapshot,
  linuxio,
  openJobAttachStream,
  openJobDataStream,
  type ProgressFrame,
} from "@/api";
import * as JobTypes from "@/constants/backgroundJobTypes";
import { useStreamResult } from "@/hooks/useStreamResult";
import {
  createProgressSpeedCalculator,
  jobIdentityKey,
} from "@/utils/backgroundJobs";

import type { BackgroundJobRuntime } from "./useBackgroundJobRuntime";

export function useDownloadJobs(runtime: BackgroundJobRuntime) {
  const [downloads, setDownloads] = useState<Download[]>([]);
  const { run: runStreamResult } = useStreamResult();
  const {
    activeFileTransferJobIdsRef,
    pendingLocalJobKeysRef,
    streamRefsRef,
    transferRatesRef,
    cancelBridgeJob,
    recordTransferRate,
    primeTransferRate,
    allocateDownloadLabelBase,
    releaseDownloadLabelBase,
  } = runtime;

  const updateDownload = useCallback(
    (
      id: string,
      updates: Partial<Omit<Download, "id" | "abortController">>,
    ) => {
      setDownloads((prev) =>
        prev.map((d) => (d.id === id ? { ...d, ...updates } : d)),
      );
    },
    [],
  );

  const removeDownload = useCallback(
    (id: string) => {
      setDownloads((prev) => prev.filter((d) => d.id !== id));
      activeFileTransferJobIdsRef.current.delete(id);
      releaseDownloadLabelBase(id);
      transferRatesRef.current.delete(id);
      streamRefsRef.current.delete(id);
    },
    [
      activeFileTransferJobIdsRef,
      releaseDownloadLabelBase,
      streamRefsRef,
      transferRatesRef,
    ],
  );

  /**
   * Stream-based download implementation.
   * Uses yamux binary streams for efficient file transfers.
   */
  const startStreamBasedDownload = useCallback(
    async (
      paths: string[],
      reqId: string,
      _downloadLabelBase: string,
      downloadJob: JobSnapshot,
      abortSignal: AbortSignal,
      formatDownloadLabel: (
        stage: string,
        options?: { percent?: number; name?: string },
      ) => string,
    ) => {
      const isSingleFile = paths.length === 1 && !paths[0].endsWith("/");
      const chunks: Uint8Array[] = [];
      const getSpeed = createProgressSpeedCalculator();
      await runStreamResult({
        open: () => openJobDataStream(downloadJob.id, 0),
        openErrorMessage: "Failed to open download stream",
        signal: abortSignal,
        onOpen: (stream) => {
          // Store stream reference for cancellation (sync ref for immediate access)
          streamRefsRef.current.set(reqId, stream);
        },
        onData: (data) => {
          chunks.push(data);
        },
        onProgress: (progress) => {
          const speed = getSpeed(progress.bytes);

          let phaseLabel: string;
          switch (progress.phase) {
            case "preparing":
              phaseLabel = "Preparing";
              break;
            case "compressing":
              phaseLabel = "Downloading (compressing)";
              break;
            case "streaming":
            default:
              phaseLabel = "Downloading";
              break;
          }

          updateDownload(reqId, {
            progress: progress.pct,
            label: formatDownloadLabel(phaseLabel, { percent: progress.pct }),
            bytes: progress.bytes,
            total: progress.total,
            ...(speed !== undefined && { speed }),
          });
        },
        closeMessage: "Stream closed before transfer completed",
        onFinally: () => {
          streamRefsRef.current.delete(reqId);
        },
      });

      const mimeType = isSingleFile
        ? "application/octet-stream"
        : "application/zip";
      return new Blob(chunks as BlobPart[], { type: mimeType });
    },
    [runStreamResult, streamRefsRef, updateDownload],
  );

  const startDownload = useCallback(
    async (paths: string[]) => {
      if (!paths.length) return;

      const isSingleFile = paths.length === 1 && !paths[0].endsWith("/");
      let reqId: string = crypto.randomUUID();
      const abortController = new AbortController();

      const sanitizeLabelBase = (path: string) => {
        const trimmed = path.replace(/\/+$/, "");
        if (!trimmed) {
          return "download";
        }
        const segments = trimmed.split("/");
        return segments[segments.length - 1] || "download";
      };
      const candidateLabelBase =
        paths.length === 1 ? sanitizeLabelBase(paths[0]) : "download.zip";
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

      const pendingKey = isSingleFile
        ? jobIdentityKey(JobTypes.JOB_TYPE_FILE_DOWNLOAD, { path: paths[0] })
        : jobIdentityKey(JobTypes.JOB_TYPE_FILE_ARCHIVE, {
            format: "zip",
            paths,
          });
      pendingLocalJobKeysRef.current.add(pendingKey);
      let pendingKeyHeld = true;

      try {
        const activeDownloadJob = isSingleFile
          ? await linuxio.filebrowser.download(paths[0])
          : await linuxio.filebrowser.archive({ format: "zip", paths });
        activeFileTransferJobIdsRef.current.add(activeDownloadJob.id);
        pendingLocalJobKeysRef.current.delete(pendingKey);
        pendingKeyHeld = false;
        releaseDownloadLabelBase(reqId);
        reqId = activeDownloadJob.id;
        downloadLabelBase = allocateDownloadLabelBase(
          candidateLabelBase,
          reqId,
        );

        const download: Download = {
          id: reqId,
          type: "download",
          jobId: activeDownloadJob.id,
          paths,
          progress: 0,
          label: formatDownloadLabel("Preparing", { percent: 0 }),
          speed: undefined,
          abortController,
        };

        setDownloads((prev) => [...prev, download]);
        const getJobSpeed = createProgressSpeedCalculator();
        void runStreamResult<unknown, ProgressFrame>({
          open: () => openJobAttachStream(activeDownloadJob.id),
          signal: abortController.signal,
          closeOnAbort: "none",
          openErrorMessage: "Failed to attach download job",
          closeMessage: "Download job stream closed unexpectedly",
          onProgress: (progress) => {
            const speed = getJobSpeed(progress.bytes);
            let phaseLabel: string;
            switch (progress.phase) {
              case "preparing":
                phaseLabel = "Preparing";
                break;
              case "compressing":
                phaseLabel = "Downloading (compressing)";
                break;
              case "waiting_for_client":
                phaseLabel = "Download waiting";
                break;
              case "streaming":
              default:
                phaseLabel = "Downloading";
                break;
            }
            updateDownload(reqId, {
              progress: progress.pct,
              label: formatDownloadLabel(phaseLabel, {
                percent: progress.pct,
              }),
              bytes: progress.bytes,
              total: progress.total,
              ...(speed !== undefined && { speed }),
            });
          },
          onError: (error) => {
            if (!abortController.signal.aborted) {
              console.debug("Download job attachment failed", error);
            }
          },
        });
        primeTransferRate(reqId, 0);
        const blob = await startStreamBasedDownload(
          paths,
          reqId,
          downloadLabelBase,
          activeDownloadJob,
          abortController.signal,
          formatDownloadLabel,
        );

        updateDownload(reqId, {
          progress: 100,
          label: formatDownloadLabel("Downloaded", {
            name: downloadLabelBase,
          }),
          speed: undefined,
        });
        recordTransferRate(reqId, undefined);

        // Trigger browser download
        const fileName = downloadLabelBase;
        const blobUrl = window.URL.createObjectURL(blob);
        const link = document.createElement("a");
        link.href = blobUrl;
        link.download = fileName;
        document.body.appendChild(link);
        link.click();
        link.remove();
        window.URL.revokeObjectURL(blobUrl);

        toast.success(
          formatDownloadLabel("Downloaded", { name: downloadLabelBase }),
        );
        setTimeout(() => removeDownload(reqId), 1000);
      } catch (err: any) {
        if (pendingKeyHeld) {
          pendingLocalJobKeysRef.current.delete(pendingKey);
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
      activeFileTransferJobIdsRef,
      allocateDownloadLabelBase,
      pendingLocalJobKeysRef,
      primeTransferRate,
      recordTransferRate,
      releaseDownloadLabelBase,
      removeDownload,
      runStreamResult,
      startStreamBasedDownload,
      updateDownload,
    ],
  );

  const cancelDownload = useCallback(
    (id: string) => {
      const download = downloads.find((d) => d.id === id);
      if (download) {
        download.abortController.abort();
        // Abort stream if using stream-based download (RST for immediate cancel)
        // Use ref first (synchronous) then fallback to state
        const stream = streamRefsRef.current.get(id) || download.stream;
        if (stream) {
          stream.abort(); // Use abort() instead of close() for immediate cancellation
          streamRefsRef.current.delete(id);
        }
        if (download.jobId) {
          cancelBridgeJob(download.jobId);
        }
        toast.info("Download cancelled");
        removeDownload(id);
      }
    },
    [downloads, cancelBridgeJob, removeDownload, streamRefsRef],
  );

  return {
    downloads,
    startDownload,
    cancelDownload,
  };
}
