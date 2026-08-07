import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  INDEXER_COUNTER_ANIMATION_DURATION_MS,
  type IndexerDisplayStats,
  useAnimatedIndexerStats,
} from "@/hooks/backgroundJobs/useAnimatedIndexerStats";
import { act, renderHook } from "@/test/render";

interface HookProps {
  enabled: boolean;
  jobId?: string;
  stats: IndexerDisplayStats;
}

let nextFrameID = 1;
let frames: Map<number, FrameRequestCallback>;

function nextFrame(timestamp: number) {
  const frame = frames.entries().next().value as
    | [number, FrameRequestCallback]
    | undefined;
  if (!frame) {
    throw new Error("expected a scheduled animation frame");
  }
  frames.delete(frame[0]);
  frame[1](timestamp);
}

function createMatchMedia(matches = false) {
  return vi.fn().mockImplementation(
    () =>
      ({
        addEventListener: vi.fn(),
        matches,
        media: "(prefers-reduced-motion: reduce)",
        removeEventListener: vi.fn(),
      }) as unknown as MediaQueryList,
  );
}

describe("useAnimatedIndexerStats", () => {
  beforeEach(() => {
    nextFrameID = 1;
    frames = new Map();
    vi.stubGlobal(
      "requestAnimationFrame",
      vi.fn((callback: FrameRequestCallback) => {
        const id = nextFrameID++;
        frames.set(id, callback);
        return id;
      }),
    );
    vi.stubGlobal(
      "cancelAnimationFrame",
      vi.fn((id: number) => {
        frames.delete(id);
      }),
    );
    vi.spyOn(window, "matchMedia").mockImplementation(createMatchMedia());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("smoothly catches the displayed counters up to a confirmed SSE sample", () => {
    const initial = {
      bytesIndexed: 2_100,
      dirsIndexed: 12_632,
      filesIndexed: 21_000,
    };
    const target = {
      bytesIndexed: 2_650,
      dirsIndexed: 12_900,
      filesIndexed: 26_500,
    };
    const { result, rerender } = renderHook(
      ({ stats, enabled, jobId }: HookProps) =>
        useAnimatedIndexerStats(stats, { enabled, jobId }),
      {
        initialProps: { enabled: true, jobId: "job-1", stats: initial },
      },
    );

    rerender({ enabled: true, jobId: "job-1", stats: target });
    expect(result.current).toEqual(initial);

    act(() => nextFrame(0));
    act(() => nextFrame(INDEXER_COUNTER_ANIMATION_DURATION_MS / 2));
    expect(result.current).toEqual({
      bytesIndexed: 2_375,
      dirsIndexed: 12_766,
      filesIndexed: 23_750,
    });

    act(() => nextFrame(INDEXER_COUNTER_ANIMATION_DURATION_MS));
    expect(result.current).toEqual(target);
  });

  it("retargets from the currently displayed counters without a visible jump", () => {
    const { result, rerender } = renderHook(
      ({ stats, enabled, jobId }: HookProps) =>
        useAnimatedIndexerStats(stats, { enabled, jobId }),
      {
        initialProps: {
          enabled: true,
          jobId: "job-1",
          stats: { bytesIndexed: 100, dirsIndexed: 100, filesIndexed: 100 },
        },
      },
    );

    rerender({
      enabled: true,
      jobId: "job-1",
      stats: { bytesIndexed: 200, dirsIndexed: 200, filesIndexed: 200 },
    });
    act(() => nextFrame(0));
    act(() => nextFrame(INDEXER_COUNTER_ANIMATION_DURATION_MS / 2));
    expect(result.current).toEqual({
      bytesIndexed: 150,
      dirsIndexed: 150,
      filesIndexed: 150,
    });

    rerender({
      enabled: true,
      jobId: "job-1",
      stats: { bytesIndexed: 300, dirsIndexed: 300, filesIndexed: 300 },
    });
    expect(result.current).toEqual({
      bytesIndexed: 150,
      dirsIndexed: 150,
      filesIndexed: 150,
    });

    act(() => nextFrame(300));
    act(() => nextFrame(300 + INDEXER_COUNTER_ANIMATION_DURATION_MS / 2));
    expect(result.current).toEqual({
      bytesIndexed: 225,
      dirsIndexed: 225,
      filesIndexed: 225,
    });
  });

  it("snaps on counter resets and when animation is disabled", () => {
    const { result, rerender } = renderHook(
      ({ stats, enabled, jobId }: HookProps) =>
        useAnimatedIndexerStats(stats, { enabled, jobId }),
      {
        initialProps: {
          enabled: true,
          jobId: "job-1",
          stats: { bytesIndexed: 100, dirsIndexed: 100, filesIndexed: 100 },
        },
      },
    );

    rerender({
      enabled: true,
      jobId: "job-1",
      stats: { bytesIndexed: 200, dirsIndexed: 200, filesIndexed: 200 },
    });
    act(() => nextFrame(0));
    act(() => nextFrame(INDEXER_COUNTER_ANIMATION_DURATION_MS / 2));

    rerender({
      enabled: true,
      jobId: "job-1",
      stats: { bytesIndexed: 20, dirsIndexed: 20, filesIndexed: 20 },
    });
    expect(result.current).toEqual({
      bytesIndexed: 20,
      dirsIndexed: 20,
      filesIndexed: 20,
    });
    expect(frames.size).toBe(0);

    rerender({
      enabled: false,
      jobId: "job-1",
      stats: { bytesIndexed: 80, dirsIndexed: 80, filesIndexed: 80 },
    });
    expect(result.current).toEqual({
      bytesIndexed: 80,
      dirsIndexed: 80,
      filesIndexed: 80,
    });
    expect(frames.size).toBe(0);
  });

  it("does not animate when reduced motion is preferred", () => {
    vi.mocked(window.matchMedia).mockImplementation(createMatchMedia(true));
    const { result, rerender } = renderHook(
      ({ stats, enabled, jobId }: HookProps) =>
        useAnimatedIndexerStats(stats, { enabled, jobId }),
      {
        initialProps: {
          enabled: true,
          jobId: "job-1",
          stats: { bytesIndexed: 100, dirsIndexed: 100, filesIndexed: 100 },
        },
      },
    );

    rerender({
      enabled: true,
      jobId: "job-1",
      stats: { bytesIndexed: 200, dirsIndexed: 200, filesIndexed: 200 },
    });

    expect(result.current).toEqual({
      bytesIndexed: 200,
      dirsIndexed: 200,
      filesIndexed: 200,
    });
    expect(frames.size).toBe(0);
  });
});
