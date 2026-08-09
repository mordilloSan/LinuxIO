import { beforeEach, describe, expect, it, vi } from "vitest";

import type { TaskSnapshot } from "@/api/generated/linuxio-types";
import type { Stream } from "@/api/StreamMultiplexer";

const mocks = vi.hoisted(() => ({
  openTaskWatchStream: vi.fn(),
  request: vi.fn(),
  waitForStreamResult: vi.fn(),
}));

vi.mock("@/api/linuxio", () => ({
  openTaskWatchStream: mocks.openTaskWatchStream,
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
  isTaskSnapshot,
  isTerminalTaskState,
  taskSnapshotResult,
  waitForTaskCompletion,
} = await import("@/api/tasks");

function snapshot(overrides: Partial<TaskSnapshot> = {}): TaskSnapshot {
  return {
    created_at: "2026-01-01T00:00:00.000Z",
    id: "task-1",
    state: "running",
    type: "test",
    updated_at: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

describe("tasks helpers", () => {
  beforeEach(() => {
    mocks.openTaskWatchStream.mockReset();
    mocks.request.mockReset();
    mocks.waitForStreamResult.mockReset();
  });

  it("identifies terminal states and task snapshots", () => {
    expect(isTerminalTaskState("completed")).toBe(true);
    expect(isTerminalTaskState("failed")).toBe(true);
    expect(isTerminalTaskState("canceled")).toBe(true);
    expect(isTerminalTaskState("running")).toBe(false);

    expect(isTaskSnapshot(snapshot())).toBe(true);
    expect(isTaskSnapshot({ id: "task-1", state: "running" })).toBe(false);
  });

  it("unwraps task snapshot results", () => {
    expect(taskSnapshotResult(snapshot({ result: { ok: true } }))).toEqual({
      ok: true,
    });
    expect(taskSnapshotResult("plain")).toBe("plain");
  });

  it("returns completed snapshots and throws failed terminal snapshots", async () => {
    await expect(
      waitForTaskCompletion(snapshot({ state: "completed", result: "done" })),
    ).resolves.toMatchObject({ result: "done" });

    await expect(
      waitForTaskCompletion(
        snapshot({
          error: { code: 500, message: "failed" },
          state: "failed",
        }),
      ),
    ).rejects.toMatchObject({ message: "failed", code: 500 });
  });

  it("polls to completion when the watch is unavailable", async () => {
    const finalSnapshot = snapshot({ state: "completed", result: "polled" });
    mocks.openTaskWatchStream.mockReturnValue(null);
    mocks.request
      .mockResolvedValueOnce(snapshot())
      .mockResolvedValueOnce(finalSnapshot);

    vi.useFakeTimers();
    const completion = waitForTaskCompletion(snapshot());
    await vi.advanceTimersByTimeAsync(1000);
    await expect(completion).resolves.toBe(finalSnapshot);
    expect(mocks.request).toHaveBeenCalledWith(
      "tasks",
      "get",
      { taskId: "task-1" },
      { retryPolicy: "connection_closed" },
    );
    expect(mocks.waitForStreamResult).not.toHaveBeenCalled();
  });

  it("throws when the polled task ends in a failed state", async () => {
    mocks.openTaskWatchStream.mockReturnValue(null);
    mocks.request.mockResolvedValue(
      snapshot({ state: "failed", error: { code: 500, message: "boom" } }),
    );

    await expect(waitForTaskCompletion(snapshot())).rejects.toMatchObject({
      message: "boom",
      code: 500,
    });
  });

  it("watches active tasks and refetches their final snapshots", async () => {
    const stream = {} as Stream;
    const finalSnapshot = snapshot({ state: "completed", result: "fresh" });
    mocks.openTaskWatchStream.mockReturnValue(stream);
    mocks.waitForStreamResult.mockResolvedValue("stream-result");
    mocks.request.mockResolvedValue(finalSnapshot);

    await expect(waitForTaskCompletion(snapshot())).resolves.toBe(
      finalSnapshot,
    );
    expect(mocks.openTaskWatchStream).toHaveBeenCalledWith("task-1");
    expect(mocks.waitForStreamResult).toHaveBeenCalledWith(stream, {
      closeMessage: "Task watch closed before completion",
    });
    expect(mocks.request).toHaveBeenCalledWith(
      "tasks",
      "get",
      { taskId: "task-1" },
      { retryPolicy: "connection_closed" },
    );
  });

  it("falls back to the stream result when final snapshot refetch fails", async () => {
    const stream = {} as Stream;
    mocks.openTaskWatchStream.mockReturnValue(stream);
    mocks.waitForStreamResult.mockResolvedValue({ ok: true });
    mocks.request.mockRejectedValue(new Error("offline"));

    await expect(waitForTaskCompletion(snapshot())).resolves.toMatchObject({
      state: "completed",
      result: { ok: true },
      finished_at: expect.any(String),
      updated_at: expect.any(String),
    });
  });
});
