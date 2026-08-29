import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useState } from "react";

import { linuxio, uploadContent } from "@/api";
import { markTerminalFeedbackEmitted } from "@/hooks/backgroundTasks/terminalTaskFeedback";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useUploadChunkSize } from "@/hooks/useUploadChunkSize";
import { withPromiseCleanup } from "@/utils/withPromiseCleanup";

import type { EditorSlice } from "./useFileEditor";
interface UseFileBrowserEditorActionsParams {
  editor: EditorSlice;
}

export interface EditorSaveConflict {
  content: string;
}

const getErrorMessage = (error: unknown, fallback: string) =>
  error instanceof Error ? error.message : fallback;

export const useFileBrowserEditorActions = ({
  editor,
}: UseFileBrowserEditorActionsParams) => {
  const { actions, editingPath, editorRef, isEditorDirty } = editor;
  const [saveConflict, setSaveConflict] = useState<EditorSaveConflict | null>(
    null,
  );
  const toast = useScopedToast({
    label: "Open files",
    params: { _splat: "" },
    to: "/filebrowser/$",
  });
  const queryClient = useQueryClient();
  const chunkSize = useUploadChunkSize();

  const saveContentViaStream = useCallback(
    async (
      path: string,
      contentBytes: Uint8Array,
      expectedVersion?: string,
    ) => {
      // Saving replaces the file being edited by design; uploads otherwise
      // never overwrite unless told to.
      await uploadContent(path, contentBytes, {
        chunkSize,
        // handleSaveContent owns the save outcome (toasts), so the global
        // background-tasks watcher must not also report this task's failure.
        onTaskStart: (task) => markTerminalFeedbackEmitted(task.id),
        overwrite: true,
        ...(expectedVersion ? { expectedVersion } : {}),
      });
    },
    [chunkSize],
  );

  const invalidateEditedFile = useCallback(
    async (path: string) => {
      await queryClient.invalidateQueries({
        queryKey: linuxio.filebrowser.read_text({ path }).queryKey,
      });
    },
    [queryClient],
  );

  const saveContent = useCallback(
    async (
      content: string,
      expectedVersion: string | undefined,
      promptOnConflict: boolean,
    ): Promise<boolean> => {
      if (!editingPath) return false;

      actions.setSaving(true);
      return withPromiseCleanup(
        (async () => {
          try {
            const contentBytes = new TextEncoder().encode(content);
            await saveContentViaStream(
              editingPath,
              contentBytes,
              expectedVersion,
            );
            toast.success("File saved successfully");
            await invalidateEditedFile(editingPath);
            return true;
          } catch (error) {
            console.error("Save error:", error);
            if (promptOnConflict && isSaveConflict(error)) {
              setSaveConflict({ content });
            } else {
              toast.error(getErrorMessage(error, "Unable to save file"));
            }
            return false;
          }
        })(),
        () => {
          actions.setSaving(false);
        },
      );
    },
    [actions, editingPath, invalidateEditedFile, saveContentViaStream, toast],
  );

  const handleSaveContent = useCallback(
    (content: string, expectedVersion?: string) => {
      if (!expectedVersion) {
        toast.error("Unable to save file because its version is unavailable");
        return Promise.resolve(false);
      }
      return saveContent(content, expectedVersion, true);
    },
    [saveContent, toast],
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
    actions.dismissClosePrompt();
    if (!saved) return;
    actions.close();
  }, [actions, editorRef]);

  const handleReloadConflict = useCallback(async () => {
    if (!editingPath) return;
    actions.setSaving(true);
    return withPromiseCleanup(
      (async () => {
        try {
          const latest = await queryClient.query({
            ...linuxio.filebrowser.read_text({ path: editingPath }),
            staleTime: 0,
          });
          editorRef.current?.reset(latest.content ?? "", latest.version);
          setSaveConflict(null);
        } catch (error) {
          setSaveConflict(null);
          toast.error(getErrorMessage(error, "Unable to reload file"));
        }
      })(),
      () => {
        actions.setSaving(false);
      },
    );
  }, [actions, editingPath, editorRef, queryClient, toast]);

  const handleOverwriteConflict = useCallback(async () => {
    const content = saveConflict?.content;
    if (content === undefined) return;
    setSaveConflict(null);
    const saved = await saveContent(content, undefined, false);
    if (saved) editorRef.current?.reset(content);
  }, [editorRef, saveConflict?.content, saveContent]);

  const handleCancelConflict = useCallback(() => {
    setSaveConflict(null);
  }, []);

  return {
    handleCancelConflict,
    handleCloseEditor,
    handleDiscardAndExit,
    handleKeepEditing,
    handleSaveAndExit,
    handleSaveContent,
    handleSaveFile,
    handleReloadConflict,
    handleOverwriteConflict,
    saveConflict,
  };
};

const isSaveConflict = (error: unknown) =>
  error instanceof Error &&
  "code" in error &&
  (error as { code?: string | number }).code === 409;
