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
  speed?: number;
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
  speed?: number;
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

interface Extraction {
  id: string;
  type: "extraction";
  archivePath: string;
  destination: string;
  progress: number;
  label: string;
  abortController: AbortController;
}

type Transfer = Download | Upload | Compression | Extraction;

export interface FileTransferContextValue {
  downloads: Download[];
  uploads: Upload[];
  compressions: Compression[];
  extractions: Extraction[];
  transfers: Transfer[];
  startDownload: (paths: string[]) => Promise<void>;
  cancelDownload: (id: string) => void;
  startCompression: (options: {
    paths: string[];
    archiveName: string;
    destination: string;
  }) => Promise<void>;
  cancelCompression: (id: string) => void;
  startExtraction: (options: {
    archivePath: string;
    destination?: string;
  }) => Promise<void>;
  cancelExtraction: (id: string) => void;
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
  const [extractions, setExtractions] = useState<Extraction[]>([]);
  const ws = useWebSocket();
  const cleanupTimersRef = useRef<Map<string, any>>(new Map());
  const activeCompressionIdsRef = useRef<Set<string>>(new Set());
  const activeExtractionIdsRef = useRef<Set<string>>(new Set());
  const downloadLabelCounterRef = useRef<Map<string, number>>(new Map());
  const downloadLabelAssignmentRef = useRef<Map<string, string>>(new Map());
  const transferRatesRef = useRef<
    Map<string, { bytes: number; timestamp: number }>
  >(new Map());
  const TRANSFER_RATE_SAMPLE_MS = 1000;

  const recordTransferRate = useCallback(
    (id: string, bytesProcessed?: number) => {
      if (!id || bytesProcessed === undefined || bytesProcessed < 0) {
        transferRatesRef.current.delete(id);
        return undefined;
      }
      const now = Date.now();
      const prev = transferRatesRef.current.get(id);
      if (!prev) {
        transferRatesRef.current.set(id, {
          bytes: bytesProcessed,
          timestamp: now,
        });
        return undefined;
      }
      if (bytesProcessed < prev.bytes) {
        transferRatesRef.current.set(id, {
          bytes: bytesProcessed,
          timestamp: now,
        });
        return undefined;
      }
      const deltaBytes = bytesProcessed - prev.bytes;
      const deltaMs = now - prev.timestamp;
      if (deltaMs < TRANSFER_RATE_SAMPLE_MS) {
        return undefined;
      }
      if (deltaBytes <= 0) {
        return undefined;
      }
      const rate = deltaBytes / (deltaMs / 1000);
      transferRatesRef.current.set(id, {
        bytes: bytesProcessed,
        timestamp: now,
      });
      return rate;
    },
    [],
  );

  const sendProgressUnsubscribe = useCallback(
    (
      _type: "download" | "compression" | "upload" | "extraction",
      requestId: string,
    ) => {
      ws.send({ type: "unsubscribe_operation_progress", data: requestId });
    },
    [ws],
  );

  const allocateDownloadLabelBase = useCallback((base: string, id: string) => {
    const counters = downloadLabelCounterRef.current;
    const current = counters.get(base) ?? 0;
    const next = current + 1;
    counters.set(base, next);
    downloadLabelAssignmentRef.current.set(id, base);
    return next === 1 ? base : `${base} (${next})`;
  }, []);

  const releaseDownloadLabelBase = useCallback((id: string) => {
    const base = downloadLabelAssignmentRef.current.get(id);
    if (!base) {
      return;
    }
    downloadLabelAssignmentRef.current.delete(id);
    const counters = downloadLabelCounterRef.current;
    const current = counters.get(base);
    if (!current) {
      return;
    }
    if (current <= 1) {
      counters.delete(base);
    } else {
      counters.set(base, current - 1);
    }
  }, []);

  const transfers: Transfer[] = [
    ...downloads,
    ...uploads,
    ...compressions,
    ...extractions,
  ];

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
      const timers = cleanupTimersRef.current.get(id);
      if (timers) {
        if (timers.fallback) clearTimeout(timers.fallback);
        if (timers.unsubscribe) timers.unsubscribe();
        cleanupTimersRef.current.delete(id);
      }
      releaseDownloadLabelBase(id);
      sendProgressUnsubscribe("download", id);
      transferRatesRef.current.delete(id);
    },
    [releaseDownloadLabelBase, sendProgressUnsubscribe],
  );

  const startDownload = useCallback(
    async (paths: string[]) => {
      if (!paths.length) return;

      const reqId = crypto.randomUUID();
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
      const downloadLabelBase = allocateDownloadLabelBase(
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

      const download: Download = {
        id: reqId,
        type: "download",
        paths,
        progress: 0,
        label: formatDownloadLabel("Preparing", { percent: 0 }),
        speed: undefined,
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
              label: formatDownloadLabel("Preparing", { percent }),
            });
          } else if (msg.type === "download_ready") {
            updateDownload(reqId, {
              progress: 100,
              label: formatDownloadLabel("Starting download"),
              speed: undefined,
            });
          }
        });

        ws.send({ type: "subscribe_operation_progress", data: reqId });

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
                    label: formatDownloadLabel("Preparing", { percent: next }),
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
            const updates: Partial<Omit<Download, "id" | "abortController">> =
              {};
            if (progressEvent.total) {
              const percentComplete = Math.round(
                (progressEvent.loaded / progressEvent.total) * 100,
              );
              updates.progress = percentComplete;
              updates.label = formatDownloadLabel("Downloading", {
                percent: percentComplete,
              });
            }
            if (typeof progressEvent.loaded === "number") {
              const speed = recordTransferRate(reqId, progressEvent.loaded);
              if (speed !== undefined) {
                updates.speed = speed;
              }
            }
            if (Object.keys(updates).length > 0) {
              updateDownload(reqId, updates);
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
        const fallbackName = downloadLabelBase;
        const fileName = utfName
          ? decodeURIComponent(utfName[1])
          : simpleName?.[1] || fallbackName;

        updateDownload(reqId, {
          progress: 100,
          label: formatDownloadLabel("Downloaded", { name: downloadLabelBase }),
          speed: undefined,
        });
        recordTransferRate(reqId, undefined);

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

        toast.success(
          formatDownloadLabel("Downloaded", { name: downloadLabelBase }),
        );
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
        recordTransferRate(reqId, undefined);
        removeDownload(reqId);
      }
    },
    [
      ws,
      updateDownload,
      removeDownload,
      allocateDownloadLabelBase,
      recordTransferRate,
    ],
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

  const removeCompression = useCallback(
    (id: string) => {
      if (!activeCompressionIdsRef.current.has(id)) {
        return;
      }
      activeCompressionIdsRef.current.delete(id);

      setCompressions((prev) => prev.filter((c) => c.id !== id));
      const timers = cleanupTimersRef.current.get(id);
      if (timers?.unsubscribe) {
        timers.unsubscribe();
      }
      cleanupTimersRef.current.delete(id);
      releaseDownloadLabelBase(id);
      sendProgressUnsubscribe("compression", id);
    },
    [releaseDownloadLabelBase, sendProgressUnsubscribe],
  );

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
      const candidateLabelBase = archiveName || "archive.zip";
      const labelBase = allocateDownloadLabelBase(candidateLabelBase, id);

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
      activeCompressionIdsRef.current.add(id);

      const applyReportedProgress = (rawPercent: number) => {
        const percent = Math.min(99, Math.round(rawPercent));
        setCompressions((prev) =>
          prev.map((c) => {
            if (c.id !== id) return c;
            const next = Math.max(c.progress, percent);
            if (next === c.progress) {
              return c;
            }
            return {
              ...c,
              progress: next,
              label: `Compressing ${labelBase} (${next}%)`,
            };
          }),
        );
      };

      const unsubscribe = ws.subscribe((msg: any) => {
        if (msg.requestId !== requestId) return;

        if (msg.type === "compression_progress" && msg.data) {
          applyReportedProgress(msg.data.percent);
        } else if (msg.type === "compression_complete") {
          updateCompression(id, {
            progress: 100,
            label: `Created ${labelBase}`,
          });
          setTimeout(() => removeCompression(id), 800);
        }
      });

      ws.send({ type: "subscribe_operation_progress", data: requestId });

      cleanupTimersRef.current.set(id, {
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
        const timers = cleanupTimersRef.current.get(id);
        if (timers?.unsubscribe) {
          timers.unsubscribe();
        }
        cleanupTimersRef.current.delete(id);
      }
    },
    [allocateDownloadLabelBase, removeCompression, updateCompression, ws],
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

  const updateExtraction = useCallback(
    (
      id: string,
      updates: Partial<Omit<Extraction, "id" | "type" | "abortController">>,
    ) => {
      setExtractions((prev) =>
        prev.map((extraction) =>
          extraction.id === id ? { ...extraction, ...updates } : extraction,
        ),
      );
    },
    [],
  );

  const removeExtraction = useCallback(
    (id: string) => {
      if (!activeExtractionIdsRef.current.has(id)) {
        return;
      }
      activeExtractionIdsRef.current.delete(id);

      setExtractions((prev) =>
        prev.filter((extraction) => extraction.id !== id),
      );
      const timers = cleanupTimersRef.current.get(id);
      if (timers?.unsubscribe) {
        timers.unsubscribe();
      }
      cleanupTimersRef.current.delete(id);
      releaseDownloadLabelBase(id);
      sendProgressUnsubscribe("extraction", id);
    },
    [releaseDownloadLabelBase, sendProgressUnsubscribe],
  );

  const startExtraction = useCallback(
    async ({
      archivePath,
      destination,
    }: {
      archivePath: string;
      destination?: string;
    }) => {
      if (!archivePath) {
        throw new Error("No archive specified for extraction");
      }

      const id = crypto.randomUUID();
      const requestId = id;
      const abortController = new AbortController();
      const deriveLabelBase = () => {
        const trimmed = archivePath.replace(/\/+$/, "");
        const parts = trimmed.split("/");
        const rawName = parts[parts.length - 1] || "archive";
        const lower = rawName.toLowerCase();
        if (lower.endsWith(".tar.gz")) {
          return rawName.slice(0, -7) || rawName;
        }
        if (lower.endsWith(".tgz")) {
          return rawName.slice(0, -4) || rawName;
        }
        if (lower.endsWith(".zip")) {
          return rawName.slice(0, -4) || rawName;
        }
        return rawName;
      };
      const labelBase = allocateDownloadLabelBase(deriveLabelBase(), id);

      const extraction: Extraction = {
        id,
        type: "extraction",
        archivePath,
        destination: destination || "",
        progress: 0,
        label: `Extracting ${labelBase} (0%)`,
        abortController,
      };

      setExtractions((prev) => [...prev, extraction]);
      activeExtractionIdsRef.current.add(id);

      const applyReportedProgress = (rawPercent: number) => {
        const percent = Math.min(99, Math.round(rawPercent));
        setExtractions((prev) =>
          prev.map((item) => {
            if (item.id !== id) return item;
            const next = Math.max(item.progress, percent);
            if (next === item.progress) {
              return item;
            }
            return {
              ...item,
              progress: next,
              label: `Extracting ${labelBase} (${next}%)`,
            };
          }),
        );
      };

      const unsubscribe = ws.subscribe((msg: any) => {
        if (msg.requestId !== requestId) return;

        if (msg.type === "extraction_progress" && msg.data) {
          applyReportedProgress(msg.data.percent);
        } else if (msg.type === "extraction_complete") {
          updateExtraction(id, {
            progress: 100,
            label: `Extracted ${labelBase}`,
          });
          setTimeout(() => removeExtraction(id), 800);
        }
      });

      ws.send({ type: "subscribe_operation_progress", data: requestId });

      cleanupTimersRef.current.set(id, {
        unsubscribe,
      });

      try {
        await axios.post(
          "/navigator/api/archive/extract",
          {
            archivePath,
            destination,
            requestId,
          },
          { signal: abortController.signal },
        );

        updateExtraction(id, {
          progress: 100,
          label: `Extracted ${labelBase}`,
        });
        setTimeout(() => removeExtraction(id), 800);
      } catch (err: any) {
        if (err?.name === "CanceledError" || err?.name === "AbortError") {
          toast.info("Extraction cancelled");
        } else {
          const message =
            err?.response?.data?.error || err?.message || "Extraction failed";
          updateExtraction(id, { label: message });
        }
        setTimeout(() => removeExtraction(id), 800);
        throw err;
      } finally {
        const timers = cleanupTimersRef.current.get(id);
        if (timers?.unsubscribe) {
          timers.unsubscribe();
        }
        cleanupTimersRef.current.delete(id);
      }
    },
    [allocateDownloadLabelBase, removeExtraction, updateExtraction, ws],
  );

  const cancelExtraction = useCallback(
    (id: string) => {
      const extraction = extractions.find((item) => item.id === id);
      if (extraction) {
        extraction.abortController.abort();
        updateExtraction(id, { label: "Cancelling..." });
      }
    },
    [extractions, updateExtraction],
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

  const removeUpload = useCallback(
    (id: string) => {
      setUploads((prev) => prev.filter((u) => u.id !== id));

      const timers = cleanupTimersRef.current.get(id);
      if (timers) {
        if (timers.fallback) clearTimeout(timers.fallback);
        if (timers.unsubscribe) timers.unsubscribe();
        cleanupTimersRef.current.delete(id);
      }

      sendProgressUnsubscribe("upload", id);
      transferRatesRef.current.delete(id);
    },
    [sendProgressUnsubscribe],
  );

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
      const requestId = uploadId;
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
        speed: undefined,
        abortController,
      };

      setUploads((prev) => [...prev, upload]);

      const conflicts: typeof entries = [];
      let uploaded = 0;
      let uploadedBytesTotal = 0;
      const failures: { path: string; message: string }[] = [];

      const buildTargetPath = (base: string, relative: string) => {
        const normalized = base.endsWith("/") ? base : `${base}/`;
        return `${normalized}${relative}`;
      };

      // WS-based upload progress (bridge-side)
      const applyReportedProgress = (rawPercent: number) => {
        const percent = Math.min(99, Math.round(rawPercent));
        setUploads((prev) =>
          prev.map((u) => {
            if (u.id !== uploadId) return u;
            const updated: Upload = {
              ...u,
              progress: percent,
              label: `Uploading ${u.completedFiles}/${u.totalFiles} files (${percent}%)`,
            };
            return updated;
          }),
        );
      };

      const unsubscribe = ws.subscribe((msg: any) => {
        if (msg.requestId !== requestId) return;

        if (msg.type === "upload_progress" && msg.data) {
          applyReportedProgress(msg.data.percent);
        }
        // Optional: handle "upload_complete" if the backend emits it
        if (msg.type === "upload_complete") {
          setUploads((prev) =>
            prev.map((u) =>
              u.id === uploadId
                ? {
                    ...u,
                    progress: 100,
                    label: `Uploaded ${u.totalFiles}/${u.totalFiles} files`,
                  }
                : u,
            ),
          );
        }
      });

      ws.send({ type: "subscribe_operation_progress", data: requestId });

      cleanupTimersRef.current.set(uploadId, {
        unsubscribe,
      });

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
                requestId,
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
          const fileOffset = uploadedBytesTotal;

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
                requestId,
              },
              headers: {
                "Content-Type": file.type || "application/octet-stream",
              },
              signal: abortController.signal,
              onUploadProgress: (progressEvent) => {
                const updates: Partial<
                  Omit<Upload, "id" | "type" | "abortController">
                > = {};
                if (progressEvent.total) {
                  const fileProgress = Math.round(
                    (progressEvent.loaded / progressEvent.total) * 100,
                  );
                  const overallProgress = Math.round(
                    ((uploaded + fileProgress / 100) / totalFiles) * 100,
                  );
                  updates.progress = overallProgress;
                  updates.label = `Uploading ${relativePath} (${fileProgress}%)`;
                }
                if (typeof progressEvent.loaded === "number") {
                  const speed = recordTransferRate(
                    uploadId,
                    fileOffset + progressEvent.loaded,
                  );
                  if (speed !== undefined) {
                    updates.speed = speed;
                  }
                }
                if (Object.keys(updates).length > 0) {
                  updateUpload(uploadId, updates);
                }
              },
            });
            uploaded += 1;
            uploadedBytesTotal += file.size;
            // reflect completedFiles
            updateUpload(uploadId, {
              completedFiles: uploaded,
              progress: Math.round((uploaded / totalFiles) * 100),
            });
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

        updateUpload(uploadId, {
          progress: 100,
          label: `Uploaded ${uploaded}/${totalFiles} files`,
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
    [updateUpload, removeUpload, ws, recordTransferRate],
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
        extractions,
        transfers,
        startDownload,
        cancelDownload,
        startCompression,
        cancelCompression,
        startExtraction,
        cancelExtraction,
        startUpload,
        cancelUpload,
      }}
    >
      {children}
    </FileTransferContext.Provider>
  );
};
