import { describe, expect, it } from "vitest";

import type { GpuDevice } from "@/api";
import {
  formatGpuBytes,
  formatGpuPercent,
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
