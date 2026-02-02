import type React from "react";
import { useCallback, useState } from "react";
import { toast } from "sonner";

import { FileTransferContextValue } from "@/contexts/FileTransferContext";
import { FileResource } from "@/types/filebrowser";

import { DroppedEntry, useFileDroppedEntries } from "./useFileDroppedEntries";

interface UseDragAndDropUploadParams {
  normalizedPath: string;
  resource?: FileResource | null;
  editingPath?: string | null;
  startUpload: FileTransferContextValue["startUpload"];
  onUploadComplete: () => void;
};

interface UseDragAndDropUploadResult {
  isDragOver: boolean;
  overwriteTargets: DroppedEntry[] | null;
  handleDragEnter: (event: React.DragEvent) => void;
  handleDragOver: (event: React.DragEvent) => void;
  handleDragLeave: (event: React.DragEvent) => void;
  handleDrop: (event: React.DragEvent) => Promise<void>;
  handleConfirmOverwrite: () => Promise<void>;
  handleCancelOverwrite: () => void;
  setOverwriteTargets: (targets: DroppedEntry[] | null) => void;
};

export const useFileDragAndDrop = ({
  normalizedPath,
  resource,
  editingPath,
  startUpload,
  onUploadComplete,
}: UseDragAndDropUploadParams): UseDragAndDropUploadResult => {
  const [isDragOver, setIsDragOver] = useState(false);
  const [overwriteTargets, setOverwriteTargets] = useState<
    DroppedEntry[] | null
  >(null);
  const extractDroppedEntries = useFileDroppedEntries();

  const uploadDroppedFiles = useCallback(
    async (entries: DroppedEntry[], options?: { override?: boolean }) => {
      const override = options?.override ?? false;
      if (!entries.length) {
        return { conflicts: [] as DroppedEntry[], uploaded: 0, failures: [] };
      }

      const result = await startUpload(entries, normalizedPath, override);

      if (result.uploaded > 0) {
        onUploadComplete();
      }

      return result;
    },
    [normalizedPath, onUploadComplete, startUpload],
  );

  const handleDragEnter = useCallback(
    (event: React.DragEvent) => {
      if (editingPath) return;
      if (!resource || resource.type !== "directory") return;
      if (!event.dataTransfer?.types?.includes("Files")) return;
      event.preventDefault();
      setIsDragOver(true);
    },
    [editingPath, resource],
  );

  const handleDragOver = useCallback(
    (event: React.DragEvent) => {
      if (editingPath) return;
      if (!resource || resource.type !== "directory") return;
      if (!event.dataTransfer?.types?.includes("Files")) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = "copy";
      setIsDragOver(true);
    },
    [editingPath, resource],
  );

  const handleDragLeave = useCallback(
    (event: React.DragEvent) => {
      if (editingPath) return;
      const nextTarget = event.relatedTarget as Node | null;
      if (
        nextTarget &&
        (event.currentTarget as HTMLElement).contains(nextTarget)
      ) {
        return;
      }
      setIsDragOver(false);
    },
    [editingPath],
  );

  const handleDrop = useCallback(
    async (event: React.DragEvent) => {
      if (editingPath) return;
      if (!resource || resource.type !== "directory") return;
      event.preventDefault();
      setIsDragOver(false);

      const dataTransfer = event.dataTransfer;
      if (!dataTransfer) return;

      try {
        const droppedEntries = await extractDroppedEntries(dataTransfer);
        if (!droppedEntries.length) {
          toast.warning(
            "Could not read dropped items. Folder drag-and-drop may not be supported in this browser.",
          );
          return;
        }

        const { conflicts } = await uploadDroppedFiles(droppedEntries);
        if (conflicts.length) {
          setOverwriteTargets(conflicts);
          toast.warning(
            `${conflicts.length} item${conflicts.length === 1 ? " is" : "s are"} already present. Overwrite them?`,
          );
        }
      } catch (err: any) {
        console.error("Failed to process drop", err);
        toast.error("Failed to upload dropped items");
      }
    },
    [editingPath, resource, uploadDroppedFiles, extractDroppedEntries],
  );

  const handleConfirmOverwrite = useCallback(async () => {
    if (!overwriteTargets || overwriteTargets.length === 0) return;
    const files = overwriteTargets;
    setOverwriteTargets(null);
    await uploadDroppedFiles(files, { override: true });
  }, [overwriteTargets, uploadDroppedFiles]);

  const handleCancelOverwrite = useCallback(() => {
    setOverwriteTargets(null);
  }, []);

  const setOverwriteTargetsForDialog = useCallback(
    (targets: DroppedEntry[] | null) => {
      setOverwriteTargets(targets);
    },
    [],
  );

  return {
    isDragOver,
    overwriteTargets,
    handleDragEnter,
    handleDragOver,
    handleDragLeave,
    handleDrop,
    handleConfirmOverwrite,
    handleCancelOverwrite,
    setOverwriteTargets: setOverwriteTargetsForDialog,
  };
};

export type { DroppedEntry };
