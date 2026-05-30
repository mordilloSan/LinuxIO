import { type RefObject, useCallback, useMemo, useRef } from "react";

import { linuxio, type Stream } from "@/api";
import { type CountedSet, makeCountedSet } from "@/utils/backgroundJobs";

const TRANSFER_RATE_SAMPLE_MS = 1000;

interface TransferRateSample {
  bytes: number;
  emitted: boolean;
  timestamp: number;
}

export interface BackgroundJobRuntime {
  activeBackgroundJobIdsRef: RefObject<Set<string>>;
  activeCompressionIdsRef: RefObject<Set<string>>;
  activeCopyIdsRef: RefObject<Set<string>>;
  activeExtractionIdsRef: RefObject<Set<string>>;
  activeFileTransferJobIdsRef: RefObject<Set<string>>;
  activeIndexerIdsRef: RefObject<Set<string>>;
  activeMoveIdsRef: RefObject<Set<string>>;
  allocateDownloadLabelBase: (base: string, id: string) => string;
  cancelBridgeJob: (id: string) => void;
  pendingLocalJobKeysRef: RefObject<CountedSet>;
  primeTransferRate: (id: string, initialBytes?: number) => void;
  recordTransferRate: (
    id: string,
    bytesProcessed?: number,
  ) => number | undefined;
  recoveringJobIdsRef: RefObject<Set<string>>;
  releaseDownloadLabelBase: (id: string) => void;
  streamRefsRef: RefObject<Map<string, Stream>>;
  transferRatesRef: RefObject<Map<string, TransferRateSample>>;
}

export function useBackgroundJobRuntime(): BackgroundJobRuntime {
  const activeCompressionIdsRef = useRef<Set<string>>(new Set());
  const activeExtractionIdsRef = useRef<Set<string>>(new Set());
  const activeIndexerIdsRef = useRef<Set<string>>(new Set());
  const activeCopyIdsRef = useRef<Set<string>>(new Set());
  const activeMoveIdsRef = useRef<Set<string>>(new Set());
  const activeBackgroundJobIdsRef = useRef<Set<string>>(new Set());
  const activeFileTransferJobIdsRef = useRef<Set<string>>(new Set());
  const recoveringJobIdsRef = useRef<Set<string>>(new Set());
  const pendingLocalJobKeysRef = useRef(makeCountedSet());
  const downloadLabelCounterRef = useRef<Map<string, number>>(new Map());
  const downloadLabelAssignmentRef = useRef<Map<string, string>>(new Map());
  const transferRatesRef = useRef<Map<string, TransferRateSample>>(new Map());
  const streamRefsRef = useRef<Map<string, Stream>>(new Map());

  const cancelBridgeJob = useCallback((id: string) => {
    void linuxio.jobs.cancel(id).catch((error) => {
      console.debug("Failed to cancel bridge job", error);
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
      activeCompressionIdsRef,
      activeExtractionIdsRef,
      activeIndexerIdsRef,
      activeCopyIdsRef,
      activeMoveIdsRef,
      activeBackgroundJobIdsRef,
      activeFileTransferJobIdsRef,
      recoveringJobIdsRef,
      pendingLocalJobKeysRef,
      streamRefsRef,
      transferRatesRef,
      cancelBridgeJob,
      recordTransferRate,
      primeTransferRate,
      allocateDownloadLabelBase,
      releaseDownloadLabelBase,
    }),
    [
      activeBackgroundJobIdsRef,
      activeCompressionIdsRef,
      activeCopyIdsRef,
      activeExtractionIdsRef,
      activeFileTransferJobIdsRef,
      activeIndexerIdsRef,
      activeMoveIdsRef,
      allocateDownloadLabelBase,
      cancelBridgeJob,
      pendingLocalJobKeysRef,
      primeTransferRate,
      recoveringJobIdsRef,
      recordTransferRate,
      releaseDownloadLabelBase,
      streamRefsRef,
      transferRatesRef,
    ],
  );
}
