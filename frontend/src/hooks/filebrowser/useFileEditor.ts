import {
  useBlocker,
  useCanGoBack,
  useNavigate,
  useRouter,
  useSearch,
} from "@tanstack/react-router";
import { useRef, useState, type RefObject } from "react";

import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";

interface EditorState {
  closeEditorDialog: boolean;
  editingPath: string | null;
  isEditorDirty: boolean;
  isSavingFile: boolean;
}

interface EditorActions {
  /** Leaves the editor; unsaved changes stop on the close prompt first. */
  close: () => void;
  /** Leaves from the close prompt, dropping unsaved changes. */
  confirmClose: () => void;
  dismissClosePrompt: () => void;
  openFile: (path: string) => void;
  setDirty: (dirty: boolean) => void;
  setSaving: (saving: boolean) => void;
}

interface EditorSlice extends EditorState {
  actions: EditorActions;
  editorRef: RefObject<FileEditorHandle | null>;
  showQuickSave: boolean;
}

/**
 * Editor slice. The open file lives in the `edit` search param, so opening it
 * pushes a history entry: Back leaves the editor like its close button, and a
 * reload or shared link reopens it. Every navigation away from unsaved
 * changes — close, Back, sidebar, breadcrumbs — stops on the close prompt.
 */
export const useFileEditor = (): EditorSlice => {
  const editingPath = useSearch({
    select: (search) => search.edit ?? null,
    strict: false,
  });
  const navigate = useNavigate();
  const router = useRouter();
  const canGoBack = useCanGoBack();
  const [isEditorDirty, setDirty] = useState(false);
  const [isSavingFile, setSaving] = useState(false);
  const editorRef = useRef<FileEditorHandle>(null);
  const blocker = useBlocker({
    disabled: !isEditorDirty,
    enableBeforeUnload: isEditorDirty,
    shouldBlockFn: () => isEditorDirty,
    withResolver: true,
  });

  const actions: EditorActions = {
    // Opening pushed the editor entry, so closing steps back to the listing;
    // a deep link has nothing behind it and drops the param in place instead.
    close: () => {
      if (canGoBack) {
        router.history.back();
        return;
      }
      void navigate({
        replace: true,
        search: (previous) => ({ ...previous, edit: undefined }),
        to: ".",
      });
    },
    confirmClose: () => {
      setDirty(false);
      blocker.proceed?.();
    },
    dismissClosePrompt: () => blocker.reset?.(),
    openFile: (path) => {
      setDirty(false);
      void navigate({
        search: (previous) => ({ ...previous, edit: path }),
        to: ".",
      });
    },
    setDirty,
    setSaving,
  };

  return {
    actions,
    closeEditorDialog: blocker.status === "blocked",
    editingPath,
    editorRef,
    isEditorDirty,
    isSavingFile,
    showQuickSave: editingPath !== null,
  };
};

export type { EditorActions, EditorSlice };
