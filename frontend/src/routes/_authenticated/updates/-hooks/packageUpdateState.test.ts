import { describe, expect, it } from "vitest";

import { MAX_INSTALL_OUTPUT_LINES } from "@/components/terminal/InstallOutputPanel";

import {
  initialPackageUpdateState,
  packageUpdateReducer,
} from "./packageUpdateState";

describe("packageUpdateReducer", () => {
  it("keeps aggregate progress monotonic and retains a bounded, de-duplicated log", () => {
    let state = packageUpdateReducer(initialPackageUpdateState, {
      type: "start",
      packageName: "nginx",
      status: "Initializing",
    });
    state = packageUpdateReducer(state, { type: "progress", percentage: 60 });
    state = packageUpdateReducer(state, { type: "progress", percentage: 40 });
    for (let index = 0; index < 10; index += 1) {
      state = packageUpdateReducer(state, {
        type: "status",
        status: `Step ${index}`,
        event: `Step ${index}`,
      });
    }
    state = packageUpdateReducer(state, {
      type: "status",
      status: "Step 9",
      event: "Step 9",
    });

    expect(state.progress).toBe(60);
    expect(state.eventLog).toEqual([
      "Step 2",
      "Step 3",
      "Step 4",
      "Step 5",
      "Step 6",
      "Step 7",
      "Step 8",
      "Step 9",
    ]);
  });

  it("bounds package manager output, flags the gap, and keeps it after a failure", () => {
    let state = packageUpdateReducer(initialPackageUpdateState, {
      type: "start",
      packageName: "nginx",
      status: "Initializing",
    });
    for (let index = 0; index <= MAX_INSTALL_OUTPUT_LINES; index += 1) {
      state = packageUpdateReducer(state, {
        type: "output",
        line: `line ${index}`,
      });
    }

    expect(state.output).toHaveLength(MAX_INSTALL_OUTPUT_LINES);
    expect(state.output[0]).toEqual({
      id: 1,
      stream: "stdout",
      text: "line 1",
    });
    expect(state.output.at(-1)?.id).toBe(MAX_INSTALL_OUTPUT_LINES);
    expect(state.outputHistoryIncomplete).toBe(true);

    const failed = packageUpdateReducer(state, { type: "failed", error: "x" });
    expect(failed.output).toBe(state.output);

    const restarted = packageUpdateReducer(failed, {
      type: "start",
      packageName: "nginx",
      status: "Initializing",
    });
    expect(restarted.output).toEqual([]);
    expect(restarted.outputHistoryIncomplete).toBe(false);
    expect(
      packageUpdateReducer(failed, {
        type: "start",
        packageName: "nginx",
        status: "Resuming update transaction",
        recovered: true,
      }).outputHistoryIncomplete,
    ).toBe(true);
  });

  it("completes immediately and gives errors and cancellation terminal state", () => {
    const running = packageUpdateReducer(initialPackageUpdateState, {
      type: "start",
      packageName: "nginx",
      status: "Initializing",
    });
    const complete = packageUpdateReducer(running, { type: "complete" });

    expect(complete).toMatchObject({
      phase: "idle",
      progress: 100,
      status: null,
      updatingPackage: null,
    });
    expect(packageUpdateReducer(running, { type: "canceled" })).toMatchObject({
      error: "Update cancelled",
      phase: "idle",
      updatingPackage: null,
    });
  });
});
