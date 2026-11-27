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
  /** Ticked when the dialog opens; "Try again" after a failure sets it. */
  recursive?: boolean;
  selectionCount: number;
}

interface DialogsState {
  createFileDialog: boolean;
  deleteDialog: boolean;
  deleteMode: DeleteMode;
  detailTarget: string[] | null;
  detailItems?: FileItem[];
  pendingDeletePaths: string[];
  /**
   * What the items to delete take on disk, when the page knew it as the
   * delete was asked for (see knownDiskSize).
   */
  pendingDeleteSize?: number;
  permissionsDialog: PermissionsDialogState | null;
  permissionsDialogOpen: boolean;
}

type DialogsEvent =
  | { type: "clearPendingDelete" }
  | { type: "closeCreateFile" }
  | { type: "closeDelete" }
  | { type: "closeDetails" }
  | { type: "closePermissions" }
  | { type: "clearPermissions" }
  | { type: "deleteForGoodInstead" }
  | { type: "openCreateFile" }
  | { dialog: PermissionsDialogState; type: "openPermissions" }
  | {
      knownSize?: number;
      mode: DeleteMode;
      paths: string[];
      type: "requestDelete";
    }
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
      return {
        ...state,
        deleteDialog: false,
        pendingDeletePaths: [],
        pendingDeleteSize: undefined,
      };
    case "closeDetails":
      return { ...state, detailItems: [], detailTarget: null };
    case "closePermissions":
      return { ...state, permissionsDialogOpen: false };
    case "clearPermissions":
      return { ...state, permissionsDialog: null };
    case "deleteForGoodInstead":
      // The same dialog asks again, about the same items.
      return { ...state, deleteMode: "permanent" };
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
        pendingDeleteSize: event.knownSize,
      };
    case "showDetails":
      return { ...state, detailItems: event.items, detailTarget: event.paths };
  }
}

interface DialogsActions {
  clearPendingDelete: () => void;
  closeCreateFile: () => void;
  closeDelete: () => void;
  closeDetails: () => void;
  closePermissions: () => void;
  clearPermissions: () => void;
  /** Turns the open move to Trash into a delete for good, asked again. */
  deleteForGoodInstead: () => void;
  openCreateFile: () => void;
  openPermissions: (dialog: PermissionsDialogState) => void;
  requestDelete: (
    paths: string[],
    mode: DeleteMode,
    knownSize?: number,
  ) => void;
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
      closeDetails: () => dispatch({ type: "closeDetails" }),
      closePermissions: () => dispatch({ type: "closePermissions" }),
      clearPermissions: () => dispatch({ type: "clearPermissions" }),
      deleteForGoodInstead: () => dispatch({ type: "deleteForGoodInstead" }),
      openCreateFile: () => dispatch({ type: "openCreateFile" }),
      openPermissions: (dialog) =>
        dispatch({ dialog, type: "openPermissions" }),
      requestDelete: (paths, mode, knownSize) =>
        dispatch({ knownSize, mode, paths, type: "requestDelete" }),
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
