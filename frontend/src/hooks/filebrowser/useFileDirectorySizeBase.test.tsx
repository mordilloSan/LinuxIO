import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";

import { AuthContext } from "@/contexts/AuthContext";
import {
  DIRECTORY_SIZE_CONFIG,
  getDirectorySizeError,
  getDirectorySizeQueryOptions,
  isDirectorySizeUnavailable,
  shouldEnableDirectorySizeQuery,
  shouldSkipSizeCalculation,
  useIndexerAvailability,
} from "@/hooks/filebrowser/useFileDirectorySizeBase";
import {
  createAuthContextValue,
  createTestQueryClient,
  renderHook,
  seedCapabilityCache,
} from "@/test/render";

describe("directory size query helpers", () => {
  it("skips empty and system directory paths", () => {
    expect(shouldSkipSizeCalculation("")).toBe(true);
    expect(shouldSkipSizeCalculation("/proc")).toBe(true);
    expect(shouldSkipSizeCalculation("/proc/1/fd")).toBe(true);
    expect(shouldSkipSizeCalculation("/dev")).toBe(true);
    expect(shouldSkipSizeCalculation("/sys/class")).toBe(true);
    expect(shouldSkipSizeCalculation("/srv/data")).toBe(false);
  });

  it("returns cache, retry, and refetch options for directory size queries", () => {
    const options = getDirectorySizeQueryOptions();

    expect(options).toMatchObject({
      gcTime: DIRECTORY_SIZE_CONFIG.CACHE_PERSISTENCE,
      refetchOnMount: false,
      refetchOnWindowFocus: false,
      staleTime: DIRECTORY_SIZE_CONFIG.CACHE_DURATION,
    });
    expect(options.retry(0)).toBe(true);
    expect(options.retry(DIRECTORY_SIZE_CONFIG.MAX_RETRIES)).toBe(false);
    expect(options.retryDelay()).toBe(DIRECTORY_SIZE_CONFIG.FAILED_RETRY_DELAY);
  });

  it("derives an unavailable error only when indexer-disabled paths need indexing", () => {
    expect(getDirectorySizeError(null, true, false)?.message).toBe(
      "Directory size indexing is unavailable",
    );
    expect(getDirectorySizeError(null, true, true)).toBeNull();
    expect(
      getDirectorySizeError(new Error("boom"), false, false)?.message,
    ).toBe("boom");
  });

  it("reports unavailable state when data cannot be served", () => {
    expect(isDirectorySizeUnavailable(null, true, false)).toBe(true);
    expect(isDirectorySizeUnavailable(new Error("boom"), false, false)).toBe(
      true,
    );
    expect(isDirectorySizeUnavailable(null, false, false)).toBe(false);
  });

  it("enables queries only for eligible paths and available indexer capability", () => {
    expect(
      shouldEnableDirectorySizeQuery(true, "/srv/data", false, false),
    ).toBe(true);
    expect(
      shouldEnableDirectorySizeQuery(false, "/srv/data", false, false),
    ).toBe(false);
    expect(shouldEnableDirectorySizeQuery(true, null, false, false)).toBe(
      false,
    );
    expect(shouldEnableDirectorySizeQuery(true, "/proc", true, false)).toBe(
      false,
    );
    expect(shouldEnableDirectorySizeQuery(true, "/srv/data", false, true)).toBe(
      false,
    );
  });
});

describe("useIndexerAvailability", () => {
  it("maps indexer capability state to disabled boolean", () => {
    const capabilityWrapper = (indexerAvailable: boolean) => {
      const queryClient = createTestQueryClient();
      seedCapabilityCache(queryClient, { indexerAvailable });
      return ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>
          <AuthContext.Provider value={createAuthContextValue()}>
            {children}
          </AuthContext.Provider>
        </QueryClientProvider>
      );
    };
    const enabled = renderHook(() => useIndexerAvailability(), {
      wrapper: capabilityWrapper(true),
    });
    const disabled = renderHook(() => useIndexerAvailability(), {
      wrapper: capabilityWrapper(false),
    });

    expect(enabled.result.current).toBe(false);
    expect(disabled.result.current).toBe(true);
  });
});
