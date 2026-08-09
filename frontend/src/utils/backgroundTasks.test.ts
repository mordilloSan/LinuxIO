import { describe, expect, it, vi } from "vitest";

import {
  createProgressSpeedCalculator,
  taskIdentityKey,
  makeCountedSet,
} from "@/utils/backgroundTasks";

describe("background task utilities", () => {
  it("creates stable identity keys for task type and safe identity parts", () => {
    expect(taskIdentityKey("copy", ["/a", "/b"])).toBe(
      JSON.stringify(["copy", ["/a", "/b"]]),
    );
    expect(taskIdentityKey("reindex")).toBe(JSON.stringify(["reindex", []]));
  });

  it("tracks counted membership until every duplicate is deleted", () => {
    const set = makeCountedSet();

    set.add("upload:/tmp/file");
    set.add("upload:/tmp/file");
    expect(set.has("upload:/tmp/file")).toBe(true);

    set.delete("upload:/tmp/file");
    expect(set.has("upload:/tmp/file")).toBe(true);

    set.delete("upload:/tmp/file");
    expect(set.has("upload:/tmp/file")).toBe(false);
  });

  it("calculates smoothed progress speeds after the minimum window", () => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    const speed = createProgressSpeedCalculator(500, 0.5);

    expect(speed(100)).toBeUndefined();

    vi.setSystemTime(500);
    expect(speed(600)).toBeUndefined();

    vi.setSystemTime(1000);
    expect(speed(1100)).toBe(1100);

    vi.setSystemTime(2000);
    expect(speed(2100)).toBe(1050);
  });

  it("ignores non-increasing byte counters", () => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    const speed = createProgressSpeedCalculator(100);

    vi.setSystemTime(200);
    expect(speed(0)).toBeUndefined();

    vi.setSystemTime(400);
    expect(speed(-1)).toBeUndefined();
  });
});
