import { describe, expect, it, vi } from "vitest";

import { ConfigContext } from "@/contexts/ConfigContext";
import { useFilePathUtilities } from "@/hooks/filebrowser/useFilePathUtilities";
import { useFileSelection } from "@/hooks/filebrowser/useFileSelection";
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

  return function Wrapper({ children }: { children: React.ReactNode }) {
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

describe("useFileSelection", () => {
  it("derives selected items from the current directory resource", () => {
    const { result } = renderHook(() =>
      useFileSelection({
        copyItems: vi.fn(),
        moveItems: vi.fn(),
        normalizedPath: "/srv/projects",
        resource: directoryResource,
      }),
    );

    act(() => {
      result.current.setSelectedPaths(
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
      useFileSelection({
        copyItems: vi.fn(),
        moveItems: vi.fn(),
        normalizedPath: "/srv/projects",
        onContextMenuClose: closeMenu,
        resource: directoryResource,
      }),
    );

    act(() => {
      result.current.setSelectedPaths(
        new Set(["/srv/projects/alpha.txt", "/srv/projects/beta"]),
      );
    });
    act(() => result.current.handleCopy());

    expect(closeMenu).toHaveBeenCalledTimes(1);
    expect(result.current.clipboard).toEqual({
      operation: "copy",
      paths: ["/srv/projects/alpha.txt", "/srv/projects/beta"],
    });
    expect(toastMocks.success).toHaveBeenCalledWith(
      "2 item(s) copied to clipboard",
      expect.objectContaining({
        meta: { href: "/filebrowser", label: "Open files" },
      }),
    );
  });

  it("pastes copy operations into the current directory", async () => {
    const copyItems = vi.fn(async () => undefined);
    const moveItems = vi.fn(async () => undefined);
    const { result } = renderHook(() =>
      useFileSelection({
        copyItems,
        moveItems,
        normalizedPath: "/srv/target",
        resource: directoryResource,
      }),
    );

    act(() => {
      result.current.setClipboard({
        operation: "copy",
        paths: ["/srv/projects/alpha.txt"],
      });
    });
    await act(async () => {
      await result.current.handlePaste();
    });

    expect(copyItems).toHaveBeenCalledWith({
      sourcePaths: ["/srv/projects/alpha.txt"],
      destinationDir: "/srv/target",
    });
    expect(moveItems).not.toHaveBeenCalled();
    expect(result.current.clipboard).toEqual({
      operation: "copy",
      paths: ["/srv/projects/alpha.txt"],
    });
  });

  it("moves cut items, then clears clipboard and selection", async () => {
    const copyItems = vi.fn(async () => undefined);
    const moveItems = vi.fn(async () => undefined);
    const { result } = renderHook(() =>
      useFileSelection({
        copyItems,
        moveItems,
        normalizedPath: "/srv/target",
        resource: directoryResource,
      }),
    );

    act(() => {
      result.current.setSelectedPaths(new Set(["/srv/projects/beta"]));
      result.current.setClipboard({
        operation: "cut",
        paths: ["/srv/projects/beta"],
      });
    });
    await act(async () => {
      await result.current.handlePaste();
    });

    expect(moveItems).toHaveBeenCalledWith({
      sourcePaths: ["/srv/projects/beta"],
      destinationDir: "/srv/target",
    });
    expect(copyItems).not.toHaveBeenCalled();
    expect(result.current.clipboard).toBeNull();
    expect(result.current.selectedPaths.size).toBe(0);
  });

  it("reports an empty clipboard instead of calling mutations", async () => {
    const copyItems = vi.fn(async () => undefined);
    const moveItems = vi.fn(async () => undefined);
    const { result } = renderHook(() =>
      useFileSelection({
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
  it("switches view modes and sort state locally", () => {
    const { result } = renderHook(() => useFileViewState(), {
      wrapper: configWrapper(),
    });

    expect(result.current.viewMode).toBe("card");
    act(() => result.current.handleSwitchView());
    expect(result.current.viewMode).toBe("list");
    act(() => result.current.setSortField("size"));
    act(() => result.current.setSortOrder("desc"));

    expect(result.current.sortField).toBe("size");
    expect(result.current.sortOrder).toBe("desc");
  });

  it("delegates hidden-file visibility changes to config", () => {
    const setKey = vi.fn();
    const { result } = renderHook(() => useFileViewState(), {
      wrapper: configWrapper({ setKey, showHiddenFiles: false }),
    });

    expect(result.current.showHiddenFiles).toBe(false);
    act(() => result.current.handleToggleHiddenFiles());
    act(() => result.current.setShowHiddenFiles(true));

    expect(setKey).toHaveBeenCalledTimes(2);
    expect(setKey.mock.calls[0][0]).toBe("showHiddenFiles");
    expect(
      (setKey.mock.calls[0][1] as (value: boolean) => boolean)(false),
    ).toBe(true);
    expect(setKey.mock.calls[1]).toEqual(["showHiddenFiles", true]);
  });
});
