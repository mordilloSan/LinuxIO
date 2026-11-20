import { useMemo, useReducer, useRef, type RefObject } from "react";

import { mergeDroppedEntries, type DroppedEntry } from "../-fileUpload";

interface UploadSummary {
  files: number;
  folders: number;
}

/**
 * Where the dialog's upload is. While it checks the destination and asks
 * about conflicts the dialog is held, since both need it; once it uploads,
 * the task tray follows the upload and the dialog can be closed.
 */
type UploadStatus = "idle" | "checking" | "uploading";

interface UploadState {
  /** Hand new files and folders to the session user (owner "session"). */
  ownUploads: boolean;
  uploadDialogOpen: boolean;
  uploadEntries: DroppedEntry[];
  uploadStatus: UploadStatus;
}

type UploadEvent =
  | { type: "clearEntries" }
  | { type: "closeDialog" }
  | { entries: DroppedEntry[]; type: "closeDialogFor" }
  | { entries: DroppedEntry[]; type: "mergeEntries" }
  | { type: "openDialog" }
  | { ownUploads: boolean; type: "setOwnUploads" }
  | { entries: DroppedEntry[]; status: UploadStatus; type: "setStatus" };

const initialUploadState: UploadState = {
  ownUploads: false,
  uploadDialogOpen: false,
  uploadEntries: [],
  uploadStatus: "idle",
};

function uploadReducer(state: UploadState, event: UploadEvent): UploadState {
  switch (event.type) {
    case "clearEntries":
      return { ...state, uploadEntries: [] };
    case "closeDialog":
      // An upload still running is the task tray's; a dialog opened again
      // starts clean.
      return {
        ...state,
        ownUploads: false,
        uploadDialogOpen: false,
        uploadEntries: [],
        uploadStatus: "idle",
      };
    case "closeDialogFor":
      // The entries array names the request, as the paths do for a delete:
      // an upload that ends closes the dialog that started it, and not one
      // opened again since.
      return state.uploadEntries === event.entries
        ? uploadReducer(state, { type: "closeDialog" })
        : state;
    case "mergeEntries":
      // The entries stay as they are while the dialog uploads them.
      if (state.uploadStatus !== "idle") return state;
      return {
        ...state,
        uploadEntries: mergeDroppedEntries(state.uploadEntries, event.entries),
      };
    case "openDialog":
      return {
        ...state,
        ownUploads: false,
        uploadDialogOpen: true,
        uploadEntries: [],
        uploadStatus: "idle",
      };
    case "setOwnUploads":
      return { ...state, ownUploads: event.ownUploads };
    case "setStatus":
      return state.uploadEntries === event.entries
        ? { ...state, uploadStatus: event.status }
        : state;
  }
}

interface UploadActions {
  clearEntries: () => void;
  closeDialog: () => void;
  /** Closes the dialog only while it is still the request for `entries`. */
  closeDialogFor: (entries: DroppedEntry[]) => void;
  mergeEntries: (entries: DroppedEntry[]) => void;
  openDialog: () => void;
  setOwnUploads: (ownUploads: boolean) => void;
  /** Sets the status of the request for `entries` while the dialog holds it. */
  setStatus: (entries: DroppedEntry[], status: UploadStatus) => void;
}

interface UploadSlice extends UploadState {
  actions: UploadActions;
  fileInputRef: RefObject<HTMLInputElement | null>;
  folderInputRef: RefObject<HTMLInputElement | null>;
  uploadSummary: UploadSummary;
}

/**
 * Upload slice: the upload dialog's state (entries, upload status, hidden
 * input refs) behind a stable semantic-action API. `actions` never changes
 * identity, so consumers can hold it in callbacks without churn.
 */
export const useFileUpload = (): UploadSlice => {
  const [state, dispatch] = useReducer(uploadReducer, initialUploadState);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const folderInputRef = useRef<HTMLInputElement>(null);

  const actions = useMemo<UploadActions>(
    () => ({
      clearEntries: () => dispatch({ type: "clearEntries" }),
      closeDialog: () => dispatch({ type: "closeDialog" }),
      closeDialogFor: (entries) =>
        dispatch({ entries, type: "closeDialogFor" }),
      mergeEntries: (entries) => dispatch({ entries, type: "mergeEntries" }),
      openDialog: () => dispatch({ type: "openDialog" }),
      setOwnUploads: (ownUploads) =>
        dispatch({ ownUploads, type: "setOwnUploads" }),
      setStatus: (entries, status) =>
        dispatch({ entries, status, type: "setStatus" }),
    }),
    [],
  );

  // Calculate upload summary
  const uploadSummary = useMemo(
    () =>
      state.uploadEntries.reduce(
        (acc, entry) => {
          if (entry.isDirectory) acc.folders += 1;
          else acc.files += 1;
          return acc;
        },
        { files: 0, folders: 0 },
      ),
    [state.uploadEntries],
  );

  return {
    ...state,
    actions,
    fileInputRef,
    folderInputRef,
    uploadSummary,
  };
};

export type { UploadActions, UploadSlice, UploadStatus, UploadSummary };
