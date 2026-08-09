import { QueryClientProvider } from "@tanstack/react-query";
import { createElement, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const apiMocks = vi.hoisted(() => ({
  searchData: undefined as unknown,
  searchError: null as Error | null,
  searchQueryOptions: vi.fn(),
}));

const capabilityMocks = vi.hoisted(() => ({
  useCapability: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        search: Object.assign(apiMocks.searchQueryOptions, {
          route: actual.linuxio.filebrowser.search.route,
        }),
      },
    },
  };
});

vi.mock("@/hooks/useCapabilities", async () => {
  const actual = await vi.importActual<
    typeof import("@/hooks/useCapabilities")
  >("@/hooks/useCapabilities");
  return {
    ...actual,
    useCapability: capabilityMocks.useCapability,
  };
});

const { useFileSearch } = await import("@/hooks/filebrowser/useFileSearch");
const { createTestQueryClient, renderHook, waitFor } =
  await import("@/test/render");
const queryClient = createTestQueryClient();
const queryWrapper = ({ children }: { children: ReactNode }) =>
  createElement(QueryClientProvider, { client: queryClient }, children);

describe("useFileSearch", () => {
  beforeEach(() => {
    queryClient.clear();
    capabilityMocks.useCapability.mockReturnValue({
      isEnabled: true,
      reason: "",
      status: "available",
      value: true,
    });
    apiMocks.searchData = undefined;
    apiMocks.searchError = null;
    apiMocks.searchQueryOptions.mockImplementation((request: unknown) => ({
      queryKey: ["test", "file-search", request],
      queryFn: () =>
        apiMocks.searchError
          ? Promise.reject(apiMocks.searchError)
          : Promise.resolve(apiMocks.searchData),
      initialData: apiMocks.searchData,
      retryDelay: 0,
    }));
  });

  it("keeps short queries disabled and suppresses loading", () => {
    const { result } = renderHook(() => useFileSearch({ query: "a" }), {
      wrapper: queryWrapper,
    });

    expect(apiMocks.searchQueryOptions).toHaveBeenCalledWith({
      basePath: "/",
      limit: "100",
      query: "a",
    });
    expect(result.current.isLoading).toBe(false);
    expect(result.current.results).toEqual([]);
    expect(result.current.count).toBe(0);
  });

  it("runs searches with query params and returns backend results", () => {
    apiMocks.searchData = {
      count: 1,
      query: "compose",
      results: [
        {
          name: "compose.yaml",
          path: "/srv/compose.yaml",
          size: 100,
          type: "file",
        },
      ],
    };

    const { result } = renderHook(
      () =>
        useFileSearch({
          basePath: "/srv",
          limit: 25,
          query: "compose",
        }),
      { wrapper: queryWrapper },
    );

    expect(apiMocks.searchQueryOptions).toHaveBeenCalledWith({
      basePath: "/srv",
      limit: "25",
      query: "compose",
    });
    expect(result.current.count).toBe(1);
    expect(result.current.results[0]).toMatchObject({
      name: "compose.yaml",
      path: "/srv/compose.yaml",
    });
  });

  it("returns an unavailable error when the indexer is disabled", () => {
    capabilityMocks.useCapability.mockReturnValue({
      isEnabled: false,
      reason: "Indexer status unknown",
      status: "unknown",
      value: null,
    });
    const { result } = renderHook(
      () => useFileSearch({ query: "compose", enabled: true }),
      { wrapper: queryWrapper },
    );

    expect(apiMocks.searchQueryOptions).toHaveBeenCalledWith({
      basePath: "/",
      limit: "100",
      query: "compose",
    });
    expect(result.current.isUnavailable).toBe(true);
    expect(result.current.isLoading).toBe(false);
    expect(result.current.error?.message).toBe("Indexer status unknown");
  });

  it("passes through backend query errors when searching is enabled", async () => {
    apiMocks.searchError = new Error("backend failed");

    const { result } = renderHook(() => useFileSearch({ query: "compose" }), {
      wrapper: queryWrapper,
    });

    await waitFor(() =>
      expect(result.current.error?.message).toBe("backend failed"),
    );
  });
});
