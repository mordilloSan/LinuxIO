import { Dispatch, SetStateAction, useMemo, useRef, useState } from "react";

import type { DroppedEntry } from "@/hooks/filebrowser/useFileDragAndDrop";

interface UploadSummary {
  files: number;
  folders: number;
}

interface useFileUploadResult {
  fileInputRef: React.RefObject<HTMLInputElement | null>;
  folderInputRef: React.RefObject<HTMLInputElement | null>;
  isUploadProcessing: boolean;
  setIsUploadProcessing: Dispatch<SetStateAction<boolean>>;
  setUploadDialogOpen: Dispatch<SetStateAction<boolean>>;
  setUploadEntries: Dispatch<SetStateAction<DroppedEntry[]>>;
  uploadDialogOpen: boolean;
  uploadEntries: DroppedEntry[];
  uploadSummary: UploadSummary;
}

/**
 * Custom hook for managing file upload state
 * Handles upload dialog, entries, and file/folder input refs
 */
export const useFileUpload = (): useFileUploadResult => {
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false);
  const [isUploadProcessing, setIsUploadProcessing] = useState(false);
  const [uploadEntries, setUploadEntries] = useState<DroppedEntry[]>([]);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const folderInputRef = useRef<HTMLInputElement>(null);

  // Calculate upload summary
  const uploadSummary = useMemo(
    () =>
      uploadEntries.reduce(
        (acc, entry) => {
          if (entry.isDirectory) acc.folders += 1;
          else acc.files += 1;
          return acc;
        },
        { files: 0, folders: 0 },
      ),
    [uploadEntries],
  );

  return {
    uploadDialogOpen,
    setUploadDialogOpen,
    isUploadProcessing,
    setIsUploadProcessing,
    uploadEntries,
    setUploadEntries,
    fileInputRef,
    folderInputRef,
    uploadSummary,
  };
};

export type { UploadSummary };
