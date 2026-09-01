import { QueryClientProvider } from "@tanstack/react-query";
import { createElement, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const apiMocks = vi.hoisted(() => ({
  searchData: undefined as unknown,
  searchError: null as Error | null,
  searchQueryOptions: vi.fn(),
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

const { useFileSearch } = await import("@/hooks/filebrowser/useFileSearch");
const { createTestQueryClient, renderHook, waitFor } =
  await import("@/test/render");
const queryClient = createTestQueryClient();
const queryWrapper = ({ children }: { children: ReactNode }) =>
  createElement(QueryClientProvider, { client: queryClient }, children);

describe("useFileSearch", () => {
  beforeEach(() => {
    queryClient.clear();
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

  it("keeps queries shorter than three characters disabled", () => {
    const { result } = renderHook(() => useFileSearch({ query: "ab" }), {
      wrapper: queryWrapper,
    });

    expect(apiMocks.searchQueryOptions).toHaveBeenCalledWith({
      basePath: "/",
      limit: "100",
      query: "ab",
    });
    expect(result.current.isLoading).toBe(false);
    expect(result.current.results).toEqual([]);
    expect(result.current.count).toBe(0);
  });

  it("uses the compatible modifier for case-sensitive searches", () => {
    renderHook(() => useFileSearch({ caseSensitive: true, query: "Alpha" }), {
      wrapper: queryWrapper,
    });

    expect(apiMocks.searchQueryOptions).toHaveBeenCalledWith({
      basePath: "/",
      limit: "100",
      query: "case:exact Alpha",
    });
  });

  it("runs searches with query params and returns backend results", () => {
    apiMocks.searchData = {
      results: [
        {
          canOpenAsText: true,
          isDir: false,
          isRegularFile: true,
          mod_time: "2026-08-29T00:00:00Z",
          name: "compose.yaml",
          path: "/srv/compose.yaml",
          size: 100,
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
