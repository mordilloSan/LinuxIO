import React, { createContext, useState, useCallback, useRef } from "react";
import { toast } from "sonner";

import {
  linuxio,
  bindStreamHandlers,
  LinuxIOError,
  isConnected,
  openFileUploadStream,
  openFileDownloadStream,
  openFileCompressStream,
  openFileExtractStream,
  openFileIndexerStream,
  openFileIndexerAttachStream,
  openFileCopyStream,
  openFileMoveStream,
  STREAM_CHUNK_SIZE,
  UPLOAD_WINDOW_SIZE,
  type Stream,
  type ProgressFrame,
} from "@/api";
import { useStreamResult } from "@/hooks/useStreamResult";

const REMOTE_INDEXER_ID = "remote-indexer";

const isIndexerConflictError = (error: unknown): boolean => {
  const message =
    error instanceof Error ? error.message.toLowerCase().trim() : "";
  if (error instanceof LinuxIOError && Number(error.code) === 409) {
    return true;
  }

  if (typeof error === "object" && error !== null && "code" in error) {
    const code = Number((error as { code?: unknown }).code);
    if (code === 409) {
      return true;
    }
  }

  return message.includes("already running") || message.includes("conflict");
};

const isIndexerAttachUnavailableError = (error: unknown): boolean => {
  const message =
    error instanceof Error ? error.message.toLowerCase().trim() : "";
  if (error instanceof LinuxIOError && Number(error.code) === 404) {
    return true;
  }

  if (typeof error === "object" && error !== null && "code" in error) {
    const code = Number((error as { code?: unknown }).code);
    if (code === 404) {
      return true;
    }
  }

  return (
    message.includes("no active") ||
    message.includes("not found") ||
    message.includes("status stream error: 404")
  );
};

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
  stream?: Stream | null;
}

interface Extraction {
  id: string;
  type: "extraction";
  archivePath: string;
  destination: string;
  progress: number;
  label: string;
  abortController: AbortController;
  stream?: Stream | null;
}

interface Indexer {
  path: string;
  filesIndexed: number;
  dirsIndexed: number;
  totalSize: number;
  durationMs: number;
  id?: string;
  type?: "indexer";
  currentPath?: string;
  phase?: string;
  progress?: number;
  label?: string;
  abortController?: AbortController;
  stream?: Stream | null;
}

type ActiveIndexer = Indexer & {
  id: string;
  type: "indexer";
  currentPath: string;
  phase: string;
  progress: number;
  label: string;
  abortController: AbortController;
};

interface Copy {
  id: string;
  type: "copy";
  source: string;
  destination: string;
  progress: number;
  label: string;
  speed?: number;
  bytes?: number;
  total?: number;
  abortController: AbortController;
  stream?: Stream | null;
}

interface Move {
  id: string;
  type: "move";
  source: string;
  destination: string;
  progress: number;
  label: string;
  speed?: number;
  bytes?: number;
  total?: number;
  abortController: AbortController;
  stream?: Stream | null;
}

type Transfer =
  | Download
  | Upload
  | Compression
  | Extraction
  | ActiveIndexer
  | Copy
  | Move;

function createProgressSpeedCalculator(minWindowMs = 500) {
  let lastBytes = 0;
  let lastTime = Date.now();

  return (bytes: number): number | undefined => {
    const now = Date.now();
    const deltaBytes = bytes - lastBytes;
    const deltaMs = now - lastTime;

    if (deltaMs > minWindowMs && deltaBytes > 0) {
      lastBytes = bytes;
      lastTime = now;
      return deltaBytes / (deltaMs / 1000);
    }

    return undefined;
  };
}

export interface FileTransferContextValue {
  downloads: Download[];
  uploads: Upload[];
  compressions: Compression[];
  extractions: Extraction[];
  indexers: ActiveIndexer[];
  copies: Copy[];
  moves: Move[];
  transfers: Transfer[];
  startDownload: (paths: string[]) => Promise<void>;
  cancelDownload: (id: string) => void;
  startCompression: (options: {
    paths: string[];
    archiveName: string;
    destination: string;
    onComplete?: () => void;
  }) => Promise<void>;
  cancelCompression: (id: string) => void;
  startExtraction: (options: {
    archivePath: string;
    destination?: string;
    onComplete?: () => void;
  }) => Promise<void>;
  cancelExtraction: (id: string) => void;
  startIndexer: (options: {
    path?: string;
    onComplete?: (result: Indexer) => void;
  }) => Promise<void>;
  isIndexing: boolean;
  isIndexerDialogOpen: boolean;
  openIndexerDialog: () => void;
  closeIndexerDialog: () => void;
  lastIndexerResult: Indexer | null;
  lastIndexerError: string | null;
  startCopy: (options: {
    source: string;
    destination: string;
    onComplete?: () => void;
  }) => Promise<void>;
  cancelCopy: (id: string) => void;
  startMove: (options: {
    source: string;
    destination: string;
    onComplete?: () => void;
  }) => Promise<void>;
  cancelMove: (id: string) => void;
  startUpload: (
    entries: { file?: File; relativePath: string; isDirectory: boolean }[],
    targetPath: string,
    override?: boolean,
  ) => Promise<{
    conflicts: {
      file?: File;
      relativePath: string;
      isDirectory: boolean;
    }[];
    uploaded: number;
    failures: { path: string; message: string }[];
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
  const [indexers, setIndexers] = useState<ActiveIndexer[]>([]);
  const [isIndexerDialogOpen, setIsIndexerDialogOpen] = useState(false);
  const [lastIndexerResult, setLastIndexerResult] = useState<Indexer | null>(
    null,
  );
  const [lastIndexerError, setLastIndexerError] = useState<string | null>(null);
  const [copies, setCopies] = useState<Copy[]>([]);
  const [moves, setMoves] = useState<Move[]>([]);
  const activeCompressionIdsRef = useRef<Set<string>>(new Set());
  const activeExtractionIdsRef = useRef<Set<string>>(new Set());
  const activeIndexerIdsRef = useRef<Set<string>>(new Set());
  const remoteIndexerActiveRef = useRef(false);
  const attachedStreamRef = useRef(false);
  const activeCopyIdsRef = useRef<Set<string>>(new Set());
  const activeMoveIdsRef = useRef<Set<string>>(new Set());
  const downloadLabelCounterRef = useRef<Map<string, number>>(new Map());
  const downloadLabelAssignmentRef = useRef<Map<string, string>>(new Map());
  const transferRatesRef = useRef<
    Map<string, { bytes: number; timestamp: number; emitted: boolean }>
  >(new Map());
  const { run: runStreamResult } = useStreamResult();
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
    ...indexers,
    ...copies,
    ...moves,
  ];

  const isIndexing = indexers.length > 0;

  const openIndexerDialog = useCallback(() => {
    setIsIndexerDialogOpen(true);
  }, []);

  const closeIndexerDialog = useCallback(() => {
    setIsIndexerDialogOpen(false);
  }, []);

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
      releaseDownloadLabelBase(id);
      transferRatesRef.current.delete(id);
      streamRefsRef.current.delete(id);
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
      _downloadLabelBase: string,
      abortSignal: AbortSignal,
      formatDownloadLabel: (
        stage: string,
        options?: { percent?: number; name?: string },
      ) => string,
    ) => {
      const isSingleFile = paths.length === 1 && !paths[0].endsWith("/");
      const chunks: Uint8Array[] = [];
      let lastBytes = 0;
      let lastTime = Date.now();
      await runStreamResult({
        open: () => openFileDownloadStream(paths),
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
          const now = Date.now();
          const deltaBytes = progress.bytes - lastBytes;
          const deltaMs = now - lastTime;

          let speed: number | undefined;
          if (deltaMs > 500 && deltaBytes > 0) {
            speed = deltaBytes / (deltaMs / 1000);
            lastBytes = progress.bytes;
            lastTime = now;
          }

          let phaseLabel: string;
          switch (progress.phase) {
            case "preparing":
              phaseLabel = "Preparing";
              break;
            case "compressing":
              phaseLabel = "Compressing";
              break;
            case "streaming":
            default:
              phaseLabel = "Downloading";
              break;
          }

          updateDownload(reqId, {
            progress: progress.pct,
            label: formatDownloadLabel(phaseLabel, { percent: progress.pct }),
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
    [runStreamResult, updateDownload],
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

      if (!isConnected()) {
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
        const fileName =
          paths.length === 1 ? downloadLabelBase : `${downloadLabelBase}.zip`;
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
        download.abortController.abort();
        // Abort stream if using stream-based download (RST for immediate cancel)
        // Use ref first (synchronous) then fallback to state
        const stream = streamRefsRef.current.get(id) || download.stream;
        if (stream) {
          stream.abort(); // Use abort() instead of close() for immediate cancellation
          streamRefsRef.current.delete(id);
        }
        toast.info("Download cancelled");
        removeDownload(id);
      }
    },
    [downloads, removeDownload],
  );

  const removeCompression = useCallback(
    (id: string) => {
      if (!activeCompressionIdsRef.current.has(id)) {
        return;
      }
      activeCompressionIdsRef.current.delete(id);

      setCompressions((prev) => prev.filter((c) => c.id !== id));
      releaseDownloadLabelBase(id);
      streamRefsRef.current.delete(id);
    },
    [releaseDownloadLabelBase],
  );

  const startCompression = useCallback(
    async ({
      paths,
      archiveName,
      destination,
      onComplete,
    }: {
      paths: string[];
      archiveName: string;
      destination: string;
      onComplete?: () => void;
    }) => {
      if (!paths.length) return;

      const id = crypto.randomUUID();
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

      if (!isConnected()) {
        toast.error("Stream connection not ready");
        removeCompression(id);
        return;
      }

      const format = archiveName.toLowerCase().endsWith(".tar.gz")
        ? "tar.gz"
        : "zip";
      const fullDestination = destination.endsWith("/")
        ? `${destination}${archiveName}`
        : `${destination}/${archiveName}`;

      void runStreamResult<void, ProgressFrame>({
        open: () => openFileCompressStream(paths, fullDestination, format),
        signal: abortController.signal,
        closeOnAbort: "none",
        openErrorMessage: "Failed to open compression stream",
        closeMessage: "Compression stream closed unexpectedly",
        onOpen: (stream) => {
          streamRefsRef.current.set(id, stream);
          setCompressions((prev) =>
            prev.map((c) => (c.id === id ? { ...c, stream } : c)),
          );
        },
        onProgress: (progress) => {
          const percent = Math.min(99, progress.pct);
          setCompressions((prev) =>
            prev.map((c) => {
              if (c.id !== id) return c;
              const next = Math.max(c.progress, percent);
              if (next === c.progress) return c;
              return {
                ...c,
                progress: next,
                label: `Compressing ${labelBase} (${next}%)`,
              };
            }),
          );
        },
        onSuccess: () => {
          toast.success(`Created ${labelBase}`);
          onComplete?.();
        },
        onError: (error: unknown) => {
          if (abortController.signal.aborted) {
            return;
          }
          const message =
            error instanceof Error ? error.message : "Compression failed";
          toast.error(message);
        },
        onFinally: () => {
          streamRefsRef.current.delete(id);
          setCompressions((prev) =>
            prev.map((c) => (c.id === id ? { ...c, stream: null } : c)),
          );
          removeCompression(id);
        },
      });
    },
    [allocateDownloadLabelBase, removeCompression, runStreamResult],
  );

  const cancelCompression = useCallback(
    (id: string) => {
      const compression = compressions.find((c) => c.id === id);
      if (compression) {
        // Abort stream if using stream-based compression
        const stream = streamRefsRef.current.get(id) || compression.stream;
        if (stream) {
          stream.abort();
          streamRefsRef.current.delete(id);
        }
        compression.abortController.abort();
        toast.info("Compression cancelled");
        removeCompression(id);
      }
    },
    [compressions, removeCompression],
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
      releaseDownloadLabelBase(id);
      streamRefsRef.current.delete(id);
    },
    [releaseDownloadLabelBase],
  );

  const startExtraction = useCallback(
    async ({
      archivePath,
      destination,
      onComplete,
    }: {
      archivePath: string;
      destination?: string;
      onComplete?: () => void;
    }) => {
      if (!archivePath) {
        throw new Error("No archive specified for extraction");
      }

      const id = crypto.randomUUID();
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

      if (!isConnected()) {
        toast.error("Stream connection not ready");
        removeExtraction(id);
        return;
      }

      void runStreamResult<void, ProgressFrame>({
        open: () => openFileExtractStream(archivePath, destination),
        signal: abortController.signal,
        closeOnAbort: "none",
        openErrorMessage: "Failed to open extraction stream",
        closeMessage: "Extraction stream closed unexpectedly",
        onOpen: (stream) => {
          streamRefsRef.current.set(id, stream);
          setExtractions((prev) =>
            prev.map((e) => (e.id === id ? { ...e, stream } : e)),
          );
        },
        onProgress: (progress) => {
          const percent = Math.min(99, progress.pct);
          setExtractions((prev) =>
            prev.map((item) => {
              if (item.id !== id) return item;
              const next = Math.max(item.progress, percent);
              if (next === item.progress) return item;
              return {
                ...item,
                progress: next,
                label: `Extracting ${labelBase} (${next}%)`,
              };
            }),
          );
        },
        onSuccess: () => {
          toast.success(`Extracted ${labelBase}`);
          onComplete?.();
        },
        onError: (error: unknown) => {
          if (abortController.signal.aborted) {
            return;
          }
          const message =
            error instanceof Error ? error.message : "Extraction failed";
          toast.error(message);
        },
        onFinally: () => {
          streamRefsRef.current.delete(id);
          setExtractions((prev) =>
            prev.map((e) => (e.id === id ? { ...e, stream: null } : e)),
          );
          removeExtraction(id);
        },
      });
    },
    [allocateDownloadLabelBase, removeExtraction, runStreamResult],
  );

  const cancelExtraction = useCallback(
    (id: string) => {
      const extraction = extractions.find((item) => item.id === id);
      if (extraction) {
        // Abort stream if using stream-based extraction
        const stream = streamRefsRef.current.get(id) || extraction.stream;
        if (stream) {
          stream.abort();
          streamRefsRef.current.delete(id);
        }
        extraction.abortController.abort();
        toast.info("Extraction cancelled");
        removeExtraction(id);
      }
    },
    [extractions, removeExtraction],
  );

  const removeIndexer = useCallback((id: string) => {
    if (!activeIndexerIdsRef.current.has(id)) {
      return;
    }
    activeIndexerIdsRef.current.delete(id);
    if (id === REMOTE_INDEXER_ID) {
      remoteIndexerActiveRef.current = false;
    }
    setIndexers((prev) => prev.filter((r) => r.id !== id));
    streamRefsRef.current.delete(id);
  }, []);

  const hasLocalIndexerInProgress = useCallback(() => {
    for (const id of activeIndexerIdsRef.current) {
      if (id !== REMOTE_INDEXER_ID) {
        return true;
      }
    }
    return false;
  }, []);

  const clearRemoteIndexer = useCallback(() => {
    if (!remoteIndexerActiveRef.current) {
      return;
    }

    remoteIndexerActiveRef.current = false;
    activeIndexerIdsRef.current.delete(REMOTE_INDEXER_ID);
    streamRefsRef.current.delete(REMOTE_INDEXER_ID);
    setIndexers((prev) =>
      prev.filter((transfer) => transfer.id !== REMOTE_INDEXER_ID),
    );
  }, []);

  const attachToRunningIndexer = useCallback(
    ({ force = false }: { force?: boolean } = {}) => {
      if (
        attachedStreamRef.current ||
        (!force && hasLocalIndexerInProgress()) ||
        !isConnected()
      ) {
        return false;
      }

      const stream = openFileIndexerAttachStream();
      if (!stream) {
        return false;
      }

      attachedStreamRef.current = true;

      void runStreamResult<
        | {
            files_indexed?: number;
            dirs_indexed?: number;
            total_size?: number;
            duration_ms?: number;
          }
        | undefined,
        ProgressFrame
      >({
        open: () => stream,
        signal: new AbortController().signal,
        closeOnAbort: "none",
        onProgress: (progress) => {
          const progressData = progress as ProgressFrame & {
            files_indexed?: number;
            dirs_indexed?: number;
            current_path?: string;
            phase?: string;
          };

          const filesIndexed = Math.max(0, progressData.files_indexed ?? 0);
          const dirsIndexed = Math.max(0, progressData.dirs_indexed ?? 0);
          const currentPath = progressData.current_path ?? "";
          const phase = progressData.phase ?? "indexing";
          const label =
            filesIndexed > 0 || dirsIndexed > 0
              ? `Indexing: ${filesIndexed} files, ${dirsIndexed} dirs`
              : "Indexing in progress...";

          setIndexers((prev) => {
            const existing = prev.find((item) => item.id === REMOTE_INDEXER_ID);
            const remoteTransfer: ActiveIndexer = {
              id: REMOTE_INDEXER_ID,
              type: "indexer",
              path: "/",
              filesIndexed: Math.max(existing?.filesIndexed ?? 0, filesIndexed),
              dirsIndexed: Math.max(existing?.dirsIndexed ?? 0, dirsIndexed),
              totalSize: 0,
              durationMs: 0,
              currentPath,
              phase,
              progress: existing?.progress ?? 0,
              label,
              abortController:
                existing?.abortController ?? new AbortController(),
              stream: null,
            };

            if (existing) {
              return prev.map((item) =>
                item.id === REMOTE_INDEXER_ID ? remoteTransfer : item,
              );
            }
            return [...prev, remoteTransfer];
          });

          activeIndexerIdsRef.current.add(REMOTE_INDEXER_ID);
          remoteIndexerActiveRef.current = true;
        },
        onSuccess: (result) => {
          const summary = {
            path: "/",
            filesIndexed: result?.files_indexed ?? 0,
            dirsIndexed: result?.dirs_indexed ?? 0,
            totalSize: result?.total_size ?? 0,
            durationMs: result?.duration_ms ?? 0,
          };
          clearRemoteIndexer();
          setLastIndexerResult(summary);
          setLastIndexerError(null);
        },
        onError: (error: unknown) => {
          clearRemoteIndexer();
          if (isIndexerAttachUnavailableError(error)) {
            return;
          }
          const message =
            error instanceof Error
              ? error.message
              : "Failed to attach to running indexer";
          setLastIndexerError(message);
        },
        onFinally: () => {
          attachedStreamRef.current = false;
        },
      });

      return true;
    },
    [clearRemoteIndexer, hasLocalIndexerInProgress, runStreamResult],
  );

  const removeCopy = useCallback((id: string) => {
    if (!activeCopyIdsRef.current.has(id)) {
      return;
    }
    activeCopyIdsRef.current.delete(id);
    setCopies((prev) => prev.filter((c) => c.id !== id));
    streamRefsRef.current.delete(id);
  }, []);

  const removeMove = useCallback((id: string) => {
    if (!activeMoveIdsRef.current.has(id)) {
      return;
    }
    activeMoveIdsRef.current.delete(id);
    setMoves((prev) => prev.filter((m) => m.id !== id));
    streamRefsRef.current.delete(id);
  }, []);

  const startIndexer = useCallback(
    async ({
      path = "/",
      onComplete,
    }: {
      path?: string;
      onComplete?: (result: Indexer) => void;
    }) => {
      // Only allow one indexer task at a time
      if (activeIndexerIdsRef.current.size > 0) {
        setIsIndexerDialogOpen(true);
        return;
      }

      setIsIndexerDialogOpen(true);

      if (!isConnected()) {
        setLastIndexerError("Stream connection not ready");
        toast.error("Stream connection not ready");
        return;
      }

      setLastIndexerResult(null);
      setLastIndexerError(null);

      const id = crypto.randomUUID();
      const abortController = new AbortController();

      const indexerTask: ActiveIndexer = {
        id,
        type: "indexer",
        path,
        filesIndexed: 0,
        dirsIndexed: 0,
        totalSize: 0,
        durationMs: 0,
        currentPath: "",
        phase: "connecting",
        progress: 0,
        label: "Starting indexer...",
        abortController,
      };

      setIndexers((prev) => [...prev, indexerTask]);
      activeIndexerIdsRef.current.add(id);

      void runStreamResult<
        | {
            files_indexed?: number;
            dirs_indexed?: number;
            total_size?: number;
            duration_ms?: number;
          }
        | undefined,
        ProgressFrame
      >({
        open: () => openFileIndexerStream(path),
        signal: abortController.signal,
        closeOnAbort: "none",
        openErrorMessage: "Failed to open indexer stream",
        closeMessage: "Indexer stream closed unexpectedly",
        onOpen: (stream) => {
          streamRefsRef.current.set(id, stream);
          setIndexers((prev) =>
            prev.map((r) => (r.id === id ? { ...r, stream } : r)),
          );
        },
        onProgress: (progress) => {
          const progressData = progress as ProgressFrame & {
            files_indexed?: number;
            dirs_indexed?: number;
            current_path?: string;
            phase?: string;
          };

          setIndexers((prev) =>
            prev.map((r) => {
              if (r.id !== id) return r;
              const filesIndexed = progressData.files_indexed ?? r.filesIndexed;
              const dirsIndexed = progressData.dirs_indexed ?? r.dirsIndexed;
              const currentPath = progressData.current_path ?? r.currentPath;
              const phase = progressData.phase ?? "indexing";

              return {
                ...r,
                filesIndexed,
                dirsIndexed,
                currentPath,
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
          const summary = {
            path,
            filesIndexed: result?.files_indexed ?? 0,
            dirsIndexed: result?.dirs_indexed ?? 0,
            totalSize: result?.total_size ?? 0,
            durationMs: result?.duration_ms ?? 0,
          };
          setLastIndexerResult(summary);
          setLastIndexerError(null);
          toast.success(
            `Indexing complete: ${result?.files_indexed ?? 0} files, ${result?.dirs_indexed ?? 0} dirs`,
          );
          onComplete?.(summary);
        },
        onError: (error: unknown) => {
          if (abortController.signal.aborted) {
            return;
          }
          if (isIndexerConflictError(error)) {
            setLastIndexerError(null);
            setIsIndexerDialogOpen(true);
            if (!attachToRunningIndexer({ force: true })) {
              const message =
                error instanceof Error
                  ? error.message
                  : "Another indexing operation is already running";
              setLastIndexerError(message);
            }
            return;
          }
          const message =
            error instanceof Error ? error.message : "Indexing failed";
          setLastIndexerError(message);
          setLastIndexerResult(null);
          toast.error(message);
        },
        onFinally: () => {
          streamRefsRef.current.delete(id);
          setIndexers((prev) =>
            prev.map((r) => (r.id === id ? { ...r, stream: null } : r)),
          );
          removeIndexer(id);
        },
      });
    },
    [attachToRunningIndexer, removeIndexer, runStreamResult],
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
    transferRatesRef.current.delete(id);
    streamRefsRef.current.delete(id);
  }, []);

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
      if (!isConnected()) {
        return { success: false, error: "Stream connection not ready" };
      }

      const stream = openFileUploadStream(targetPath, file.size);
      if (!stream) {
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
          setUploads((prev) =>
            prev.map((u) => (u.id === uploadId ? { ...u, stream: null } : u)),
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
          if (bytesSent - bytesAcked >= UPLOAD_WINDOW_SIZE) {
            pendingSend = true;
            return; // Will resume when onProgress fires
          }

          const slice = file.slice(offset, offset + STREAM_CHUNK_SIZE);
          reader.readAsArrayBuffer(slice);
        };

        const unbind = bindStreamHandlers(stream, {
          onProgress: (progress: ProgressFrame) => {
            bytesAcked = progress.bytes;
            onProgress(progress.bytes, progress.total);

            // Window opened - resume sending if we were waiting
            if (pendingSend && bytesSent - bytesAcked < UPLOAD_WINDOW_SIZE) {
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
    [],
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
            // Check if aborted before making the call
            if (abortController.signal.aborted) break;
            await linuxio.filebrowser.resource_post.call(
              dirPath,
              override ? "true" : undefined,
            );
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
      updateUpload,
      removeUpload,
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

  const startCopy = useCallback(
    async ({
      source,
      destination,
      onComplete,
    }: {
      source: string;
      destination: string;
      onComplete?: () => void;
    }) => {
      if (!source || !destination) {
        throw new Error("Invalid copy parameters");
      }

      const id = crypto.randomUUID();
      const abortController = new AbortController();

      const deriveLabelBase = () => {
        const trimmed = source.replace(/\/+$/, "");
        const parts = trimmed.split("/");
        return parts[parts.length - 1] || "item";
      };
      const labelBase = deriveLabelBase();

      const copy: Copy = {
        id,
        type: "copy",
        source,
        destination,
        progress: 0,
        label: `Copying ${labelBase} (0%)`,
        speed: undefined,
        abortController,
      };

      setCopies((prev) => [...prev, copy]);
      activeCopyIdsRef.current.add(id);

      if (!isConnected()) {
        toast.error("Stream connection not ready");
        removeCopy(id);
        return;
      }

      const getSpeed = createProgressSpeedCalculator();

      void runStreamResult<void, ProgressFrame>({
        open: () => openFileCopyStream(source, destination),
        signal: abortController.signal,
        closeOnAbort: "none",
        openErrorMessage: "Failed to open copy stream",
        closeMessage: "Copy stream closed unexpectedly",
        onOpen: (stream) => {
          streamRefsRef.current.set(id, stream);
          setCopies((prev) =>
            prev.map((c) => (c.id === id ? { ...c, stream } : c)),
          );
        },
        onProgress: (progress) => {
          const percent = Math.min(99, progress.pct);
          const speed = getSpeed(progress.bytes);

          setCopies((prev) =>
            prev.map((c) => {
              if (c.id !== id) return c;
              const next = Math.max(c.progress, percent);
              if (next === c.progress && speed === undefined) return c;
              return {
                ...c,
                progress: next,
                label: `Copying ${labelBase} (${next}%)`,
                bytes: progress.bytes,
                total: progress.total,
                ...(speed !== undefined && { speed }),
              };
            }),
          );
        },
        onSuccess: () => {
          toast.success(`Copied ${labelBase}`);
          onComplete?.();
        },
        onError: (error: unknown) => {
          if (abortController.signal.aborted) {
            return;
          }
          const message =
            error instanceof Error ? error.message : "Copy failed";
          toast.error(message);
        },
        onFinally: () => {
          streamRefsRef.current.delete(id);
          setCopies((prev) =>
            prev.map((c) => (c.id === id ? { ...c, stream: null } : c)),
          );
          removeCopy(id);
        },
      });
    },
    [removeCopy, runStreamResult],
  );

  const cancelCopy = useCallback(
    (id: string) => {
      const copy = copies.find((c) => c.id === id);
      if (copy) {
        const stream = streamRefsRef.current.get(id) || copy.stream;
        if (stream) {
          stream.abort();
          streamRefsRef.current.delete(id);
        }
        copy.abortController.abort();
        toast.info("Copy cancelled");
        removeCopy(id);
      }
    },
    [copies, removeCopy],
  );

  const startMove = useCallback(
    async ({
      source,
      destination,
      onComplete,
    }: {
      source: string;
      destination: string;
      onComplete?: () => void;
    }) => {
      if (!source || !destination) {
        throw new Error("Invalid move parameters");
      }

      const id = crypto.randomUUID();
      const abortController = new AbortController();

      const deriveLabelBase = () => {
        const trimmed = source.replace(/\/+$/, "");
        const parts = trimmed.split("/");
        return parts[parts.length - 1] || "item";
      };
      const labelBase = deriveLabelBase();

      const move: Move = {
        id,
        type: "move",
        source,
        destination,
        progress: 0,
        label: `Moving ${labelBase} (0%)`,
        speed: undefined,
        abortController,
      };

      setMoves((prev) => [...prev, move]);
      activeMoveIdsRef.current.add(id);

      if (!isConnected()) {
        toast.error("Stream connection not ready");
        removeMove(id);
        return;
      }

      const getSpeed = createProgressSpeedCalculator();

      void runStreamResult<void, ProgressFrame>({
        open: () => openFileMoveStream(source, destination),
        signal: abortController.signal,
        closeOnAbort: "none",
        openErrorMessage: "Failed to open move stream",
        closeMessage: "Move stream closed unexpectedly",
        onOpen: (stream) => {
          streamRefsRef.current.set(id, stream);
          setMoves((prev) =>
            prev.map((m) => (m.id === id ? { ...m, stream } : m)),
          );
        },
        onProgress: (progress) => {
          const percent = Math.min(99, progress.pct);
          const speed = getSpeed(progress.bytes);

          setMoves((prev) =>
            prev.map((m) => {
              if (m.id !== id) return m;
              const next = Math.max(m.progress, percent);
              if (next === m.progress && speed === undefined) return m;
              return {
                ...m,
                progress: next,
                label: `Moving ${labelBase} (${next}%)`,
                bytes: progress.bytes,
                total: progress.total,
                ...(speed !== undefined && { speed }),
              };
            }),
          );
        },
        onSuccess: () => {
          toast.success(`Moved ${labelBase}`);
          onComplete?.();
        },
        onError: (error: unknown) => {
          if (abortController.signal.aborted) {
            return;
          }
          const message =
            error instanceof Error ? error.message : "Move failed";
          toast.error(message);
        },
        onFinally: () => {
          streamRefsRef.current.delete(id);
          setMoves((prev) =>
            prev.map((m) => (m.id === id ? { ...m, stream: null } : m)),
          );
          removeMove(id);
        },
      });
    },
    [removeMove, runStreamResult],
  );

  const cancelMove = useCallback(
    (id: string) => {
      const move = moves.find((m) => m.id === id);
      if (move) {
        const stream = streamRefsRef.current.get(id) || move.stream;
        if (stream) {
          stream.abort();
          streamRefsRef.current.delete(id);
        }
        move.abortController.abort();
        toast.info("Move cancelled");
        removeMove(id);
      }
    },
    [moves, removeMove],
  );

  return (
    <FileTransferContext.Provider
      value={{
        downloads,
        uploads,
        compressions,
        extractions,
        indexers,
        copies,
        moves,
        transfers,
        startDownload,
        cancelDownload,
        startCompression,
        cancelCompression,
        startExtraction,
        cancelExtraction,
        startIndexer,
        isIndexing,
        isIndexerDialogOpen,
        openIndexerDialog,
        closeIndexerDialog,
        lastIndexerResult,
        lastIndexerError,
        startCopy,
        cancelCopy,
        startMove,
        cancelMove,
        startUpload,
        cancelUpload,
      }}
    >
      {children}
    </FileTransferContext.Provider>
  );
};
