import { describe, expect, it } from "vitest";

import {
  formatDataUnits,
  formatPowerOnTime,
  getHealthColor,
  getSmartNumber,
  getSmartString,
  getSmartValue,
  getTemperature,
  getTemperatureColor,
  parseSizeToBytes,
} from "@/pages/main/storage/DiskOverview/utils";

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
    expect(getHealthColor({ smart_status: { passed: true } })).toBe("success");
    expect(getHealthColor({ smart_status: { passed: false } })).toBe("error");
    expect(getHealthColor({ smart_status: {} })).toBe("warning");
  });

  it("formats power-on time and NVMe data units", () => {
    expect(formatPowerOnTime()).toBe("N/A");
    expect(formatPowerOnTime(23)).toBe("23h");
    expect(formatPowerOnTime(49)).toBe("2d 1h");
    expect(formatDataUnits()).toBe("N/A");
    expect(formatDataUnits(2)).toContain("2 [");
  });

  it("prefers NVMe temperature before ATA temperature", () => {
    expect(
      getTemperature({
        nvme_smart_health_information_log: { temperature: 44 },
        temperature: { current: 55 },
      }),
    ).toBe(44);
    expect(getTemperature({ temperature: { current: 55 } })).toBe(55);
    expect(getTemperature()).toBeNull();
  });

  it("maps temperature to theme color buckets", () => {
    expect(getTemperatureColor(null)).toBe("text.secondary");
    expect(getTemperatureColor(45)).toBe("success.main");
    expect(getTemperatureColor(51)).toBe("warning.main");
    expect(getTemperatureColor(71)).toBe("error.main");
  });

  it("extracts smart object values as strings or numbers", () => {
    expect(getSmartValue(undefined)).toBeNull();
    expect(getSmartValue("ok")).toBe("ok");
    expect(getSmartValue({ string: "10 hours", value: 10 })).toBe("10 hours");
    expect(getSmartValue({ string: "10 hours", value: 10 }, false)).toBe(10);
    expect(getSmartNumber({ string: "12.5", value: 10 })).toBe(10);
    expect(getSmartNumber("12.5")).toBe(12.5);
    expect(getSmartNumber("n/a")).toBeNull();
    expect(getSmartString({ string: "ready", value: 1 })).toBe("ready");
  });
});
