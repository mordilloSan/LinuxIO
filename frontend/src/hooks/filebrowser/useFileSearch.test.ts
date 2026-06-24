import { beforeEach, describe, expect, it, vi } from "vitest";

const apiMocks = vi.hoisted(() => ({
  searchUseQuery: vi.fn(),
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
        search: {
          useQuery: apiMocks.searchUseQuery,
        },
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
const { renderHook } = await import("@/test/render");

describe("useFileSearch", () => {
  beforeEach(() => {
    capabilityMocks.useCapability.mockReturnValue({
      isEnabled: true,
      reason: "",
      status: "available",
      value: true,
    });
    apiMocks.searchUseQuery.mockReturnValue({
      data: undefined,
      error: null,
      isLoading: false,
    });
  });

  it("keeps short queries disabled and suppresses loading", () => {
    apiMocks.searchUseQuery.mockReturnValue({
      data: undefined,
      error: null,
      isLoading: true,
    });

    const { result } = renderHook(() => useFileSearch({ query: "a" }));

    expect(apiMocks.searchUseQuery).toHaveBeenCalledWith(
      { basePath: "/", limit: "100", query: "a" },
      expect.objectContaining({ enabled: false }),
    );
    expect(result.current.isLoading).toBe(false);
    expect(result.current.results).toEqual([]);
    expect(result.current.count).toBe(0);
  });

  it("runs searches with query params and returns backend results", () => {
    apiMocks.searchUseQuery.mockReturnValue({
      data: {
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
      },
      error: null,
      isLoading: false,
    });

    const { result } = renderHook(() =>
      useFileSearch({
        basePath: "/srv",
        limit: 25,
        query: "compose",
      }),
    );

    expect(apiMocks.searchUseQuery).toHaveBeenCalledWith(
      { basePath: "/srv", limit: "25", query: "compose" },
      expect.objectContaining({
        enabled: true,
        refetchOnMount: false,
        refetchOnWindowFocus: false,
        retry: 1,
      }),
    );
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
    apiMocks.searchUseQuery.mockReturnValue({
      data: undefined,
      error: null,
      isLoading: true,
    });

    const { result } = renderHook(() =>
      useFileSearch({ query: "compose", enabled: true }),
    );

    expect(apiMocks.searchUseQuery).toHaveBeenCalledWith(
      { basePath: "/", limit: "100", query: "compose" },
      expect.objectContaining({ enabled: false }),
    );
    expect(result.current.isUnavailable).toBe(true);
    expect(result.current.isLoading).toBe(false);
    expect(result.current.error?.message).toBe("Indexer status unknown");
  });

  it("passes through backend query errors when searching is enabled", () => {
    apiMocks.searchUseQuery.mockReturnValue({
      data: undefined,
      error: new Error("backend failed"),
      isLoading: false,
    });

    const { result } = renderHook(() => useFileSearch({ query: "compose" }));

    expect(result.current.error?.message).toBe("backend failed");
  });
});
