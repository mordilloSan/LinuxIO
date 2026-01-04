import { Dispatch, SetStateAction, useRef, useState } from "react";

import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";

interface useFileEditorResult {
  editingPath: string | null;
  setEditingPath: Dispatch<SetStateAction<string | null>>;
  isSavingFile: boolean;
  setIsSavingFile: Dispatch<SetStateAction<boolean>>;
  isEditorDirty: boolean;
  setIsEditorDirty: Dispatch<SetStateAction<boolean>>;
  closeEditorDialog: boolean;
  setCloseEditorDialog: Dispatch<SetStateAction<boolean>>;
  editorRef: React.RefObject<FileEditorHandle | null>;
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
