import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useFileBrowserItemActions } from "@/hooks/filebrowser/useFileBrowserItemActions";
import type { DialogsSlice } from "@/hooks/filebrowser/useFileDialogs";
import type { EditorSlice } from "@/hooks/filebrowser/useFileEditor";
import type { ViewSlice } from "@/hooks/filebrowser/useFileViewState";
import { act, createTestQueryClient, renderHook } from "@/test/render";
import type { FileItem } from "@/types/filebrowser";

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
    call: vi.fn((route: string, request: { path: string }) => {
      if (route === actual.linuxio.filebrowser.resource_stat.route) {
        return statMock(request.path);
      }
      return actual.call(route as never, request as never);
    }),
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
      },
    },
  };
});

type Params = Parameters<typeof useFileBrowserItemActions>[0];

function fileItem(name: string, type = "file"): FileItem {
  return {
    canOpenAsText: type === "file",
    isRegularFile: type === "file",
    name,
    path: `/srv/projects/${name}`,
    type,
  };
}

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={createTestQueryClient()}>
      {children}
    </QueryClientProvider>
  );
}

function dialogsSlice(overrides: Partial<DialogsSlice> = {}): DialogsSlice {
  return {
    actions: {
      clearPendingDelete: vi.fn(),
      clearPermissions: vi.fn(),
      closeCreateFile: vi.fn(),
      closeCreateFolder: vi.fn(),
      closeDelete: vi.fn(),
      closeDetails: vi.fn(),
      closePermissions: vi.fn(),
      openCreateFile: vi.fn(),
      openCreateFolder: vi.fn(),
      openPermissions: vi.fn(),
      requestDelete: vi.fn(),
      showDetails: vi.fn(),
    },
    createFileDialog: false,
    createFolderDialog: false,
    deleteDialog: false,
    detailTarget: null,
    pendingDeletePaths: [],
    permissionsDialog: null,
    permissionsDialogOpen: false,
    ...overrides,
  };
}

function viewSlice(): ViewSlice {
  return {
    actions: {
      changeSort: vi.fn(),
      clearSearch: vi.fn(),
      closeContextMenu: vi.fn(),
      openContextMenu: vi.fn(),
      setSearchCaseSensitive: vi.fn(),
      setSearch: vi.fn(),
      switchView: vi.fn(),
      toggleHiddenFiles: vi.fn(),
    },
    contextMenuPosition: null,
    searchCaseSensitive: false,
    searchQuery: "",
    showHiddenFiles: false,
    sortField: "name",
    sortOrder: "asc",
    viewMode: "card",
  };
}

function editorSlice(): EditorSlice {
  return {
    actions: {
      close: vi.fn(),
      dismissClosePrompt: vi.fn(),
      openFile: vi.fn(),
      promptClose: vi.fn(),
      setDirty: vi.fn(),
      setSaving: vi.fn(),
    },
    closeEditorDialog: false,
    editingPath: null,
    editorRef: { current: null },
    isEditorDirty: false,
    isSavingFile: false,
    showQuickSave: false,
  };
}

function setup(overrides: Partial<Params> = {}) {
  const successfulBatchResult = {
    failed: [],
    succeeded: 1,
    total: 1,
  };
  const params: Params = {
    changePermissions: vi.fn().mockResolvedValue(successfulBatchResult),
    createFile: vi.fn().mockResolvedValue(undefined),
    createFolder: vi.fn().mockResolvedValue(undefined),
    deleteItems: vi.fn().mockResolvedValue(successfulBatchResult),
    dialogs: dialogsSlice(),
    editor: editorSlice(),
    handleOpenDirectory: vi.fn(),
    renameItem: vi.fn().mockResolvedValue(undefined),
    resource: {
      items: [fileItem("readme.md")],
      name: "projects",
      path: "/srv/projects",
      type: "directory",
    },
    selectedItems: [],
    selectedPaths: new Set<string>(),
    startDownload: vi.fn().mockResolvedValue(undefined),
    view: viewSlice(),
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
    it("opens only files the backend marks as eligible", () => {
      const { result, params } = setup();

      act(() => result.current.handleDoubleClickFile(fileItem("notes.txt")));
      expect(params.editor.actions.openFile).toHaveBeenCalledWith(
        "/srv/projects/notes.txt",
      );

      act(() =>
        result.current.handleDoubleClickFile({
          ...fileItem("photo.png"),
          canOpenAsText: false,
        }),
      );
      expect(params.editor.actions.openFile).toHaveBeenCalledTimes(1);
    });

    it("edits a file from the detail view and clears the detail target", () => {
      const { result, params } = setup();

      act(() => result.current.handleEditFile("/srv/projects/readme.md"));

      expect(params.editor.actions.openFile).toHaveBeenCalledWith(
        "/srv/projects/readme.md",
      );
      expect(params.dialogs.actions.closeDetails).toHaveBeenCalledTimes(1);
    });

    it("opens a search-result detail outside the current directory", () => {
      const { result, params } = setup();

      act(() => result.current.handleEditFile("/var/log/messages"));

      expect(params.editor.actions.openFile).toHaveBeenCalledWith(
        "/var/log/messages",
      );
      expect(params.dialogs.actions.closeDetails).toHaveBeenCalledTimes(1);
    });
  });

  describe("details and downloads", () => {
    it("exposes detail availability and targets the current selection", () => {
      const selectedItems = [fileItem("a"), fileItem("b")];
      const { result, params } = setup({
        selectedItems,
        selectedPaths: new Set(["/srv/projects/a", "/srv/projects/b"]),
      });

      expect(result.current.canShowDetails).toBe(true);

      act(() => result.current.handleShowDetails());

      expect(params.view.actions.closeContextMenu).toHaveBeenCalledTimes(1);
      expect(params.dialogs.actions.showDetails).toHaveBeenCalledWith(
        ["/srv/projects/a", "/srv/projects/b"],
        selectedItems,
      );
    });

    it("skips showing details when nothing is selected", () => {
      const { result, params } = setup();

      act(() => result.current.handleShowDetails());

      expect(params.dialogs.actions.showDetails).not.toHaveBeenCalled();
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

      expect(params.view.actions.clearSearch).toHaveBeenCalledTimes(1);
      expect(params.handleOpenDirectory).toHaveBeenCalledWith("/srv/projects");
    });
  });

  describe("create dialogs", () => {
    it("opens and confirms the create-file flow", async () => {
      const { result, params } = setup();

      act(() => result.current.handleCreateFile());
      expect(params.view.actions.closeContextMenu).toHaveBeenCalledTimes(1);
      expect(params.dialogs.actions.openCreateFile).toHaveBeenCalledTimes(1);

      act(() => result.current.handleCloseCreateFileDialog());
      expect(params.dialogs.actions.closeCreateFile).toHaveBeenCalledTimes(1);

      await act(async () => result.current.handleConfirmCreateFile("new.txt"));
      expect(params.createFile).toHaveBeenCalledWith("new.txt");
    });

    it("opens and confirms the create-folder flow", async () => {
      const { result, params } = setup();

      act(() => result.current.handleCreateFolder());
      expect(params.dialogs.actions.openCreateFolder).toHaveBeenCalledTimes(1);

      await act(async () => result.current.handleConfirmCreateFolder("assets"));
      expect(params.createFolder).toHaveBeenCalledWith("assets");
    });
  });

  describe("delete flow", () => {
    it("stages selected paths for deletion", () => {
      const { result, params } = setup({
        selectedPaths: new Set(["/srv/projects/a"]),
      });

      act(() => result.current.handleDelete());

      expect(params.dialogs.actions.requestDelete).toHaveBeenCalledWith([
        "/srv/projects/a",
      ]);
    });

    it("warns when deleting with nothing selected", () => {
      const { result, params } = setup();

      act(() => result.current.handleDelete());

      expect(toastMocks.error).toHaveBeenCalledWith(
        "No items selected",
        expect.anything(),
      );
      expect(params.dialogs.actions.requestDelete).not.toHaveBeenCalled();
    });

    it("closes the delete dialog only after a successful batch settles", async () => {
      const { result, params } = setup({
        dialogs: dialogsSlice({ pendingDeletePaths: ["/srv/projects/a"] }),
      });

      await act(async () => result.current.handleConfirmDelete());

      expect(params.deleteItems).toHaveBeenCalledWith(["/srv/projects/a"]);
      expect(params.dialogs.actions.closeDelete).toHaveBeenCalledTimes(1);
    });

    it("retains the delete dialog after failure or a partial result", async () => {
      const failedResult = {
        failed: [{ error: "denied", path: "/srv/projects/a" }],
        succeeded: 0,
        total: 1,
      };
      const failed = setup({
        deleteItems: vi.fn().mockRejectedValue(new Error("delete failed")),
        dialogs: dialogsSlice({ pendingDeletePaths: ["/srv/projects/a"] }),
      });
      const partial = setup({
        deleteItems: vi.fn().mockResolvedValue(failedResult),
        dialogs: dialogsSlice({ pendingDeletePaths: ["/srv/projects/a"] }),
      });

      await act(async () => failed.result.current.handleConfirmDelete());
      await act(async () => partial.result.current.handleConfirmDelete());

      expect(failed.params.dialogs.actions.closeDelete).not.toHaveBeenCalled();
      expect(partial.params.dialogs.actions.closeDelete).not.toHaveBeenCalled();
    });

    it("ignores confirmation when there are no pending paths", async () => {
      const { result, params } = setup({
        dialogs: dialogsSlice({ pendingDeletePaths: [] }),
      });

      await act(async () => result.current.handleConfirmDelete());

      expect(params.deleteItems).not.toHaveBeenCalled();
    });

    it("clears pending paths when the delete dialog closes", () => {
      const { result, params } = setup();

      act(() => result.current.handleCloseDeleteDialog());

      expect(params.dialogs.actions.closeDelete).toHaveBeenCalledTimes(1);
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
        },
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

    it("keeps the rename handler stable while reading refreshed items", async () => {
      const { result, params, rerender } = setup({
        resource: {
          items: [fileItem("old", "file")],
          name: "projects",
          path: "/srv/projects",
          type: "directory",
        },
      });
      const handleConfirmInlineRename =
        result.current.handleConfirmInlineRename;

      params.resource = {
        items: [fileItem("old", "directory")],
        name: "projects",
        path: "/srv/projects",
        type: "directory",
      };
      rerender();

      expect(result.current.handleConfirmInlineRename).toBe(
        handleConfirmInlineRename,
      );

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

    it("retains the inline editor when rename fails", async () => {
      const { result } = setup({
        renameItem: vi.fn().mockRejectedValue(new Error("rename failed")),
        selectedPaths: new Set(["/srv/projects/old.txt"]),
      });

      act(() => result.current.handleStartInlineRename());
      await act(async () =>
        result.current.handleConfirmInlineRename(
          "/srv/projects/old.txt",
          "new.txt",
        ),
      );

      expect(result.current.renamingPath).toBe("/srv/projects/old.txt");
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
      expect(params.dialogs.actions.openPermissions).toHaveBeenCalledWith(
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
      expect(params.dialogs.actions.openPermissions).not.toHaveBeenCalled();
    });

    it("does not fetch when nothing is selected", async () => {
      const { result, params } = setup();

      await act(async () => {
        await result.current.handleChangePermissions();
      });

      expect(statMock).not.toHaveBeenCalled();
      expect(params.dialogs.actions.openPermissions).not.toHaveBeenCalled();
    });

    it("applies permissions to the whole selection in one call", async () => {
      const { result, params } = setup({
        dialogs: dialogsSlice({
          permissionsDialog: {
            isDirectory: false,
            mode: "0644",
            pathLabel: "2 items",
            paths: ["/srv/projects/a", "/srv/projects/b"],
            selectionCount: 2,
          },
        }),
      });

      await act(async () => {
        await result.current.handleConfirmPermissions(
          "0600",
          true,
          "me",
          "grp",
        );
      });

      expect(params.changePermissions).toHaveBeenCalledTimes(1);
      expect(params.changePermissions).toHaveBeenCalledWith({
        group: "grp",
        mode: "0600",
        owner: "me",
        paths: ["/srv/projects/a", "/srv/projects/b"],
        recursive: true,
      });
      expect(params.dialogs.actions.closePermissions).toHaveBeenCalledTimes(1);
    });

    it("retains permissions after failure or a partial result", async () => {
      const permissionsDialog = {
        isDirectory: false,
        mode: "0644",
        pathLabel: "one item",
        paths: ["/srv/projects/a"],
        selectionCount: 1,
      };
      const failed = setup({
        changePermissions: vi
          .fn()
          .mockRejectedValue(new Error("permissions failed")),
        dialogs: dialogsSlice({ permissionsDialog }),
      });
      const partial = setup({
        changePermissions: vi.fn().mockResolvedValue({
          failed: [{ error: "denied", path: "/srv/projects/a" }],
          succeeded: 0,
          total: 1,
        }),
        dialogs: dialogsSlice({ permissionsDialog }),
      });

      await act(async () =>
        failed.result.current.handleConfirmPermissions("0600", false),
      );
      await act(async () =>
        partial.result.current.handleConfirmPermissions("0600", false),
      );

      expect(
        failed.params.dialogs.actions.closePermissions,
      ).not.toHaveBeenCalled();
      expect(
        partial.params.dialogs.actions.closePermissions,
      ).not.toHaveBeenCalled();
    });

    it("ignores a permissions confirmation with no open dialog", async () => {
      const { result, params } = setup({
        dialogs: dialogsSlice({ permissionsDialog: null }),
      });

      await act(async () => {
        await result.current.handleConfirmPermissions("0600", false);
      });

      expect(params.changePermissions).not.toHaveBeenCalled();
    });
  });
});
