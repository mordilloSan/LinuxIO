import { useCallback, type ChangeEvent } from "react";

import type { BackgroundTasksContextValue } from "@/types/backgroundTasks";
import { buildEntriesFromFileList } from "@/utils/fileUpload";
import { joinPath } from "@/utils/path";

import type { ResolveCollisionsFn } from "./useFileConflicts";
import type { UploadSlice } from "./useFileUpload";
import { useScopedToast } from "../useScopedToast";

interface UseFileBrowserUploadActionsParams {
  invalidateListing: () => void;
  normalizedPath: string;
  onContextMenuClose: () => void;
  resolveCollisions: ResolveCollisionsFn;
  startUpload: BackgroundTasksContextValue["startUpload"];
  upload: UploadSlice;
}

export const useFileBrowserUploadActions = ({
  invalidateListing,
  normalizedPath,
  onContextMenuClose,
  resolveCollisions,
  startUpload,
  upload,
}: UseFileBrowserUploadActionsParams) => {
  const toast = useScopedToast({
    label: "Open files",
    params: { _splat: "" },
    to: "/filebrowser/$",
  });
  const {
    actions: uploadActions,
    fileInputRef,
    folderInputRef,
    isUploadProcessing,
    uploadEntries,
  } = upload;

  const handleUpload = useCallback(() => {
    onContextMenuClose();
    uploadActions.openDialog();
  }, [onContextMenuClose, uploadActions]);

  const handleUploadInputChange = useCallback(
    (event: ChangeEvent<HTMLInputElement>) => {
      const files = event.target.files;
      if (!files?.length) {
        event.target.value = "";
        return;
      }

      const entries = buildEntriesFromFileList(files);
      if (!entries.length) {
        event.target.value = "";
        toast.error("No files detected in selection");
        return;
      }

      uploadActions.mergeEntries(entries);
      event.target.value = "";
    },
    [toast, uploadActions],
  );

  const handleCloseUploadDialog = useCallback(() => {
    if (isUploadProcessing) return;
    uploadActions.closeDialog();
  }, [isUploadProcessing, uploadActions]);

  const handleClearUploadSelection = useCallback(() => {
    if (isUploadProcessing) return;
    uploadActions.clearEntries();
  }, [isUploadProcessing, uploadActions]);

  const handlePickFiles = useCallback(() => {
    fileInputRef.current?.click();
  }, [fileInputRef]);

  const handlePickFolder = useCallback(() => {
    folderInputRef.current?.click();
  }, [folderInputRef]);

  const handleStartUpload = useCallback(async () => {
    if (uploadEntries.length === 0) {
      toast.error("Select files or folders to upload");
      return;
    }

    uploadActions.setProcessing(true);
    try {
      // Uploads never overwrite silently: check the landing paths and ask the
      // user per collision before any bytes move.
      const resolution = await resolveCollisions(
        uploadEntries.filter((entry) => !entry.isDirectory),
        (entry) => joinPath(normalizedPath, entry.relativePath),
        normalizedPath,
      );
      if (!resolution) {
        return; // user cancelled the conflict prompt; keep the dialog open
      }
      const kept = new Set(resolution.kept);
      const entriesToSend = uploadEntries.filter(
        (entry) => entry.isDirectory || kept.has(entry),
      );
      if (!entriesToSend.length) {
        toast.info("All items skipped");
        return;
      }

      const result = await startUpload(
        entriesToSend,
        normalizedPath,
        resolution.overwrite,
      );
      if (result.uploaded > 0) {
        invalidateListing();
      }
      uploadActions.closeDialog();
    } catch (error) {
      console.error("Upload failed", error);
      toast.error("Upload failed");
    } finally {
      uploadActions.setProcessing(false);
    }
  }, [
    invalidateListing,
    normalizedPath,
    resolveCollisions,
    startUpload,
    toast,
    uploadActions,
    uploadEntries,
  ]);

  return {
    handleClearUploadSelection,
    handleCloseUploadDialog,
    handlePickFiles,
    handlePickFolder,
    handleStartUpload,
    handleUpload,
    handleUploadInputChange,
  };
};
