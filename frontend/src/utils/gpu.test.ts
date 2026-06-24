import { describe, expect, it } from "vitest";

import type { GpuDevice } from "@/api";
import {
  formatGpuBytes,
  formatGpuClock,
  formatGpuDisplays,
  formatGpuPercent,
  formatGpuTemperature,
  formatGpuWatts,
  getGpuType,
  getGpuVendorLabel,
  hasGpuValue,
} from "@/utils/gpu";

const gpu = (overrides: Partial<GpuDevice>): GpuDevice =>
  overrides as GpuDevice;

describe("gpu utilities", () => {
  it("detects present GPU values", () => {
    expect(hasGpuValue(0)).toBe(true);
    expect(hasGpuValue("")).toBe(false);
    expect(hasGpuValue(null)).toBe(false);
    expect(hasGpuValue(undefined)).toBe(false);
  });

  it("formats numeric GPU metrics and missing values", () => {
    expect(formatGpuPercent(42.4)).toBe("42%");
    expect(formatGpuBytes(512)).toBe("512 B");
    expect(formatGpuBytes(1536)).toBe("1.5 KiB");
    expect(formatGpuBytes(1024 ** 2)).toBe("1.0 MiB");
    expect(formatGpuBytes(2 * 1024 ** 3)).toBe("2.00 GiB");
    expect(formatGpuBytes(3 * 1024 ** 4)).toBe("3.00 TiB");
    expect(formatGpuBytes(-1)).toBe("—");
    expect(formatGpuWatts(75)).toBe("75 W");
    expect(formatGpuTemperature(64)).toBe("64°C");
    expect(formatGpuClock(1200)).toBe("1200 MHz");
    expect(formatGpuClock(Number.NaN)).toBe("—");
  });

  it("formats display names before connected display counts", () => {
    expect(
      formatGpuDisplays(
        gpu({ display_names: ["HDMI-A-1", "DP-1"], connected_displays: 1 }),
      ),
    ).toBe("HDMI-A-1, DP-1");
    expect(formatGpuDisplays(gpu({ connected_displays: 2 }))).toBe(
      "2 connected",
    );
    expect(formatGpuDisplays(gpu({}))).toBe("—");
  });

  it("detects common GPU vendors from vendor ids, drivers, models, and vendor names", () => {
    expect(getGpuVendorLabel(gpu({ vendor_id: "0x8086" }))).toBe("Intel");
    expect(getGpuVendorLabel(gpu({ vendor_id: "10de" }))).toBe("NVIDIA");
    expect(getGpuVendorLabel(gpu({ vendor_id: "1002" }))).toBe("AMD");
    expect(getGpuVendorLabel(gpu({ driver_module: "nouveau" }))).toBe("NVIDIA");
    expect(getGpuVendorLabel(gpu({ model: "Radeon RX 7900" }))).toBe("AMD");
    expect(getGpuVendorLabel(gpu({ vendor: "Advanced Micro Devices" }))).toBe(
      "AMD",
    );
    expect(getGpuVendorLabel(gpu({ vendor: "Matrox" }))).toBe("Matrox");
    expect(getGpuVendorLabel(null)).toBe("—");
  });

  it("returns the most specific GPU type label", () => {
    expect(
      getGpuType(
        gpu({
          class_name: "Display controller",
          subclass_name: "VGA compatible controller",
        }),
      ),
    ).toBe("VGA compatible controller");
    expect(getGpuType(gpu({ class_name: "Display controller" }))).toBe(
      "Display controller",
    );
    expect(getGpuType(gpu({}))).toBe("Graphics controller");
  });
});
