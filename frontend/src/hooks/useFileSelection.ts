import { useCallback, useMemo, useState } from "react";

import type { FileItem, FileResource } from "@/types/filebrowser";

import { useScopedToast } from "@/hooks/useScopedToast";

type ClipboardOperation = "copy" | "cut";

interface ClipboardData {
  operation: ClipboardOperation;
  paths: string[];
}

interface useFileSelectionParams {
  copyItems: (params: {
    sourcePaths: string[];
    destinationDir: string;
  }) => Promise<void>;
  moveItems: (params: {
    sourcePaths: string[];
    destinationDir: string;
  }) => Promise<void>;
  normalizedPath: string;
  onContextMenuClose?: () => void;
  resource: FileResource | undefined;
}

interface useFileSelectionResult {
  clipboard: ClipboardData | null;
  handleCopy: () => void;
  handleCut: () => void;
  handlePaste: () => Promise<void>;
  selectedItems: FileItem[];
  selectedPaths: Set<string>;
  setClipboard: (data: ClipboardData | null) => void;
  setSelectedPaths: (paths: Set<string>) => void;
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
  const toast = useScopedToast({ href: "/filebrowser", label: "Open files" });
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
