import { useMemo, useReducer, useRef, type RefObject } from "react";

import type { DroppedEntry } from "@/hooks/filebrowser/useFileDragAndDrop";
import { mergeDroppedEntries } from "@/utils/fileUpload";

interface UploadSummary {
  files: number;
  folders: number;
}

interface UploadState {
  isUploadProcessing: boolean;
  uploadDialogOpen: boolean;
  uploadEntries: DroppedEntry[];
}

type UploadEvent =
  | { type: "clearEntries" }
  | { type: "closeDialog" }
  | { entries: DroppedEntry[]; type: "mergeEntries" }
  | { type: "openDialog" }
  | { processing: boolean; type: "setProcessing" };

const initialUploadState: UploadState = {
  isUploadProcessing: false,
  uploadDialogOpen: false,
  uploadEntries: [],
};

function uploadReducer(state: UploadState, event: UploadEvent): UploadState {
  switch (event.type) {
    case "clearEntries":
      return { ...state, uploadEntries: [] };
    case "closeDialog":
      return { ...state, uploadDialogOpen: false, uploadEntries: [] };
    case "mergeEntries":
      return {
        ...state,
        uploadEntries: mergeDroppedEntries(state.uploadEntries, event.entries),
      };
    case "openDialog":
      return { ...state, uploadDialogOpen: true, uploadEntries: [] };
    case "setProcessing":
      return { ...state, isUploadProcessing: event.processing };
  }
}

interface UploadActions {
  clearEntries: () => void;
  closeDialog: () => void;
  mergeEntries: (entries: DroppedEntry[]) => void;
  openDialog: () => void;
  setProcessing: (processing: boolean) => void;
}

interface UploadSlice extends UploadState {
  actions: UploadActions;
  fileInputRef: RefObject<HTMLInputElement | null>;
  folderInputRef: RefObject<HTMLInputElement | null>;
  uploadSummary: UploadSummary;
}

/**
 * Upload slice: the upload dialog's state (entries, processing flag, hidden
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
      mergeEntries: (entries) => dispatch({ entries, type: "mergeEntries" }),
      openDialog: () => dispatch({ type: "openDialog" }),
      setProcessing: (processing) =>
        dispatch({ processing, type: "setProcessing" }),
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

export type { UploadActions, UploadSlice, UploadSummary };
