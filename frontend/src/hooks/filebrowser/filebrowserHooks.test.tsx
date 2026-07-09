import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { ConfigContext } from "@/contexts/ConfigContext";
import { useFilePathUtilities } from "@/hooks/filebrowser/useFilePathUtilities";
import {
  useFileSelection,
  useFileSelectionState,
} from "@/hooks/filebrowser/useFileSelection";
import { useFileViewState } from "@/hooks/filebrowser/useFileViewState";
import { act, renderHook } from "@/test/render";
import type { ConfigContextType } from "@/types/config";
import type { FileResource } from "@/types/filebrowser";

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: {
    error: toastMocks.error,
    success: toastMocks.success,
  },
}));

const directoryResource: FileResource = {
  name: "projects",
  path: "/srv/projects",
  type: "directory",
  items: [
    {
      name: "alpha.txt",
      path: "/srv/projects/alpha.txt",
      type: "file",
    },
    {
      name: "beta",
      path: "/srv/projects/beta",
      type: "directory",
    },
  ],
};

function configWrapper({
  setKey = vi.fn(),
  showHiddenFiles = true,
}: {
  setKey?: ConfigContextType["setKey"];
  showHiddenFiles?: boolean;
} = {}) {
  const value = {
    config: {
      appSettings: {
        chunkSizeMB: 1,
        containerOrder: [],
        dashboardOrder: [],
        hiddenCards: [],
        primaryColor: "#2196f3",
        showHiddenFiles,
        sidebarCollapsed: false,
        theme: "DARK",
        viewModes: {},
      },
      docker: {
        folders: [],
        requireMountsForFolders: false,
        proxy: {
          baseDomain: "",
          caddyEnabled: false,
          tlsEmail: "",
        },
      },
      jobs: {
        archiveCompressionWorkers: 0,
        archiveExtractWorkers: 0,
        heavyArchiveConcurrency: 1,
        notificationMinIntervalMs: 1000,
        progressMinBytesMB: 16,
        progressMinIntervalMs: 250,
      },
    },
    isLoaded: true,
    setKey,
    updateConfig: vi.fn(),
  } satisfies ConfigContextType;

  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>
    );
  };
}

describe("useFilePathUtilities", () => {
  it("joins paths without duplicating slashes", () => {
    const { result } = renderHook(() => useFilePathUtilities());

    expect(result.current.joinPath("/srv/projects", "alpha.txt")).toBe(
      "/srv/projects/alpha.txt",
    );
    expect(result.current.joinPath("/srv/projects/", "alpha.txt")).toBe(
      "/srv/projects/alpha.txt",
    );
  });

  it("returns parent paths and base names for root and trailing slashes", () => {
    const { result } = renderHook(() => useFilePathUtilities());

    expect(result.current.getParentPath("/srv/projects/alpha.txt")).toBe(
      "/srv/projects",
    );
    expect(result.current.getParentPath("/srv/projects/")).toBe("/srv");
    expect(result.current.getParentPath("/")).toBe("/");
    expect(result.current.getBaseName("/srv/projects/")).toBe("projects");
    expect(result.current.getBaseName("/")).toBe("");
  });
});

// Drives the clipboard behaviors against a real selection slice so the
// state transitions are exercised end-to-end.
function useSelectionHarness(
  params: Omit<Parameters<typeof useFileSelection>[0], "selection">,
) {
  const selection = useFileSelectionState(params.normalizedPath);
  const api = useFileSelection({ ...params, selection });
  return { ...api, selection };
}

describe("useFileSelection", () => {
  it("clears selection on navigation while preserving the clipboard", () => {
    const { result, rerender } = renderHook(
      ({ normalizedPath }) =>
        useSelectionHarness({
          copyItems: vi.fn(),
          moveItems: vi.fn(),
          normalizedPath,
          resource: directoryResource,
        }),
      { initialProps: { normalizedPath: "/srv/projects" } },
    );

    act(() => {
      result.current.selection.actions.select(
        new Set(["/srv/projects/alpha.txt"]),
      );
      result.current.selection.actions.copyToClipboard([
        "/srv/projects/alpha.txt",
      ]);
    });

    rerender({ normalizedPath: "/srv/target" });
    expect(result.current.selection.selectedPaths.size).toBe(0);
    expect(result.current.selection.clipboard).toEqual({
      operation: "copy",
      paths: ["/srv/projects/alpha.txt"],
    });

    rerender({ normalizedPath: "/srv/projects" });
    expect(result.current.selection.selectedPaths.size).toBe(0);
  });

  it("derives selected items from the current directory resource", () => {
    const { result } = renderHook(() =>
      useSelectionHarness({
        copyItems: vi.fn(),
        moveItems: vi.fn(),
        normalizedPath: "/srv/projects",
        resource: directoryResource,
      }),
    );

    act(() => {
      result.current.selection.actions.select(
        new Set(["/srv/projects/beta", "/srv/projects/missing"]),
      );
    });

    expect(result.current.selectedItems).toEqual([
      expect.objectContaining({
        name: "beta",
        path: "/srv/projects/beta",
      }),
    ]);
  });

  it("copies selected paths to clipboard and notifies the scoped toast", () => {
    const closeMenu = vi.fn();
    const { result } = renderHook(() =>
      useSelectionHarness({
        copyItems: vi.fn(),
        moveItems: vi.fn(),
        normalizedPath: "/srv/projects",
        onContextMenuClose: closeMenu,
        resource: directoryResource,
      }),
    );

    act(() => {
      result.current.selection.actions.select(
        new Set(["/srv/projects/alpha.txt", "/srv/projects/beta"]),
      );
    });
    act(() => result.current.handleCopy());

    expect(closeMenu).toHaveBeenCalledTimes(1);
    expect(result.current.selection.clipboard).toEqual({
      operation: "copy",
      paths: ["/srv/projects/alpha.txt", "/srv/projects/beta"],
    });
    expect(result.current.selection.cutPaths.size).toBe(0);
    expect(toastMocks.success).toHaveBeenCalledWith(
      "2 item(s) copied to clipboard",
      expect.objectContaining({
        meta: { href: "/filebrowser", label: "Open files" },
      }),
    );
  });

  it("exposes cut paths for visual dimming until pasted", () => {
    const { result } = renderHook(() =>
      useSelectionHarness({
        copyItems: vi.fn(),
        moveItems: vi.fn(),
        normalizedPath: "/srv/projects",
        resource: directoryResource,
      }),
    );

    act(() => {
      result.current.selection.actions.select(new Set(["/srv/projects/beta"]));
    });
    act(() => result.current.handleCut());

    expect([...result.current.selection.cutPaths]).toEqual([
      "/srv/projects/beta",
    ]);
  });

  it("pastes copy operations into the current directory", async () => {
    const copyItems = vi.fn(async () => undefined);
    const moveItems = vi.fn(async () => undefined);
    const { result } = renderHook(() =>
      useSelectionHarness({
        copyItems,
        moveItems,
        normalizedPath: "/srv/target",
        resource: directoryResource,
      }),
    );

    act(() => {
      result.current.selection.actions.copyToClipboard([
        "/srv/projects/alpha.txt",
      ]);
    });
    await act(async () => {
      await result.current.handlePaste();
    });

    expect(copyItems).toHaveBeenCalledWith({
      sourcePaths: ["/srv/projects/alpha.txt"],
      destinationDir: "/srv/target",
    });
    expect(moveItems).not.toHaveBeenCalled();
    expect(result.current.selection.clipboard).toEqual({
      operation: "copy",
      paths: ["/srv/projects/alpha.txt"],
    });
  });

  it("moves cut items, then clears clipboard and selection", async () => {
    const copyItems = vi.fn(async () => undefined);
    const moveItems = vi.fn(async () => undefined);
    const { result } = renderHook(() =>
      useSelectionHarness({
        copyItems,
        moveItems,
        normalizedPath: "/srv/target",
        resource: directoryResource,
      }),
    );

    act(() => {
      result.current.selection.actions.select(new Set(["/srv/projects/beta"]));
      result.current.selection.actions.cutToClipboard(["/srv/projects/beta"]);
    });
    await act(async () => {
      await result.current.handlePaste();
    });

    expect(moveItems).toHaveBeenCalledWith({
      sourcePaths: ["/srv/projects/beta"],
      destinationDir: "/srv/target",
    });
    expect(copyItems).not.toHaveBeenCalled();
    expect(result.current.selection.clipboard).toBeNull();
    expect(result.current.selection.selectedPaths.size).toBe(0);
  });

  it("reports an empty clipboard instead of calling mutations", async () => {
    const copyItems = vi.fn(async () => undefined);
    const moveItems = vi.fn(async () => undefined);
    const { result } = renderHook(() =>
      useSelectionHarness({
        copyItems,
        moveItems,
        normalizedPath: "/srv/target",
        resource: directoryResource,
      }),
    );

    await act(async () => {
      await result.current.handlePaste();
    });

    expect(toastMocks.error).toHaveBeenCalledWith(
      "Nothing to paste",
      expect.objectContaining({
        meta: { href: "/filebrowser", label: "Open files" },
      }),
    );
    expect(copyItems).not.toHaveBeenCalled();
    expect(moveItems).not.toHaveBeenCalled();
  });
});

describe("useFileViewState", () => {
  it("switches view modes locally", () => {
    const { result } = renderHook(() => useFileViewState(), {
      wrapper: configWrapper(),
    });
    const initialActions = result.current.actions;

    expect(result.current.viewMode).toBe("card");
    act(() => result.current.actions.switchView());
    expect(result.current.viewMode).toBe("list");
    expect(result.current.actions).toBe(initialActions);
  });

  it("toggles the sort order when the active field is selected again", () => {
    const { result } = renderHook(() => useFileViewState(), {
      wrapper: configWrapper(),
    });

    act(() => result.current.actions.changeSort("name"));
    expect(result.current.sortField).toBe("name");
    expect(result.current.sortOrder).toBe("desc");

    act(() => result.current.actions.changeSort("name"));
    expect(result.current.sortOrder).toBe("asc");
  });

  it("switches field and resets order to ascending for a new field", () => {
    const { result } = renderHook(() => useFileViewState(), {
      wrapper: configWrapper(),
    });

    act(() => result.current.actions.changeSort("name")); // -> desc
    act(() => result.current.actions.changeSort("size")); // new field -> asc

    expect(result.current.sortField).toBe("size");
    expect(result.current.sortOrder).toBe("asc");
  });

  it("tracks the search query and clears it on demand", () => {
    const { result } = renderHook(() => useFileViewState(), {
      wrapper: configWrapper(),
    });

    expect(result.current.searchQuery).toBe("");
    act(() => result.current.actions.setSearch("readme"));
    expect(result.current.searchQuery).toBe("readme");
    act(() => result.current.actions.clearSearch());
    expect(result.current.searchQuery).toBe("");
  });

  it("opens and closes the context menu at a position", () => {
    const { result } = renderHook(() => useFileViewState(), {
      wrapper: configWrapper(),
    });

    act(() => result.current.actions.openContextMenu({ left: 4, top: 8 }));
    expect(result.current.contextMenuPosition).toEqual({ left: 4, top: 8 });
    act(() => result.current.actions.closeContextMenu());
    expect(result.current.contextMenuPosition).toBeNull();
  });

  it("delegates hidden-file visibility changes to config", () => {
    const setKey = vi.fn();
    const { result } = renderHook(() => useFileViewState(), {
      wrapper: configWrapper({ setKey, showHiddenFiles: false }),
    });

    expect(result.current.showHiddenFiles).toBe(false);
    act(() => result.current.actions.toggleHiddenFiles());

    expect(setKey).toHaveBeenCalledTimes(1);
    expect(setKey.mock.calls[0][0]).toBe("showHiddenFiles");
    expect(
      (setKey.mock.calls[0][1] as (value: boolean) => boolean)(false),
    ).toBe(true);
  });
});
