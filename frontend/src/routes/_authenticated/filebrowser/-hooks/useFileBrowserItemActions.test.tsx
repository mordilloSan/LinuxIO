import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { act, createTestQueryClient, renderHook } from "@/test/render";

import type { FileItem } from "../-types";
import { useFileBrowserItemActions } from "./useFileBrowserItemActions";
import { type DialogsSlice, useFileDialogs } from "./useFileDialogs";
import type { EditorSlice } from "./useFileEditor";
import type { ViewSlice } from "./useFileViewState";

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  info: vi.fn(),
  success: vi.fn(),
}));

const statMock = vi.hoisted(() => vi.fn());

vi.mock("sonner", () => ({
  toast: {
    error: toastMocks.error,
    info: toastMocks.info,
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
      closeDelete: vi.fn(),
      closeDetails: vi.fn(),
      closePermissions: vi.fn(),
      deleteForGoodInstead: vi.fn(),
      openCreateFile: vi.fn(),
      openPermissions: vi.fn(),
      requestDelete: vi.fn(),
      showDetails: vi.fn(),
    },
    createFileDialog: false,
    deleteDialog: false,
    deleteMode: "trash",
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
      setCardSize: vi.fn(),
      setSearchCaseSensitive: vi.fn(),
      setSearch: vi.fn(),
      switchView: vi.fn(),
      toggleHiddenFiles: vi.fn(),
    },
    cardSize: 3,
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
      confirmClose: vi.fn(),
      dismissClosePrompt: vi.fn(),
      openFile: vi.fn(),
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

const successfulBatchResult = {
  failed: [],
  succeeded: 1,
  total: 1,
};

function makeParams(overrides: Partial<Params> = {}): Params {
  return {
    changePermissions: vi.fn().mockResolvedValue(successfulBatchResult),
    createFile: vi.fn().mockResolvedValue(undefined),
    createFolder: vi.fn().mockResolvedValue(undefined),
    deleteItems: vi.fn().mockResolvedValue(successfulBatchResult),
    trashItems: vi.fn().mockResolvedValue(successfulBatchResult),
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
}

function setup(overrides: Partial<Params> = {}) {
  const params = makeParams(overrides);
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
    it("opens text files the backend marks as eligible and any image", () => {
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
      expect(params.editor.actions.openFile).toHaveBeenCalledWith(
        "/srv/projects/photo.png",
      );

      act(() =>
        result.current.handleDoubleClickFile({
          ...fileItem("app.bin"),
          canOpenAsText: false,
        }),
      );
      expect(params.editor.actions.openFile).toHaveBeenCalledTimes(2);
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

  describe("create file", () => {
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
  });

  describe("new folder", () => {
    it('creates "New folder" and opens it in inline rename', async () => {
      const { result, params } = setup();

      await act(async () => result.current.handleCreateFolder());

      expect(params.view.actions.closeContextMenu).toHaveBeenCalledTimes(1);
      expect(params.createFolder).toHaveBeenCalledWith("New folder");
      expect(result.current.renamingPath).toBe("/srv/projects/New folder/");
    });

    it("picks the first free numbered name", async () => {
      const { result, params } = setup({
        resource: {
          items: [
            fileItem("New folder", "directory"),
            fileItem("New folder (2)", "directory"),
            fileItem("New folder (4)", "directory"),
          ],
          name: "projects",
          path: "/srv/projects",
          type: "directory",
        },
      });

      await act(async () => result.current.handleCreateFolder());

      expect(params.createFolder).toHaveBeenCalledWith("New folder (3)");
      expect(result.current.renamingPath).toBe("/srv/projects/New folder (3)/");
    });

    it("builds the path under the root without a doubled slash", async () => {
      const { result } = setup({
        resource: { items: [], name: "/", path: "/", type: "directory" },
      });

      await act(async () => result.current.handleCreateFolder());

      expect(result.current.renamingPath).toBe("/New folder/");
    });

    it("does not rename anything when the folder could not be created", async () => {
      const { result } = setup({
        createFolder: vi.fn().mockRejectedValue(new Error("create failed")),
      });

      await act(async () => result.current.handleCreateFolder());

      expect(result.current.renamingPath).toBeNull();
    });
  });

  describe("delete flow", () => {
    it("stages selected paths for the trash or permanent deletion", () => {
      const { result, params } = setup({
        selectedPaths: new Set(["/srv/projects/a"]),
      });

      act(() => result.current.handleDelete());
      act(() => result.current.handleDeletePermanently());

      expect(params.dialogs.actions.requestDelete).toHaveBeenNthCalledWith(
        1,
        ["/srv/projects/a"],
        "trash",
        // What the items take, which the page knows nothing of here.
        undefined,
      );
      expect(params.dialogs.actions.requestDelete).toHaveBeenNthCalledWith(
        2,
        ["/srv/projects/a"],
        "permanent",
        undefined,
      );
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

    it("moves to the trash in trash mode and closes the dialog as it starts", () => {
      const { result, params } = setup({
        dialogs: dialogsSlice({
          deleteDialog: true,
          pendingDeletePaths: ["/srv/projects/a"],
        }),
        // The job runs on; the dialog does not wait for it.
        trashItems: vi.fn(() => new Promise<never>(() => {})),
      });

      act(() => result.current.handleConfirmDelete());

      expect(params.trashItems).toHaveBeenCalledWith(["/srv/projects/a"]);
      expect(params.deleteItems).not.toHaveBeenCalled();
      expect(params.dialogs.actions.closeDelete).toHaveBeenCalledTimes(1);
    });

    it("deletes permanently in permanent mode and closes the dialog as it starts", () => {
      const { result, params } = setup({
        deleteItems: vi.fn(() => new Promise<never>(() => {})),
        dialogs: dialogsSlice({
          deleteDialog: true,
          deleteMode: "permanent",
          pendingDeletePaths: ["/srv/projects/a"],
        }),
      });

      act(() => result.current.handleConfirmDelete());

      expect(params.deleteItems).toHaveBeenCalledWith(["/srv/projects/a"]);
      expect(params.trashItems).not.toHaveBeenCalled();
      expect(params.dialogs.actions.closeDelete).toHaveBeenCalledTimes(1);
    });

    it("starts another delete of either kind while earlier ones run", () => {
      const running = () => new Promise<never>(() => {});
      const trashItems = vi.fn(running);
      const deleteItems = vi.fn(running);
      let selectedPaths = new Set(["/srv/projects/a"]);
      const { result, rerender } = renderHook(
        () => {
          const dialogs = useFileDialogs();
          const actions = useFileBrowserItemActions(
            makeParams({ deleteItems, dialogs, selectedPaths, trashItems }),
          );
          return { actions, dialogs };
        },
        { wrapper },
      );
      const startDelete = (paths: string[], permanent = false) => {
        selectedPaths = new Set(paths);
        rerender();
        act(() =>
          permanent
            ? result.current.actions.handleDeletePermanently()
            : result.current.actions.handleDelete(),
        );
        expect(result.current.dialogs.deleteDialog).toBe(true);
        act(() => result.current.actions.handleConfirmDelete());
        expect(result.current.dialogs.deleteDialog).toBe(false);
      };

      startDelete(["/srv/projects/a"]);
      startDelete(["/srv/projects/b"]);
      startDelete(["/srv/projects/c"], true);

      expect(trashItems.mock.calls).toEqual([
        [["/srv/projects/a"]],
        [["/srv/projects/b"]],
      ]);
      expect(deleteItems.mock.calls).toEqual([[["/srv/projects/c"]]]);
      expect(toastMocks.info).not.toHaveBeenCalled();
    });

    it("does not start the job again from a confirm that is closing", () => {
      const trashItems = vi.fn(() => new Promise<never>(() => {}));
      const { result } = renderHook(
        () => {
          const dialogs = useFileDialogs();
          const actions = useFileBrowserItemActions(
            makeParams({
              dialogs,
              selectedPaths: new Set(["/srv/projects/a"]),
              trashItems,
            }),
          );
          return { actions, dialogs };
        },
        { wrapper },
      );

      act(() => result.current.actions.handleDelete());
      // The confirm fades out with the handler it had when it was clicked.
      const { handleConfirmDelete } = result.current.actions;
      act(() => handleConfirmDelete());
      act(() => handleConfirmDelete());

      expect(trashItems).toHaveBeenCalledTimes(1);
    });

    it("closes the dialog whatever the job's outcome, which the mutation reports", async () => {
      const dialogs = dialogsSlice({
        deleteDialog: true,
        pendingDeletePaths: ["/srv/projects/a"],
      });
      const failed = setup({
        dialogs,
        trashItems: vi.fn().mockRejectedValue(new Error("delete failed")),
      });

      await act(async () => failed.result.current.handleConfirmDelete());

      expect(dialogs.actions.closeDelete).toHaveBeenCalledTimes(1);
    });

    it("ignores confirmation when there are no pending paths", async () => {
      const { result, params } = setup({
        dialogs: dialogsSlice({ deleteDialog: true, pendingDeletePaths: [] }),
      });

      await act(async () => result.current.handleConfirmDelete());

      expect(params.trashItems).not.toHaveBeenCalled();
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

    it("rejects an invalid name with a toast and keeps the editor open", async () => {
      const { result, params } = setup({
        selectedPaths: new Set(["/srv/projects/old.txt"]),
      });

      act(() => result.current.handleStartInlineRename());
      await act(async () =>
        result.current.handleConfirmInlineRename(
          "/srv/projects/old.txt",
          "a/b",
        ),
      );
      await act(async () =>
        result.current.handleConfirmInlineRename("/srv/projects/old.txt", ".."),
      );

      expect(toastMocks.error).toHaveBeenNthCalledWith(
        1,
        "Names cannot contain “/”.",
        expect.anything(),
      );
      expect(toastMocks.error).toHaveBeenNthCalledWith(
        2,
        "That name is reserved.",
        expect.anything(),
      );
      expect(params.renameItem).not.toHaveBeenCalled();
      expect(result.current.renamingPath).toBe("/srv/projects/old.txt");
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

    it("applies permissions to the whole selection in one call and closes as it starts", () => {
      const paths = ["/srv/projects/a", "/srv/projects/b"];
      const { result, params } = setup({
        // The job runs on; the dialog does not wait for it.
        changePermissions: vi.fn(() => new Promise<never>(() => {})),
        dialogs: dialogsSlice({
          permissionsDialog: {
            isDirectory: false,
            mode: "0644",
            pathLabel: "2 items",
            paths,
            selectionCount: 2,
          },
          permissionsDialogOpen: true,
        }),
      });

      act(() =>
        result.current.handleConfirmPermissions("0600", true, "me", "grp"),
      );

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

    it("starts another permission change while an earlier one runs", async () => {
      statMock.mockResolvedValue({ group: "staff", mode: "0644", owner: "me" });
      const changePermissions = vi.fn(
        (_request: { paths: string[] }) => new Promise<never>(() => {}),
      );
      let selectedPaths = new Set(["/srv/projects/a.txt"]);
      const { result, rerender } = renderHook(
        () => {
          const dialogs = useFileDialogs();
          const actions = useFileBrowserItemActions(
            makeParams({
              changePermissions,
              dialogs,
              selectedItems: [],
              selectedPaths,
            }),
          );
          return { actions, dialogs };
        },
        { wrapper },
      );
      const startChange = async (path: string) => {
        selectedPaths = new Set([path]);
        rerender();
        await act(async () => {
          await result.current.actions.handleChangePermissions();
        });
        expect(result.current.dialogs.permissionsDialogOpen).toBe(true);
        act(() =>
          result.current.actions.handleConfirmPermissions("0600", false),
        );
        expect(result.current.dialogs.permissionsDialogOpen).toBe(false);
      };

      await startChange("/srv/projects/a.txt");
      await startChange("/srv/projects/b.txt");

      expect(
        changePermissions.mock.calls.map(([request]) => request.paths),
      ).toEqual([["/srv/projects/a.txt"], ["/srv/projects/b.txt"]]);
      expect(toastMocks.info).not.toHaveBeenCalled();
    });

    it("does not start the change again from a dialog that is closing", () => {
      const changePermissions = vi.fn(() => new Promise<never>(() => {}));
      const { result } = renderHook(
        () => {
          const dialogs = useFileDialogs();
          const actions = useFileBrowserItemActions(
            makeParams({ changePermissions, dialogs }),
          );
          return { actions, dialogs };
        },
        { wrapper },
      );

      act(() =>
        result.current.dialogs.actions.openPermissions({
          isDirectory: false,
          mode: "0644",
          pathLabel: "/srv/projects/a",
          paths: ["/srv/projects/a"],
          selectionCount: 1,
        }),
      );
      // The dialog fades out with the handler it had when Apply was clicked.
      const { handleConfirmPermissions } = result.current.actions;
      act(() => handleConfirmPermissions("0600", false));
      act(() => handleConfirmPermissions("0600", false));

      expect(changePermissions).toHaveBeenCalledTimes(1);
    });

    it("closes the dialog whatever the change's outcome, which the mutation reports", async () => {
      const dialogs = dialogsSlice({
        permissionsDialog: {
          isDirectory: false,
          mode: "0644",
          pathLabel: "one item",
          paths: ["/srv/projects/a"],
          selectionCount: 1,
        },
        permissionsDialogOpen: true,
      });
      const failed = setup({
        changePermissions: vi
          .fn()
          .mockRejectedValue(new Error("permissions failed")),
        dialogs,
      });

      await act(async () =>
        failed.result.current.handleConfirmPermissions("0600", false),
      );

      expect(dialogs.actions.closePermissions).toHaveBeenCalledTimes(1);
    });

    it("ignores a permissions confirmation with no open dialog", () => {
      const { result, params } = setup({
        dialogs: dialogsSlice({ permissionsDialog: null }),
      });

      act(() => result.current.handleConfirmPermissions("0600", false));

      expect(params.changePermissions).not.toHaveBeenCalled();
    });
  });
});
