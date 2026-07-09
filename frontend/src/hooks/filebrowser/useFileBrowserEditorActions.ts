import { useCallback } from "react";

import { linuxio, uploadContent } from "@/api";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useUploadChunkSize } from "@/hooks/useUploadChunkSize";

import type { EditorSlice } from "./useFileEditor";

interface UseFileBrowserEditorActionsParams {
  editor: EditorSlice;
}

const getErrorMessage = (error: unknown, fallback: string) =>
  error instanceof Error ? error.message : fallback;

export const useFileBrowserEditorActions = ({
  editor,
}: UseFileBrowserEditorActionsParams) => {
  const { actions, editingPath, editorRef, isEditorDirty } = editor;
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

    actions.setSaving(true);
    try {
      const content = editorRef.current.getContent();
      const contentBytes = new TextEncoder().encode(content);
      await saveContentViaStream(editingPath, contentBytes);
      toast.success("File saved successfully!");
      actions.setDirty(false);
      invalidateEditedFile(editingPath);
      return true;
    } catch (error) {
      console.error("Save error:", error);
      toast.error(getErrorMessage(error, "Failed to save file"));
      return false;
    } finally {
      actions.setSaving(false);
    }
  }, [
    actions,
    editingPath,
    editorRef,
    invalidateEditedFile,
    saveContentViaStream,
    toast,
  ]);

  const handleSaveFile = useCallback(async () => {
    await saveCurrentEditor();
  }, [saveCurrentEditor]);

  const handleCloseEditor = useCallback(() => {
    if (isEditorDirty) {
      actions.promptClose();
    } else {
      actions.close();
    }
  }, [actions, isEditorDirty]);

  const handleKeepEditing = useCallback(() => {
    actions.dismissClosePrompt();
  }, [actions]);

  const handleDiscardAndExit = useCallback(() => {
    actions.close();
  }, [actions]);

  const handleSaveAndExit = useCallback(async () => {
    const saved = await saveCurrentEditor();
    if (!saved) return;
    actions.close();
  }, [actions, saveCurrentEditor]);

  return {
    handleCloseEditor,
    handleDiscardAndExit,
    handleKeepEditing,
    handleSaveAndExit,
    handleSaveFile,
  };
};
