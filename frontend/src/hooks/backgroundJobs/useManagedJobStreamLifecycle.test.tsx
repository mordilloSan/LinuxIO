import { describe, expect, it, vi } from "vitest";

import type { JobSnapshot, Stream } from "@/api";
import { act, renderHook } from "@/test/render";

import { useManagedJobStreamLifecycle } from "./useManagedJobStreamLifecycle";

const job = (id: string) => ({ id, state: "running" }) as JobSnapshot;

const stream = (): Stream =>
  ({ abort: vi.fn(), close: vi.fn() }) as unknown as Stream;

describe("useManagedJobStreamLifecycle", () => {
  it("accepts a current submission failure before a job snapshot exists", () => {
    const { result } = renderHook(() => useManagedJobStreamLifecycle(vi.fn()));
    const request = { packageIds: ["nginx"] };

    act(() => expect(result.current.begin(request)).toBe(true));
    expect(result.current.begin({ packageIds: ["curl"] })).toBe(false);
    expect(result.current.accepts(request)).toBe(true);
    expect(result.current.settle(request)).toBe(true);
  });

  it("ignores canceled run A after run B begins", () => {
    const onCancel = vi.fn();
    const { result } = renderHook(() => useManagedJobStreamLifecycle(onCancel));
    const requestA = { packageIds: ["a"] };
    const requestB = { packageIds: ["b"] };
    const streamA = stream();

    act(() => {
      result.current.begin(requestA);
      result.current.onJobStart(job("a"), requestA);
      result.current.onOpen(streamA, job("a"), requestA);
      expect(result.current.cancel()).toBe(true);
      expect(result.current.begin(requestB)).toBe(true);
      result.current.onJobStart(job("b"), requestB);
    });

    expect(streamA.abort).toHaveBeenCalledTimes(1);
    expect(onCancel).toHaveBeenCalledWith(job("a"), requestA);
    expect(result.current.accepts(requestA)).toBe(false);
    expect(result.current.settle(requestA)).toBe(false);
    expect(result.current.isActive).toBe(true);
  });

  it("keeps a detached run detached when its job and stream arrive late", () => {
    const { result } = renderHook(() => useManagedJobStreamLifecycle(vi.fn()));
    const request = { packageIds: ["nginx"] };
    const lateStream = stream();

    act(() => {
      result.current.begin(request);
      result.current.detach();
    });

    expect(result.current.onJobStart(job("late"), request)).toBe(false);
    expect(result.current.onOpen(lateStream, job("late"), request)).toBe(false);
    expect(lateStream.close).toHaveBeenCalledTimes(1);
  });
});
