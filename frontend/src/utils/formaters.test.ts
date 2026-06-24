import { describe, expect, it } from "vitest";

import {
  formatDate,
  formatFileSize,
  formatThroughput,
} from "@/utils/formaters";

describe("formaters", () => {
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
  });

  it("formats dates with sensible fallback", () => {
    expect(formatDate()).toBe("Unknown");
    expect(formatDate("not-a-date")).toBe("Invalid Date");
  });
});
