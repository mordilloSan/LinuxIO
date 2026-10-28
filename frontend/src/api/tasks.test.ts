import { describe, expect, it } from "vitest";

import { LinuxIOError } from "@/api/errors";
import {
  isTaskCancellationError,
  isTerminalTaskState,
  TASK_CANCELED_CODE,
} from "@/api/tasks";

describe("tasks helpers", () => {
  it("identifies terminal states", () => {
    expect(isTerminalTaskState("completed")).toBe(true);
    expect(isTerminalTaskState("failed")).toBe(true);
    expect(isTerminalTaskState("canceled")).toBe(true);
    expect(isTerminalTaskState("interrupted")).toBe(true);
    expect(isTerminalTaskState("running")).toBe(false);
  });

  it("tells a cancellation from a failure", () => {
    expect(
      isTaskCancellationError(new LinuxIOError("canceled", TASK_CANCELED_CODE)),
    ).toBe(true);
    expect(isTaskCancellationError(new LinuxIOError("boom", 500))).toBe(false);
    expect(isTaskCancellationError(new Error("canceled"))).toBe(false);
  });
});
