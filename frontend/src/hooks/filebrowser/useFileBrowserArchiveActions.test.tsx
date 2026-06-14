import { beforeEach, describe, expect, it, vi } from "vitest";

import { useFileBrowserArchiveActions } from "@/hooks/filebrowser/useFileBrowserArchiveActions";
import { act, renderHook } from "@/test/render";
import type { FileItem, FileResource } from "@/types/filebrowser";

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: {
    error: toastMocks.error,
    info: vi.fn(),
    success: toastMocks.success,
    warning: toastMocks.warning,
  },
}));

type Params = Parameters<typeof useFileBrowserArchiveActions>[0];

function fileItem(name: string, type = "file"): FileItem {
  return { name, path: `/srv/data/${name}`, type };
}

function directory(items: FileItem[]): FileResource {
  return { items, name: "data", path: "/srv/data", type: "directory" };
}

function setup(overrides: Partial<Params> = {}) {
  const params: Params = {
    compressItems: vi.fn().mockResolvedValue(undefined),
    extractArchive: vi.fn().mockResolvedValue(undefined),
    normalizedPath: "/srv/data",
    onContextMenuClose: vi.fn(),
    resource: directory([]),
    selectedItems: [],
    selectedPaths: new Set<string>(),
    ...overrides,
  };

  const utils = renderHook(() => useFileBrowserArchiveActions(params));
  return { ...utils, params };
}

describe("useFileBrowserArchiveActions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("reports compress availability from the selection size", () => {
    const { result } = setup({ selectedPaths: new Set(["/srv/data/a.txt"]) });
    expect(result.current.canCompressSelection).toBe(true);
  });

  it("allows extraction only for a single archive selection", () => {
    expect(
      setup({ selectedItems: [fileItem("bundle.zip")] }).result.current
        .canExtractSelection,
    ).toBe(true);
    expect(
      setup({ selectedItems: [fileItem("notes.txt")] }).result.current
        .canExtractSelection,
    ).toBe(false);
    expect(
      setup({
        selectedItems: [fileItem("a.zip"), fileItem("b.zip")],
      }).result.current.canExtractSelection,
    ).toBe(false);
  });

  it("opens the format dialog with a default base name for multiple items", () => {
    const { result, params } = setup({
      selectedItems: [fileItem("a.txt"), fileItem("b.txt")],
      selectedPaths: new Set(["/srv/data/a.txt", "/srv/data/b.txt"]),
    });

    act(() => result.current.handleCompressSelection());

    expect(params.onContextMenuClose).toHaveBeenCalledTimes(1);
    expect(result.current.compressFormatDialog).toEqual({
      baseName: "archive",
      paths: ["/srv/data/a.txt", "/srv/data/b.txt"],
    });
  });

  it("derives the base name from a single archive item", () => {
    const { result } = setup({
      selectedItems: [fileItem("project.zip")],
      selectedPaths: new Set(["/srv/data/project.zip"]),
    });

    act(() => result.current.handleCompressSelection());

    expect(result.current.compressFormatDialog?.baseName).toBe("project");
  });

  it("does nothing when compressing an empty selection", () => {
    const { result, params } = setup();

    act(() => result.current.handleCompressSelection());

    expect(result.current.compressFormatDialog).toBeNull();
    expect(params.onContextMenuClose).toHaveBeenCalledTimes(1);
  });

  it("compresses to a zip archive at the current directory", async () => {
    const { result, params } = setup({
      selectedItems: [fileItem("a.txt"), fileItem("b.txt")],
      selectedPaths: new Set(["/srv/data/a.txt", "/srv/data/b.txt"]),
    });

    act(() => result.current.handleCompressSelection());
    await act(async () => {
      await result.current.handleCompressConfirm("zip");
    });

    expect(params.compressItems).toHaveBeenCalledWith({
      archiveName: "archive.zip",
      destination: "/srv/data",
      paths: ["/srv/data/a.txt", "/srv/data/b.txt"],
    });
  });

  it("compresses to a tar.gz archive when that format is chosen", async () => {
    const { result, params } = setup({
      selectedItems: [fileItem("a.txt")],
      selectedPaths: new Set(["/srv/data/a.txt"]),
    });

    act(() => result.current.handleCompressSelection());
    await act(async () => {
      await result.current.handleCompressConfirm("tar.gz");
    });

    // A single non-archive item keeps its extension in the base name, so the
    // resulting archive is named "<full name>.tar.gz".
    expect(params.compressItems).toHaveBeenCalledWith(
      expect.objectContaining({ archiveName: "a.txt.tar.gz" }),
    );
  });

  it("uniquifies the archive name against existing directory entries", async () => {
    const { result, params } = setup({
      resource: directory([fileItem("archive.zip")]),
      selectedItems: [fileItem("a.txt"), fileItem("b.txt")],
      selectedPaths: new Set(["/srv/data/a.txt", "/srv/data/b.txt"]),
    });

    act(() => result.current.handleCompressSelection());
    await act(async () => {
      await result.current.handleCompressConfirm("zip");
    });

    expect(params.compressItems).toHaveBeenCalledWith(
      expect.objectContaining({ archiveName: "archive (1).zip" }),
    );
  });

  it("does not compress without an open format dialog", async () => {
    const { result, params } = setup();

    await act(async () => {
      await result.current.handleCompressConfirm("zip");
    });

    expect(params.compressItems).not.toHaveBeenCalled();
  });

  it("surfaces a 409 conflict error from the compress mutation", async () => {
    const compressItems = vi.fn().mockRejectedValue({
      response: { data: { error: "archive.zip already exists" }, status: 409 },
    });
    const { result } = setup({
      compressItems,
      selectedItems: [fileItem("a.txt"), fileItem("b.txt")],
      selectedPaths: new Set(["/srv/data/a.txt", "/srv/data/b.txt"]),
    });

    act(() => result.current.handleCompressSelection());
    await act(async () => {
      await result.current.handleCompressConfirm("zip");
    });

    expect(toastMocks.error).toHaveBeenCalledWith(
      "archive.zip already exists",
      expect.anything(),
    );
  });

  it("falls back to a generic message for non-conflict compress errors", async () => {
    const compressItems = vi.fn().mockRejectedValue({});
    const { result } = setup({
      compressItems,
      selectedItems: [fileItem("a.txt"), fileItem("b.txt")],
      selectedPaths: new Set(["/srv/data/a.txt", "/srv/data/b.txt"]),
    });

    act(() => result.current.handleCompressSelection());
    await act(async () => {
      await result.current.handleCompressConfirm("zip");
    });

    expect(toastMocks.error).toHaveBeenCalledWith(
      "Failed to create archive",
      expect.anything(),
    );
  });

  it("stays silent when a compress is cancelled", async () => {
    const compressItems = vi
      .fn()
      .mockRejectedValue({ name: "CanceledError", message: "canceled" });
    const { result } = setup({
      compressItems,
      selectedItems: [fileItem("a.txt")],
      selectedPaths: new Set(["/srv/data/a.txt"]),
    });

    act(() => result.current.handleCompressSelection());
    await act(async () => {
      await result.current.handleCompressConfirm("zip");
    });

    expect(toastMocks.error).not.toHaveBeenCalled();
  });

  it("extracts the selected archive into a uniquely named folder", async () => {
    const { result, params } = setup({
      selectedItems: [fileItem("bundle.zip")],
      selectedPaths: new Set(["/srv/data/bundle.zip"]),
    });

    await act(async () => {
      await result.current.handleExtractSelection();
    });

    expect(params.onContextMenuClose).toHaveBeenCalledTimes(1);
    expect(params.extractArchive).toHaveBeenCalledWith({
      archivePath: "/srv/data/bundle.zip",
      destination: "/srv/data/bundle",
    });
  });

  it("does not extract when the selection is not a single archive", async () => {
    const { result, params } = setup({
      selectedItems: [fileItem("notes.txt")],
      selectedPaths: new Set(["/srv/data/notes.txt"]),
    });

    await act(async () => {
      await result.current.handleExtractSelection();
    });

    expect(params.extractArchive).not.toHaveBeenCalled();
  });
});
