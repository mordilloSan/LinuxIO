import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";

import type { FileItem, FileResource } from "@/types/filebrowser";

type ClipboardOperation = "copy" | "cut";

interface ClipboardData {
  paths: string[];
  operation: ClipboardOperation;
}

interface useFileSelectionParams {
  resource: FileResource | undefined;
  normalizedPath: string;
  copyItems: (params: {
    sourcePaths: string[];
    destinationDir: string;
  }) => Promise<void>;
  moveItems: (params: {
    sourcePaths: string[];
    destinationDir: string;
  }) => Promise<void>;
  onContextMenuClose?: () => void;
}

interface useFileSelectionResult {
  selectedPaths: Set<string>;
  setSelectedPaths: (paths: Set<string>) => void;
  selectedItems: FileItem[];
  clipboard: ClipboardData | null;
  setClipboard: (data: ClipboardData | null) => void;
  handleCopy: () => void;
  handleCut: () => void;
  handlePaste: () => Promise<void>;
}

/**
 * Custom hook for managing file browser selection and clipboard operations
 * Handles multi-select, copy/cut/paste operations
 */
export const useFileSelection = ({
  resource,
  normalizedPath,
  copyItems,
  moveItems,
  onContextMenuClose,
}: useFileSelectionParams): useFileSelectionResult => {
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
  const [clipboard, setClipboard] = useState<ClipboardData | null>(null);

  // Memoize selected items from resource
  const selectedItems = useMemo(() => {
    if (!resource || resource.type !== "directory" || !resource.items) {
      return [];
    }
    const itemMap = new Map(resource.items.map((item) => [item.path, item]));
    return Array.from(selectedPaths)
      .map((path) => itemMap.get(path))
      .filter(Boolean) as FileItem[];
  }, [resource, selectedPaths]);

  const handleCopy = useCallback(() => {
    onContextMenuClose?.();
    if (selectedPaths.size === 0) return;
    setClipboard({
      paths: Array.from(selectedPaths),
      operation: "copy",
    });
    toast.success(`${selectedPaths.size} item(s) copied to clipboard`);
  }, [onContextMenuClose, selectedPaths]);

  const handleCut = useCallback(() => {
    onContextMenuClose?.();
    if (selectedPaths.size === 0) return;
    setClipboard({
      paths: Array.from(selectedPaths),
      operation: "cut",
    });
    toast.success(`${selectedPaths.size} item(s) cut to clipboard`);
  }, [onContextMenuClose, selectedPaths]);

  const handlePaste = useCallback(async () => {
    onContextMenuClose?.();
    if (!clipboard) {
      toast.error("Nothing to paste");
      return;
    }

    try {
      if (clipboard.operation === "copy") {
        await copyItems({
          sourcePaths: clipboard.paths,
          destinationDir: normalizedPath,
        });
      } else {
        await moveItems({
          sourcePaths: clipboard.paths,
          destinationDir: normalizedPath,
        });
        // Clear clipboard after cut operation
        setClipboard(null);
        setSelectedPaths(new Set());
      }
    } catch {
      // Error is handled by the mutation
    }
  }, [onContextMenuClose, clipboard, copyItems, moveItems, normalizedPath]);

  return {
    selectedPaths,
    setSelectedPaths,
    selectedItems,
    clipboard,
    setClipboard,
    handleCopy,
    handleCut,
    handlePaste,
  };
};
