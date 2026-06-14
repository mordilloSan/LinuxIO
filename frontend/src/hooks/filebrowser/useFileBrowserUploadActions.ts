import {
  useCallback,
  type Dispatch,
  type RefObject,
  type SetStateAction,
} from "react";

import type { BackgroundJobsContextValue } from "@/types/backgroundJobs";
import {
  buildEntriesFromFileList,
  mergeDroppedEntries,
} from "@/utils/fileUpload";

import type { DroppedEntry } from "./useFileDroppedEntries";
import { useScopedToast } from "../useScopedToast";

interface UseFileBrowserUploadActionsParams {
  fileInputRef: RefObject<HTMLInputElement | null>;
  folderInputRef: RefObject<HTMLInputElement | null>;
  invalidateListing: () => void;
  isUploadProcessing: boolean;
  normalizedPath: string;
  onContextMenuClose: () => void;
  setIsUploadProcessing: Dispatch<SetStateAction<boolean>>;
  setOverwriteTargets: (targets: DroppedEntry[] | null) => void;
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
  setIsUploadProcessing,
  setOverwriteTargets,
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
    (event: React.ChangeEvent<HTMLInputElement>) => {
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
      const result = await startUpload(uploadEntries, normalizedPath);
      if (result.conflicts.length) {
        setOverwriteTargets(result.conflicts);
        toast.warning(
          `${result.conflicts.length} item${result.conflicts.length === 1 ? " is" : "s are"} already present. Overwrite them?`,
        );
      }
      if (result.uploaded > 0) {
        invalidateListing();
      }
      if (!result.conflicts.length) {
        setUploadDialogOpen(false);
        setUploadEntries([]);
      }
    } catch (error) {
      console.error("Upload failed", error);
      toast.error("Upload failed");
    } finally {
      setIsUploadProcessing(false);
    }
  }, [
    invalidateListing,
    normalizedPath,
    setIsUploadProcessing,
    setOverwriteTargets,
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
