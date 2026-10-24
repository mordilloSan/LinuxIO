import { describe, expect, it } from "vitest";

import {
  formatDate,
  formatFileSize,
  formatPercent,
  formatSystemdUsec,
  formatThroughput,
  formatUptimeClock,
} from "@/utils/formatters";

describe("formatters", () => {
  it("formats throughput boundaries", () => {
    expect(formatThroughput(0)).toBe("0 B/s");
    expect(formatThroughput(Number.NaN)).toBe("0 B/s");
    expect(formatThroughput(512)).toBe("512 B/s");
    expect(formatThroughput(1536)).toBe("1.5 kB/s");
    expect(formatThroughput(12 * 1024)).toBe("12 kB/s");
    expect(formatThroughput(1.5 * 1024 * 1024)).toBe("1.5 MB/s");
    expect(formatThroughput(2 * 1024 * 1024 * 1024)).toBe("2.0 GB/s");
  });

  it("formats file sizes and fallbacks", () => {
    expect(formatFileSize(null)).toBe("Unknown");
    expect(formatFileSize(undefined, 2, "n/a")).toBe("n/a");
    expect(formatFileSize(0)).toBe("0 Bytes");
    expect(formatFileSize(1536, 1)).toBe("1.5 KB");
    expect(formatFileSize(1024 * 1024, 0)).toBe("1 MB");
    expect(formatFileSize(4.1 * 1024 ** 5, 1)).toBe("4.1 PB");
    expect(formatFileSize(2 ** 70, 1)).toBe("1024 EB");
  });

  it("formats dates with sensible fallback", () => {
    expect(formatDate()).toBe("Unknown");
    expect(formatDate("not-a-date")).toBe("Invalid Date");
  });

  it("formats percentages", () => {
    expect(formatPercent(12.345)).toBe("12.3%");
    expect(formatPercent(12.345, 0)).toBe("12%");
  });

  it("formats uptime like htop", () => {
    expect(formatUptimeClock(59)).toBe("00:00:59");
    expect(formatUptimeClock(86_400 + 61)).toBe("1 day, 00:01:01");
    expect(formatUptimeClock(2 * 86_400)).toBe("2 days, 00:00:00");
  });

  it("guards systemd microsecond timestamps", () => {
    expect(formatSystemdUsec(0)).toBe("—");
    expect(formatSystemdUsec(undefined)).toBe("—");
    expect(formatSystemdUsec(Number.MAX_SAFE_INTEGER)).toBe("—");
    expect(formatSystemdUsec(Number.NaN, undefined, "never")).toBe("never");
    expect(formatSystemdUsec(1_700_000_000_000_000)).toBe(
      new Date(1_700_000_000_000).toLocaleString(),
    );
  });
});
