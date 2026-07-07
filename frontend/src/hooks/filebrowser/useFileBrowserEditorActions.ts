import {
  useCallback,
  type Dispatch,
  type RefObject,
  type SetStateAction,
} from "react";

import { linuxio, uploadContent } from "@/api";
import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useUploadChunkSize } from "@/hooks/useUploadChunkSize";

interface UseFileBrowserEditorActionsParams {
  editingPath: string | null;
  editorRef: RefObject<FileEditorHandle | null>;
  isEditorDirty: boolean;
  setCloseEditorDialog: Dispatch<SetStateAction<boolean>>;
  setEditingPath: Dispatch<SetStateAction<string | null>>;
  setIsEditorDirty: Dispatch<SetStateAction<boolean>>;
  setIsSavingFile: Dispatch<SetStateAction<boolean>>;
}

const getErrorMessage = (error: unknown, fallback: string) =>
  error instanceof Error ? error.message : fallback;

export const useFileBrowserEditorActions = ({
  editingPath,
  editorRef,
  isEditorDirty,
  setCloseEditorDialog,
  setEditingPath,
  setIsEditorDirty,
  setIsSavingFile,
}: UseFileBrowserEditorActionsParams) => {
  const toast = useScopedToast({ href: "/filebrowser", label: "Open files" });
  const resourceCache = linuxio.filebrowser.resource_get.useCache();
  const chunkSize = useUploadChunkSize();

  const saveContentViaStream = useCallback(
    async (path: string, contentBytes: Uint8Array) => {
      // Saving replaces the file being edited by design; uploads otherwise
      // never overwrite unless told to.
      await uploadContent(path, contentBytes, { chunkSize, overwrite: true });
    },
    [chunkSize],
  );

  const invalidateEditedFile = useCallback(
    (path: string) => {
      void resourceCache.invalidate({
        path,
        unused: "",
        getContent: "true",
      });
    },
    [resourceCache],
  );

  const saveCurrentEditor = useCallback(async () => {
    if (!editorRef.current || !editingPath) return false;

    setIsSavingFile(true);
    try {
      const content = editorRef.current.getContent();
      const contentBytes = new TextEncoder().encode(content);
      await saveContentViaStream(editingPath, contentBytes);
      toast.success("File saved successfully!");
      setIsEditorDirty(false);
      invalidateEditedFile(editingPath);
      return true;
    } catch (error) {
      console.error("Save error:", error);
      toast.error(getErrorMessage(error, "Failed to save file"));
      return false;
    } finally {
      setIsSavingFile(false);
    }
  }, [
    editingPath,
    editorRef,
    invalidateEditedFile,
    saveContentViaStream,
    setIsEditorDirty,
    setIsSavingFile,
    toast,
  ]);

  const handleSaveFile = useCallback(async () => {
    await saveCurrentEditor();
  }, [saveCurrentEditor]);

  const handleCloseEditor = useCallback(() => {
    if (isEditorDirty) {
      setCloseEditorDialog(true);
    } else {
      setEditingPath(null);
      setIsEditorDirty(false);
    }
  }, [isEditorDirty, setCloseEditorDialog, setEditingPath, setIsEditorDirty]);

  const handleKeepEditing = useCallback(() => {
    setCloseEditorDialog(false);
  }, [setCloseEditorDialog]);

  const handleDiscardAndExit = useCallback(() => {
    setEditingPath(null);
    setIsEditorDirty(false);
    setCloseEditorDialog(false);
  }, [setCloseEditorDialog, setEditingPath, setIsEditorDirty]);

  const handleSaveAndExit = useCallback(async () => {
    const saved = await saveCurrentEditor();
    if (!saved) return;
    setEditingPath(null);
    setCloseEditorDialog(false);
  }, [saveCurrentEditor, setCloseEditorDialog, setEditingPath]);

  return {
    handleCloseEditor,
    handleDiscardAndExit,
    handleKeepEditing,
    handleSaveAndExit,
    handleSaveFile,
  };
};
