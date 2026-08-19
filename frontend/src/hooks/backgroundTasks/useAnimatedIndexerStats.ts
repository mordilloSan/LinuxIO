import { useEffect, useRef, useState, useSyncExternalStore } from "react";

export interface IndexerDisplayStats {
  bytesIndexed: number;
  dirsIndexed: number;
  filesIndexed: number;
}

interface UseAnimatedIndexerStatsOptions {
  enabled: boolean;
  taskId?: string;
}

export const INDEXER_COUNTER_ANIMATION_DURATION_MS = 450;

const reducedMotionQuery = "(prefers-reduced-motion: reduce)";

function normalizeCounter(value: number): number {
  if (!Number.isFinite(value)) {
    return 0;
  }
  return Math.max(0, Math.floor(value));
}

function normalizeStats(stats: IndexerDisplayStats): IndexerDisplayStats {
  return {
    bytesIndexed: normalizeCounter(stats.bytesIndexed),
    dirsIndexed: normalizeCounter(stats.dirsIndexed),
    filesIndexed: normalizeCounter(stats.filesIndexed),
  };
}

function statsEqual(
  left: IndexerDisplayStats,
  right: IndexerDisplayStats,
): boolean {
  return (
    left.bytesIndexed === right.bytesIndexed &&
    left.dirsIndexed === right.dirsIndexed &&
    left.filesIndexed === right.filesIndexed
  );
}

function hasCounterDecrease(
  current: IndexerDisplayStats,
  target: IndexerDisplayStats,
): boolean {
  return (
    target.bytesIndexed < current.bytesIndexed ||
    target.dirsIndexed < current.dirsIndexed ||
    target.filesIndexed < current.filesIndexed
  );
}

function interpolateStats(
  from: IndexerDisplayStats,
  target: IndexerDisplayStats,
  progress: number,
): IndexerDisplayStats {
  const interpolateCounter = (start: number, end: number) =>
    Math.floor(start + (end - start) * progress);

  return {
    bytesIndexed: interpolateCounter(from.bytesIndexed, target.bytesIndexed),
    dirsIndexed: interpolateCounter(from.dirsIndexed, target.dirsIndexed),
    filesIndexed: interpolateCounter(from.filesIndexed, target.filesIndexed),
  };
}

function getPrefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia(reducedMotionQuery).matches
  );
}

function subscribeToReducedMotion(onStoreChange: () => void): () => void {
  if (
    typeof window === "undefined" ||
    typeof window.matchMedia !== "function"
  ) {
    return () => {};
  }

  const mediaQuery = window.matchMedia(reducedMotionQuery);
  mediaQuery.addEventListener("change", onStoreChange);
  return () => mediaQuery.removeEventListener("change", onStoreChange);
}

function usePrefersReducedMotion(): boolean {
  return useSyncExternalStore(
    subscribeToReducedMotion,
    getPrefersReducedMotion,
    () => false,
  );
}

/**
 * Smoothly catches displayed counters up to the latest confirmed indexer SSE
 * sample. It never predicts work beyond that sample, keeping the underlying
 * task state as the source of truth while making high-throughput scans legible.
 */
export function useAnimatedIndexerStats(
  stats: IndexerDisplayStats,
  { enabled, taskId }: UseAnimatedIndexerStatsOptions,
): IndexerDisplayStats {
  const { bytesIndexed, dirsIndexed, filesIndexed } = stats;
  const [displayed, setDisplayed] = useState(() => normalizeStats(stats));
  const displayedRef = useRef(displayed);
  const frameRef = useRef<number | null>(null);
  const previousTaskIDRef = useRef(taskId);
  const prefersReducedMotion = usePrefersReducedMotion();

  useEffect(() => {
    const target = normalizeStats({
      bytesIndexed,
      dirsIndexed,
      filesIndexed,
    });
    const cancelAnimation = () => {
      if (frameRef.current !== null) {
        window.cancelAnimationFrame?.(frameRef.current);
        frameRef.current = null;
      }
    };
    const setDisplayedStats = (next: IndexerDisplayStats) => {
      displayedRef.current = next;
      setDisplayed((current) => (statsEqual(current, next) ? current : next));
    };
    const taskChanged = previousTaskIDRef.current !== taskId;
    previousTaskIDRef.current = taskId;
    const current = displayedRef.current;
    const animationAvailable =
      typeof window !== "undefined" &&
      typeof window.requestAnimationFrame === "function";

    if (
      !enabled ||
      prefersReducedMotion ||
      taskChanged ||
      hasCounterDecrease(current, target) ||
      !animationAvailable
    ) {
      cancelAnimation();
      // oxlint-disable-next-line react-you-might-not-need-an-effect/no-adjust-state-on-prop-change, react-you-might-not-need-an-effect/no-external-store-subscription -- Snap to confirmed counters when animation cannot run.
      setDisplayedStats(target);
      return cancelAnimation;
    }

    if (statsEqual(current, target)) {
      cancelAnimation();
      return cancelAnimation;
    }

    cancelAnimation();
    const from = current;
    let startedAt: number | undefined;
    const animate = (timestamp: number) => {
      if (startedAt === undefined) {
        startedAt = timestamp;
      }
      const progress = Math.min(
        (timestamp - startedAt) / INDEXER_COUNTER_ANIMATION_DURATION_MS,
        1,
      );
      setDisplayedStats(interpolateStats(from, target, progress));

      if (progress < 1) {
        frameRef.current = window.requestAnimationFrame(animate);
      } else {
        frameRef.current = null;
      }
    };

    frameRef.current = window.requestAnimationFrame(animate);
    return cancelAnimation;
  }, [
    bytesIndexed,
    dirsIndexed,
    enabled,
    filesIndexed,
    taskId,
    prefersReducedMotion,
  ]);

  return displayed;
}
