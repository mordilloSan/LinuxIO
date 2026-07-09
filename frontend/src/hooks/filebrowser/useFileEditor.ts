import { useMemo, useReducer, useRef, type RefObject } from "react";

import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";

interface EditorState {
  closeEditorDialog: boolean;
  editingPath: string | null;
  isEditorDirty: boolean;
  isSavingFile: boolean;
}

type EditorEvent =
  | { type: "close" }
  | { type: "dismissClosePrompt" }
  | { path: string; type: "openFile" }
  | { type: "promptClose" }
  | { dirty: boolean; type: "setDirty" }
  | { saving: boolean; type: "setSaving" };

const initialEditorState: EditorState = {
  closeEditorDialog: false,
  editingPath: null,
  isEditorDirty: false,
  isSavingFile: false,
};

function editorReducer(state: EditorState, event: EditorEvent): EditorState {
  switch (event.type) {
    case "close":
      return {
        ...state,
        closeEditorDialog: false,
        editingPath: null,
        isEditorDirty: false,
      };
    case "dismissClosePrompt":
      return { ...state, closeEditorDialog: false };
    case "openFile":
      return {
        ...state,
        closeEditorDialog: false,
        editingPath: event.path,
        isEditorDirty: false,
      };
    case "promptClose":
      return { ...state, closeEditorDialog: true };
    case "setDirty":
      return { ...state, isEditorDirty: event.dirty };
    case "setSaving":
      return { ...state, isSavingFile: event.saving };
  }
}

interface EditorActions {
  close: () => void;
  dismissClosePrompt: () => void;
  openFile: (path: string) => void;
  promptClose: () => void;
  setDirty: (dirty: boolean) => void;
  setSaving: (saving: boolean) => void;
}

interface EditorSlice extends EditorState {
  actions: EditorActions;
  editorRef: RefObject<FileEditorHandle | null>;
  showQuickSave: boolean;
}

/**
 * Editor slice: the file editor's UI state (open path, dirty/save flags, the
 * unsaved-changes prompt) behind a stable semantic-action API. `actions` never
 * changes identity, so consumers can hold it in callbacks without churn.
 */
export const useFileEditor = (): EditorSlice => {
  const [state, dispatch] = useReducer(editorReducer, initialEditorState);
  const editorRef = useRef<FileEditorHandle>(null);

  const actions = useMemo<EditorActions>(
    () => ({
      close: () => dispatch({ type: "close" }),
      dismissClosePrompt: () => dispatch({ type: "dismissClosePrompt" }),
      openFile: (path) => dispatch({ path, type: "openFile" }),
      promptClose: () => dispatch({ type: "promptClose" }),
      setDirty: (dirty) => dispatch({ dirty, type: "setDirty" }),
      setSaving: (saving) => dispatch({ saving, type: "setSaving" }),
    }),
    [],
  );

  return {
    ...state,
    actions,
    editorRef,
    showQuickSave: state.editingPath !== null,
  };
};

export type { EditorActions, EditorSlice };
