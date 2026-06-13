import { describe, expect, it } from "vitest";

import {
  aggregatePercent,
  isLayer,
  mergeTask,
  prettyId,
  shortId,
  type ComposeTask,
} from "@/components/docker/composeProgress";

describe("composeProgress helpers", () => {
  it("merges progress without regressing percent or losing previous totals", () => {
    let tasks = new Map<string, ComposeTask>();

    tasks = mergeTask(tasks, {
      current: 50,
      id: "layer-1",
      parent_id: "Image alpine",
      percent: 50,
      status: "Working",
      text: "Downloading",
      total: 100,
    });
    tasks = mergeTask(tasks, {
      current: 20,
      id: "layer-1",
      parent_id: "Image alpine",
      percent: 20,
      status: "Working",
      text: "Extracting",
      total: 0,
    });

    expect(tasks.get("layer-1")).toMatchObject({
      pct: 50,
      total: 100,
      text: "Extracting",
    });
  });

  it("pins completed tasks to 100 percent", () => {
    const tasks = mergeTask(new Map(), {
      id: "layer-1",
      parent_id: "Image alpine",
      percent: 12,
      status: "Done",
      text: "Pull complete",
    });

    expect(tasks.get("layer-1")?.pct).toBe(100);
  });

  it("aggregates only layer rows and returns null without layers", () => {
    const layerA: ComposeTask = {
      id: "a",
      parent_id: "Image alpine",
      pct: 20,
      status: "Working",
      text: "Downloading",
    };
    const layerB: ComposeTask = {
      id: "b",
      parent_id: "Image alpine",
      pct: 70,
      status: "Working",
      text: "Extracting",
    };
    const group: ComposeTask = {
      id: "Image alpine",
      pct: 0,
      status: "Working",
      text: "Pulling",
    };

    expect(isLayer(layerA)).toBe(true);
    expect(isLayer(group)).toBe(false);
    expect(aggregatePercent([group, layerA, layerB])).toBe(45);
    expect(aggregatePercent([group])).toBeNull();
  });

  it("formats display ids", () => {
    expect(prettyId("Image alpine:3.19")).toBe("alpine:3.19");
    expect(prettyId("Container linuxio")).toBe("linuxio");
    expect(shortId("1234567890abcdef")).toBe("1234567890ab");
    expect(shortId("short")).toBe("short");
  });
});
