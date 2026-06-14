import { useQueryClient } from "@tanstack/react-query";
import {
  useCallback,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";

import { CACHE_TTL_MS, linuxio } from "@/api";
import { isEditableFile } from "@/components/filebrowser/utils";
import type { BackgroundJobsContextValue } from "@/types/backgroundJobs";
import type { FileItem, FileResource } from "@/types/filebrowser";

import type { PermissionsDialogState } from "./useFileDialogs";
import { useFilePathUtilities } from "./useFilePathUtilities";
import { useScopedToast } from "../useScopedToast";

interface ChangePermissionsPayload {
  group?: string;
  mode: string;
  owner?: string;
  path: string;
  recursive?: boolean;
}

interface RenamePayload {
  destination: string;
  from: string;
}

interface UseFileBrowserItemActionsParams {
  changePermissions: (payload: ChangePermissionsPayload) => Promise<void>;
  createFile: (fileName: string) => void;
  createFolder: (folderName: string) => void;
  deleteItems: (paths: string[]) => void;
  handleOpenDirectory: (path: string) => void;
  onContextMenuClose: () => void;
  pendingDeletePaths: string[];
  permissionsDialog: PermissionsDialogState | null;
  renameItem: (payload: RenamePayload) => Promise<void>;
  resource?: FileResource;
  selectedItems: FileItem[];
  selectedPaths: Set<string>;
  setCreateFileDialog: Dispatch<SetStateAction<boolean>>;
  setCreateFolderDialog: Dispatch<SetStateAction<boolean>>;
  setDeleteDialog: Dispatch<SetStateAction<boolean>>;
  setDetailTarget: Dispatch<SetStateAction<string[] | null>>;
  setEditingPath: Dispatch<SetStateAction<string | null>>;
  setPendingDeletePaths: Dispatch<SetStateAction<string[]>>;
  setPermissionsDialog: Dispatch<SetStateAction<PermissionsDialogState | null>>;
  setSearchQuery: Dispatch<SetStateAction<string>>;
  startDownload: BackgroundJobsContextValue["startDownload"];
}

export const useFileBrowserItemActions = ({
  changePermissions,
  createFile,
  createFolder,
  deleteItems,
  handleOpenDirectory,
  onContextMenuClose,
  pendingDeletePaths,
  permissionsDialog,
  renameItem,
  resource,
  selectedItems,
  selectedPaths,
  setCreateFileDialog,
  setCreateFolderDialog,
  setDeleteDialog,
  setDetailTarget,
  setEditingPath,
  setPendingDeletePaths,
  setPermissionsDialog,
  setSearchQuery,
  startDownload,
}: UseFileBrowserItemActionsParams) => {
  const toast = useScopedToast({ href: "/filebrowser", label: "Open files" });
  const queryClient = useQueryClient();
  const { joinPath, getParentPath } = useFilePathUtilities();
  const [renamingPath, setRenamingPath] = useState<string | null>(null);
  const [unsupportedEditPath, setUnsupportedEditPath] = useState<string | null>(
    null,
  );

  const downloadPaths = useCallback(
    async (paths: string[]) => {
      await startDownload(paths);
    },
    [startDownload],
  );

  const handleCloseDetailDialog = useCallback(() => {
    setDetailTarget(null);
  }, [setDetailTarget]);

  const handleCloseUnsupportedEditDialog = useCallback(() => {
    setUnsupportedEditPath(null);
  }, []);

  const handleConfirmUnsupportedEdit = useCallback(() => {
    if (unsupportedEditPath) {
      setEditingPath(unsupportedEditPath);
    }
    setUnsupportedEditPath(null);
    setDetailTarget(null);
  }, [setDetailTarget, setEditingPath, unsupportedEditPath]);

  const handleDoubleClickFile = useCallback(
    (item: FileItem) => {
      if (isEditableFile(item.name)) {
        setEditingPath(item.path);
      } else {
        setUnsupportedEditPath(item.path);
      }
    },
    [setEditingPath],
  );

  const handleDownloadCurrent = useCallback(
    (path: string) => {
      downloadPaths([path]);
    },
    [downloadPaths],
  );

  const handleShowDetails = useCallback(() => {
    onContextMenuClose();
    if (selectedPaths.size === 0) return;
    setDetailTarget(Array.from(selectedPaths));
  }, [onContextMenuClose, selectedPaths, setDetailTarget]);

  const handleDownloadDetail = useCallback(
    (path: string) => {
      downloadPaths([path]);
    },
    [downloadPaths],
  );

  const handleCreateFile = useCallback(() => {
    onContextMenuClose();
    setCreateFileDialog(true);
  }, [onContextMenuClose, setCreateFileDialog]);

  const handleCreateFolder = useCallback(() => {
    onContextMenuClose();
    setCreateFolderDialog(true);
  }, [onContextMenuClose, setCreateFolderDialog]);

  const handleCloseCreateFileDialog = useCallback(() => {
    setCreateFileDialog(false);
  }, [setCreateFileDialog]);

  const handleCloseCreateFolderDialog = useCallback(() => {
    setCreateFolderDialog(false);
  }, [setCreateFolderDialog]);

  const handleConfirmCreateFile = useCallback(
    (fileName: string) => {
      createFile(fileName);
    },
    [createFile],
  );

  const handleConfirmCreateFolder = useCallback(
    (folderName: string) => {
      createFolder(folderName);
    },
    [createFolder],
  );

  const handleChangePermissions = useCallback(async () => {
    onContextMenuClose();
    if (selectedPaths.size === 0) return;
    const selectedPathList = Array.from(selectedPaths);
    const selectedPath = selectedPathList[0];
    const selectionCount = selectedPathList.length;
    const hasDirectorySelected = selectedItems.some(
      (item) => item.type === "directory",
    );
    try {
      const stat = await queryClient.fetchQuery(
        linuxio.filebrowser.resource_stat.queryOptions(selectedPath, {
          staleTime: CACHE_TTL_MS.FIVE_SECONDS,
        }),
      );
      const mode = stat.mode || "0644";
      const isDirectory = stat.mode?.startsWith("d") || hasDirectorySelected;
      const owner = stat.owner || undefined;
      const group = stat.group || undefined;
      setPermissionsDialog({
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
  }, [
    onContextMenuClose,
    queryClient,
    selectedItems,
    selectedPaths,
    setPermissionsDialog,
    toast,
  ]);

  const handleStartInlineRename = useCallback(() => {
    onContextMenuClose();
    if (selectedPaths.size !== 1) {
      return;
    }
    const selectedPath = Array.from(selectedPaths)[0];
    setRenamingPath(selectedPath);
  }, [onContextMenuClose, selectedPaths]);

  const handleConfirmInlineRename = useCallback(
    async (path: string, newName: string) => {
      const trimmed = newName.trim();
      if (!trimmed) {
        setRenamingPath(null);
        return;
      }
      const target = resource?.items?.find((item) => item.path === path);
      const isDirectory = target?.type === "directory" || path.endsWith("/");
      const parent = getParentPath(path);
      let destination = joinPath(parent, trimmed);
      if (isDirectory && !destination.endsWith("/")) {
        destination += "/";
      }
      try {
        await renameItem({
          from: path,
          destination,
        });
        setRenamingPath(null);
      } catch {
        setRenamingPath(null);
      }
    },
    [getParentPath, joinPath, renameItem, resource?.items],
  );

  const handleCancelInlineRename = useCallback(() => {
    setRenamingPath(null);
  }, []);

  const handleContextMenuRename = useCallback(() => {
    handleStartInlineRename();
  }, [handleStartInlineRename]);

  const handleDelete = useCallback(() => {
    onContextMenuClose();
    const paths = Array.from(selectedPaths);
    if (paths.length > 0) {
      setPendingDeletePaths(paths);
      setDeleteDialog(true);
    } else {
      toast.error("No items selected");
    }
  }, [
    onContextMenuClose,
    selectedPaths,
    setDeleteDialog,
    setPendingDeletePaths,
    toast,
  ]);

  const handleConfirmDelete = useCallback(() => {
    if (!pendingDeletePaths.length) {
      return;
    }
    deleteItems(pendingDeletePaths);
    setPendingDeletePaths([]);
  }, [deleteItems, pendingDeletePaths, setPendingDeletePaths]);

  const handleCloseDeleteDialog = useCallback(() => {
    setDeleteDialog(false);
    setPendingDeletePaths([]);
  }, [setDeleteDialog, setPendingDeletePaths]);

  const handleDownloadSelected = useCallback(() => {
    onContextMenuClose();
    const paths = Array.from(selectedPaths);
    if (paths.length === 0) return;
    downloadPaths(paths);
  }, [downloadPaths, onContextMenuClose, selectedPaths]);

  const handleOpenContainingFolder = useCallback(() => {
    onContextMenuClose();
    const [selectedPath] = Array.from(selectedPaths);
    if (!selectedPath) return;
    const parentDir =
      selectedPath.substring(0, selectedPath.lastIndexOf("/")) || "/";
    setSearchQuery("");
    handleOpenDirectory(parentDir);
  }, [handleOpenDirectory, onContextMenuClose, selectedPaths, setSearchQuery]);

  const handleClosePermissionsDialog = useCallback(() => {
    setPermissionsDialog(null);
  }, [setPermissionsDialog]);

  const handleConfirmPermissions = useCallback(
    async (
      mode: string,
      recursive: boolean,
      owner?: string,
      group?: string,
    ) => {
      if (!permissionsDialog) return;
      try {
        await Promise.all(
          permissionsDialog.paths.map((path) =>
            changePermissions({
              path,
              mode,
              recursive,
              owner,
              group,
            }),
          ),
        );
        setPermissionsDialog(null);
      } catch {
        // Errors are surfaced via toast in the mutation.
      }
    },
    [changePermissions, permissionsDialog, setPermissionsDialog],
  );

  const handleEditFile = useCallback(
    (filePath: string) => {
      const fileName = filePath.split("/").pop() ?? filePath;
      if (isEditableFile(fileName)) {
        setEditingPath(filePath);
        setDetailTarget(null);
      } else {
        setUnsupportedEditPath(filePath);
      }
    },
    [setDetailTarget, setEditingPath],
  );

  return {
    canShowDetails: selectedPaths.size > 0,
    handleCancelInlineRename,
    handleChangePermissions,
    handleCloseCreateFileDialog,
    handleCloseCreateFolderDialog,
    handleCloseDeleteDialog,
    handleCloseDetailDialog,
    handleClosePermissionsDialog,
    handleCloseUnsupportedEditDialog,
    handleConfirmCreateFile,
    handleConfirmCreateFolder,
    handleConfirmDelete,
    handleConfirmInlineRename,
    handleConfirmPermissions,
    handleConfirmUnsupportedEdit,
    handleContextMenuRename,
    handleCreateFile,
    handleCreateFolder,
    handleDelete,
    handleDoubleClickFile,
    handleDownloadCurrent,
    handleDownloadDetail,
    handleDownloadSelected,
    handleEditFile,
    handleOpenContainingFolder,
    handleShowDetails,
    handleStartInlineRename,
    renamingPath,
    unsupportedEditPath,
  };
};
