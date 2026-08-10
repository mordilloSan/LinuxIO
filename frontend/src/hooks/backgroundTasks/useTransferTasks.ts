import { useCallback, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import {
  isConnected,
  type TaskSnapshot,
  linuxio,
  openTaskWatchStream,
  type ProgressFrame,
} from "@/api";
import * as TaskTypes from "@/constants/backgroundTaskTypes";
import { useLatestRef } from "@/hooks/useLatestRef";
import { useStreamResult } from "@/hooks/useStreamResult";
import type {
  Compression,
  Copy,
  CopyMoveStartOptions,
  Extraction,
  ExtractionStartOptions,
  Move,
  TransferItem,
} from "@/types/backgroundTasks";
import {
  createProgressSpeedCalculator,
  taskIdentityKey,
  taskMetadataObject,
  requestString,
} from "@/utils/backgroundTasks";
import { joinPath } from "@/utils/path";

import type { BackgroundTaskRuntime } from "./useBackgroundTaskRuntime";

type TransferKind = TransferItem["type"];

// Omit distributed over the union, so each kind keeps its specific fields.
type DistributiveOmit<T, K extends PropertyKey> = T extends unknown
  ? Omit<T, K>
  : never;

/** Kind-specific fields; the engine adds id/label/progress/abortController. */
type TransferSeed = DistributiveOmit<
  TransferItem,
  "id" | "taskId" | "abortController" | "label" | "progress" | "speed"
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

/**
 * Per-kind presentation and recovery data. Everything that differs between
 * the four transfers lives here; the lifecycle (start task → watch → progress
 * → toast → remove) is implemented once in `useTransferTasks`.
 */
interface TransferDescriptor {
  kind: TransferKind;
  taskType: string;
  /** "Compressing" — progress labels are `${active} ${labelBase} (n%)`. */
  active: string;
  /** "Created" — success toasts are `${done} ${labelBase}`. */
  done: string;
  /** "compression" — start/stream/failure/cancel copy derives from this. */
  noun: string;
  /** Archive kinds share the download-label counter for duplicate names. */
  usesLabelAllocator: boolean;
  /**
   * Whether `start*` resolves at task completion (copy/move — callers pace
   * bulk operations on it) or at registration (compress/extract — completion
   * is reported via the navbar item and toasts).
   */
  awaitCompletion: boolean;
  /**
   * Rebuild the navbar item from a recovered task's safe metadata (page reload,
   * another session): the candidate label base plus the item builder that
   * receives the possibly de-duplicated label.
   */
  fromTask: (metadata: Record<string, unknown>) => {
    candidate: string;
    build: (labelBase: string) => TransferSeed;
  };
}

const DESCRIPTORS: Record<TransferKind, TransferDescriptor> = {
  compression: {
    kind: "compression",
    taskType: TaskTypes.TASK_TYPE_FILE_COMPRESS,
    active: "Compressing",
    done: "Created",
    noun: "compression",
    usesLabelAllocator: true,
    awaitCompletion: false,
    fromTask: (metadata) => {
      const destination = requestString(metadata, "path") ?? "";
      return {
        candidate: basename(destination, "archive"),
        build: (labelBase) => ({
          type: "compression",
          archiveName: labelBase,
          destination,
          paths: [],
        }),
      };
    },
  },
  extraction: {
    kind: "extraction",
    taskType: TaskTypes.TASK_TYPE_FILE_EXTRACT,
    active: "Extracting",
    done: "Extracted",
    noun: "extraction",
    usesLabelAllocator: true,
    awaitCompletion: false,
    fromTask: (metadata) => {
      const archivePath = requestString(metadata, "path") ?? "";
      return {
        candidate: extractionLabelBase(archivePath),
        build: () => ({
          type: "extraction",
          archivePath,
          destination: "",
        }),
      };
    },
  },
  copy: {
    kind: "copy",
    taskType: TaskTypes.TASK_TYPE_FILE_COPY_BATCH,
    active: "Copying",
    done: "Copied",
    noun: "copy",
    usesLabelAllocator: false,
    awaitCompletion: true,
    fromTask: (metadata) => {
      const sources: string[] = [];
      return {
        candidate: requestString(metadata, "label") ?? "items",
        build: () => ({
          type: "copy",
          source: sources[0] ?? "",
          destination: requestString(metadata, "path") ?? "",
        }),
      };
    },
  },
  move: {
    kind: "move",
    taskType: TaskTypes.TASK_TYPE_FILE_MOVE_BATCH,
    active: "Moving",
    done: "Moved",
    noun: "move",
    usesLabelAllocator: false,
    awaitCompletion: true,
    fromTask: (metadata) => {
      const sources: string[] = [];
      return {
        candidate: requestString(metadata, "label") ?? "items",
        build: () => ({
          type: "move",
          source: sources[0] ?? "",
          destination: requestString(metadata, "path") ?? "",
        }),
      };
    },
  },
};

const DESCRIPTOR_BY_TASK_TYPE = new Map(
  Object.values(DESCRIPTORS).map((d) => [d.taskType, d]),
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
 * One engine for the four bridge-task transfers (compress, extract, copy,
 * move): fresh starts and page-reload recovery share the same lifecycle and
 * presentation, so labels, toasts, and progress handling cannot drift
 * between the two paths.
 */
export function useTransferTasks(runtime: BackgroundTaskRuntime) {
  const [transfers, setTransfers] = useState<TransferItem[]>([]);
  const transfersRef = useLatestRef(transfers);
  const activeTransferIdsRef = useRef<Set<string>>(new Set());
  const { run: runStreamResult } = useStreamResult();
  const {
    pendingLocalTaskKeysRef,
    streamRefsRef,
    cancelBridgeTask,
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
   * Watch the task stream and drive the navbar item to completion. Shared
   * verbatim by fresh starts and recovery; resolves when the task finishes.
   */
  const watchTransfer = useCallback(
    (
      descriptor: TransferDescriptor,
      id: string,
      labelBase: string,
      abortController: AbortController,
      onComplete?: () => void,
    ) => {
      const getSpeed = createProgressSpeedCalculator();
      return runStreamResult<void>({
        open: () => openTaskWatchStream(id),
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
   * Start a transfer task and register it in the navbar. Resolves per the
   * descriptor's `awaitCompletion`: at task completion (copy/move) or once
   * the task is registered (compress/extract).
   */
  const startTransfer = useCallback(
    async (
      kind: TransferKind,
      identity: readonly string[],
      startTask: () => Promise<TaskSnapshot>,
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
      // on the events stream, so recovery never adopts our own task.
      const pendingKey = taskIdentityKey(descriptor.taskType, identity);
      pendingLocalTaskKeysRef.current.add(pendingKey);

      let task: TaskSnapshot;
      try {
        task = await startTask();
      } catch (error) {
        pendingLocalTaskKeysRef.current.delete(pendingKey);
        toast.error(
          error instanceof Error
            ? error.message
            : `Failed to start ${descriptor.noun}`,
        );
        return;
      }

      const id = task.id;
      const abortController = new AbortController();
      const labelBase = descriptor.usesLabelAllocator
        ? allocateDownloadLabelBase(candidateLabelBase, id)
        : candidateLabelBase;

      activeTransferIdsRef.current.add(id);
      pendingLocalTaskKeysRef.current.delete(pendingKey);
      const item = {
        ...makeItem(labelBase),
        id,
        taskId: id,
        abortController,
        progress: 0,
        label: progressLabel(descriptor, labelBase, 0),
        speed: undefined,
      } as TransferItem;
      setTransfers((prev) =>
        prev.some((existing) => existing.id === id) ? prev : [...prev, item],
      );

      const completion = watchTransfer(
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
    [allocateDownloadLabelBase, pendingLocalTaskKeysRef, watchTransfer],
  );

  /**
   * Adopt an already-running transfer task (page reload, another session) into
   * the navbar. Returns false when the task type is not a transfer, so the
   * caller can fall through to its generic handling.
   */
  const recoverTransfer = useCallback(
    (task: TaskSnapshot): boolean => {
      const descriptor = DESCRIPTOR_BY_TASK_TYPE.get(task.type);
      if (!descriptor) {
        return false;
      }
      if (activeTransferIdsRef.current.has(task.id)) {
        return true;
      }

      const { candidate, build } = descriptor.fromTask(
        taskMetadataObject(task.metadata),
      );
      const labelBase = descriptor.usesLabelAllocator
        ? allocateDownloadLabelBase(candidate, task.id)
        : candidate;
      const progress = task.progress as ProgressFrame | undefined;
      const initialPct = Math.min(99, progress?.pct ?? 0);
      const abortController = new AbortController();

      activeTransferIdsRef.current.add(task.id);
      setTransfers((prev) => [
        ...prev,
        {
          ...build(labelBase),
          id: task.id,
          taskId: task.id,
          abortController,
          progress: initialPct,
          label: progressLabel(descriptor, labelBase, initialPct),
          bytes: progress?.bytes,
          total: progress?.total,
        },
      ]);
      void watchTransfer(descriptor, task.id, labelBase, abortController);
      return true;
    },
    [allocateDownloadLabelBase, watchTransfer],
  );

  const cancelTransfer = useCallback(
    (id: string) => {
      const item = transfersRef.current.find((transfer) => transfer.id === id);
      if (!item) {
        return;
      }
      item.abortController.abort();
      const stream = streamRefsRef.current.get(id) || item.stream;
      if (stream) {
        stream.abort();
        streamRefsRef.current.delete(id);
      }
      cancelBridgeTask(id);
      toast.info(`${capitalize(DESCRIPTORS[item.type].noun)} cancelled`);
      removeTransfer(id);
    },
    [cancelBridgeTask, removeTransfer, streamRefsRef, transfersRef],
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
        [request.targetPath],
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
      const identity = destination ? [archivePath, destination] : [archivePath];
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
        [...sources, destination],
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
        [...sources, destination],
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
