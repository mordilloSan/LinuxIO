import { useCallback, type ChangeEvent } from "react";

import { useScopedToast } from "@/hooks/useScopedToast";
import type { BackgroundTaskActions } from "@/tasks/types";
import { joinPath } from "@/utils/path";
import { withPromiseCleanup } from "@/utils/withPromiseCleanup";

import { buildEntriesFromFileList } from "../-fileUpload";
import type { ResolveCollisionsFn } from "./useFileConflicts";
import type { UploadSlice } from "./useFileUpload";

interface UseFileBrowserUploadActionsParams {
  invalidateListing: () => void;
  normalizedPath: string;
  onContextMenuClose: () => void;
  resolveCollisions: ResolveCollisionsFn;
  startUpload: BackgroundTaskActions["startUpload"];
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
    ownUploads,
    uploadEntries,
    uploadStatus,
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

  // The destination check and the conflict prompt need the dialog. Once the
  // upload runs, the task tray shows it and can cancel it, and closing the
  // dialog leaves it running.
  const handleCloseUploadDialog = useCallback(() => {
    if (uploadStatus === "checking") return;
    uploadActions.closeDialog();
  }, [uploadActions, uploadStatus]);

  const handleClearUploadSelection = useCallback(() => {
    if (uploadStatus !== "idle") return;
    uploadActions.clearEntries();
  }, [uploadActions, uploadStatus]);

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

    // The entries array names this request; see closeDialogFor.
    uploadActions.setStatus(uploadEntries, "checking");
    return withPromiseCleanup(
      (async () => {
        try {
          // Uploads never overwrite silently: check the landing paths and ask
          // the user per collision before any bytes move.
          const resolution = await resolveCollisions(
            uploadEntries.filter((entry) => !entry.isDirectory),
            (entry) => joinPath(normalizedPath, entry.relativePath),
            normalizedPath,
            (entry) =>
              entry.file
                ? {
                    size: entry.file.size,
                    // 0 means the browser does not know the time, not 1970.
                    modified:
                      entry.file.lastModified > 0
                        ? new Date(entry.file.lastModified).toISOString()
                        : undefined,
                  }
                : undefined,
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

          // Bytes move from here and the task tray lists the upload with its
          // progress and Cancel, so the dialog can be closed while it runs.
          uploadActions.setStatus(uploadEntries, "uploading");
          const result = await startUpload(
            entriesToSend,
            normalizedPath,
            resolution.overwrite,
            resolution.keepBoth.map((entry) => entry.relativePath),
            ownUploads ? "session" : undefined,
          );
          if (result.uploaded > 0) {
            invalidateListing();
          }
          // A dialog opened again while the upload ran stays open.
          uploadActions.closeDialogFor(uploadEntries);
        } catch (error) {
          console.error("Upload failed", error);
          toast.error("Upload failed");
        }
      })(),
      () => {
        uploadActions.setStatus(uploadEntries, "idle");
      },
    );
  }, [
    invalidateListing,
    normalizedPath,
    ownUploads,
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
