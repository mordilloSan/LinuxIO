import { useCallback, useState } from "react";
import { toast } from "sonner";

import type { Upload } from "@/types/backgroundJobs";

import {
  bindStreamHandlers,
  type FileUploadBatchRequest,
  isConnected,
  type JobSnapshot,
  linuxio,
  openJobDataStream,
  type ProgressFrame,
  STREAM_MULTIPLEXER_CONFIG,
} from "@/api";
import * as JobTypes from "@/constants/backgroundJobTypes";
import { useLatestRef } from "@/hooks/useLatestRef";
import { jobIdentityKey } from "@/utils/backgroundJobs";

import type { BackgroundJobRuntime } from "./useBackgroundJobRuntime";

// Progress frames of filebrowser.upload_batch: aggregate bytes plus how many
// manifest files the bridge has fully processed.
interface BatchUploadProgressFrame extends ProgressFrame {
  filesDone?: number;
  filesTotal?: number;
}

// Result payload of filebrowser.upload_batch (batchResult shape).
interface BatchUploadResult {
  total?: number;
  succeeded?: number;
  failed?: { path: string; error: string }[];
}

interface BatchStreamOutcome {
  success: boolean;
  cancelled?: boolean;
  error?: string;
  result?: BatchUploadResult;
}

interface UploadFileEntry {
  file: File;
  relativePath: string;
}

export function useUploadJobs(
  runtime: BackgroundJobRuntime,
  // Getter, not a value: reading the chunk size at upload start keeps
  // BackgroundJobsProvider (and the actions context identity) decoupled from
  // config changes.
  getChunkSize: () => number,
) {
  const [uploads, setUploads] = useState<Upload[]>([]);
  const uploadsRef = useLatestRef(uploads);
  const {
    activeFileTransferJobIdsRef,
    pendingLocalJobKeysRef,
    streamRefsRef,
    transferRatesRef,
    cancelBridgeJob,
    recordTransferRate,
    primeTransferRate,
  } = runtime;

  const updateUpload = useCallback(
    (
      id: string,
      updates: Partial<Omit<Upload, "id" | "type" | "abortController">>,
    ) => {
      setUploads((prev) =>
        prev.map((u) => (u.id === id ? { ...u, ...updates } : u)),
      );
    },
    [],
  );

  const removeUpload = useCallback(
    (id: string) => {
      setUploads((prev) => prev.filter((u) => u.id !== id));
      transferRatesRef.current.delete(id);
      streamRefsRef.current.delete(id);
    },
    [streamRefsRef, transferRatesRef],
  );

  /**
   * Job-backed batch upload: one bridge job and one data stream carry the
   * whole selection. The manifest (paths + sizes) travels in the job request,
   * so the stream is just every file's bytes back-to-back in manifest order —
   * the bridge splits them on the manifest boundaries.
   */
  const uploadBatchViaStream = useCallback(
    async (
      files: UploadFileEntry[],
      directories: string[],
      destination: string,
      overwrite: boolean,
      uploadId: string,
      onProgress: (progress: BatchUploadProgressFrame) => void,
      abortSignal: AbortSignal,
    ): Promise<BatchStreamOutcome> => {
      if (!isConnected()) {
        return { success: false, error: "Stream connection not ready" };
      }

      const chunkSize = getChunkSize();
      const uploadWindowSize =
        chunkSize * STREAM_MULTIPLEXER_CONFIG.uploadWindowChunks;

      const request: FileUploadBatchRequest = {
        destination,
        files: files.map(({ file, relativePath }) => ({
          path: relativePath,
          size: String(file.size),
        })),
        // Falsy fields omitted so the identity key matches the Go-marshaled
        // request (`directories` and `overwrite` are omitempty on the wire).
        ...(directories.length ? { directories } : {}),
        ...(overwrite ? { overwrite: true } : {}),
      };

      const pendingUploadKey = jobIdentityKey(
        JobTypes.JOB_TYPE_FILE_UPLOAD_BATCH,
        request,
      );
      pendingLocalJobKeysRef.current.add(pendingUploadKey);

      let job: JobSnapshot;
      try {
        job = await linuxio.filebrowser.upload_batch(request);
      } catch (error) {
        pendingLocalJobKeysRef.current.delete(pendingUploadKey);
        return {
          success: false,
          error:
            error instanceof Error ? error.message : "Failed to start upload",
        };
      }

      updateUpload(uploadId, { jobId: job.id });
      activeFileTransferJobIdsRef.current.add(job.id);
      pendingLocalJobKeysRef.current.delete(pendingUploadKey);

      const stream = openJobDataStream(job.id, 0);
      if (!stream) {
        activeFileTransferJobIdsRef.current.delete(job.id);
        cancelBridgeJob(job.id);
        return { success: false, error: "Failed to open upload stream" };
      }

      // Store stream reference for cancellation (sync ref for immediate access)
      streamRefsRef.current.set(uploadId, stream);

      return new Promise<BatchStreamOutcome>((resolve) => {
        let settled = false;
        let resultReceived = false;

        // Sequential pump over the manifest with flow control: the bridge's
        // progress frames ack aggregate bytes, gating how far ahead we send.
        const reader = new FileReader();
        let fileIndex = 0;
        let offsetInFile = 0;
        let bytesSent = 0;
        let bytesAcked = 0;
        let pendingSend = false;

        const resolveSafe = (outcome: BatchStreamOutcome) => {
          if (settled) return;
          settled = true;
          unbind();
          streamRefsRef.current.delete(uploadId);
          activeFileTransferJobIdsRef.current.delete(job.id);
          // Free the bridge job slot when abandoning an incomplete upload. On a
          // stream error/early close the bridge parks the job in
          // `waiting_for_client` (to allow resume) rather than failing it, so
          // without an explicit cancel the job lingers and holds one of the
          // limited per-user upload slots. (User-cancelled uploads are already
          // cancelled by cancelUpload.)
          if (!outcome.success && !outcome.cancelled) {
            cancelBridgeJob(job.id);
          }
          setUploads((prev) =>
            prev.map((u) =>
              u.id === uploadId ? { ...u, stream: null, jobId: undefined } : u,
            ),
          );
          resolve(outcome);
        };

        const sendNextChunk = () => {
          // Stop if stream was closed/aborted
          if (stream.status !== "open") {
            return;
          }

          // Zero-size files contribute no bytes; the bridge finalizes them
          // from the manifest alone.
          while (fileIndex < files.length && files[fileIndex].file.size === 0) {
            fileIndex += 1;
          }

          if (fileIndex >= files.length) {
            // Done sending - close stream to signal completion
            stream.close();
            return;
          }

          // Flow control: wait if window is full
          if (bytesSent - bytesAcked >= uploadWindowSize) {
            pendingSend = true;
            return; // Will resume when onProgress fires
          }

          const current = files[fileIndex].file;
          const slice = current.slice(offsetInFile, offsetInFile + chunkSize);
          reader.readAsArrayBuffer(slice);
        };

        const unbind = bindStreamHandlers(stream, {
          onProgress: (progress: BatchUploadProgressFrame) => {
            bytesAcked = progress.bytes;
            onProgress(progress);

            // Window opened - resume sending if we were waiting
            if (pendingSend && bytesSent - bytesAcked < uploadWindowSize) {
              pendingSend = false;
              sendNextChunk();
            }
          },
          onResult: (result) => {
            resultReceived = true;
            // Check if cancelled - even if bridge reports success, user cancelled
            if (abortSignal.aborted) {
              resolveSafe({ success: false, cancelled: true });
              return;
            }
            if (result.status === "ok") {
              resolveSafe({
                success: true,
                result: (result.data ?? undefined) as
                  | BatchUploadResult
                  | undefined,
              });
            } else {
              resolveSafe({
                success: false,
                error: result.error || "Upload failed",
              });
            }
          },
          onClose: () => {
            if (resultReceived) {
              return;
            }
            if (abortSignal.aborted) {
              resolveSafe({ success: false, cancelled: true });
              return;
            }
            resolveSafe({
              success: false,
              error: "Stream closed before upload completed",
            });
          },
        });

        reader.onload = () => {
          // Stop if stream was closed/aborted
          if (stream.status !== "open") {
            return;
          }

          if (!reader.result) return;

          const chunk = new Uint8Array(reader.result as ArrayBuffer);
          stream.write(chunk);
          bytesSent += chunk.length;
          offsetInFile += chunk.length;
          if (offsetInFile >= files[fileIndex].file.size) {
            fileIndex += 1;
            offsetInFile = 0;
          }

          // Send next chunk (will check window)
          sendNextChunk();
        };

        reader.onerror = () => {
          // The bridge expects this file's bytes next on the stream, so an
          // unreadable file cannot be skipped client-side — abort the batch.
          // Files already finalized server-side stay in place.
          const name =
            files[fileIndex]?.relativePath ||
            files[fileIndex]?.file.name ||
            "file";
          stream.abort();
          resolveSafe({ success: false, error: `Failed to read ${name}` });
        };

        // Start sending
        sendNextChunk();
      });
    },
    [
      activeFileTransferJobIdsRef,
      cancelBridgeJob,
      getChunkSize,
      pendingLocalJobKeysRef,
      streamRefsRef,
      updateUpload,
    ],
  );

  const startUpload = useCallback(
    async (
      entries: {
        file?: File;
        relativePath: string;
        isDirectory: boolean;
      }[],
      targetPath: string,
      // Callers resolve collisions with the user first (conflict prompt) and
      // pass overwrite only when the user explicitly chose it; without it the
      // bridge skips existing destinations and reports them as failures.
      overwrite?: boolean,
    ) => {
      if (!entries.length) {
        return { uploaded: 0, failures: [] };
      }

      const uploadId = crypto.randomUUID();
      const abortController = new AbortController();
      const directories = entries
        .filter((item) => item.isDirectory)
        .map((item) => item.relativePath.replace(/\/+$/, ""))
        .filter(Boolean);
      const files = entries.filter(
        (item): item is UploadFileEntry & { isDirectory: boolean } =>
          !item.isDirectory && !!item.file,
      );
      const totalFiles = directories.length + files.length;
      const totalBytes = files.reduce((sum, item) => sum + item.file.size, 0);

      const describeEntry = (
        entry: (typeof entries)[number] | undefined,
      ): string => {
        if (!entry) return "";
        const trimmed = entry.relativePath.replace(/\/+$/, "");
        if (trimmed) {
          return trimmed;
        }
        if (entry.file?.name) {
          return entry.file.name;
        }
        return entry.isDirectory ? "folder" : "file";
      };

      const singleEntrySource =
        entries.find((item) => item.isDirectory) ?? files[0] ?? entries[0];
      const isSingleUpload = totalFiles === 1;
      const singleEntryLabel = isSingleUpload
        ? describeEntry(singleEntrySource) || "item"
        : "";

      const initialLabel =
        isSingleUpload && singleEntryLabel
          ? `Uploading ${singleEntryLabel} (0%)`
          : files.length > 0
            ? `Uploading 0/${files.length} files`
            : `Creating ${directories.length} folder${directories.length === 1 ? "" : "s"}`;

      const upload: Upload = {
        id: uploadId,
        type: "upload",
        totalFiles,
        completedFiles: 0,
        currentFile: "",
        progress: 0,
        label: initialLabel,
        displayName:
          isSingleUpload && singleEntryLabel ? singleEntryLabel : undefined,
        speed: undefined,
        abortController,
      };

      setUploads((prev) => [...prev, upload]);
      primeTransferRate(uploadId, 0);

      const failures: { path: string; message: string }[] = [];
      let uploaded = 0;

      const onProgress = (progress: BatchUploadProgressFrame) => {
        const filesDone = progress.filesDone ?? 0;
        const pct =
          totalBytes > 0
            ? Math.min(100, Math.round((progress.bytes / totalBytes) * 100))
            : progress.pct;
        const speed = recordTransferRate(uploadId, progress.bytes);
        const label =
          isSingleUpload && singleEntryLabel
            ? `Uploading ${singleEntryLabel} (${pct}%)`
            : `Uploading ${Math.min(filesDone, files.length)}/${files.length} files (${pct}%)`;
        updateUpload(uploadId, {
          completedFiles: filesDone,
          progress: pct,
          label,
          ...(speed !== undefined && { speed }),
        });
      };

      try {
        const outcome = await uploadBatchViaStream(
          files,
          directories,
          targetPath,
          overwrite ?? false,
          uploadId,
          onProgress,
          abortController.signal,
        );

        if (outcome.cancelled) {
          recordTransferRate(uploadId, undefined);
          removeUpload(uploadId);
          return { uploaded, failures };
        }

        if (outcome.success) {
          uploaded = outcome.result?.succeeded ?? totalFiles;
          for (const failure of outcome.result?.failed ?? []) {
            failures.push({ path: failure.path, message: failure.error });
          }
        } else if (outcome.error) {
          failures.push({ path: targetPath, message: outcome.error });
        }

        if (uploaded > 0 && !abortController.signal.aborted) {
          toast.success(
            `Uploaded ${uploaded} item${uploaded === 1 ? "" : "s"} to ${targetPath}`,
          );
        }

        if (failures.length > 0) {
          const first = failures[0];
          toast.error(
            `Failed to upload ${failures.length} item${failures.length === 1 ? "" : "s"}: ${first.message}`,
          );
        }

        const completionLabel =
          totalFiles === 1 && singleEntryLabel
            ? `Uploaded ${singleEntryLabel}`
            : `Uploaded ${uploaded}/${totalFiles} files`;
        updateUpload(uploadId, {
          progress: 100,
          label: completionLabel,
          speed: undefined,
        });
        recordTransferRate(uploadId, undefined);
        setTimeout(() => removeUpload(uploadId), 1000);
        return { uploaded, failures };
      } catch (err: any) {
        if (err.name === "CanceledError") {
          console.log("Upload cancelled by user");
        } else {
          console.error("Upload failed", err);
          toast.error("Upload failed");
        }
        recordTransferRate(uploadId, undefined);
        removeUpload(uploadId);
        return { uploaded, failures };
      }
    },
    [
      primeTransferRate,
      recordTransferRate,
      removeUpload,
      updateUpload,
      uploadBatchViaStream,
    ],
  );

  const cancelUpload = useCallback(
    (id: string) => {
      const upload = uploadsRef.current.find((u) => u.id === id);
      if (upload) {
        // Abort stream if using stream-based upload (RST for immediate cancel)
        // Use ref first (synchronous) then fallback to state
        const stream = streamRefsRef.current.get(id) || upload.stream;
        if (stream) {
          stream.abort(); // RST for immediate cancellation
          streamRefsRef.current.delete(id);
        }
        if (upload.jobId) {
          activeFileTransferJobIdsRef.current.delete(upload.jobId);
          cancelBridgeJob(upload.jobId);
        }
        upload.abortController.abort();
        toast.info("Upload cancelled");
        removeUpload(id);
      }
    },
    [
      activeFileTransferJobIdsRef,
      cancelBridgeJob,
      removeUpload,
      streamRefsRef,
      uploadsRef,
    ],
  );

  return {
    uploads,
    startUpload,
    cancelUpload,
  };
}
