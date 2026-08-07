import { useMemo, useReducer } from "react";

interface PermissionsDialogState {
  group?: string;
  isDirectory: boolean;
  mode: string;
  owner?: string;
  pathLabel: string;
  paths: string[];
  selectionCount: number;
}

interface DialogsState {
  createFileDialog: boolean;
  createFolderDialog: boolean;
  deleteDialog: boolean;
  detailTarget: string[] | null;
  pendingDeletePaths: string[];
  permissionsDialog: PermissionsDialogState | null;
}

type DialogsEvent =
  | { type: "clearPendingDelete" }
  | { type: "closeCreateFile" }
  | { type: "closeCreateFolder" }
  | { type: "closeDelete" }
  | { type: "closeDetails" }
  | { type: "closePermissions" }
  | { type: "openCreateFile" }
  | { type: "openCreateFolder" }
  | { dialog: PermissionsDialogState; type: "openPermissions" }
  | { paths: string[]; type: "requestDelete" }
  | { paths: string[]; type: "showDetails" };

const initialDialogsState: DialogsState = {
  createFileDialog: false,
  createFolderDialog: false,
  deleteDialog: false,
  detailTarget: null,
  pendingDeletePaths: [],
  permissionsDialog: null,
};

function dialogsReducer(
  state: DialogsState,
  event: DialogsEvent,
): DialogsState {
  switch (event.type) {
    case "clearPendingDelete":
      return { ...state, pendingDeletePaths: [] };
    case "closeCreateFile":
      return { ...state, createFileDialog: false };
    case "closeCreateFolder":
      return { ...state, createFolderDialog: false };
    case "closeDelete":
      return { ...state, deleteDialog: false, pendingDeletePaths: [] };
    case "closeDetails":
      return { ...state, detailTarget: null };
    case "closePermissions":
      return { ...state, permissionsDialog: null };
    case "openCreateFile":
      return { ...state, createFileDialog: true };
    case "openCreateFolder":
      return { ...state, createFolderDialog: true };
    case "openPermissions":
      return { ...state, permissionsDialog: event.dialog };
    case "requestDelete":
      return { ...state, deleteDialog: true, pendingDeletePaths: event.paths };
    case "showDetails":
      return { ...state, detailTarget: event.paths };
  }
}

interface DialogsActions {
  clearPendingDelete: () => void;
  closeCreateFile: () => void;
  closeCreateFolder: () => void;
  closeDelete: () => void;
  closeDetails: () => void;
  closePermissions: () => void;
  openCreateFile: () => void;
  openCreateFolder: () => void;
  openPermissions: (dialog: PermissionsDialogState) => void;
  requestDelete: (paths: string[]) => void;
  showDetails: (paths: string[]) => void;
}

interface DialogsSlice extends DialogsState {
  actions: DialogsActions;
}

/**
 * Dialogs slice: visibility and payload of the file browser's dialogs behind
 * a stable semantic-action API. `actions` never changes identity, so
 * consumers can hold it in callbacks without churn.
 */
export const useFileDialogs = (): DialogsSlice => {
  const [state, dispatch] = useReducer(dialogsReducer, initialDialogsState);

  const actions = useMemo<DialogsActions>(
    () => ({
      clearPendingDelete: () => dispatch({ type: "clearPendingDelete" }),
      closeCreateFile: () => dispatch({ type: "closeCreateFile" }),
      closeCreateFolder: () => dispatch({ type: "closeCreateFolder" }),
      closeDelete: () => dispatch({ type: "closeDelete" }),
      closeDetails: () => dispatch({ type: "closeDetails" }),
      closePermissions: () => dispatch({ type: "closePermissions" }),
      openCreateFile: () => dispatch({ type: "openCreateFile" }),
      openCreateFolder: () => dispatch({ type: "openCreateFolder" }),
      openPermissions: (dialog) =>
        dispatch({ dialog, type: "openPermissions" }),
      requestDelete: (paths) => dispatch({ paths, type: "requestDelete" }),
      showDetails: (paths) => dispatch({ paths, type: "showDetails" }),
    }),
    [],
  );

  return { ...state, actions };
};

export type { DialogsActions, DialogsSlice, PermissionsDialogState };
