import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useFileBrowserItemActions } from "@/hooks/filebrowser/useFileBrowserItemActions";
import { act, createTestQueryClient, renderHook } from "@/test/render";
import type { FileItem, FileResource } from "@/types/filebrowser";

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
}));

const statMock = vi.hoisted(() => vi.fn());

vi.mock("sonner", () => ({
  toast: {
    error: toastMocks.error,
    info: vi.fn(),
    success: toastMocks.success,
    warning: vi.fn(),
  },
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        resource_stat: {
          ...actual.linuxio.filebrowser.resource_stat,
          queryOptions: (path: string) => ({
            queryFn: () => statMock(path),
            queryKey: ["resource_stat", path],
          }),
        },
      },
    },
  };
});

type Params = Parameters<typeof useFileBrowserItemActions>[0];

function fileItem(name: string, type = "file"): FileItem {
  return { name, path: `/srv/projects/${name}`, type };
}

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={createTestQueryClient()}>
      {children}
    </QueryClientProvider>
  );
}

function setup(overrides: Partial<Params> = {}) {
  const params: Params = {
    changePermissions: vi.fn().mockResolvedValue(undefined),
    createFile: vi.fn(),
    createFolder: vi.fn(),
    deleteItems: vi.fn(),
    handleOpenDirectory: vi.fn(),
    onContextMenuClose: vi.fn(),
    pendingDeletePaths: [],
    permissionsDialog: null,
    renameItem: vi.fn().mockResolvedValue(undefined),
    resource: undefined,
    selectedItems: [],
    selectedPaths: new Set<string>(),
    setCreateFileDialog: vi.fn(),
    setCreateFolderDialog: vi.fn(),
    setDeleteDialog: vi.fn(),
    setDetailTarget: vi.fn(),
    setEditingPath: vi.fn(),
    setPendingDeletePaths: vi.fn(),
    setPermissionsDialog: vi.fn(),
    setSearchQuery: vi.fn(),
    startDownload: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };

  const utils = renderHook(() => useFileBrowserItemActions(params), {
    wrapper,
  });
  return { ...utils, params };
}

describe("useFileBrowserItemActions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("editing entry points", () => {
    it("opens editable files inline and routes others to the unsupported dialog", () => {
      const { result, params } = setup();

      act(() => result.current.handleDoubleClickFile(fileItem("notes.txt")));
      expect(params.setEditingPath).toHaveBeenCalledWith(
        "/srv/projects/notes.txt",
      );

      act(() => result.current.handleDoubleClickFile(fileItem("photo.png")));
      expect(result.current.unsupportedEditPath).toBe(
        "/srv/projects/photo.png",
      );
    });

    it("edits a file from the detail view and clears the detail target", () => {
      const { result, params } = setup();

      act(() => result.current.handleEditFile("/srv/projects/readme.md"));

      expect(params.setEditingPath).toHaveBeenCalledWith(
        "/srv/projects/readme.md",
      );
      expect(params.setDetailTarget).toHaveBeenCalledWith(null);
    });

    it("confirms an unsupported edit by promoting it to the editor", () => {
      const { result, params } = setup();

      act(() => result.current.handleEditFile("/srv/projects/photo.png"));
      act(() => result.current.handleConfirmUnsupportedEdit());

      expect(params.setEditingPath).toHaveBeenCalledWith(
        "/srv/projects/photo.png",
      );
      expect(result.current.unsupportedEditPath).toBeNull();
      expect(params.setDetailTarget).toHaveBeenCalledWith(null);
    });
  });

  describe("details and downloads", () => {
    it("exposes detail availability and targets the current selection", () => {
      const { result, params } = setup({
        selectedPaths: new Set(["/srv/projects/a", "/srv/projects/b"]),
      });

      expect(result.current.canShowDetails).toBe(true);

      act(() => result.current.handleShowDetails());

      expect(params.onContextMenuClose).toHaveBeenCalledTimes(1);
      expect(params.setDetailTarget).toHaveBeenCalledWith([
        "/srv/projects/a",
        "/srv/projects/b",
      ]);
    });

    it("skips showing details when nothing is selected", () => {
      const { result, params } = setup();

      act(() => result.current.handleShowDetails());

      expect(params.setDetailTarget).not.toHaveBeenCalled();
    });

    it("downloads the current selection and individual paths", () => {
      const { result, params } = setup({
        selectedPaths: new Set(["/srv/projects/a", "/srv/projects/b"]),
      });

      act(() => result.current.handleDownloadSelected());
      expect(params.startDownload).toHaveBeenCalledWith([
        "/srv/projects/a",
        "/srv/projects/b",
      ]);

      act(() => result.current.handleDownloadCurrent("/srv/projects/c"));
      expect(params.startDownload).toHaveBeenCalledWith(["/srv/projects/c"]);
    });

    it("does not download an empty selection", () => {
      const { result, params } = setup();

      act(() => result.current.handleDownloadSelected());

      expect(params.startDownload).not.toHaveBeenCalled();
    });

    it("opens the containing folder and clears the search query", () => {
      const { result, params } = setup({
        selectedPaths: new Set(["/srv/projects/file.txt"]),
      });

      act(() => result.current.handleOpenContainingFolder());

      expect(params.setSearchQuery).toHaveBeenCalledWith("");
      expect(params.handleOpenDirectory).toHaveBeenCalledWith("/srv/projects");
    });
  });

  describe("create dialogs", () => {
    it("opens and confirms the create-file flow", () => {
      const { result, params } = setup();

      act(() => result.current.handleCreateFile());
      expect(params.onContextMenuClose).toHaveBeenCalledTimes(1);
      expect(params.setCreateFileDialog).toHaveBeenCalledWith(true);

      act(() => result.current.handleCloseCreateFileDialog());
      expect(params.setCreateFileDialog).toHaveBeenCalledWith(false);

      act(() => result.current.handleConfirmCreateFile("new.txt"));
      expect(params.createFile).toHaveBeenCalledWith("new.txt");
    });

    it("opens and confirms the create-folder flow", () => {
      const { result, params } = setup();

      act(() => result.current.handleCreateFolder());
      expect(params.setCreateFolderDialog).toHaveBeenCalledWith(true);

      act(() => result.current.handleConfirmCreateFolder("assets"));
      expect(params.createFolder).toHaveBeenCalledWith("assets");
    });
  });

  describe("delete flow", () => {
    it("stages selected paths for deletion", () => {
      const { result, params } = setup({
        selectedPaths: new Set(["/srv/projects/a"]),
      });

      act(() => result.current.handleDelete());

      expect(params.setPendingDeletePaths).toHaveBeenCalledWith([
        "/srv/projects/a",
      ]);
      expect(params.setDeleteDialog).toHaveBeenCalledWith(true);
    });

    it("warns when deleting with nothing selected", () => {
      const { result, params } = setup();

      act(() => result.current.handleDelete());

      expect(toastMocks.error).toHaveBeenCalledWith(
        "No items selected",
        expect.anything(),
      );
      expect(params.setDeleteDialog).not.toHaveBeenCalled();
    });

    it("confirms deletion of the pending paths", () => {
      const { result, params } = setup({
        pendingDeletePaths: ["/srv/projects/a"],
      });

      act(() => result.current.handleConfirmDelete());

      expect(params.deleteItems).toHaveBeenCalledWith(["/srv/projects/a"]);
      expect(params.setPendingDeletePaths).toHaveBeenCalledWith([]);
    });

    it("ignores confirmation when there are no pending paths", () => {
      const { result, params } = setup({ pendingDeletePaths: [] });

      act(() => result.current.handleConfirmDelete());

      expect(params.deleteItems).not.toHaveBeenCalled();
    });

    it("clears pending paths when the delete dialog closes", () => {
      const { result, params } = setup();

      act(() => result.current.handleCloseDeleteDialog());

      expect(params.setDeleteDialog).toHaveBeenCalledWith(false);
      expect(params.setPendingDeletePaths).toHaveBeenCalledWith([]);
    });
  });

  describe("inline rename", () => {
    it("starts a rename only for a single selected path", () => {
      const single = setup({
        selectedPaths: new Set(["/srv/projects/a.txt"]),
      });
      act(() => single.result.current.handleStartInlineRename());
      expect(single.result.current.renamingPath).toBe("/srv/projects/a.txt");

      const multiple = setup({
        selectedPaths: new Set(["/srv/projects/a.txt", "/srv/projects/b.txt"]),
      });
      act(() => multiple.result.current.handleStartInlineRename());
      expect(multiple.result.current.renamingPath).toBeNull();
    });

    it("renames a file to the trimmed name in the same directory", async () => {
      const { result, params } = setup();

      await act(async () => {
        await result.current.handleConfirmInlineRename(
          "/srv/projects/old.txt",
          "  new.txt  ",
        );
      });

      expect(params.renameItem).toHaveBeenCalledWith({
        destination: "/srv/projects/new.txt",
        from: "/srv/projects/old.txt",
      });
    });

    it("keeps the trailing slash when renaming a directory", async () => {
      const { result, params } = setup({
        resource: {
          items: [fileItem("old", "directory")],
          name: "projects",
          path: "/srv/projects",
          type: "directory",
        } as FileResource,
      });

      await act(async () => {
        await result.current.handleConfirmInlineRename(
          "/srv/projects/old",
          "new",
        );
      });

      expect(params.renameItem).toHaveBeenCalledWith({
        destination: "/srv/projects/new/",
        from: "/srv/projects/old",
      });
    });

    it("cancels a rename for a blank name without calling the mutation", async () => {
      const { result, params } = setup();

      await act(async () => {
        await result.current.handleConfirmInlineRename(
          "/srv/projects/a",
          "   ",
        );
      });

      expect(params.renameItem).not.toHaveBeenCalled();
      expect(result.current.renamingPath).toBeNull();
    });

    it("cancels an in-progress rename", () => {
      const { result } = setup({
        selectedPaths: new Set(["/srv/projects/a.txt"]),
      });

      act(() => result.current.handleStartInlineRename());
      act(() => result.current.handleCancelInlineRename());

      expect(result.current.renamingPath).toBeNull();
    });
  });

  describe("permissions", () => {
    it("fetches the stat and opens the permissions dialog", async () => {
      statMock.mockResolvedValue({
        group: "wheel",
        mode: "0755",
        owner: "root",
      });
      const { result, params } = setup({
        selectedItems: [fileItem("a.txt")],
        selectedPaths: new Set(["/srv/projects/a.txt"]),
      });

      await act(async () => {
        await result.current.handleChangePermissions();
      });

      expect(statMock).toHaveBeenCalledWith("/srv/projects/a.txt");
      expect(params.setPermissionsDialog).toHaveBeenCalledWith(
        expect.objectContaining({
          group: "wheel",
          isDirectory: false,
          mode: "0755",
          owner: "root",
          paths: ["/srv/projects/a.txt"],
          pathLabel: "/srv/projects/a.txt",
          selectionCount: 1,
        }),
      );
    });

    it("reports an error when the stat fetch fails", async () => {
      statMock.mockRejectedValue(new Error("nope"));
      const { result, params } = setup({
        selectedItems: [fileItem("a.txt")],
        selectedPaths: new Set(["/srv/projects/a.txt"]),
      });

      await act(async () => {
        await result.current.handleChangePermissions();
      });

      expect(toastMocks.error).toHaveBeenCalledWith(
        "Failed to fetch file permissions",
        expect.anything(),
      );
      expect(params.setPermissionsDialog).not.toHaveBeenCalled();
    });

    it("does not fetch when nothing is selected", async () => {
      const { result, params } = setup();

      await act(async () => {
        await result.current.handleChangePermissions();
      });

      expect(statMock).not.toHaveBeenCalled();
      expect(params.setPermissionsDialog).not.toHaveBeenCalled();
    });

    it("applies permissions to every path in the dialog", async () => {
      const { result, params } = setup({
        permissionsDialog: {
          isDirectory: false,
          mode: "0644",
          pathLabel: "2 items",
          paths: ["/srv/projects/a", "/srv/projects/b"],
          selectionCount: 2,
        },
      });

      await act(async () => {
        await result.current.handleConfirmPermissions(
          "0600",
          true,
          "me",
          "grp",
        );
      });

      expect(params.changePermissions).toHaveBeenCalledTimes(2);
      expect(params.changePermissions).toHaveBeenCalledWith({
        group: "grp",
        mode: "0600",
        owner: "me",
        path: "/srv/projects/a",
        recursive: true,
      });
      expect(params.setPermissionsDialog).toHaveBeenCalledWith(null);
    });

    it("ignores a permissions confirmation with no open dialog", async () => {
      const { result, params } = setup({ permissionsDialog: null });

      await act(async () => {
        await result.current.handleConfirmPermissions("0600", false);
      });

      expect(params.changePermissions).not.toHaveBeenCalled();
    });
  });
});
