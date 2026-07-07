import { useCallback, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import type {
  Compression,
  Copy,
  CopyMoveStartOptions,
  Extraction,
  ExtractionStartOptions,
  Move,
  TransferItem,
} from "@/types/backgroundJobs";

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

type TransferKind = TransferItem["type"];

// Omit distributed over the union, so each kind keeps its specific fields.
type DistributiveOmit<T, K extends PropertyKey> = T extends unknown
  ? Omit<T, K>
  : never;

/** Kind-specific fields; the engine adds id/label/progress/abortController. */
type TransferSeed = DistributiveOmit<
  TransferItem,
  "id" | "jobId" | "abortController" | "label" | "progress" | "speed"
>;

function basename(path: string | undefined, fallback: string): string {
  const trimmed = (path ?? "").replace(/\/+$/, "");
  if (!trimmed) return fallback;
  const parts = trimmed.split("/");
  return parts[parts.length - 1] || fallback;
}

// batchLabelBase summarizes a multi-selection for a single navbar entry:
// the item's name when there is one, otherwise "<n> items".
function batchLabelBase(sources: string[]): string {
  if (sources.length === 1) {
    return basename(sources[0], "item");
  }
  return `${sources.length} items`;
}

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

/**
 * Per-kind presentation and recovery data. Everything that differs between
 * the four transfers lives here; the lifecycle (start job → attach → progress
 * → toast → remove) is implemented once in `useTransferJobs`.
 */
interface TransferDescriptor {
  kind: TransferKind;
  jobType: string;
  /** "Compressing" — progress labels are `${active} ${labelBase} (n%)`. */
  active: string;
  /** "Created" — success toasts are `${done} ${labelBase}`. */
  done: string;
  /** "compression" — start/stream/failure/cancel copy derives from this. */
  noun: string;
  /** Archive kinds share the download-label counter for duplicate names. */
  usesLabelAllocator: boolean;
  /**
   * Whether `start*` resolves at job completion (copy/move — callers pace
   * bulk operations on it) or at registration (compress/extract — completion
   * is reported via the navbar item and toasts).
   */
  awaitCompletion: boolean;
  /**
   * Rebuild the navbar item from a recovered job's request (page reload,
   * another session): the candidate label base plus the item builder that
   * receives the possibly de-duplicated label.
   */
  fromJob: (request: Record<string, unknown>) => {
    candidate: string;
    build: (labelBase: string) => TransferSeed;
  };
}

const DESCRIPTORS: Record<TransferKind, TransferDescriptor> = {
  compression: {
    kind: "compression",
    jobType: JobTypes.JOB_TYPE_FILE_COMPRESS,
    active: "Compressing",
    done: "Created",
    noun: "compression",
    usesLabelAllocator: true,
    awaitCompletion: false,
    fromJob: (request) => {
      const destination = requestString(request, "targetPath") ?? "";
      return {
        candidate: basename(destination, "archive"),
        build: (labelBase) => ({
          type: "compression",
          archiveName: labelBase,
          destination,
          paths: requestStringArray(request, "paths"),
        }),
      };
    },
  },
  extraction: {
    kind: "extraction",
    jobType: JobTypes.JOB_TYPE_FILE_EXTRACT,
    active: "Extracting",
    done: "Extracted",
    noun: "extraction",
    usesLabelAllocator: true,
    awaitCompletion: false,
    fromJob: (request) => {
      const archivePath = requestString(request, "archivePath") ?? "";
      return {
        candidate: extractionLabelBase(archivePath),
        build: () => ({
          type: "extraction",
          archivePath,
          destination: requestString(request, "destination") ?? "",
        }),
      };
    },
  },
  copy: {
    kind: "copy",
    jobType: JobTypes.JOB_TYPE_FILE_COPY_BATCH,
    active: "Copying",
    done: "Copied",
    noun: "copy",
    usesLabelAllocator: false,
    awaitCompletion: true,
    fromJob: (request) => {
      const sources = requestStringArray(request, "sources");
      return {
        candidate: batchLabelBase(sources),
        build: () => ({
          type: "copy",
          source: sources[0] ?? "",
          destination: requestString(request, "destination") ?? "",
        }),
      };
    },
  },
  move: {
    kind: "move",
    jobType: JobTypes.JOB_TYPE_FILE_MOVE_BATCH,
    active: "Moving",
    done: "Moved",
    noun: "move",
    usesLabelAllocator: false,
    awaitCompletion: true,
    fromJob: (request) => {
      const sources = requestStringArray(request, "sources");
      return {
        candidate: batchLabelBase(sources),
        build: () => ({
          type: "move",
          source: sources[0] ?? "",
          destination: requestString(request, "destination") ?? "",
        }),
      };
    },
  },
};

const DESCRIPTOR_BY_JOB_TYPE = new Map(
  Object.values(DESCRIPTORS).map((d) => [d.jobType, d]),
);

function capitalize(noun: string): string {
  return noun.charAt(0).toUpperCase() + noun.slice(1);
}

function progressLabel(
  descriptor: TransferDescriptor,
  labelBase: string,
  pct: number,
): string {
  return `${descriptor.active} ${labelBase} (${pct}%)`;
}

// Extraction navbar entries drop the archive extension.
function extractionLabelBase(archivePath: string): string {
  const raw = basename(archivePath, "archive");
  return raw.replace(/\.(tar\.gz|tgz|zip)$/i, "") || raw;
}

/**
 * One engine for the four bridge-job transfers (compress, extract, copy,
 * move): fresh starts and page-reload recovery share the same lifecycle and
 * presentation, so labels, toasts, and progress handling cannot drift
 * between the two paths.
 */
export function useTransferJobs(runtime: BackgroundJobRuntime) {
  const [transfers, setTransfers] = useState<TransferItem[]>([]);
  const activeTransferIdsRef = useRef<Set<string>>(new Set());
  const { run: runStreamResult } = useStreamResult();
  const {
    pendingLocalJobKeysRef,
    streamRefsRef,
    cancelBridgeJob,
    allocateDownloadLabelBase,
    releaseDownloadLabelBase,
  } = runtime;

  const removeTransfer = useCallback(
    (id: string) => {
      if (!activeTransferIdsRef.current.has(id)) {
        return;
      }
      activeTransferIdsRef.current.delete(id);
      setTransfers((prev) => prev.filter((item) => item.id !== id));
      releaseDownloadLabelBase(id);
      streamRefsRef.current.delete(id);
    },
    [releaseDownloadLabelBase, streamRefsRef],
  );

  /**
   * Attach to the job stream and drive the navbar item to completion. Shared
   * verbatim by fresh starts and recovery; resolves when the job finishes.
   */
  const attachTransfer = useCallback(
    (
      descriptor: TransferDescriptor,
      id: string,
      labelBase: string,
      abortController: AbortController,
      onComplete?: () => void,
    ) => {
      const getSpeed = createProgressSpeedCalculator();
      return runStreamResult<void, ProgressFrame>({
        open: () => openJobAttachStream(id),
        signal: abortController.signal,
        closeOnAbort: "none",
        openErrorMessage: `Failed to open ${descriptor.noun} stream`,
        closeMessage: `${capitalize(descriptor.noun)} stream closed unexpectedly`,
        onOpen: (stream) => {
          streamRefsRef.current.set(id, stream);
          setTransfers((prev) =>
            prev.map((item) => (item.id === id ? { ...item, stream } : item)),
          );
        },
        onProgress: (progress) => {
          const percent = Math.min(99, progress.pct);
          const speed = getSpeed(progress.bytes);
          setTransfers((prev) =>
            prev.map((item) => {
              if (item.id !== id) return item;
              const next = Math.max(item.progress, percent);
              if (next === item.progress && speed === undefined) return item;
              return {
                ...item,
                progress: next,
                label: progressLabel(descriptor, labelBase, next),
                bytes: progress.bytes,
                total: progress.total,
                ...(speed !== undefined && { speed }),
              };
            }),
          );
        },
        onSuccess: () => {
          toast.success(`${descriptor.done} ${labelBase}`);
          onComplete?.();
        },
        onError: (error: unknown) => {
          if (abortController.signal.aborted) {
            return;
          }
          toast.error(
            error instanceof Error
              ? error.message
              : `${capitalize(descriptor.noun)} failed`,
          );
        },
        onFinally: () => {
          removeTransfer(id);
        },
      });
    },
    [removeTransfer, runStreamResult, streamRefsRef],
  );

  /**
   * Start a transfer job and register it in the navbar. Resolves per the
   * descriptor's `awaitCompletion`: at job completion (copy/move) or once
   * the job is registered (compress/extract).
   */
  const startTransfer = useCallback(
    async (
      kind: TransferKind,
      identity: unknown,
      startJob: () => Promise<JobSnapshot>,
      candidateLabelBase: string,
      makeItem: (labelBase: string) => TransferSeed,
      onComplete?: () => void,
    ): Promise<void> => {
      const descriptor = DESCRIPTORS[kind];
      if (!isConnected()) {
        toast.error("Stream connection not ready");
        return;
      }

      // Bridges the gap between the start request and the snapshot arriving
      // on the events stream, so recovery never adopts our own job.
      const pendingKey = jobIdentityKey(descriptor.jobType, identity);
      pendingLocalJobKeysRef.current.add(pendingKey);

      let job: JobSnapshot;
      try {
        job = await startJob();
      } catch (error) {
        pendingLocalJobKeysRef.current.delete(pendingKey);
        toast.error(
          error instanceof Error
            ? error.message
            : `Failed to start ${descriptor.noun}`,
        );
        return;
      }

      const id = job.id;
      const abortController = new AbortController();
      const labelBase = descriptor.usesLabelAllocator
        ? allocateDownloadLabelBase(candidateLabelBase, id)
        : candidateLabelBase;

      activeTransferIdsRef.current.add(id);
      pendingLocalJobKeysRef.current.delete(pendingKey);
      const item = {
        ...makeItem(labelBase),
        id,
        jobId: id,
        abortController,
        progress: 0,
        label: progressLabel(descriptor, labelBase, 0),
        speed: undefined,
      } as TransferItem;
      setTransfers((prev) =>
        prev.some((existing) => existing.id === id) ? prev : [...prev, item],
      );

      const completion = attachTransfer(
        descriptor,
        id,
        labelBase,
        abortController,
        onComplete,
      );
      if (descriptor.awaitCompletion) {
        return completion;
      }
      void completion;
    },
    [allocateDownloadLabelBase, attachTransfer, pendingLocalJobKeysRef],
  );

  /**
   * Adopt an already-running transfer job (page reload, another session) into
   * the navbar. Returns false when the job type is not a transfer, so the
   * caller can fall through to its generic handling.
   */
  const recoverTransfer = useCallback(
    (job: JobSnapshot): boolean => {
      const descriptor = DESCRIPTOR_BY_JOB_TYPE.get(job.type);
      if (!descriptor) {
        return false;
      }
      if (activeTransferIdsRef.current.has(job.id)) {
        return true;
      }

      const { candidate, build } = descriptor.fromJob(
        requestObject(job.request),
      );
      const labelBase = descriptor.usesLabelAllocator
        ? allocateDownloadLabelBase(candidate, job.id)
        : candidate;
      const progress = job.progress as ProgressFrame | undefined;
      const initialPct = Math.min(99, progress?.pct ?? 0);
      const abortController = new AbortController();

      activeTransferIdsRef.current.add(job.id);
      setTransfers((prev) => [
        ...prev,
        {
          ...build(labelBase),
          id: job.id,
          jobId: job.id,
          abortController,
          progress: initialPct,
          label: progressLabel(descriptor, labelBase, initialPct),
          bytes: progress?.bytes,
          total: progress?.total,
        } as TransferItem,
      ]);
      void attachTransfer(descriptor, job.id, labelBase, abortController);
      return true;
    },
    [allocateDownloadLabelBase, attachTransfer],
  );

  const cancelTransfer = useCallback(
    (id: string) => {
      const item = transfers.find((transfer) => transfer.id === id);
      if (!item) {
        return;
      }
      item.abortController.abort();
      const stream = streamRefsRef.current.get(id) || item.stream;
      if (stream) {
        stream.abort();
        streamRefsRef.current.delete(id);
      }
      cancelBridgeJob(id);
      toast.info(`${capitalize(DESCRIPTORS[item.type].noun)} cancelled`);
      removeTransfer(id);
    },
    [cancelBridgeJob, removeTransfer, streamRefsRef, transfers],
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
      const format = archiveName.toLowerCase().endsWith(".tar.gz")
        ? "tar.gz"
        : "zip";
      const request = {
        format,
        targetPath: joinPath(destination, archiveName),
        paths,
      };
      return startTransfer(
        "compression",
        request,
        () => linuxio.filebrowser.compress(request),
        archiveName || "archive.zip",
        (labelBase) => ({
          type: "compression",
          archiveName: labelBase,
          destination,
          paths,
        }),
        onComplete,
      );
    },
    [startTransfer],
  );

  const startExtraction = useCallback(
    async ({
      archivePath,
      destination,
      onComplete,
    }: ExtractionStartOptions) => {
      if (!archivePath) {
        throw new Error("No archive specified for extraction");
      }
      const identity = destination
        ? { archivePath, destination }
        : { archivePath };
      return startTransfer(
        "extraction",
        identity,
        () => linuxio.filebrowser.extract({ archivePath, destination }),
        extractionLabelBase(archivePath),
        () => ({
          type: "extraction",
          archivePath,
          destination: destination || "",
        }),
        onComplete,
      );
    },
    [startTransfer],
  );

  const startCopy = useCallback(
    async ({
      sources,
      destination,
      overwrite,
      onComplete,
    }: CopyMoveStartOptions) => {
      if (!sources.length || !destination) {
        throw new Error("Invalid copy parameters");
      }
      return startTransfer(
        "copy",
        { sources, destination },
        () =>
          linuxio.filebrowser.copy_batch({ sources, destination, overwrite }),
        batchLabelBase(sources),
        () => ({ type: "copy", source: sources[0], destination }),
        onComplete,
      );
    },
    [startTransfer],
  );

  const startMove = useCallback(
    async ({
      sources,
      destination,
      overwrite,
      onComplete,
    }: CopyMoveStartOptions) => {
      if (!sources.length || !destination) {
        throw new Error("Invalid move parameters");
      }
      return startTransfer(
        "move",
        { sources, destination },
        () =>
          linuxio.filebrowser.move_batch({ sources, destination, overwrite }),
        batchLabelBase(sources),
        () => ({ type: "move", source: sources[0], destination }),
        onComplete,
      );
    },
    [startTransfer],
  );

  const byKind = useMemo(
    () => ({
      compressions: transfers.filter(
        (item): item is Compression => item.type === "compression",
      ),
      extractions: transfers.filter(
        (item): item is Extraction => item.type === "extraction",
      ),
      copies: transfers.filter((item): item is Copy => item.type === "copy"),
      moves: transfers.filter((item): item is Move => item.type === "move"),
    }),
    [transfers],
  );

  return {
    ...byKind,
    startCompression,
    startExtraction,
    startCopy,
    startMove,
    cancelTransfer,
    recoverTransfer,
  };
}
