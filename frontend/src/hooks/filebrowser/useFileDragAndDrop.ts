import type React from "react";

import { useCallback, useState } from "react";

import type { BackgroundJobsContextValue } from "@/types/backgroundJobs";

import type { ResolveCollisionsFn } from "@/hooks/filebrowser/useFileConflicts";
import { useScopedToast } from "@/hooks/useScopedToast";
import { FileResource } from "@/types/filebrowser";
import { joinPath } from "@/utils/path";

import { DroppedEntry, useFileDroppedEntries } from "./useFileDroppedEntries";

interface UseDragAndDropUploadParams {
  editingPath?: string | null;
  normalizedPath: string;
  onUploadComplete: () => void;
  resolveCollisions: ResolveCollisionsFn;
  resource?: FileResource | null;
  startUpload: BackgroundJobsContextValue["startUpload"];
}

interface UseDragAndDropUploadResult {
  handleDragEnter: (event: React.DragEvent) => void;
  handleDragLeave: (event: React.DragEvent) => void;
  handleDragOver: (event: React.DragEvent) => void;
  handleDrop: (event: React.DragEvent) => Promise<void>;
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
  const toast = useScopedToast({ href: "/filebrowser", label: "Open files" });
  const [isDragOver, setIsDragOver] = useState(false);
  const extractDroppedEntries = useFileDroppedEntries();

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

export type { DroppedEntry };
