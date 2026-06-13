import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api/StreamMultiplexer";
import type { JobSnapshot } from "@/api/generated/linuxio-types";

const mocks = vi.hoisted(() => ({
  openJobAttachStream: vi.fn(),
  request: vi.fn(),
  waitForStreamResult: vi.fn(),
}));

vi.mock("@/api/linuxio", () => ({
  openJobAttachStream: mocks.openJobAttachStream,
}));

vi.mock("@/api/linuxio-core", () => ({
  LinuxIOError: class LinuxIOError extends Error {
    constructor(
      message: string,
      public code?: string | number,
    ) {
      super(message);
      this.name = "LinuxIOError";
    }
  },
  request: mocks.request,
}));

vi.mock("@/api/stream-helpers", () => ({
  waitForStreamResult: mocks.waitForStreamResult,
}));

const {
  isJobLocallyHandled,
  isJobSnapshot,
  isTerminalJobState,
  jobSnapshotResult,
  markJobLocallyHandled,
  unmarkJobLocallyHandled,
  waitForJobCompletion,
} = await import("@/api/jobs");

function snapshot(overrides: Partial<JobSnapshot> = {}): JobSnapshot {
  return {
    created_at: "2026-01-01T00:00:00.000Z",
    id: "job-1",
    state: "running",
    type: "test",
    updated_at: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

describe("jobs helpers", () => {
  beforeEach(() => {
    mocks.openJobAttachStream.mockReset();
    mocks.request.mockReset();
    mocks.waitForStreamResult.mockReset();
  });

  it("identifies terminal states and job snapshots", () => {
    expect(isTerminalJobState("completed")).toBe(true);
    expect(isTerminalJobState("failed")).toBe(true);
    expect(isTerminalJobState("canceled")).toBe(true);
    expect(isTerminalJobState("running")).toBe(false);

    expect(isJobSnapshot(snapshot())).toBe(true);
    expect(isJobSnapshot({ id: "job-1", state: "running" })).toBe(false);
  });

  it("unwraps job snapshot results", () => {
    expect(jobSnapshotResult(snapshot({ result: { ok: true } }))).toEqual({
      ok: true,
    });
    expect(jobSnapshotResult("plain")).toBe("plain");
  });

  it("retains locally handled job ids briefly", () => {
    vi.useFakeTimers();
    markJobLocallyHandled("job-1");
    expect(isJobLocallyHandled("job-1")).toBe(true);

    unmarkJobLocallyHandled("job-1");
    vi.advanceTimersByTime(4999);
    expect(isJobLocallyHandled("job-1")).toBe(true);
    vi.advanceTimersByTime(1);
    expect(isJobLocallyHandled("job-1")).toBe(false);
  });

  it("returns completed snapshots and throws failed terminal snapshots", async () => {
    await expect(
      waitForJobCompletion(snapshot({ state: "completed", result: "done" })),
    ).resolves.toMatchObject({ result: "done" });

    await expect(
      waitForJobCompletion(
        snapshot({
          error: { code: 500, message: "failed" },
          state: "failed",
        }),
      ),
    ).rejects.toMatchObject({ message: "failed", code: 500 });
  });

  it("returns the original active snapshot when attach is unavailable", async () => {
    const active = snapshot();
    mocks.openJobAttachStream.mockReturnValue(null);

    await expect(waitForJobCompletion(active)).resolves.toBe(active);
  });

  it("attaches active jobs, refetches final snapshots, and clears local handling", async () => {
    vi.useFakeTimers();
    const stream = {} as Stream;
    const finalSnapshot = snapshot({ state: "completed", result: "fresh" });
    mocks.openJobAttachStream.mockReturnValue(stream);
    mocks.waitForStreamResult.mockResolvedValue("stream-result");
    mocks.request.mockResolvedValue(finalSnapshot);

    await expect(waitForJobCompletion(snapshot())).resolves.toBe(finalSnapshot);
    expect(mocks.openJobAttachStream).toHaveBeenCalledWith("job-1");
    expect(mocks.waitForStreamResult).toHaveBeenCalledWith(stream, {
      closeMessage: "Job stream closed before completion",
    });
    expect(mocks.request).toHaveBeenCalledWith(
      "jobs",
      "get",
      { jobId: "job-1" },
      { retryPolicy: "connection_closed" },
    );
    expect(isJobLocallyHandled("job-1")).toBe(true);
    vi.advanceTimersByTime(5000);
    expect(isJobLocallyHandled("job-1")).toBe(false);
  });

  it("falls back to the stream result when final snapshot refetch fails", async () => {
    const stream = {} as Stream;
    mocks.openJobAttachStream.mockReturnValue(stream);
    mocks.waitForStreamResult.mockResolvedValue({ ok: true });
    mocks.request.mockRejectedValue(new Error("offline"));

    await expect(waitForJobCompletion(snapshot())).resolves.toMatchObject({
      state: "completed",
      result: { ok: true },
      finished_at: expect.any(String),
      updated_at: expect.any(String),
    });
  });
});
