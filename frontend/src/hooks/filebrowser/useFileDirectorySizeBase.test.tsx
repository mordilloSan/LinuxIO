import { describe, expect, it } from "vitest";

import {
  DIRECTORY_SIZE_CONFIG,
  getDirectorySizeError,
  getDirectorySizeQueryOptions,
  isDirectorySizeUnavailable,
  shouldEnableDirectorySizeQuery,
  shouldSkipSizeCalculation,
} from "@/hooks/filebrowser/useFileDirectorySizeBase";

describe("directory size query helpers", () => {
  it("skips system paths owned by the operating system or indexer", () => {
    expect(shouldSkipSizeCalculation("")).toBe(true);
    expect(shouldSkipSizeCalculation("/proc/1/fd")).toBe(true);
    expect(
      shouldSkipSizeCalculation("/var/lib/linuxio/indexer/indexer.db"),
    ).toBe(true);
    expect(shouldSkipSizeCalculation("/srv/data")).toBe(false);
  });

  it("returns cache and retry options", () => {
    const options = getDirectorySizeQueryOptions();
    expect(options).toMatchObject({
      gcTime: DIRECTORY_SIZE_CONFIG.CACHE_PERSISTENCE,
      refetchOnMount: false,
      refetchOnWindowFocus: false,
      staleTime: DIRECTORY_SIZE_CONFIG.CACHE_DURATION,
    });
    expect(options.retry(0)).toBe(true);
    expect(options.retry(DIRECTORY_SIZE_CONFIG.MAX_RETRIES)).toBe(false);
  });

  it("passes service errors through and ignores skipped paths", () => {
    expect(
      getDirectorySizeError(new Error("service unavailable"), false)?.message,
    ).toBe("service unavailable");
    expect(getDirectorySizeError(null, false)).toBeNull();
    expect(getDirectorySizeError(new Error("ignored"), true)).toBeNull();
  });

  it("enables queries for eligible paths without a capability gate", () => {
    expect(shouldEnableDirectorySizeQuery(true, "/srv/data", false)).toBe(true);
    expect(shouldEnableDirectorySizeQuery(false, "/srv/data", false)).toBe(
      false,
    );
    expect(shouldEnableDirectorySizeQuery(true, null, false)).toBe(false);
    expect(shouldEnableDirectorySizeQuery(true, "/proc", true)).toBe(false);
    expect(isDirectorySizeUnavailable(new Error("boom"), false)).toBe(true);
    expect(isDirectorySizeUnavailable(null, false)).toBe(false);
  });
});
