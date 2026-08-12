import { type RefObject, useCallback, useMemo, useRef } from "react";

import { call, type Stream } from "@/api";
import { type CountedSet, makeCountedSet } from "@/utils/backgroundTasks";

const TRANSFER_RATE_SAMPLE_MS = 1000;

interface TransferRateSample {
  bytes: number;
  emitted: boolean;
  timestamp: number;
}

export interface BackgroundTaskRuntime {
  activeBackgroundTaskIdsRef: RefObject<Set<string>>;
  activeFileTransferTaskIdsRef: RefObject<Set<string>>;
  activeIndexerIdsRef: RefObject<Set<string>>;
  allocateDownloadLabelBase: (base: string, id: string) => string;
  cancelBridgeTask: (id: string) => void;
  pendingLocalTaskKeysRef: RefObject<CountedSet>;
  primeTransferRate: (id: string, initialBytes?: number) => void;
  recordTransferRate: (
    id: string,
    bytesProcessed?: number,
  ) => number | undefined;
  recoveringTaskIdsRef: RefObject<Set<string>>;
  releaseDownloadLabelBase: (id: string) => void;
  streamRefsRef: RefObject<Map<string, Stream>>;
  transferRatesRef: RefObject<Map<string, TransferRateSample>>;
}

export function useBackgroundTaskRuntime(): BackgroundTaskRuntime {
  const activeIndexerIdsRef = useRef<Set<string>>(new Set());
  const activeBackgroundTaskIdsRef = useRef<Set<string>>(new Set());
  const activeFileTransferTaskIdsRef = useRef<Set<string>>(new Set());
  const recoveringTaskIdsRef = useRef<Set<string>>(new Set());
  const pendingLocalTaskKeysRef = useMemo<RefObject<CountedSet>>(
    () => ({ current: makeCountedSet() }),
    [],
  );
  const downloadLabelCounterRef = useRef<Map<string, number>>(new Map());
  const downloadLabelAssignmentRef = useRef<Map<string, string>>(new Map());
  const transferRatesRef = useRef<Map<string, TransferRateSample>>(new Map());
  const streamRefsRef = useRef<Map<string, Stream>>(new Map());

  const cancelBridgeTask = useCallback((id: string) => {
    void call("tasks.cancel", { taskId: id }).catch((error) => {
      console.debug("Failed to cancel bridge task", error);
    });
  }, []);

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

  return useMemo(
    () => ({
      activeIndexerIdsRef,
      activeBackgroundTaskIdsRef,
      activeFileTransferTaskIdsRef,
      recoveringTaskIdsRef,
      pendingLocalTaskKeysRef,
      streamRefsRef,
      transferRatesRef,
      cancelBridgeTask,
      recordTransferRate,
      primeTransferRate,
      allocateDownloadLabelBase,
      releaseDownloadLabelBase,
    }),
    [
      activeBackgroundTaskIdsRef,
      activeFileTransferTaskIdsRef,
      activeIndexerIdsRef,
      allocateDownloadLabelBase,
      cancelBridgeTask,
      pendingLocalTaskKeysRef,
      primeTransferRate,
      recoveringTaskIdsRef,
      recordTransferRate,
      releaseDownloadLabelBase,
      streamRefsRef,
      transferRatesRef,
    ],
  );
}
