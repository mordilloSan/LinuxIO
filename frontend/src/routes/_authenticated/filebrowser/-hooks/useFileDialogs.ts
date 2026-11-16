import { useMemo, useReducer } from "react";

import type { FileItem } from "../-types";

type DeleteMode = "permanent" | "trash";

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
  deleteDialog: boolean;
  deleteMode: DeleteMode;
  detailTarget: string[] | null;
  detailItems?: FileItem[];
  pendingDeletePaths: string[];
  permissionsDialog: PermissionsDialogState | null;
  permissionsDialogOpen: boolean;
}

type DialogsEvent =
  | { type: "clearPendingDelete" }
  | { type: "closeCreateFile" }
  | { type: "closeDelete" }
  | { paths: string[]; type: "closeDeleteFor" }
  | { type: "closeDetails" }
  | { type: "closePermissions" }
  | { paths: string[]; type: "closePermissionsFor" }
  | { type: "clearPermissions" }
  | { type: "openCreateFile" }
  | { dialog: PermissionsDialogState; type: "openPermissions" }
  | { mode: DeleteMode; paths: string[]; type: "requestDelete" }
  | { items: FileItem[]; paths: string[]; type: "showDetails" };

const initialDialogsState: DialogsState = {
  createFileDialog: false,
  deleteDialog: false,
  deleteMode: "trash",
  detailTarget: null,
  detailItems: [],
  pendingDeletePaths: [],
  permissionsDialog: null,
  permissionsDialogOpen: false,
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
    case "closeDelete":
      return { ...state, deleteDialog: false, pendingDeletePaths: [] };
    case "closeDeleteFor":
      // Each request holds its own paths array, which names it: a job that
      // ends closes the confirm that started it, and not one opened since
      // for other items after that confirm was closed while the job ran.
      return state.deleteDialog && state.pendingDeletePaths === event.paths
        ? { ...state, deleteDialog: false, pendingDeletePaths: [] }
        : state;
    case "closeDetails":
      return { ...state, detailItems: [], detailTarget: null };
    case "closePermissions":
      return { ...state, permissionsDialogOpen: false };
    case "closePermissionsFor":
      // As for a delete, the request's paths array names it.
      return state.permissionsDialogOpen &&
        state.permissionsDialog?.paths === event.paths
        ? { ...state, permissionsDialogOpen: false }
        : state;
    case "clearPermissions":
      return { ...state, permissionsDialog: null };
    case "openCreateFile":
      return { ...state, createFileDialog: true };
    case "openPermissions":
      return {
        ...state,
        permissionsDialog: event.dialog,
        permissionsDialogOpen: true,
      };
    case "requestDelete":
      return {
        ...state,
        deleteDialog: true,
        deleteMode: event.mode,
        pendingDeletePaths: event.paths,
      };
    case "showDetails":
      return { ...state, detailItems: event.items, detailTarget: event.paths };
  }
}

interface DialogsActions {
  clearPendingDelete: () => void;
  closeCreateFile: () => void;
  closeDelete: () => void;
  /** Closes the delete confirm only while it is the request for `paths`. */
  closeDeleteFor: (paths: string[]) => void;
  closeDetails: () => void;
  closePermissions: () => void;
  /** Closes the Permissions dialog only while it is the request for `paths`. */
  closePermissionsFor: (paths: string[]) => void;
  clearPermissions: () => void;
  openCreateFile: () => void;
  openPermissions: (dialog: PermissionsDialogState) => void;
  requestDelete: (paths: string[], mode: DeleteMode) => void;
  showDetails: (paths: string[], items?: FileItem[]) => void;
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
      closeDelete: () => dispatch({ type: "closeDelete" }),
      closeDeleteFor: (paths) => dispatch({ paths, type: "closeDeleteFor" }),
      closeDetails: () => dispatch({ type: "closeDetails" }),
      closePermissions: () => dispatch({ type: "closePermissions" }),
      closePermissionsFor: (paths) =>
        dispatch({ paths, type: "closePermissionsFor" }),
      clearPermissions: () => dispatch({ type: "clearPermissions" }),
      openCreateFile: () => dispatch({ type: "openCreateFile" }),
      openPermissions: (dialog) =>
        dispatch({ dialog, type: "openPermissions" }),
      requestDelete: (paths, mode) =>
        dispatch({ mode, paths, type: "requestDelete" }),
      showDetails: (paths, items = []) =>
        dispatch({ items, paths, type: "showDetails" }),
    }),
    [],
  );

  return { ...state, actions };
};

export type {
  DeleteMode,
  DialogsActions,
  DialogsSlice,
  PermissionsDialogState,
};
