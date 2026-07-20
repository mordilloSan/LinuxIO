import { useEffect, useRef, useState } from "react";

export interface IndexerDisplayStats {
  bytesIndexed: number;
  dirsIndexed: number;
  filesIndexed: number;
}

interface UseAnimatedIndexerStatsOptions {
  enabled: boolean;
  jobId?: string;
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

function usePrefersReducedMotion(): boolean {
  const [prefersReducedMotion, setPrefersReducedMotion] = useState(
    getPrefersReducedMotion,
  );

  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return;
    }

    const mediaQuery = window.matchMedia(reducedMotionQuery);
    const onChange = (event: MediaQueryListEvent) => {
      setPrefersReducedMotion(event.matches);
    };
    mediaQuery.addEventListener("change", onChange);
    return () => mediaQuery.removeEventListener("change", onChange);
  }, []);

  return prefersReducedMotion;
}

/**
 * Smoothly catches displayed counters up to the latest confirmed indexer SSE
 * sample. It never predicts work beyond that sample, keeping the underlying
 * job state as the source of truth while making high-throughput scans legible.
 */
export function useAnimatedIndexerStats(
  stats: IndexerDisplayStats,
  { enabled, jobId }: UseAnimatedIndexerStatsOptions,
): IndexerDisplayStats {
  const target = normalizeStats(stats);
  const [displayed, setDisplayed] = useState(target);
  const displayedRef = useRef(displayed);
  const frameRef = useRef<number | null>(null);
  const previousJobIDRef = useRef(jobId);
  const prefersReducedMotion = usePrefersReducedMotion();

  useEffect(() => {
    const cancelAnimation = () => {
      if (frameRef.current !== null) {
        window.cancelAnimationFrame(frameRef.current);
        frameRef.current = null;
      }
    };
    const setDisplayedStats = (next: IndexerDisplayStats) => {
      displayedRef.current = next;
      setDisplayed((current) => (statsEqual(current, next) ? current : next));
    };
    const jobChanged = previousJobIDRef.current !== jobId;
    previousJobIDRef.current = jobId;
    const current = displayedRef.current;

    if (
      !enabled ||
      prefersReducedMotion ||
      jobChanged ||
      hasCounterDecrease(current, target) ||
      typeof window.requestAnimationFrame !== "function"
    ) {
      cancelAnimation();
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
      startedAt ??= timestamp;
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
    enabled,
    jobId,
    prefersReducedMotion,
    target.bytesIndexed,
    target.dirsIndexed,
    target.filesIndexed,
  ]);

  return displayed;
}
