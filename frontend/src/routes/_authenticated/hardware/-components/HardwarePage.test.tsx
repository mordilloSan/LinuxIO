import { describe, expect, it } from "vitest";

import type { SensorGroup } from "@/api";

import { selectVisibleSensorGroupIdentities } from "./HardwarePage";

const group = (adapter: string, labels: string[]): SensorGroup => ({
  adapter,
  readings: labels.map((label) => ({
    kind: "number",
    label,
    unit: "C",
    value: 42,
  })),
});

describe("selectVisibleSensorGroupIdentities", () => {
  it("keeps the source index of every adapter with visible readings", () => {
    expect(
      selectVisibleSensorGroupIdentities([
        group("empty-adapter", []),
        group("coretemp-isa-0000", ["temp1_input"]),
      ]),
    ).toEqual([
      {
        adapter: "coretemp-isa-0000",
        sourceIndex: 1,
        visibleReadingCount: 1,
      },
    ]);
  });

  // `sensors` failing or reporting no chips leaves the Go handler with a nil
  // slice, which reaches the browser as null.
  it.each([
    ["null", null],
    ["undefined", undefined],
  ])("returns no identities for %s", (_label, groups) => {
    expect(selectVisibleSensorGroupIdentities(groups)).toEqual([]);
  });
});
