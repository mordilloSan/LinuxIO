import { beforeEach, describe, expect, it, vi } from "vitest";

import { useFileBrowserFilteredResource } from "@/hooks/filebrowser/useFileBrowserFilteredResource";
import type { SearchResult } from "@/hooks/filebrowser/useFileSearch";
import { renderHook } from "@/test/render";
import type { FileResource } from "@/types/filebrowser";

const useFileSearchMock = vi.hoisted(() => vi.fn());

vi.mock("@/hooks/filebrowser/useFileSearch", () => ({
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
    isUnavailable: boolean;
    results: SearchResult[];
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
  resource?: FileResource;
  searchQuery: string;
}) {
  return renderHook(() => useFileBrowserFilteredResource(params));
}

describe("useFileBrowserFilteredResource", () => {
  beforeEach(() => {
    mockSearch();
  });

  it("returns the resource unchanged when the query is blank", () => {
    const { result } = renderFiltered({
      resource: directoryResource,
      searchQuery: "   ",
    });

    expect(result.current).toBe(directoryResource);
  });

  it("returns undefined when there is no resource", () => {
    const { result } = renderFiltered({
      resource: undefined,
      searchQuery: "alpha",
    });

    expect(result.current).toBeUndefined();
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

    expect(result.current).toBe(fileResource);
  });

  it("only enables the indexer search once the query reaches two characters", () => {
    renderFiltered({ resource: directoryResource, searchQuery: "a" });

    expect(useFileSearchMock).toHaveBeenLastCalledWith({
      basePath: "/",
      enabled: false,
      query: "a",
    });
  });

  it("filters items client-side (case-insensitive) when the indexer is unavailable", () => {
    mockSearch({ isUnavailable: true });

    const { result } = renderFiltered({
      resource: directoryResource,
      searchQuery: "ALPHA",
    });

    expect(result.current?.items?.map((item) => item.name)).toEqual([
      "Alpha.txt",
    ]);
  });

  it("maps remote search results into file items", () => {
    const results: SearchResult[] = [
      {
        mod_time: "2026-01-01",
        name: "report.pdf",
        path: "/docs/report.pdf",
        size: 2048,
        type: "file",
      },
      { name: "photos", path: "/media/photos/", size: 0, type: "" },
      { isDir: true, name: "config", path: "/etc/config", size: 12 },
      {
        modTime: "2026-02-02",
        name: "shortcut.lnk",
        path: "/srv/shortcut.lnk",
        size: 1,
        type: "symlink",
      },
    ];
    mockSearch({ results });

    const { result } = renderFiltered({
      resource: directoryResource,
      searchQuery: "anything",
    });

    expect(result.current?.items).toEqual([
      expect.objectContaining({
        extension: "pdf",
        isDirectory: false,
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
        type: "symlink",
      }),
    ]);
  });

  it("preserves the resource shell but empties items when search yields nothing", () => {
    mockSearch({ results: [] });

    const { result } = renderFiltered({
      resource: directoryResource,
      searchQuery: "no-match",
    });

    expect(result.current?.items).toEqual([]);
    expect(result.current?.path).toBe(directoryResource.path);
    expect(result.current?.name).toBe(directoryResource.name);
  });
});
