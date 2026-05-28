import { Dispatch, SetStateAction, useState } from "react";

interface PermissionsDialogState {
  group?: string;
  isDirectory: boolean;
  mode: string;
  owner?: string;
  pathLabel: string;
  paths: string[];
  selectionCount: number;
}

interface RenameDialogState {
  isDirectory: boolean;
  name: string;
  path: string;
}

interface useFileDialogsResult {
  // Create dialogs
  createFileDialog: boolean;
  createFolderDialog: boolean;
  // Delete dialog
  deleteDialog: boolean;
  // Detail dialog
  detailTarget: string[] | null;

  pendingDeletePaths: string[];
  // Permissions dialog
  permissionsDialog: PermissionsDialogState | null;
  // Rename dialog
  renameDialog: RenameDialogState | null;
  setCreateFileDialog: Dispatch<SetStateAction<boolean>>;

  setCreateFolderDialog: Dispatch<SetStateAction<boolean>>;
  setDeleteDialog: Dispatch<SetStateAction<boolean>>;

  setDetailTarget: Dispatch<SetStateAction<string[] | null>>;
  setPendingDeletePaths: Dispatch<SetStateAction<string[]>>;

  setPermissionsDialog: Dispatch<SetStateAction<PermissionsDialogState | null>>;
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
