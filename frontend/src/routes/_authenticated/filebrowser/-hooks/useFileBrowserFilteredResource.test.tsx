import { beforeEach, describe, expect, it, vi } from "vitest";

import type { FileSearchWalk, SearchResult } from "@/api";
import { act, renderHook } from "@/test/render";

import type { FileResource } from "../-types";
import { useFileBrowserFilteredResource } from "./useFileBrowserFilteredResource";

const useFileSearchMock = vi.hoisted(() => vi.fn());
const authState = vi.hoisted(() => ({ privileged: true }));

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({ privileged: authState.privileged }),
}));

vi.mock("./useFileSearch", () => ({
  isSearchableQuery: (query: string) => Array.from(query.trim()).length >= 3,
  useFileSearch: useFileSearchMock,
}));

const directoryResource: FileResource = {
  name: "projects",
  path: "/srv/projects",
  type: "directory",
  items: [
    { name: "Alpha.txt", path: "/srv/projects/Alpha.txt", type: "file" },
    { name: "beta", path: "/srv/projects/beta", type: "directory" },
    { name: "gamma.log", path: "/srv/projects/gamma.log", type: "file" },
  ],
};

function mockSearch(
  overrides: Partial<{
    error: Error | null;
    isLoading: boolean;
    isUnavailable: boolean;
    results: SearchResult[];
    walk: FileSearchWalk;
  }> = {},
) {
  useFileSearchMock.mockReturnValue({
    count: 0,
    error: null,
    isLoading: false,
    isUnavailable: false,
    results: [],
    ...overrides,
  });
}

function renderFiltered(params: {
  basePath?: string;
  caseSensitive?: boolean;
  resource?: FileResource;
  searchQuery: string;
}) {
  return renderHook(() =>
    useFileBrowserFilteredResource({ basePath: "/", ...params }),
  );
}

describe("useFileBrowserFilteredResource", () => {
  beforeEach(() => {
    authState.privileged = true;
    mockSearch();
  });

  it("does not search without sudo and keeps the listing", () => {
    authState.privileged = false;
    const { result } = renderFiltered({
      resource: directoryResource,
      searchQuery: "Alpha",
    });

    expect(useFileSearchMock).toHaveBeenLastCalledWith({
      basePath: "/",
      caseSensitive: false,
      enabled: false,
      query: "Alpha",
    });
    expect(result.current.filteredResource).toBe(directoryResource);
  });

  it("returns the resource unchanged when the query is blank", () => {
    const { result } = renderFiltered({
      resource: directoryResource,
      searchQuery: "   ",
    });

    expect(result.current.filteredResource).toBe(directoryResource);
    expect(result.current.isSearchLoading).toBe(false);
  });

  it("returns undefined when there is no resource", () => {
    const { result } = renderFiltered({
      resource: undefined,
      searchQuery: "alpha",
    });

    expect(result.current.filteredResource).toBeUndefined();
  });

  it("returns non-directory resources unchanged", () => {
    const fileResource: FileResource = {
      name: "notes.txt",
      path: "/srv/notes.txt",
      type: "file",
    };

    const { result } = renderFiltered({
      resource: fileResource,
      searchQuery: "notes",
    });

    expect(result.current.filteredResource).toBe(fileResource);
  });

  it("preserves the listing until the query reaches three characters", () => {
    const { result } = renderFiltered({
      resource: directoryResource,
      searchQuery: "ab",
    });

    expect(useFileSearchMock).toHaveBeenLastCalledWith({
      basePath: "/",
      caseSensitive: false,
      enabled: false,
      query: "ab",
    });
    expect(result.current.filteredResource).toBe(directoryResource);
  });

  it("forwards case-sensitive searches", () => {
    renderFiltered({
      caseSensitive: true,
      resource: directoryResource,
      searchQuery: "Alpha",
    });

    expect(useFileSearchMock).toHaveBeenLastCalledWith({
      basePath: "/",
      caseSensitive: true,
      enabled: true,
      query: "Alpha",
    });
  });

  it("preserves the directory and surfaces service errors", () => {
    const error = new Error("indexer service unavailable");
    mockSearch({ error, isUnavailable: true });

    const { result } = renderFiltered({
      resource: directoryResource,
      searchQuery: "ALPHA",
    });

    expect(result.current.filteredResource).toBe(directoryResource);
    expect(result.current.searchError).toBe(error);
  });

  it("preserves the directory and exposes search errors instead of showing an empty result", () => {
    const error = new Error("indexer request failed");
    mockSearch({ error });

    const { result } = renderFiltered({
      resource: directoryResource,
      searchQuery: "ALPHA",
    });

    expect(result.current.filteredResource).toBe(directoryResource);
    expect(result.current.searchError).toBe(error);
  });

  it("maps remote search results into file items", () => {
    const results: SearchResult[] = [
      {
        isDir: false,
        canOpenAsText: false,
        isRegularFile: true,
        mod_time: "2026-01-01",
        name: "report.pdf",
        path: "/docs/report.pdf",
        size: 2048,
      },
      {
        isDir: true,
        isRegularFile: false,
        mod_time: "",
        name: "photos",
        path: "/media/photos/",
        size: 0,
      },
      {
        isDir: true,
        isRegularFile: false,
        mod_time: "",
        name: "config",
        path: "/etc/config",
        size: 12,
      },
      {
        isDir: false,
        isRegularFile: false,
        mod_time: "2026-02-02",
        name: "shortcut.lnk",
        path: "/srv/shortcut.lnk",
        size: 1,
      },
    ];
    mockSearch({ results });

    const { result } = renderFiltered({
      resource: directoryResource,
      searchQuery: "anything",
    });

    expect(result.current.filteredResource?.items).toEqual([
      expect.objectContaining({
        extension: "pdf",
        isDirectory: false,
        isRegularFile: true,
        canOpenAsText: false,
        modTime: "2026-01-01",
        name: "report.pdf",
        path: "/docs/report.pdf",
        showFullPath: true,
        size: 2048,
        type: "file",
      }),
      expect.objectContaining({
        extension: "",
        isDirectory: true,
        modTime: "",
        name: "photos",
        type: "directory",
      }),
      expect.objectContaining({
        extension: "",
        isDirectory: true,
        name: "config",
        type: "directory",
      }),
      expect.objectContaining({
        extension: "lnk",
        isDirectory: false,
        modTime: "2026-02-02",
        name: "shortcut.lnk",
        type: "file",
      }),
    ]);
  });

  it("searches the folder it shows", () => {
    renderFiltered({
      basePath: "/srv/projects",
      resource: directoryResource,
      searchQuery: "Alpha",
    });

    expect(useFileSearchMock).toHaveBeenLastCalledWith({
      basePath: "/srv/projects",
      caseSensitive: false,
      enabled: true,
      query: "Alpha",
    });
  });

  it("says when the folder searched in was not read whole on disk", () => {
    mockSearch({ walk: "partial" });
    const partial = renderFiltered({
      basePath: "/srv/projects",
      resource: directoryResource,
      searchQuery: "alpha",
    });
    expect(partial.result.current.isSearchPartial).toBe(true);

    mockSearch({ walk: "complete" });
    const complete = renderFiltered({
      basePath: "/srv/projects",
      resource: directoryResource,
      searchQuery: "alpha",
    });
    expect(complete.result.current.isSearchPartial).toBe(false);

    mockSearch({ walk: "partial" });
    const cleared = renderFiltered({
      basePath: "/srv/projects",
      resource: directoryResource,
      searchQuery: "al",
    });
    expect(cleared.result.current.isSearchPartial).toBe(false);

    const file = renderFiltered({
      basePath: "/srv/notes.txt",
      resource: { name: "notes.txt", path: "/srv/notes.txt", type: "file" },
      searchQuery: "alpha",
    });
    expect(file.result.current.isSearchPartial).toBe(false);
  });

  it("sends one search for a query typed quickly", () => {
    vi.useFakeTimers();
    try {
      const { rerender } = renderHook(
        ({ searchQuery }: { searchQuery: string }) =>
          useFileBrowserFilteredResource({
            basePath: "/srv/projects",
            resource: directoryResource,
            searchQuery,
          }),
        { initialProps: { searchQuery: "" } },
      );
      for (const typed of ["r", "re", "rep", "repo", "repor", "report"]) {
        rerender({ searchQuery: typed });
        act(() => {
          vi.advanceTimersByTime(50);
        });
      }
      act(() => {
        vi.advanceTimersByTime(250);
      });

      const searched = new Set(
        useFileSearchMock.mock.calls
          .filter(([options]) => options.enabled)
          .map(([options]) => options.query),
      );
      expect([...searched]).toEqual(["report"]);
    } finally {
      vi.useRealTimers();
    }
  });

  // Retyping a settled query must not bring its results back under the new
  // one while the new one settles.
  it.each([
    ["typed over", ["x", "xy", "xyz"]],
    ["corrected", ["ab", "abd"]],
  ])(
    "never searches for another query than the one typed (%s)",
    (_, typing) => {
      vi.useFakeTimers();
      try {
        let typed = "abc";
        const searched: Array<[string, string]> = [];
        useFileSearchMock.mockImplementation(
          (options: { enabled: boolean; query: string }) => {
            if (options.enabled) searched.push([typed, options.query]);
            return {
              count: 0,
              error: null,
              isLoading: false,
              isUnavailable: false,
              results: [],
            };
          },
        );
        const { rerender } = renderHook(
          ({ searchQuery }: { searchQuery: string }) => {
            typed = searchQuery;
            return useFileBrowserFilteredResource({
              basePath: "/srv/projects",
              resource: directoryResource,
              searchQuery,
            });
          },
          { initialProps: { searchQuery: "abc" } },
        );
        for (const next of typing) {
          rerender({ searchQuery: next });
          act(() => {
            vi.advanceTimersByTime(50);
          });
        }
        act(() => {
          vi.advanceTimersByTime(300);
        });

        expect(searched.filter(([shown, query]) => shown !== query)).toEqual(
          [],
        );
        expect(searched.at(-1)).toEqual([typing.at(-1), typing.at(-1)]);
      } finally {
        vi.useRealTimers();
      }
    },
  );

  it("preserves the resource shell but empties items when search yields nothing", () => {
    mockSearch({ results: [] });

    const { result } = renderFiltered({
      resource: directoryResource,
      searchQuery: "no-match",
    });

    expect(result.current.filteredResource?.items).toEqual([]);
    expect(result.current.filteredResource?.path).toBe(directoryResource.path);
    expect(result.current.filteredResource?.name).toBe(directoryResource.name);
  });

  it("propagates the search loading state", () => {
    mockSearch({ isLoading: true });

    const { result } = renderFiltered({
      resource: directoryResource,
      searchQuery: "alpha",
    });

    expect(result.current.isSearchLoading).toBe(true);
  });
});
