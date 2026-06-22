import { useCallback, useState } from "react";
import { toast } from "sonner";

import type { Upload } from "@/types/backgroundJobs";

import {
  bindStreamHandlers,
  isConnected,
  type JobSnapshot,
  linuxio,
  openJobDataStream,
  type ProgressFrame,
} from "@/api";
import * as JobTypes from "@/constants/backgroundJobTypes";
import { jobIdentityKey } from "@/utils/backgroundJobs";
import { ensureTrailingSlash, joinPath } from "@/utils/path";

import type { BackgroundJobRuntime } from "./useBackgroundJobRuntime";

export function useUploadJobs(
  runtime: BackgroundJobRuntime,
  {
    chunkSize,
    uploadWindowSize,
  }: { chunkSize: number; uploadWindowSize: number },
) {
  const [uploads, setUploads] = useState<Upload[]>([]);
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
   * Job-backed single file upload implementation.
   * The job owns progress and the server-side partial file; this stream only
   * attaches browser-owned bytes while the frontend is connected.
   */
  const uploadFileViaStream = useCallback(
    async (
      file: File,
      targetPath: string,
      uploadId: string,
      onProgress: (loaded: number, total: number) => void,
      abortSignal: AbortSignal,
    ): Promise<{ success: boolean; error?: string; cancelled?: boolean }> => {
      if (!isConnected()) {
        return { success: false, error: "Stream connection not ready" };
      }

      const pendingUploadKey = jobIdentityKey(JobTypes.JOB_TYPE_FILE_UPLOAD, {
        targetPath,
        size: String(file.size),
      });
      pendingLocalJobKeysRef.current.add(pendingUploadKey);

      let job: JobSnapshot;
      try {
        job = await linuxio.filebrowser.upload({
          targetPath,
          size: String(file.size),
        });
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

      return new Promise<{
        success: boolean;
        error?: string;
        cancelled?: boolean;
      }>((resolve) => {
        let settled = false;
        let resultReceived = false;

        // Flow control: track bytes in flight to allow meaningful cancellation
        const reader = new FileReader();
        let offset = 0;
        let bytesSent = 0;
        let bytesAcked = 0;
        let pendingSend = false;

        const resolveSafe = (result: {
          success: boolean;
          error?: string;
          cancelled?: boolean;
        }) => {
          if (settled) return;
          settled = true;
          unbind();
          streamRefsRef.current.delete(uploadId);
          activeFileTransferJobIdsRef.current.delete(job.id);
          setUploads((prev) =>
            prev.map((u) =>
              u.id === uploadId ? { ...u, stream: null, jobId: undefined } : u,
            ),
          );
          resolve(result);
        };

        const sendNextChunk = () => {
          // Stop if stream was closed/aborted
          if (stream.status !== "open") {
            return;
          }

          if (offset >= file.size) {
            // Done sending - close stream to signal completion
            stream.close();
            return;
          }

          // Flow control: wait if window is full (max ~2 chunks in flight)
          if (bytesSent - bytesAcked >= uploadWindowSize) {
            pendingSend = true;
            return; // Will resume when onProgress fires
          }

          const slice = file.slice(offset, offset + chunkSize);
          reader.readAsArrayBuffer(slice);
        };

        const unbind = bindStreamHandlers(stream, {
          onProgress: (progress: ProgressFrame) => {
            bytesAcked = progress.bytes;
            onProgress(progress.bytes, progress.total);

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
              resolveSafe({ success: true });
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
          offset += chunk.length;

          // Send next chunk (will check window)
          sendNextChunk();
        };

        reader.onerror = () => {
          stream.close();
          resolveSafe({ success: false, error: "Failed to read file" });
        };

        // Start sending
        sendNextChunk();
      });
    },
    [
      activeFileTransferJobIdsRef,
      cancelBridgeJob,
      chunkSize,
      pendingLocalJobKeysRef,
      streamRefsRef,
      updateUpload,
      uploadWindowSize,
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
      override?: boolean,
    ) => {
      if (!entries.length) {
        return { conflicts: [], uploaded: 0, failures: [] };
      }

      const uploadId = crypto.randomUUID();
      const abortController = new AbortController();
      const directories = entries
        .filter((item) => item.isDirectory)
        .sort(
          (a, b) =>
            a.relativePath.split("/").length - b.relativePath.split("/").length,
        );
      const files = entries.filter((item) => !item.isDirectory);
      const totalFiles = directories.length + files.length;

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

      const singleEntrySource = directories[0] ?? files[0] ?? entries[0];
      const isSingleUpload = totalFiles === 1;
      const singleEntryLabel = isSingleUpload
        ? describeEntry(singleEntrySource) || "item"
        : "";

      const upload: Upload = {
        id: uploadId,
        type: "upload",
        totalFiles,
        completedFiles: 0,
        currentFile: "",
        progress: 0,
        label:
          isSingleUpload && singleEntryLabel
            ? `Uploading ${singleEntryLabel} (0%)`
            : `Uploading 0/${totalFiles} files`,
        displayName:
          isSingleUpload && singleEntryLabel ? singleEntryLabel : undefined,
        speed: undefined,
        abortController,
      };

      setUploads((prev) => [...prev, upload]);
      primeTransferRate(uploadId, 0);

      const conflicts: typeof entries = [];
      let uploaded = 0;
      let uploadedBytesTotal = 0;
      const failures: { path: string; message: string }[] = [];

      const buildTargetPath = (base: string, relative: string) =>
        joinPath(base, relative);

      try {
        // Create directories first
        for (const { relativePath } of directories) {
          if (abortController.signal.aborted) break;

          const targetBase = buildTargetPath(targetPath, relativePath);
          const dirPath = ensureTrailingSlash(targetBase);

          updateUpload(uploadId, {
            currentFile: relativePath,
            completedFiles: uploaded,
            progress: Math.round((uploaded / totalFiles) * 100),
            label: `Creating folder ${uploaded + 1}/${totalFiles}`,
          });

          try {
            // Check if aborted before making the call
            if (abortController.signal.aborted) break;
            await linuxio.filebrowser.resource_post({
              path: dirPath,
              override: override || undefined,
            });
            uploaded += 1;
          } catch (err: any) {
            if (abortController.signal.aborted) break;
            // 409 conflict - folder already exists
            if (err.code === 409 && !override) {
              continue;
            }
            const message = err.message || "Failed to create folder";
            failures.push({ path: relativePath, message });
          }
        }

        // Upload files in parallel with concurrency limit
        const fileProgress = new Map<number, number>(); // Track progress per file index
        const fileBytes = new Map<number, number>(); // Track bytes per file for speed calculation
        let fileIndex = 0;

        const updateOverallProgress = (
          bytesLoaded?: number,
          fileIdx?: number,
        ) => {
          // Track bytes for speed calculation
          if (bytesLoaded !== undefined && fileIdx !== undefined) {
            fileBytes.set(fileIdx, bytesLoaded);
          }

          // Calculate total bytes uploaded so far
          let totalBytesUploaded = uploadedBytesTotal;
          fileBytes.forEach((bytes) => {
            totalBytesUploaded += bytes;
          });

          // Calculate speed
          const speed = recordTransferRate(uploadId, totalBytesUploaded);

          // Sum progress of all files (completed = 100, in-progress = their %, pending = 0)
          let totalProgress = uploaded * 100; // Completed files contribute 100% each
          fileProgress.forEach((pct) => {
            totalProgress += pct;
          });
          const overallPct = Math.round(totalProgress / files.length);
          const activeCount = fileProgress.size;
          const label =
            isSingleUpload && singleEntryLabel
              ? `Uploading ${singleEntryLabel} (${overallPct}%)`
              : activeCount > 1
                ? `Uploading ${activeCount} files (${overallPct}%)`
                : `Uploading ${uploaded + activeCount}/${files.length} files (${overallPct}%)`;
          updateUpload(uploadId, {
            completedFiles: uploaded,
            progress: overallPct,
            label,
            ...(speed !== undefined && { speed }),
          });
        };

        const uploadSingleFile = async (
          entry: { file?: File; relativePath: string },
          idx: number,
        ): Promise<void> => {
          const { file, relativePath } = entry;
          if (!file) return;

          const targetFilePath = buildTargetPath(targetPath, relativePath);
          fileProgress.set(idx, 0);
          fileBytes.set(idx, 0);
          updateOverallProgress(0, idx);

          if (!isConnected()) {
            failures.push({
              path: relativePath,
              message: "Stream connection not ready",
            });
            fileProgress.delete(idx);
            fileBytes.delete(idx);
            return;
          }

          const result = await uploadFileViaStream(
            file,
            targetFilePath,
            uploadId,
            (loaded, total) => {
              const pct = Math.round((loaded / total) * 100);
              fileProgress.set(idx, pct);
              updateOverallProgress(loaded, idx);
            },
            abortController.signal,
          );

          // If cancelled, don't count as success or error
          if (result.cancelled) {
            return;
          }

          const uploadSuccess = result.success;
          const uploadError = result.error;

          // Remove from active progress tracking
          fileProgress.delete(idx);
          fileBytes.delete(idx);

          if (uploadSuccess) {
            uploaded += 1;
            uploadedBytesTotal += file.size;
          } else if (uploadError) {
            failures.push({ path: relativePath, message: uploadError });
          }
          updateOverallProgress();
        };

        // Process files sequentially
        for (const { file, relativePath } of files) {
          if (abortController.signal.aborted) break;
          if (!file) continue;

          const idx = fileIndex++;
          await uploadSingleFile({ file, relativePath }, idx);
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
        return { conflicts, uploaded, failures };
      } catch (err: any) {
        if (err.name === "CanceledError") {
          console.log("Upload cancelled by user");
        } else {
          console.error("Upload failed", err);
          toast.error("Upload failed");
        }
        recordTransferRate(uploadId, undefined);
        removeUpload(uploadId);
        return { conflicts, uploaded, failures };
      }
    },
    [
      primeTransferRate,
      recordTransferRate,
      removeUpload,
      updateUpload,
      uploadFileViaStream,
    ],
  );

  const cancelUpload = useCallback(
    (id: string) => {
      const upload = uploads.find((u) => u.id === id);
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
      uploads,
    ],
  );

  return {
    uploads,
    startUpload,
    cancelUpload,
  };
}
