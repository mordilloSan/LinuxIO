import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { act, createTestQueryClient, renderHook } from "@/test/render";

import { useFileDirectorySize } from "./useFileDirectorySize";
import { DIRECTORY_SIZE_CONFIG } from "./useFileDirectorySizeBase";

const dirSize = vi.hoisted(() => vi.fn());

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({ privileged: true }),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        dir_size: ({ path }: { path: string }) => ({
          queryFn: dirSize,
          queryKey: ["linuxio", "filebrowser", "dir_size", path],
        }),
      },
    },
  };
});

function renderDirectorySize(path: string) {
  const queryClient = createTestQueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return renderHook(() => useFileDirectorySize(path), { wrapper });
}

const advance = (ms: number) =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });

describe("useFileDirectorySize", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    dirSize.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("asks again while the index still has an update for the folder", async () => {
    dirSize
      .mockResolvedValueOnce({
        fileCount: 1,
        folderCount: 0,
        pending: true,
        size: 10,
      })
      .mockResolvedValueOnce({ fileCount: 2, folderCount: 0, size: 30 });
    const { result } = renderDirectorySize("/srv/data");

    await advance(0);
    expect(result.current.size).toBe(10);

    await advance(DIRECTORY_SIZE_CONFIG.PENDING_REFETCH_DELAY);
    expect(dirSize).toHaveBeenCalledTimes(2);
    // The answer reaches the hook one timer turn after the request ends.
    await advance(1);
    expect(result.current.size).toBe(30);
    expect(result.current.fileCount).toBe(2);

    // The answer that is final is kept: nothing asks a third time.
    await advance(10 * DIRECTORY_SIZE_CONFIG.PENDING_REFETCH_DELAY);
    expect(dirSize).toHaveBeenCalledTimes(2);
  });

  it("asks once for a size nothing is pending for", async () => {
    dirSize.mockResolvedValue({ fileCount: 1, folderCount: 0, size: 10 });
    const { result } = renderDirectorySize("/srv/data");

    await advance(0);
    expect(result.current.size).toBe(10);
    await advance(10 * DIRECTORY_SIZE_CONFIG.PENDING_REFETCH_DELAY);
    expect(dirSize).toHaveBeenCalledTimes(1);
  });
});
