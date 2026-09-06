import { describe, expect, it } from "vitest";

import {
  formatDataUnits,
  formatPowerOnTime,
  getHealthColor,
  getTemperature,
  getTemperatureColor,
  parseSizeToBytes,
} from "./utils";

describe("DiskOverview utils", () => {
  it("parses human drive sizes into bytes", () => {
    expect(parseSizeToBytes("1KB")).toBe(1024);
    expect(parseSizeToBytes("1.5 GB")).toBe(1610612736);
    expect(parseSizeToBytes("2T")).toBe(2 * 1024 ** 4);
    expect(parseSizeToBytes("bad")).toBe(0);
    expect(parseSizeToBytes("-1G")).toBe(0);
    expect(parseSizeToBytes(null)).toBe(0);
  });

  it("maps SMART health to alert colors", () => {
    expect(getHealthColor(undefined)).toBe("default");
    expect(getHealthColor({ smart_status: "PASSED" })).toBe("success");
    expect(getHealthColor({ smart_status: "FAILED" })).toBe("error");
    expect(getHealthColor({ smart_status: "UNKNOWN" })).toBe("warning");
  });

  it("formats power-on time and NVMe data units", () => {
    expect(formatPowerOnTime()).toBe("N/A");
    expect(formatPowerOnTime(23)).toBe("23h");
    expect(formatPowerOnTime(49)).toBe("2d 1h");
    expect(formatDataUnits()).toBe("N/A");
    expect(formatDataUnits(2)).toContain("2 [");
  });

  it("reads the typed SMART temperature", () => {
    expect(getTemperature({ temperature_celsius: 44 })).toBe(44);
    expect(getTemperature()).toBeNull();
  });

  it("maps temperature to theme color buckets", () => {
    expect(getTemperatureColor(null)).toBe("text.secondary");
    expect(getTemperatureColor(45)).toBe("success.main");
    expect(getTemperatureColor(51)).toBe("warning.main");
    expect(getTemperatureColor(71)).toBe("error.main");
  });
});
