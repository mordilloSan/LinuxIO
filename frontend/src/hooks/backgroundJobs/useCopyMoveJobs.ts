import { useCallback, useState } from "react";
import { toast } from "sonner";

import type { Copy, Move } from "@/types/backgroundJobs";

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

import type { BackgroundJobRuntime } from "./useBackgroundJobRuntime";

export function useCopyMoveJobs(runtime: BackgroundJobRuntime) {
  const [copies, setCopies] = useState<Copy[]>([]);
  const [moves, setMoves] = useState<Move[]>([]);
  const { run: runStreamResult } = useStreamResult();
  const {
    activeCopyIdsRef,
    activeMoveIdsRef,
    pendingLocalJobKeysRef,
    streamRefsRef,
    cancelBridgeJob,
  } = runtime;

  const removeCopy = useCallback(
    (id: string) => {
      if (!activeCopyIdsRef.current.has(id)) {
        return;
      }
      activeCopyIdsRef.current.delete(id);
      setCopies((prev) => prev.filter((c) => c.id !== id));
      streamRefsRef.current.delete(id);
    },
    [activeCopyIdsRef, streamRefsRef],
  );

  const removeMove = useCallback(
    (id: string) => {
      if (!activeMoveIdsRef.current.has(id)) {
        return;
      }
      activeMoveIdsRef.current.delete(id);
      setMoves((prev) => prev.filter((m) => m.id !== id));
      streamRefsRef.current.delete(id);
    },
    [activeMoveIdsRef, streamRefsRef],
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

      if (!isConnected()) {
        toast.error("Stream connection not ready");
        return;
      }

      const pendingKey = jobIdentityKey(JobTypes.JOB_TYPE_FILE_COPY, {
        source,
        destination,
      });
      pendingLocalJobKeysRef.current.add(pendingKey);

      let job: JobSnapshot;
      try {
        job = await linuxio.filebrowser.copy({ source, destination });
      } catch (error) {
        toast.error(
          error instanceof Error ? error.message : "Failed to start copy",
        );
        pendingLocalJobKeysRef.current.delete(pendingKey);
        return;
      }

      const id = job.id;
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

      activeCopyIdsRef.current.add(id);
      pendingLocalJobKeysRef.current.delete(pendingKey);
      setCopies((prev) =>
        prev.some((item) => item.id === id) ? prev : [...prev, copy],
      );

      const getSpeed = createProgressSpeedCalculator();

      void runStreamResult<void, ProgressFrame>({
        open: () => openJobAttachStream(id),
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
    [
      activeCopyIdsRef,
      pendingLocalJobKeysRef,
      removeCopy,
      runStreamResult,
      streamRefsRef,
    ],
  );

  const cancelCopy = useCallback(
    (id: string) => {
      const copy = copies.find((c) => c.id === id);
      if (copy) {
        copy.abortController.abort();
        const stream = streamRefsRef.current.get(id) || copy.stream;
        if (stream) {
          stream.abort();
          streamRefsRef.current.delete(id);
        }
        cancelBridgeJob(id);
        toast.info("Copy cancelled");
        removeCopy(id);
      }
    },
    [cancelBridgeJob, copies, removeCopy, streamRefsRef],
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

      if (!isConnected()) {
        toast.error("Stream connection not ready");
        return;
      }

      const pendingKey = jobIdentityKey(JobTypes.JOB_TYPE_FILE_MOVE, {
        source,
        destination,
      });
      pendingLocalJobKeysRef.current.add(pendingKey);

      let job: JobSnapshot;
      try {
        job = await linuxio.filebrowser.move({ source, destination });
      } catch (error) {
        toast.error(
          error instanceof Error ? error.message : "Failed to start move",
        );
        pendingLocalJobKeysRef.current.delete(pendingKey);
        return;
      }

      const id = job.id;
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

      activeMoveIdsRef.current.add(id);
      pendingLocalJobKeysRef.current.delete(pendingKey);
      setMoves((prev) =>
        prev.some((item) => item.id === id) ? prev : [...prev, move],
      );

      const getSpeed = createProgressSpeedCalculator();

      void runStreamResult<void, ProgressFrame>({
        open: () => openJobAttachStream(id),
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
    [
      activeMoveIdsRef,
      pendingLocalJobKeysRef,
      removeMove,
      runStreamResult,
      streamRefsRef,
    ],
  );

  const cancelMove = useCallback(
    (id: string) => {
      const move = moves.find((m) => m.id === id);
      if (move) {
        move.abortController.abort();
        const stream = streamRefsRef.current.get(id) || move.stream;
        if (stream) {
          stream.abort();
          streamRefsRef.current.delete(id);
        }
        cancelBridgeJob(id);
        toast.info("Move cancelled");
        removeMove(id);
      }
    },
    [cancelBridgeJob, moves, removeMove, streamRefsRef],
  );

  return {
    copies,
    moves,
    startCopy,
    cancelCopy,
    startMove,
    cancelMove,
    recoveryControls: {
      setCopies,
      setMoves,
      removeCopy,
      removeMove,
    },
  };
}
