import { useCallback, useState, type DragEvent } from "react";

import { useScopedToast } from "@/hooks/useScopedToast";
import type { BackgroundTaskActions } from "@/tasks/types";
import { joinPath } from "@/utils/path";

import { INTERNAL_DRAG_TYPE } from "../-dragTypes";
import { extractDroppedEntries } from "../-fileUpload";
import {
  acceptTrashDrop,
  hasTrashDrag,
  type TrashDragItem,
} from "../-internalDrag";
import type { FileResource } from "../-types";
import type { ResolveCollisionsFn } from "./useFileConflicts";

// Drags started inside the browser (moving items between folders) are not
// uploads: they carry the internal type next to whatever else the browser adds.
const isInternalDrag = (event: DragEvent) =>
  event.dataTransfer?.types?.includes(INTERNAL_DRAG_TYPE) ?? false;

interface UseDragAndDropUploadParams {
  editingPath?: string | null;
  normalizedPath: string;
  /** Items dragged out of the Trash onto the folder are restored into it. */
  onRestoreItems?: (items: TrashDragItem[], destinationDir: string) => void;
  onUploadComplete: () => void;
  resolveCollisions: ResolveCollisionsFn;
  resource?: FileResource | null;
  startUpload: BackgroundTaskActions["startUpload"];
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
  onRestoreItems,
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

  const handleDragEnter = useCallback(
    (event: DragEvent) => {
      if (editingPath || isInternalDrag(event)) return;
      if (!resource || resource.type !== "directory") return;
      if (!event.dataTransfer?.types?.includes("Files")) return;
      event.preventDefault();
      setIsDragOver(true);
    },
    [editingPath, resource],
  );

  const handleDragOver = useCallback(
    (event: DragEvent) => {
      if (editingPath || !resource || resource.type !== "directory") return;
      // Items dragged out of the Trash and dropped on the folder itself, not
      // on a subfolder's row, go back into this folder.
      if (onRestoreItems && hasTrashDrag(event.dataTransfer)) {
        event.preventDefault();
        event.dataTransfer.dropEffect = "move";
        return;
      }
      if (isInternalDrag(event)) return;
      if (!event.dataTransfer?.types?.includes("Files")) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = "copy";
      setIsDragOver(true);
    },
    [editingPath, onRestoreItems, resource],
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
      if (editingPath || !resource || resource.type !== "directory") return;
      if (onRestoreItems && hasTrashDrag(event.dataTransfer)) {
        event.preventDefault();
        const items = acceptTrashDrop(event.dataTransfer);
        if (items.length > 0) onRestoreItems(items, normalizedPath);
        return;
      }
      if (isInternalDrag(event)) return;
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
          resolution.keepBoth.map((entry) => entry.relativePath),
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
      normalizedPath,
      onRestoreItems,
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
