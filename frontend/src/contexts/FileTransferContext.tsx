import React, {
  createContext,
  useState,
  useCallback,
  useRef,
  useEffect,
} from "react";
import { toast } from "sonner";

import useWebSocket from "@/hooks/useWebSocket";
import {
  getStreamMux,
  Stream,
  ProgressFrame,
  ResultFrame,
  encodeString,
  STREAM_CHUNK_SIZE,
  UPLOAD_WINDOW_SIZE,
} from "@/utils/StreamMultiplexer";
import axios from "@/utils/axios";

// Stream types matching backend constants
const STREAM_TYPE_FB_DOWNLOAD = "fb-download";
const STREAM_TYPE_FB_UPLOAD = "fb-upload";

interface Download {
  id: string;
  type: "download";
  paths: string[];
  progress: number;
  label: string;
  speed?: number;
  abortController: AbortController;
  stream?: Stream | null; // For stream-based downloads
}

interface Upload {
  id: string;
  type: "upload";
  totalFiles: number;
  completedFiles: number;
  currentFile: string;
  progress: number;
  label: string;
  displayName?: string;
  speed?: number;
  abortController: AbortController;
  stream?: Stream | null; // For stream-based uploads
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
    Map<string, { bytes: number; timestamp: number; emitted: boolean }>
  >(new Map());
  // Store stream references synchronously for immediate cancellation access
  const streamRefsRef = useRef<Map<string, Stream>>(new Map());
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
          emitted: false,
        });
        return undefined;
      }
      if (bytesProcessed < prev.bytes) {
        transferRatesRef.current.set(id, {
          bytes: bytesProcessed,
          timestamp: now,
          emitted: prev.emitted,
        });
        return undefined;
      }
      const deltaBytes = bytesProcessed - prev.bytes;
      const deltaMs = now - prev.timestamp;
      if (deltaBytes <= 0) {
        return undefined;
      }
      if (prev.emitted && deltaMs < TRANSFER_RATE_SAMPLE_MS) {
        return undefined;
      }
      const rate = deltaBytes / (deltaMs / 1000);
      transferRatesRef.current.set(id, {
        bytes: bytesProcessed,
        timestamp: now,
        emitted: true,
      });
      return rate;
    },
    [],
  );

  const primeTransferRate = useCallback((id: string, initialBytes = 0) => {
    if (!id) {
      return;
    }
    transferRatesRef.current.set(id, {
      bytes: initialBytes,
      timestamp: Date.now(),
      emitted: false,
    });
  }, []);

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
      transferRatesRef.current.delete(id);
      streamRefsRef.current.delete(id);
      // Note: No need to sendProgressUnsubscribe for stream-based downloads
      // Progress comes through stream.onProgress, not WebSocket subscription
    },
    [releaseDownloadLabelBase],
  );

  /**
   * Stream-based download implementation.
   * Uses yamux binary streams for efficient file transfers.
   */
  const startStreamBasedDownload = useCallback(
    async (
      paths: string[],
      reqId: string,
      downloadLabelBase: string,
      formatDownloadLabel: (
        stage: string,
        options?: { percent?: number; name?: string },
      ) => string,
    ) => {
      const mux = getStreamMux();
      if (!mux || mux.status !== "open") {
        throw new Error("Stream connection not ready");
      }

      // Determine stream type based on number of paths
      const isSingleFile = paths.length === 1;
      const streamType = isSingleFile ? STREAM_TYPE_FB_DOWNLOAD : "fb-archive";

      // Build payload
      const payloadParts = isSingleFile
        ? [STREAM_TYPE_FB_DOWNLOAD, paths[0]]
        : ["fb-archive", "zip", ...paths];
      const payload = encodeString(payloadParts.join("\0"));

      const stream = mux.openStream(streamType, payload);
      if (!stream) {
        throw new Error("Failed to open download stream");
      }

      // Store stream reference for cancellation (sync ref for immediate access)
      streamRefsRef.current.set(reqId, stream);
      setDownloads((prev) =>
        prev.map((d) => (d.id === reqId ? { ...d, stream } : d)),
      );

      const chunks: Uint8Array[] = [];
      let lastBytes = 0;
      let lastTime = Date.now();

      return new Promise<Blob>((resolve, reject) => {
        stream.onProgress = (progress: ProgressFrame) => {
          const now = Date.now();
          const deltaBytes = progress.bytes - lastBytes;
          const deltaMs = now - lastTime;

          let speed: number | undefined;
          if (deltaMs > 500 && deltaBytes > 0) {
            speed = deltaBytes / (deltaMs / 1000);
            lastBytes = progress.bytes;
            lastTime = now;
          }

          updateDownload(reqId, {
            progress: progress.pct,
            label: formatDownloadLabel(
              progress.phase === "preparing" ? "Preparing" : "Downloading",
              { percent: progress.pct },
            ),
            ...(speed !== undefined && { speed }),
          });
        };

        stream.onData = (data: Uint8Array) => {
          chunks.push(data);
        };

        stream.onResult = (result: ResultFrame) => {
          if (result.status === "ok") {
            const mimeType = isSingleFile
              ? "application/octet-stream"
              : "application/zip";
            const blob = new Blob(chunks as BlobPart[], { type: mimeType });
            resolve(blob);
          } else {
            reject(new Error(result.error || "Download failed"));
          }
        };

        stream.onClose = () => {
          // If no result was received, treat as error
          if (chunks.length === 0) {
            reject(new Error("Stream closed before transfer completed"));
          }
        };
      });
    },
    [updateDownload],
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

      const mux = getStreamMux();
      if (!mux || mux.status !== "open") {
        toast.error("Stream connection not ready");
        removeDownload(reqId);
        return;
      }

      try {
        primeTransferRate(reqId, 0);
        const blob = await startStreamBasedDownload(
          paths,
          reqId,
          downloadLabelBase,
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
        const fileName =
          paths.length === 1
            ? downloadLabelBase
            : `${downloadLabelBase}.zip`;
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
          const message = err?.message || "Download failed";
          toast.error(message);
        }
        recordTransferRate(reqId, undefined);
        removeDownload(reqId);
      }
    },
    [
      updateDownload,
      removeDownload,
      allocateDownloadLabelBase,
      recordTransferRate,
      primeTransferRate,
      startStreamBasedDownload,
    ],
  );

  const cancelDownload = useCallback(
    (id: string) => {
      const download = downloads.find((d) => d.id === id);
      if (download) {
        // Abort stream if using stream-based download (RST for immediate cancel)
        // Use ref first (synchronous) then fallback to state
        const stream = streamRefsRef.current.get(id) || download.stream;
        if (stream) {
          stream.abort(); // Use abort() instead of close() for immediate cancellation
          streamRefsRef.current.delete(id);
        }
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
        // Send cancel message to backend FIRST so it stops the operation
        sendProgressUnsubscribe("compression", id);
        compression.abortController.abort();
        updateCompression(id, { label: "Cancelling..." });
      }
    },
    [compressions, sendProgressUnsubscribe, updateCompression],
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
        // Send cancel message to backend FIRST so it stops the operation
        sendProgressUnsubscribe("extraction", id);
        extraction.abortController.abort();
        updateExtraction(id, { label: "Cancelling..." });
      }
    },
    [extractions, sendProgressUnsubscribe, updateExtraction],
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

      transferRatesRef.current.delete(id);
      streamRefsRef.current.delete(id);
      // Note: No need to sendProgressUnsubscribe for stream-based uploads
      // Progress comes through stream.onProgress, not WebSocket subscription
    },
    [],
  );

  /**
   * Stream-based single file upload implementation.
   * Sends file directly to bridge via yamux stream - no temp files on server.
   */
  const uploadFileViaStream = useCallback(
    async (
      file: File,
      targetPath: string,
      uploadId: string,
      onProgress: (loaded: number, total: number) => void,
      abortSignal: AbortSignal,
    ): Promise<{ success: boolean; error?: string; cancelled?: boolean }> => {
      const mux = getStreamMux();
      if (!mux || mux.status !== "open") {
        return { success: false, error: "Stream connection not ready" };
      }

      // Build payload: "fb-upload\0/path/to/file\0size"
      const payload = encodeString(
        `${STREAM_TYPE_FB_UPLOAD}\0${targetPath}\0${file.size}`,
      );
      const stream = mux.openStream(STREAM_TYPE_FB_UPLOAD, payload);

      if (!stream) {
        return { success: false, error: "Failed to open upload stream" };
      }

      // Store stream reference for cancellation (sync ref for immediate access)
      streamRefsRef.current.set(uploadId, stream);
      setUploads((prev) =>
        prev.map((u) => (u.id === uploadId ? { ...u, stream } : u)),
      );

      return new Promise<{ success: boolean; error?: string; cancelled?: boolean }>((resolve) => {
        let resultReceived = false;

        // Use backend progress updates (bytes actually written to disk)
        stream.onProgress = (progress: ProgressFrame) => {
          onProgress(progress.bytes, progress.total);
        };

        stream.onResult = (result: ResultFrame) => {
          resultReceived = true;
          // Clear stream reference
          streamRefsRef.current.delete(uploadId);
          setUploads((prev) =>
            prev.map((u) => (u.id === uploadId ? { ...u, stream: null } : u)),
          );
          // Check if cancelled - even if bridge reports success, user cancelled
          if (abortSignal.aborted) {
            resolve({ success: false, cancelled: true });
            return;
          }
          if (result.status === "ok") {
            resolve({ success: true });
          } else {
            resolve({
              success: false,
              error: result.error || "Upload failed",
            });
          }
        };

        stream.onClose = () => {
          if (!resultReceived) {
            streamRefsRef.current.delete(uploadId);
            setUploads((prev) =>
              prev.map((u) => (u.id === uploadId ? { ...u, stream: null } : u)),
            );
            // Check if cancelled
            if (abortSignal.aborted) {
              resolve({ success: false, cancelled: true });
              return;
            }
            resolve({
              success: false,
              error: "Stream closed before upload completed",
            });
          }
        };

        // Flow control: track bytes in flight to allow meaningful cancellation
        const reader = new FileReader();
        let offset = 0;
        let bytesSent = 0;
        let bytesAcked = 0;
        let pendingSend = false;

        // Update onProgress to handle flow control ACKs
        const originalOnProgress = stream.onProgress;
        stream.onProgress = (progress: ProgressFrame) => {
          bytesAcked = progress.bytes;
          originalOnProgress?.(progress);

          // Window opened - resume sending if we were waiting
          if (pendingSend && bytesSent - bytesAcked < UPLOAD_WINDOW_SIZE) {
            pendingSend = false;
            sendNextChunk();
          }
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
          if (bytesSent - bytesAcked >= UPLOAD_WINDOW_SIZE) {
            pendingSend = true;
            return; // Will resume when onProgress fires
          }

          const slice = file.slice(offset, offset + STREAM_CHUNK_SIZE);
          reader.readAsArrayBuffer(slice);
        };

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
          resolve({ success: false, error: "Failed to read file" });
        };

        // Start sending
        sendNextChunk();
      });
    },
    [],
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
            const label = u.displayName
              ? `Uploading ${u.displayName} (${percent}%)`
              : `Uploading ${u.completedFiles}/${u.totalFiles} files (${percent}%)`;
            const updated: Upload = {
              ...u,
              progress: percent,
              label,
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
                  label: u.displayName
                    ? `Uploaded ${u.displayName}`
                    : `Uploaded ${u.totalFiles}/${u.totalFiles} files`,
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

          const mux = getStreamMux();
          if (!mux || mux.status !== "open") {
            failures.push({ path: relativePath, message: "Stream connection not ready" });
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
      updateUpload,
      removeUpload,
      ws,
      recordTransferRate,
      primeTransferRate,
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
