import { beforeEach, describe, expect, it, vi } from "vitest";

import type { SearchResult } from "@/api";
import { useFileBrowserFilteredResource } from "@/hooks/filebrowser/useFileBrowserFilteredResource";
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
    error: Error | null;
    isLoading: boolean;
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

  it("only enables the indexer search once the query reaches two characters", () => {
    renderFiltered({ resource: directoryResource, searchQuery: "a" });

    expect(useFileSearchMock).toHaveBeenLastCalledWith({
      basePath: "/",
      enabled: false,
      query: "a",
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
