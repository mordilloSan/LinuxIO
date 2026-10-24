import { describe, expect, it } from "vitest";

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
      event: "Initializing update transaction",
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

  it("completes immediately and gives errors and cancellation terminal state", () => {
    const running = packageUpdateReducer(initialPackageUpdateState, {
      type: "start",
      packageName: "nginx",
      status: "Initializing",
      event: "Initializing update transaction",
    });
    const complete = packageUpdateReducer(running, { type: "complete" });

    expect(complete).toMatchObject({
      phase: "idle",
      progress: 100,
      status: null,
      updatingPackage: null,
    });
    expect(complete.eventLog.at(-1)).toBe("Finished");
    expect(packageUpdateReducer(running, { type: "canceled" })).toMatchObject({
      error: "Update cancelled",
      phase: "idle",
      updatingPackage: null,
    });
  });
});
