import { describe, expect, it } from "vitest";

import { formatSensorLabel } from "./sensors";

describe("formatSensorLabel", () => {
  it.each([
    ["package", "Package"],
    ["core0", "Core 0"],
    ["coretemp3", "Coretemp 3"],
    ["mb12", "Mb 12"],
    ["package0", "Package 0"],
    ["package1core3", "Package 1 Core 3"],
    ["cpu_thermal", "Cpu_thermal"],
  ])("formats %s as %s", (key, label) => {
    expect(formatSensorLabel(key)).toBe(label);
  });
});
