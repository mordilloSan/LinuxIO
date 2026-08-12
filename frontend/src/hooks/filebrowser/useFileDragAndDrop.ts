import { useCallback, useState, type DragEvent } from "react";

import type { ResolveCollisionsFn } from "@/hooks/filebrowser/useFileConflicts";
import { useScopedToast } from "@/hooks/useScopedToast";
import type { BackgroundTasksContextValue } from "@/types/backgroundTasks";
import type { FileResource } from "@/types/filebrowser";
import { joinPath } from "@/utils/path";

import { useFileDroppedEntries } from "./useFileDroppedEntries";

interface UseDragAndDropUploadParams {
  editingPath?: string | null;
  normalizedPath: string;
  onUploadComplete: () => void;
  resolveCollisions: ResolveCollisionsFn;
  resource?: FileResource | null;
  startUpload: BackgroundTasksContextValue["startUpload"];
}

interface UseDragAndDropUploadResult {
  handleDragEnter: (event: DragEvent) => void;
  handleDragLeave: (event: DragEvent) => void;
  handleDragOver: (event: DragEvent) => void;
  handleDrop: (event: DragEvent) => Promise<void>;
  isDragOver: boolean;
}

export const useFileDragAndDrop = ({
  normalizedPath,
  resource,
  editingPath,
  resolveCollisions,
  startUpload,
  onUploadComplete,
}: UseDragAndDropUploadParams): UseDragAndDropUploadResult => {
  const toast = useScopedToast({
    label: "Open files",
    params: { _splat: "" },
    to: "/filebrowser/$",
  });
  const [isDragOver, setIsDragOver] = useState(false);
  const extractDroppedEntries = useFileDroppedEntries();

  const handleDragEnter = useCallback(
    (event: DragEvent) => {
      if (editingPath) return;
      if (!resource || resource.type !== "directory") return;
      if (!event.dataTransfer?.types?.includes("Files")) return;
      event.preventDefault();
      setIsDragOver(true);
    },
    [editingPath, resource],
  );

  const handleDragOver = useCallback(
    (event: DragEvent) => {
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
    (event: DragEvent) => {
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
    async (event: DragEvent) => {
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

        // Uploads never overwrite silently: check the landing paths and ask
        // the user per collision before any bytes move.
        const resolution = await resolveCollisions(
          droppedEntries.filter((entry) => !entry.isDirectory),
          (entry) => joinPath(normalizedPath, entry.relativePath),
          normalizedPath,
        );
        if (!resolution) {
          return; // user cancelled the conflict prompt
        }
        const kept = new Set(resolution.kept);
        const entriesToSend = droppedEntries.filter(
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
          onUploadComplete();
        }
      } catch (err: any) {
        console.error("Failed to process drop", err);
        toast.error("Failed to upload dropped items");
      }
    },
    [
      editingPath,
      extractDroppedEntries,
      normalizedPath,
      onUploadComplete,
      resolveCollisions,
      resource,
      startUpload,
      toast,
    ],
  );

  return {
    isDragOver,
    handleDragEnter,
    handleDragOver,
    handleDragLeave,
    handleDrop,
  };
};
