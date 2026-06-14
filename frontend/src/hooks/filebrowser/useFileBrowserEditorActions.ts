import { useQueryClient } from "@tanstack/react-query";
import {
  useCallback,
  type Dispatch,
  type RefObject,
  type SetStateAction,
} from "react";

import {
  isConnected,
  linuxio,
  openJobDataStream,
  STREAM_MULTIPLEXER_CONFIG,
} from "@/api";
import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";
import { useConfig } from "@/hooks/useConfig";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useStreamResult } from "@/hooks/useStreamResult";

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
  const { config } = useConfig();
  const queryClient = useQueryClient();
  const { runChunked: runChunkedStreamResult } = useStreamResult();
  const chunkSize =
    (config.appSettings.chunkSizeMB ?? 0) > 0
      ? (config.appSettings.chunkSizeMB as number) * 1024 * 1024
      : STREAM_MULTIPLEXER_CONFIG.uploadChunkSize;

  const saveContentViaStream = useCallback(
    async (path: string, contentBytes: Uint8Array) => {
      const job = await linuxio.filebrowser.upload({
        targetPath: path,
        size: String(contentBytes.length),
      });
      await runChunkedStreamResult<void>({
        open: () => openJobDataStream(job.id, 0),
        openErrorMessage: "Failed to open save stream",
        data: contentBytes,
        chunkSize,
        yieldMs: 0,
        closeMessage: "Stream closed unexpectedly",
      });
    },
    [chunkSize, runChunkedStreamResult],
  );

  const invalidateEditedFile = useCallback(
    (path: string) => {
      queryClient.invalidateQueries({
        queryKey: linuxio.filebrowser.resource_get.queryKey({
          path,
          unused: "",
          getContent: "true",
        }),
      });
    },
    [queryClient],
  );

  const saveCurrentEditor = useCallback(async () => {
    if (!editorRef.current || !editingPath) return false;
    if (!isConnected()) {
      toast.error("Stream connection not ready");
      return false;
    }

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
