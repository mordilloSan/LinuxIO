import {
  useCallback,
  type ChangeEvent,
  type Dispatch,
  type RefObject,
  type SetStateAction,
} from "react";

import type { BackgroundJobsContextValue } from "@/types/backgroundJobs";
import {
  buildEntriesFromFileList,
  mergeDroppedEntries,
} from "@/utils/fileUpload";
import { joinPath } from "@/utils/path";

import type { ResolveCollisionsFn } from "./useFileConflicts";
import type { DroppedEntry } from "./useFileDroppedEntries";
import { useScopedToast } from "../useScopedToast";

interface UseFileBrowserUploadActionsParams {
  fileInputRef: RefObject<HTMLInputElement | null>;
  folderInputRef: RefObject<HTMLInputElement | null>;
  invalidateListing: () => void;
  isUploadProcessing: boolean;
  normalizedPath: string;
  onContextMenuClose: () => void;
  resolveCollisions: ResolveCollisionsFn;
  setIsUploadProcessing: Dispatch<SetStateAction<boolean>>;
  setUploadDialogOpen: Dispatch<SetStateAction<boolean>>;
  setUploadEntries: Dispatch<SetStateAction<DroppedEntry[]>>;
  startUpload: BackgroundJobsContextValue["startUpload"];
  uploadEntries: DroppedEntry[];
}

export const useFileBrowserUploadActions = ({
  fileInputRef,
  folderInputRef,
  invalidateListing,
  isUploadProcessing,
  normalizedPath,
  onContextMenuClose,
  resolveCollisions,
  setIsUploadProcessing,
  setUploadDialogOpen,
  setUploadEntries,
  startUpload,
  uploadEntries,
}: UseFileBrowserUploadActionsParams) => {
  const toast = useScopedToast({ href: "/filebrowser", label: "Open files" });

  const handleUpload = useCallback(() => {
    onContextMenuClose();
    setUploadEntries([]);
    setUploadDialogOpen(true);
  }, [onContextMenuClose, setUploadDialogOpen, setUploadEntries]);

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

      setUploadEntries((prev) => mergeDroppedEntries(prev, entries));
      event.target.value = "";
    },
    [setUploadEntries, toast],
  );

  const handleCloseUploadDialog = useCallback(() => {
    if (isUploadProcessing) return;
    setUploadDialogOpen(false);
    setUploadEntries([]);
  }, [isUploadProcessing, setUploadDialogOpen, setUploadEntries]);

  const handleClearUploadSelection = useCallback(() => {
    if (isUploadProcessing) return;
    setUploadEntries([]);
  }, [isUploadProcessing, setUploadEntries]);

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

    setIsUploadProcessing(true);
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
      setUploadDialogOpen(false);
      setUploadEntries([]);
    } catch (error) {
      console.error("Upload failed", error);
      toast.error("Upload failed");
    } finally {
      setIsUploadProcessing(false);
    }
  }, [
    invalidateListing,
    normalizedPath,
    resolveCollisions,
    setIsUploadProcessing,
    setUploadDialogOpen,
    setUploadEntries,
    startUpload,
    toast,
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
