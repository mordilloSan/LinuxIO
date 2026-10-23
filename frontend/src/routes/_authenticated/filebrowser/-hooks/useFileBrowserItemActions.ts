import { useCallback, useState } from "react";

import {
  call,
  type FileBatchResult,
  type FileChmodBatchRequest,
  linuxio,
} from "@/api";
import { useLatestRef } from "@/hooks/useLatestRef";
import { useScopedToast } from "@/hooks/useScopedToast";
import type { BackgroundTaskActions } from "@/tasks/types";
import { ensureTrailingSlash, isDirectoryPath } from "@/utils/path";

import type { DeleteMode, DialogsSlice } from "./useFileDialogs";
import type { EditorSlice } from "./useFileEditor";
import { useFilePathUtilities } from "./useFilePathUtilities";
import type { ViewSlice } from "./useFileViewState";
import { isImageFile, validateFileName } from "../-fileUtils";
import type { FileItem, FileResource } from "../-types";

const NEW_FOLDER_NAME = "New folder";

type ChangePermissionsPayload = Pick<
  FileChmodBatchRequest,
  "mode" | "paths" | "recursive"
> &
  Partial<Pick<FileChmodBatchRequest, "group" | "owner">>;

interface RenamePayload {
  destination: string;
  from: string;
}

interface UseFileBrowserItemActionsParams {
  changePermissions: (
    payload: ChangePermissionsPayload,
  ) => Promise<FileBatchResult>;
  permissionsPending?: boolean;
  createFile: (fileName: string) => Promise<void>;
  createFolder: (folderName: string) => Promise<void>;
  deleteItems: (paths: string[]) => Promise<FileBatchResult | undefined>;
  deletePending?: boolean;
  trashItems: (paths: string[]) => Promise<FileBatchResult | undefined>;
  trashPending?: boolean;
  dialogs: DialogsSlice;
  editor: EditorSlice;
  handleOpenDirectory: (path: string) => void;
  renameItem: (payload: RenamePayload) => Promise<void>;
  renamePending?: boolean;
  resource?: FileResource;
  selectedItems: FileItem[];
  selectedPaths: Set<string>;
  startDownload: BackgroundTaskActions["startDownload"];
  view: ViewSlice;
}

export const useFileBrowserItemActions = ({
  changePermissions,
  permissionsPending = false,
  createFile,
  createFolder,
  deleteItems,
  deletePending = false,
  trashItems,
  trashPending = false,
  dialogs,
  editor,
  handleOpenDirectory,
  renameItem,
  renamePending = false,
  resource,
  selectedItems,
  selectedPaths,
  startDownload,
  view,
}: UseFileBrowserItemActionsParams) => {
  const toast = useScopedToast({
    label: "Open files",
    params: { _splat: "" },
    to: "/filebrowser/$",
  });
  const {
    actions: dialogActions,
    deleteMode,
    pendingDeletePaths,
    permissionsDialog,
  } = dialogs;
  const { actions: editorActions } = editor;
  const { clearSearch, closeContextMenu } = view.actions;
  const { joinPath, getParentPath } = useFilePathUtilities();
  const resourceItemsRef = useLatestRef(resource?.items);
  const [renamingPath, setRenamingPath] = useState<string | null>(null);

  const downloadPaths = useCallback(
    async (paths: string[]) => {
      await startDownload(paths);
    },
    [startDownload],
  );

  const handleCloseDetailDialog = useCallback(() => {
    dialogActions.closeDetails();
  }, [dialogActions]);

  const handleDoubleClickFile = useCallback(
    (item: FileItem) => {
      if (
        item.isRegularFile === true &&
        (item.canOpenAsText === true || isImageFile(item.name))
      ) {
        editorActions.openFile(item.path);
      }
    },
    [editorActions],
  );

  const handleDownloadCurrent = useCallback(
    (path: string) => {
      void downloadPaths([path]);
    },
    [downloadPaths],
  );

  const handleShowDetails = useCallback(() => {
    closeContextMenu();
    if (selectedPaths.size === 0) return;
    dialogActions.showDetails(Array.from(selectedPaths), selectedItems);
  }, [dialogActions, closeContextMenu, selectedItems, selectedPaths]);

  const handleDownloadDetail = useCallback(
    (path: string) => {
      void downloadPaths([path]);
    },
    [downloadPaths],
  );

  const handleCreateFile = useCallback(() => {
    closeContextMenu();
    dialogActions.openCreateFile();
  }, [dialogActions, closeContextMenu]);

  // Explorer-style: create "New folder" right away and rename it in place.
  const handleCreateFolder = useCallback(async () => {
    closeContextMenu();
    if (!resource) return;
    // The new row only shows in the unfiltered listing.
    clearSearch();
    const taken = new Set(resource.items?.map((item) => item.name));
    let name = NEW_FOLDER_NAME;
    for (let n = 2; taken.has(name); n++) {
      name = `${NEW_FOLDER_NAME} (${n})`;
    }
    try {
      await createFolder(name);
      setRenamingPath(`${joinPath(resource.path, name)}/`);
    } catch {
      // The mutation reports the error.
    }
  }, [clearSearch, closeContextMenu, createFolder, joinPath, resource]);

  const handleCloseCreateFileDialog = useCallback(() => {
    dialogActions.closeCreateFile();
  }, [dialogActions]);

  const handleConfirmCreateFile = useCallback(
    (fileName: string) => createFile(fileName),
    [createFile],
  );

  const handleChangePermissions = useCallback(async () => {
    closeContextMenu();
    if (selectedPaths.size === 0) return;
    const selectedPathList = Array.from(selectedPaths);
    const selectedPath = selectedPathList[0];
    const selectionCount = selectedPathList.length;
    const hasDirectorySelected = selectedItems.some(
      (item) => item.type === "directory",
    );
    try {
      const stat = await call(linuxio.filebrowser.resource_stat.route, {
        path: selectedPath,
      });
      const mode = stat.mode || "0644";
      const isDirectory = stat.mode?.startsWith("d") || hasDirectorySelected;
      const owner = stat.owner || undefined;
      const group = stat.group || undefined;
      dialogActions.openPermissions({
        paths: selectedPathList,
        pathLabel:
          selectionCount > 1 ? `${selectionCount} items` : selectedPath,
        selectionCount,
        mode,
        isDirectory,
        owner,
        group,
      });
    } catch (error) {
      console.error("Failed to fetch file stat:", error);
      toast.error("Failed to fetch file permissions");
    }
  }, [dialogActions, closeContextMenu, selectedItems, selectedPaths, toast]);

  const handleStartInlineRename = useCallback(() => {
    closeContextMenu();
    if (selectedPaths.size !== 1) {
      return;
    }
    const selectedPath = Array.from(selectedPaths)[0];
    setRenamingPath(selectedPath);
  }, [closeContextMenu, selectedPaths]);

  const handleConfirmInlineRename = useCallback(
    async (path: string, newName: string) => {
      if (renamePending) return;
      const trimmed = newName.trim();
      if (!trimmed) {
        setRenamingPath(null);
        return;
      }
      const invalid = validateFileName(newName);
      if (invalid) {
        // Keep the inline editor mounted so the user can correct the name.
        toast.error(invalid);
        return;
      }
      const target = resourceItemsRef.current?.find(
        (item) => item.path === path,
      );
      const isDirectory = target?.type === "directory" || isDirectoryPath(path);
      const parent = getParentPath(path);
      let destination = joinPath(parent, trimmed);
      if (isDirectory) {
        destination = ensureTrailingSlash(destination);
      }
      try {
        await renameItem({
          from: path,
          destination,
        });
        setRenamingPath(null);
      } catch {
        // Keep the inline editor mounted so the user can correct and retry.
      }
    },
    [
      getParentPath,
      joinPath,
      renameItem,
      renamePending,
      resourceItemsRef,
      toast,
    ],
  );

  const handleCancelInlineRename = useCallback(() => {
    if (renamePending) return;
    setRenamingPath(null);
  }, [renamePending]);

  const handleContextMenuRename = useCallback(() => {
    handleStartInlineRename();
  }, [handleStartInlineRename]);

  const requestDelete = useCallback(
    (mode: DeleteMode) => {
      closeContextMenu();
      const paths = Array.from(selectedPaths);
      if (paths.length > 0) {
        dialogActions.requestDelete(paths, mode);
      } else {
        toast.error("No items selected");
      }
    },
    [dialogActions, closeContextMenu, selectedPaths, toast],
  );

  const handleDelete = useCallback(
    () => requestDelete("trash"),
    [requestDelete],
  );
  const handleDeletePermanently = useCallback(
    () => requestDelete("permanent"),
    [requestDelete],
  );

  const handleConfirmDelete = useCallback(async () => {
    if (!pendingDeletePaths.length) {
      return;
    }
    if (deleteMode === "trash" ? trashPending : deletePending) return;
    try {
      const result = await (deleteMode === "trash" ? trashItems : deleteItems)(
        pendingDeletePaths,
      );
      // The mutation refreshes the listing and names per-item failures in a
      // toast, so the dialog is done once a result came back.
      if (result) {
        dialogActions.closeDelete();
      }
    } catch {
      // The mutation owns error feedback; retain the dialog for retry.
    }
  }, [
    deleteItems,
    deleteMode,
    deletePending,
    dialogActions,
    pendingDeletePaths,
    trashItems,
    trashPending,
  ]);

  const handleCloseDeleteDialog = useCallback(() => {
    dialogActions.closeDelete();
  }, [dialogActions]);

  const handleDownloadSelected = useCallback(() => {
    closeContextMenu();
    const paths = Array.from(selectedPaths);
    if (paths.length === 0) return;
    void downloadPaths(paths);
  }, [downloadPaths, closeContextMenu, selectedPaths]);

  const handleOpenContainingFolder = useCallback(() => {
    closeContextMenu();
    const [selectedPath] = Array.from(selectedPaths);
    if (!selectedPath) return;
    const parentDir =
      selectedPath.substring(0, selectedPath.lastIndexOf("/")) || "/";
    clearSearch();
    handleOpenDirectory(parentDir);
  }, [clearSearch, handleOpenDirectory, closeContextMenu, selectedPaths]);

  const handleClosePermissionsDialog = useCallback(() => {
    dialogActions.closePermissions();
  }, [dialogActions]);

  const handleConfirmPermissions = useCallback(
    async (
      mode: string,
      recursive: boolean,
      owner?: string,
      group?: string,
    ) => {
      if (!permissionsDialog) return;
      if (permissionsPending) return;
      try {
        const result = await changePermissions({
          paths: permissionsDialog.paths,
          mode,
          recursive,
          owner,
          group,
        });
        if (result.failed.length === 0) {
          dialogActions.closePermissions();
        }
      } catch {
        // Errors are surfaced via toast in the mutation.
      }
    },
    [changePermissions, dialogActions, permissionsDialog, permissionsPending],
  );

  const handleEditFile = useCallback(
    (filePath: string) => {
      editorActions.openFile(filePath);
      dialogActions.closeDetails();
    },
    [dialogActions, editorActions],
  );

  return {
    canShowDetails: selectedPaths.size > 0,
    handleCancelInlineRename,
    handleChangePermissions,
    handleCloseCreateFileDialog,
    handleCloseDeleteDialog,
    handleCloseDetailDialog,
    handleClosePermissionsDialog,
    handleConfirmCreateFile,
    handleConfirmDelete,
    handleConfirmInlineRename,
    handleConfirmPermissions,
    handleContextMenuRename,
    handleCreateFile,
    handleCreateFolder,
    handleDelete,
    handleDeletePermanently,
    handleDoubleClickFile,
    handleDownloadCurrent,
    handleDownloadDetail,
    handleDownloadSelected,
    handleEditFile,
    handleOpenContainingFolder,
    handleShowDetails,
    handleStartInlineRename,
    renamingPath,
  };
};
