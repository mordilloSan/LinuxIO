import { Dispatch, SetStateAction, useRef, useState } from "react";

import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";

interface useFileEditorResult {
  closeEditorDialog: boolean;
  editingPath: string | null;
  editorRef: React.RefObject<FileEditorHandle | null>;
  isEditorDirty: boolean;
  isSavingFile: boolean;
  setCloseEditorDialog: Dispatch<SetStateAction<boolean>>;
  setEditingPath: Dispatch<SetStateAction<string | null>>;
  setIsEditorDirty: Dispatch<SetStateAction<boolean>>;
  setIsSavingFile: Dispatch<SetStateAction<boolean>>;
  showQuickSave: boolean;
}

/**
 * Custom hook for managing file editor state
 * Handles editor path, dirty state, save state, and close confirmation
 */
export const useFileEditor = (): useFileEditorResult => {
  const [editingPath, setEditingPath] = useState<string | null>(null);
  const [isSavingFile, setIsSavingFile] = useState(false);
  const [isEditorDirty, setIsEditorDirty] = useState(false);
  const [closeEditorDialog, setCloseEditorDialog] = useState(false);
  const editorRef = useRef<FileEditorHandle>(null);

  const showQuickSave = editingPath !== null;

  return {
    editingPath,
    setEditingPath,
    isSavingFile,
    setIsSavingFile,
    isEditorDirty,
    setIsEditorDirty,
    closeEditorDialog,
    setCloseEditorDialog,
    editorRef,
    showQuickSave,
  };
};
