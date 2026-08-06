import { useCallback } from "react";

import { linuxio, uploadContent } from "@/api";
import { markTerminalFeedbackEmitted } from "@/hooks/backgroundJobs/terminalJobFeedback";
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
  const toast = useScopedToast({
    label: "Open files",
    params: { _splat: "" },
    to: "/filebrowser/$",
  });
  const resourceCache = linuxio.filebrowser.resource_get.useCache();
  const chunkSize = useUploadChunkSize();

  const saveContentViaStream = useCallback(
    async (path: string, contentBytes: Uint8Array) => {
      // Saving replaces the file being edited by design; uploads otherwise
      // never overwrite unless told to.
      await uploadContent(path, contentBytes, {
        chunkSize,
        // handleSaveContent owns the save outcome (toasts), so the global
        // background-jobs watcher must not also report this job's failure.
        onJobStart: (job) => markTerminalFeedbackEmitted(job.id),
        overwrite: true,
      });
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

  const handleSaveContent = useCallback(
    async (content: string): Promise<boolean> => {
      if (!editingPath) return false;

      actions.setSaving(true);
      try {
        const contentBytes = new TextEncoder().encode(content);
        await saveContentViaStream(editingPath, contentBytes);
        toast.success("File saved successfully!");
        invalidateEditedFile(editingPath);
        return true;
      } catch (error) {
        console.error("Save error:", error);
        toast.error(getErrorMessage(error, "Failed to save file"));
        return false;
      } finally {
        actions.setSaving(false);
      }
    },
    [actions, editingPath, invalidateEditedFile, saveContentViaStream, toast],
  );

  const handleSaveFile = useCallback(async () => {
    await editorRef.current?.save();
  }, [editorRef]);

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
    const saved = await editorRef.current?.save();
    if (!saved) return;
    actions.close();
  }, [actions, editorRef]);

  return {
    handleCloseEditor,
    handleDiscardAndExit,
    handleKeepEditing,
    handleSaveAndExit,
    handleSaveContent,
    handleSaveFile,
  };
};
