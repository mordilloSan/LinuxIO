import { useCallback, useState } from "react";
import { toast } from "sonner";

import type { Compression, Extraction } from "@/types/backgroundJobs";

import {
  isConnected,
  type JobSnapshot,
  linuxio,
  openJobAttachStream,
  type ProgressFrame,
} from "@/api";
import * as JobTypes from "@/constants/backgroundJobTypes";
import { useStreamResult } from "@/hooks/useStreamResult";
import {
  createProgressSpeedCalculator,
  jobIdentityKey,
} from "@/utils/backgroundJobs";
import { joinPath } from "@/utils/path";

import type { BackgroundJobRuntime } from "./useBackgroundJobRuntime";

export function useArchiveJobs(runtime: BackgroundJobRuntime) {
  const [compressions, setCompressions] = useState<Compression[]>([]);
  const [extractions, setExtractions] = useState<Extraction[]>([]);
  const { run: runStreamResult } = useStreamResult();
  const {
    activeCompressionIdsRef,
    activeExtractionIdsRef,
    pendingLocalJobKeysRef,
    streamRefsRef,
    cancelBridgeJob,
    allocateDownloadLabelBase,
    releaseDownloadLabelBase,
  } = runtime;

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
    [activeCompressionIdsRef, releaseDownloadLabelBase, streamRefsRef],
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

      if (!isConnected()) {
        toast.error("Stream connection not ready");
        return;
      }

      const format = archiveName.toLowerCase().endsWith(".tar.gz")
        ? "tar.gz"
        : "zip";
      const fullDestination = joinPath(destination, archiveName);
      const pendingKey = jobIdentityKey(JobTypes.JOB_TYPE_FILE_COMPRESS, {
        format,
        targetPath: fullDestination,
        paths,
      });
      pendingLocalJobKeysRef.current.add(pendingKey);
      let job: JobSnapshot;
      try {
        job = await linuxio.filebrowser.compress({
          format,
          targetPath: fullDestination,
          paths,
        });
      } catch (error) {
        pendingLocalJobKeysRef.current.delete(pendingKey);
        toast.error(
          error instanceof Error
            ? error.message
            : "Failed to start compression",
        );
        return;
      }

      const id = job.id;
      const abortController = new AbortController();
      const candidateLabelBase = archiveName || "archive.zip";
      const labelBase = allocateDownloadLabelBase(candidateLabelBase, id);

      const getSpeed = createProgressSpeedCalculator();

      const compression: Compression = {
        id,
        type: "compression",
        archiveName: labelBase,
        destination,
        paths,
        progress: 0,
        label: `Compressing ${labelBase} (0%)`,
        speed: undefined,
        abortController,
      };

      setCompressions((prev) => [...prev, compression]);
      activeCompressionIdsRef.current.add(id);
      pendingLocalJobKeysRef.current.delete(pendingKey);

      void runStreamResult<void, ProgressFrame>({
        open: () => openJobAttachStream(id),
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
          const speed = getSpeed(progress.bytes);

          setCompressions((prev) =>
            prev.map((c) => {
              if (c.id !== id) return c;
              const next = Math.max(c.progress, percent);
              if (next === c.progress && speed === undefined) return c;
              return {
                ...c,
                progress: next,
                label: `Compressing ${labelBase} (${next}%)`,
                bytes: progress.bytes,
                total: progress.total,
                ...(speed !== undefined && { speed }),
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
    [
      activeCompressionIdsRef,
      allocateDownloadLabelBase,
      pendingLocalJobKeysRef,
      removeCompression,
      runStreamResult,
      streamRefsRef,
    ],
  );

  const cancelCompression = useCallback(
    (id: string) => {
      const compression = compressions.find((c) => c.id === id);
      if (compression) {
        // Abort stream if using stream-based compression
        compression.abortController.abort();
        const stream = streamRefsRef.current.get(id) || compression.stream;
        if (stream) {
          stream.abort();
          streamRefsRef.current.delete(id);
        }
        cancelBridgeJob(id);
        toast.info("Compression cancelled");
        removeCompression(id);
      }
    },
    [cancelBridgeJob, compressions, removeCompression, streamRefsRef],
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
    [activeExtractionIdsRef, releaseDownloadLabelBase, streamRefsRef],
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

      if (!isConnected()) {
        toast.error("Stream connection not ready");
        return;
      }

      const pendingKey = jobIdentityKey(
        JobTypes.JOB_TYPE_FILE_EXTRACT,
        destination ? { archivePath, destination } : { archivePath },
      );
      pendingLocalJobKeysRef.current.add(pendingKey);
      let job: JobSnapshot;
      try {
        job = await linuxio.filebrowser.extract({
          archivePath,
          destination,
        });
      } catch (error) {
        pendingLocalJobKeysRef.current.delete(pendingKey);
        toast.error(
          error instanceof Error ? error.message : "Failed to start extraction",
        );
        return;
      }

      const id = job.id;
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

      const getSpeed = createProgressSpeedCalculator();

      const extraction: Extraction = {
        id,
        type: "extraction",
        archivePath,
        destination: destination || "",
        progress: 0,
        label: `Extracting ${labelBase} (0%)`,
        speed: undefined,
        abortController,
      };

      setExtractions((prev) => [...prev, extraction]);
      activeExtractionIdsRef.current.add(id);
      pendingLocalJobKeysRef.current.delete(pendingKey);

      void runStreamResult<void, ProgressFrame>({
        open: () => openJobAttachStream(id),
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
          const speed = getSpeed(progress.bytes);

          setExtractions((prev) =>
            prev.map((item) => {
              if (item.id !== id) return item;
              const next = Math.max(item.progress, percent);
              if (next === item.progress && speed === undefined) return item;
              return {
                ...item,
                progress: next,
                label: `Extracting ${labelBase} (${next}%)`,
                bytes: progress.bytes,
                total: progress.total,
                ...(speed !== undefined && { speed }),
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
    [
      activeExtractionIdsRef,
      allocateDownloadLabelBase,
      pendingLocalJobKeysRef,
      removeExtraction,
      runStreamResult,
      streamRefsRef,
    ],
  );

  const cancelExtraction = useCallback(
    (id: string) => {
      const extraction = extractions.find((item) => item.id === id);
      if (extraction) {
        // Abort stream if using stream-based extraction
        extraction.abortController.abort();
        const stream = streamRefsRef.current.get(id) || extraction.stream;
        if (stream) {
          stream.abort();
          streamRefsRef.current.delete(id);
        }
        cancelBridgeJob(id);
        toast.info("Extraction cancelled");
        removeExtraction(id);
      }
    },
    [cancelBridgeJob, extractions, removeExtraction, streamRefsRef],
  );

  return {
    compressions,
    extractions,
    startCompression,
    cancelCompression,
    startExtraction,
    cancelExtraction,
    recoveryControls: {
      setCompressions,
      setExtractions,
      removeCompression,
      removeExtraction,
    },
  };
}
