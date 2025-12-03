import React, {
  createContext,
  useState,
  useCallback,
  useRef,
  useEffect,
} from "react";
import { toast } from "sonner";

import useWebSocket from "@/hooks/useWebSocket";
import axios from "@/utils/axios";

interface Download {
  id: string;
  type: "download";
  paths: string[];
  progress: number;
  label: string;
  abortController: AbortController;
}

interface Upload {
  id: string;
  type: "upload";
  totalFiles: number;
  completedFiles: number;
  currentFile: string;
  progress: number;
  label: string;
  abortController: AbortController;
}

interface Compression {
  id: string;
  type: "compression";
  archiveName: string;
  destination: string;
  paths: string[];
  progress: number;
  label: string;
  abortController: AbortController;
}

type Transfer = Download | Upload | Compression;

export interface FileTransferContextValue {
  downloads: Download[];
  uploads: Upload[];
  compressions: Compression[];
  transfers: Transfer[];
  startDownload: (paths: string[]) => Promise<void>;
  cancelDownload: (id: string) => void;
  startCompression: (options: {
    paths: string[];
    archiveName: string;
    destination: string;
  }) => Promise<void>;
  cancelCompression: (id: string) => void;
  startUpload: (
    entries: Array<{ file?: File; relativePath: string; isDirectory: boolean }>,
    targetPath: string,
    override?: boolean,
  ) => Promise<{
    conflicts: Array<{
      file?: File;
      relativePath: string;
      isDirectory: boolean;
    }>;
    uploaded: number;
    failures: Array<{ path: string; message: string }>;
  }>;
  cancelUpload: (id: string) => void;
}

export const FileTransferContext =
  createContext<FileTransferContextValue | null>(null);

export const FileTransferProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [downloads, setDownloads] = useState<Download[]>([]);
  const [uploads, setUploads] = useState<Upload[]>([]);
  const [compressions, setCompressions] = useState<Compression[]>([]);
  const ws = useWebSocket();
  const cleanupTimersRef = useRef<Map<string, any>>(new Map());

  const transfers: Transfer[] = [...downloads, ...uploads, ...compressions];

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

  const removeDownload = useCallback((id: string) => {
    setDownloads((prev) => prev.filter((d) => d.id !== id));
    const timers = cleanupTimersRef.current.get(id);
    if (timers) {
      if (timers.fallback) clearTimeout(timers.fallback);
      if (timers.unsubscribe) timers.unsubscribe();
      cleanupTimersRef.current.delete(id);
    }
  }, []);

  const startDownload = useCallback(
    async (paths: string[]) => {
      if (!paths.length) return;

      const reqId = crypto.randomUUID();
      const abortController = new AbortController();

      const download: Download = {
        id: reqId,
        type: "download",
        paths,
        progress: 0,
        label: "Preparing archive (0%)",
        abortController,
      };

      setDownloads((prev) => [...prev, download]);

      const filesParam = paths
        .map(
          (path) => `${encodeURIComponent("/")}::${encodeURIComponent(path)}`,
        )
        .join("||");
      const url = `/navigator/api/raw?files=${filesParam}&reqId=${encodeURIComponent(reqId)}`;

      let unsubscribe: (() => void) | null = null;
      let fallbackTimer: ReturnType<typeof setTimeout> | null = null;

      try {
        let hasReceivedProgress = false;

        // Subscribe to WebSocket progress events
        unsubscribe = ws.subscribe((msg: any) => {
          if (msg.requestId !== reqId) return;

          if (msg.type === "download_progress" && msg.data) {
            hasReceivedProgress = true;
            const percent = Math.min(99, Math.round(msg.data.percent));
            updateDownload(reqId, {
              progress: percent,
              label: `Preparing archive (${percent}%)`,
            });
          } else if (msg.type === "download_ready") {
            updateDownload(reqId, {
              progress: 100,
              label: "Starting download...",
            });
          }
        });

        ws.send({ type: "subscribe_download_progress", data: reqId });

        // Fallback timer if WebSocket doesn't send progress
        fallbackTimer = setTimeout(() => {
          if (!hasReceivedProgress) {
            const prepTimer = setInterval(() => {
              setDownloads((prev) =>
                prev.map((d) => {
                  if (d.id !== reqId) return d;
                  const next = Math.min(d.progress + 5, 90);
                  return {
                    ...d,
                    progress: next,
                    label: `Preparing archive (${next}%)`,
                  };
                }),
              );
            }, 400);
            fallbackTimer = prepTimer as any;
          }
        }, 2000);

        cleanupTimersRef.current.set(reqId, {
          fallback: fallbackTimer,
          unsubscribe,
        });

        const response = await axios.get(url, {
          responseType: "blob",
          signal: abortController.signal,
          onDownloadProgress: (progressEvent) => {
            if (progressEvent.total) {
              const percentComplete = Math.round(
                (progressEvent.loaded / progressEvent.total) * 100,
              );
              updateDownload(reqId, {
                progress: percentComplete,
                label: `Downloading (${percentComplete}%)`,
              });
            }
          },
        });

        // Clean up timers
        if (fallbackTimer) {
          clearTimeout(fallbackTimer);
        }
        if (unsubscribe) {
          unsubscribe();
        }
        cleanupTimersRef.current.delete(reqId);

        // Extract filename
        const disposition = response.headers["content-disposition"] || "";
        const utfName = disposition.match(/filename\*?=utf-8''([^;]+)/i);
        const simpleName = disposition.match(/filename="?([^";]+)"?/i);
        const fallbackName =
          paths.length === 1
            ? paths[0].split("/").pop() || "download"
            : "download.zip";
        const fileName = utfName
          ? decodeURIComponent(utfName[1])
          : simpleName?.[1] || fallbackName;

        updateDownload(reqId, {
          progress: 100,
          label: "Finalizing...",
        });

        // Trigger browser download
        const blob = response.data;
        const blobUrl = window.URL.createObjectURL(blob);
        const link = document.createElement("a");
        link.href = blobUrl;
        link.download = fileName;
        document.body.appendChild(link);
        link.click();
        link.remove();
        window.URL.revokeObjectURL(blobUrl);

        toast.success("Download started");
        setTimeout(() => removeDownload(reqId), 1000);
      } catch (err: any) {
        if (err?.name === "AbortError" || err?.name === "CanceledError") {
          console.log("Download cancelled by user");
        } else {
          console.error("Download failed", err);
          const message =
            err?.response?.data?.error ||
            err?.message ||
            (typeof err === "string" ? err : null) ||
            "Download failed";
          toast.error(message);
        }
        removeDownload(reqId);
      }
    },
    [ws, updateDownload, removeDownload],
  );

  const cancelDownload = useCallback(
    (id: string) => {
      const download = downloads.find((d) => d.id === id);
      if (download) {
        download.abortController.abort();
        toast.info("Download cancelled");
        removeDownload(id);
      }
    },
    [downloads, removeDownload],
  );

  const updateCompression = useCallback(
    (
      id: string,
      updates: Partial<Omit<Compression, "id" | "type" | "abortController">>,
    ) => {
      setCompressions((prev) =>
        prev.map((c) => (c.id === id ? { ...c, ...updates } : c)),
      );
    },
    [],
  );

  const removeCompression = useCallback((id: string) => {
    setCompressions((prev) => prev.filter((c) => c.id !== id));
    const timers = cleanupTimersRef.current.get(id);
    if (timers) {
      if (timers.fallback) clearTimeout(timers.fallback);
      if (timers.unsubscribe) timers.unsubscribe();
      cleanupTimersRef.current.delete(id);
    }
  }, []);

  const startCompression = useCallback(
    async ({
      paths,
      archiveName,
      destination,
    }: {
      paths: string[];
      archiveName: string;
      destination: string;
    }) => {
      if (!paths.length) return;

      const id = crypto.randomUUID();
      const requestId = id;
      const abortController = new AbortController();
      const labelBase = archiveName || "archive.zip";

      const compression: Compression = {
        id,
        type: "compression",
        archiveName: labelBase,
        destination,
        paths,
        progress: 0,
        label: `Compressing ${labelBase} (0%)`,
        abortController,
      };

      setCompressions((prev) => [...prev, compression]);

      let hasProgress = false;
      let fallbackTimer:
        | ReturnType<typeof setTimeout>
        | ReturnType<typeof setInterval>
        | null = null;

      const unsubscribe = ws.subscribe((msg: any) => {
        if (msg.requestId !== requestId) return;

        if (msg.type === "compression_progress" && msg.data) {
          hasProgress = true;
          const percent = Math.min(99, Math.round(msg.data.percent));
          updateCompression(id, {
            progress: percent,
            label: `Compressing ${labelBase} (${percent}%)`,
          });
        } else if (msg.type === "compression_complete") {
          hasProgress = true;
          updateCompression(id, {
            progress: 100,
            label: `Created ${labelBase}`,
          });
          setTimeout(() => removeCompression(id), 800);
        }
      });

      ws.send({ type: "subscribe_compression_progress", data: requestId });

      fallbackTimer = setTimeout(() => {
        if (hasProgress) return;
        fallbackTimer = setInterval(() => {
          setCompressions((prev) =>
            prev.map((c) => {
              if (c.id !== id) return c;
              const next = Math.min((c.progress || 0) + 4, 90);
              return {
                ...c,
                progress: next,
                label: `Compressing ${labelBase} (${next}%)`,
              };
            }),
          );
        }, 450);
        cleanupTimersRef.current.set(id, {
          fallback: fallbackTimer,
          unsubscribe,
        });
      }, 1500);

      cleanupTimersRef.current.set(id, {
        fallback: fallbackTimer,
        unsubscribe,
      });

      try {
        await axios.post(
          "/navigator/api/archive/compress",
          {
            paths,
            archiveName,
            destination,
            format: "zip",
            requestId,
          },
          { signal: abortController.signal },
        );

        updateCompression(id, {
          progress: 100,
          label: `Created ${labelBase}`,
        });
        setTimeout(() => removeCompression(id), 800);
      } catch (err: any) {
        if (err?.name === "CanceledError" || err?.name === "AbortError") {
          toast.info("Compression cancelled");
        } else {
          const message =
            err?.response?.data?.error || err?.message || "Compression failed";
          updateCompression(id, { label: message });
        }
        setTimeout(() => removeCompression(id), 800);
        throw err;
      } finally {
        if (fallbackTimer) {
          clearTimeout(fallbackTimer as any);
        }
        const timers = cleanupTimersRef.current.get(id);
        if (timers?.unsubscribe) {
          timers.unsubscribe();
        }
        cleanupTimersRef.current.delete(id);
      }
    },
    [removeCompression, updateCompression, ws],
  );

  const cancelCompression = useCallback(
    (id: string) => {
      const compression = compressions.find((c) => c.id === id);
      if (compression) {
        compression.abortController.abort();
        updateCompression(id, { label: "Cancelling..." });
      }
    },
    [compressions, updateCompression],
  );

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

  const removeUpload = useCallback((id: string) => {
    setUploads((prev) => prev.filter((u) => u.id !== id));
  }, []);

  const startUpload = useCallback(
    async (
      entries: Array<{
        file?: File;
        relativePath: string;
        isDirectory: boolean;
      }>,
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

      const upload: Upload = {
        id: uploadId,
        type: "upload",
        totalFiles,
        completedFiles: 0,
        currentFile: "",
        progress: 0,
        label: `Uploading 0/${totalFiles} files`,
        abortController,
      };

      setUploads((prev) => [...prev, upload]);

      const conflicts: typeof entries = [];
      let uploaded = 0;
      const failures: { path: string; message: string }[] = [];

      const buildTargetPath = (base: string, relative: string) => {
        const normalized = base.endsWith("/") ? base : `${base}/`;
        return `${normalized}${relative}`;
      };

      try {
        // Create directories first
        for (const { relativePath } of directories) {
          if (abortController.signal.aborted) break;

          const targetBase = buildTargetPath(targetPath, relativePath);
          const dirPath = targetBase.endsWith("/")
            ? targetBase
            : `${targetBase}/`;

          updateUpload(uploadId, {
            currentFile: relativePath,
            completedFiles: uploaded,
            progress: Math.round((uploaded / totalFiles) * 100),
            label: `Creating folder ${uploaded + 1}/${totalFiles}`,
          });

          try {
            await axios.post("/navigator/api/resources", null, {
              params: {
                path: dirPath,
                override: override ? "true" : undefined,
                source: "/",
              },
              signal: abortController.signal,
            });
            uploaded += 1;
          } catch (err: any) {
            if (err.name === "CanceledError") break;
            if (err.response?.status === 409 && !override) {
              continue;
            }
            const message =
              err.response?.data?.error ||
              err.message ||
              "Failed to create folder";
            failures.push({ path: relativePath, message });
          }
        }

        // Upload files
        for (const { file, relativePath } of files) {
          if (abortController.signal.aborted) break;
          if (!file) continue;

          const targetFilePath = buildTargetPath(targetPath, relativePath);

          updateUpload(uploadId, {
            currentFile: relativePath,
            completedFiles: uploaded,
            progress: Math.round((uploaded / totalFiles) * 100),
            label: `Uploading ${uploaded + 1}/${totalFiles} files`,
          });

          try {
            await axios.post("/navigator/api/resources", file, {
              params: {
                path: targetFilePath,
                override: override ? "true" : undefined,
              },
              headers: {
                "Content-Type": file.type || "application/octet-stream",
              },
              signal: abortController.signal,
              onUploadProgress: (progressEvent) => {
                if (progressEvent.total) {
                  const fileProgress = Math.round(
                    (progressEvent.loaded / progressEvent.total) * 100,
                  );
                  const overallProgress = Math.round(
                    ((uploaded + fileProgress / 100) / totalFiles) * 100,
                  );
                  updateUpload(uploadId, {
                    progress: overallProgress,
                    label: `Uploading ${relativePath} (${fileProgress}%)`,
                  });
                }
              },
            });
            uploaded += 1;
          } catch (err: any) {
            if (err.name === "CanceledError") break;
            if (err.response?.status === 409 && !override) {
              conflicts.push({ file, relativePath, isDirectory: false });
              continue;
            }
            const message =
              err.response?.data?.error ||
              err.message ||
              "Failed to upload file";
            failures.push({ path: relativePath, message });
          }
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

        removeUpload(uploadId);
        return { conflicts, uploaded, failures };
      } catch (err: any) {
        if (err.name === "CanceledError") {
          console.log("Upload cancelled by user");
        } else {
          console.error("Upload failed", err);
          toast.error("Upload failed");
        }
        removeUpload(uploadId);
        return { conflicts, uploaded, failures };
      }
    },
    [updateUpload, removeUpload],
  );

  const cancelUpload = useCallback(
    (id: string) => {
      const upload = uploads.find((u) => u.id === id);
      if (upload) {
        upload.abortController.abort();
        toast.info("Upload cancelled");
        removeUpload(id);
      }
    },
    [uploads, removeUpload],
  );

  // Cleanup on unmount
  useEffect(() => {
    const timersMap = cleanupTimersRef.current;
    return () => {
      timersMap.forEach((timers) => {
        if (timers.fallback) clearTimeout(timers.fallback);
        if (timers.unsubscribe) timers.unsubscribe();
      });
    };
  }, []);

  return (
    <FileTransferContext.Provider
      value={{
        downloads,
        uploads,
        compressions,
        transfers,
        startDownload,
        cancelDownload,
        startCompression,
        cancelCompression,
        startUpload,
        cancelUpload,
      }}
    >
      {children}
    </FileTransferContext.Provider>
  );
};
