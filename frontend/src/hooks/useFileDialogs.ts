import { Dispatch, SetStateAction, useState } from "react";

interface PermissionsDialogState {
  paths: string[];
  pathLabel: string;
  selectionCount: number;
  mode: string;
  isDirectory: boolean;
  owner?: string;
  group?: string;
}

interface RenameDialogState {
  path: string;
  name: string;
  isDirectory: boolean;
}

interface useFileDialogsResult {
  // Create dialogs
  createFileDialog: boolean;
  setCreateFileDialog: Dispatch<SetStateAction<boolean>>;
  createFolderDialog: boolean;
  setCreateFolderDialog: Dispatch<SetStateAction<boolean>>;

  // Delete dialog
  deleteDialog: boolean;
  setDeleteDialog: Dispatch<SetStateAction<boolean>>;
  pendingDeletePaths: string[];
  setPendingDeletePaths: Dispatch<SetStateAction<string[]>>;

  // Detail dialog
  detailTarget: string[] | null;
  setDetailTarget: Dispatch<SetStateAction<string[] | null>>;

  // Permissions dialog
  permissionsDialog: PermissionsDialogState | null;
  setPermissionsDialog: Dispatch<SetStateAction<PermissionsDialogState | null>>;

  // Rename dialog
  renameDialog: RenameDialogState | null;
  setRenameDialog: Dispatch<SetStateAction<RenameDialogState | null>>;
}

/**
 * Custom hook for managing all file browser dialog states
 * Consolidates dialog visibility and data management
 */
export const useFileDialogs = (): useFileDialogsResult => {
  const [createFileDialog, setCreateFileDialog] = useState(false);
  const [createFolderDialog, setCreateFolderDialog] = useState(false);
  const [deleteDialog, setDeleteDialog] = useState(false);
  const [pendingDeletePaths, setPendingDeletePaths] = useState<string[]>([]);
  const [detailTarget, setDetailTarget] = useState<string[] | null>(null);
  const [permissionsDialog, setPermissionsDialog] =
    useState<PermissionsDialogState | null>(null);
  const [renameDialog, setRenameDialog] = useState<RenameDialogState | null>(
    null,
  );

  return {
    createFileDialog,
    setCreateFileDialog,
    createFolderDialog,
    setCreateFolderDialog,

    deleteDialog,
    setDeleteDialog,
    pendingDeletePaths,
    setPendingDeletePaths,

    detailTarget,
    setDetailTarget,

    permissionsDialog,
    setPermissionsDialog,

    renameDialog,
    setRenameDialog,
  };
};

export type { PermissionsDialogState, RenameDialogState };
