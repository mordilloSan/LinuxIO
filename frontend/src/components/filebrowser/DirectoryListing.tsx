import { Box } from "@mui/material";
import React, {
  useState,
  useRef,
  useCallback,
  useMemo,
  useEffect,
} from "react";

import EmptyState from "./EmptyState";
import FilesList from "./FilesList";
import FoldersList from "./FoldersList";
import {
  FileResource,
  FileItem,
  SortField,
  SortOrder,
  ViewMode,
} from "../../types/filebrowser";

import { useFileListKeyboardNavigation } from "@/hooks/useFileListKeyboardNavigation";

interface DirectoryListingProps {
  resource: FileResource;
  showHiddenFiles: boolean;
  viewMode: ViewMode;
  sortField: SortField;
  sortOrder: SortOrder;
  onOpenDirectory: (path: string) => void;
  onDownloadFile: (item: FileItem) => void;
  selectedPaths: Set<string>;
  onSelectedPathsChange: (paths: Set<string>) => void;
  isContextMenuOpen: boolean;
  onDelete?: () => void;
}

const DirectoryListing: React.FC<DirectoryListingProps> = ({
  resource,
  showHiddenFiles,
  viewMode,
  sortField,
  sortOrder,
  onOpenDirectory,
  onDownloadFile,
  selectedPaths,
  onSelectedPathsChange,
  isContextMenuOpen,
  onDelete,
}) => {
  const [focusedIndex, setFocusedIndex] = useState<number>(0);
  const [lastSelectedIndex, setLastSelectedIndex] = useState<number>(-1);
  const containerRef = useRef<HTMLDivElement>(null);

  const clearSelection = useCallback(() => {
    onSelectedPathsChange(new Set());
    setFocusedIndex(-1);
  }, [onSelectedPathsChange]);

  const { folders, files } = useMemo(() => {
    const filtered = (resource.items ?? []).filter((item) =>
      showHiddenFiles ? true : !item.hidden,
    );

    // Separate folders and files
    const folders: FileItem[] = [];
    const files: FileItem[] = [];

    filtered.forEach((item) => {
      if (item.type === "directory") {
        folders.push(item);
      } else {
        files.push(item);
      }
    });

    // Sort function
    const sortItems = (items: FileItem[]) => {
      return [...items].sort((a, b) => {
        let comparison = 0;

        switch (sortField) {
          case "name":
            comparison = a.name.localeCompare(b.name);
            break;
          case "size":
            comparison = (a.size ?? 0) - (b.size ?? 0);
            break;
          case "modTime": {
            const aTime = a.modTime ? new Date(a.modTime).getTime() : 0;
            const bTime = b.modTime ? new Date(b.modTime).getTime() : 0;
            comparison = aTime - bTime;
            break;
          }
        }

        return sortOrder === "asc" ? comparison : -comparison;
      });
    };

    return {
      folders: sortItems(folders),
      files: sortItems(files),
    };
  }, [resource.items, showHiddenFiles, sortField, sortOrder]);

  // Combine all items for keyboard navigation
  const allItems = useMemo(() => [...folders, ...files], [folders, files]);

  // Use keyboard navigation hook
  useFileListKeyboardNavigation({
    containerRef: containerRef as React.RefObject<HTMLDivElement>,
    allItems,
    focusedIndex,
    selectedPaths,
    onFocusChange: setFocusedIndex,
    onSelectionChange: onSelectedPathsChange,
    onDelete: onDelete,
    global: true, // Enable global keyboard navigation
  });

  // Handle document click to clear selection
  useEffect(() => {
    const handleDocumentMouseDown = (event: MouseEvent) => {
      if (isContextMenuOpen) {
        return;
      }
      if (!containerRef.current) return;
      if (containerRef.current.contains(event.target as Node)) {
        return;
      }
      clearSelection();
    };

    document.addEventListener("mousedown", handleDocumentMouseDown);
    return () => {
      document.removeEventListener("mousedown", handleDocumentMouseDown);
    };
  }, [clearSelection, isContextMenuOpen]);

  // Clear selection and reset focus when changing directories
  useEffect(() => {
    setFocusedIndex(0);
  }, [resource.path]);

  useEffect(() => {
    onSelectedPathsChange(new Set());
  }, [resource.path, onSelectedPathsChange]);

  const focusItemByPath = useCallback(
    (path: string) => {
      const index = allItems.findIndex((item) => item.path === path);
      if (index === -1) return;
      setFocusedIndex(index);
    },
    [allItems],
  );

  const handleItemSelection = useCallback(
    (event: React.MouseEvent, path: string) => {
      const currentIndex = allItems.findIndex((item) => item.path === path);
      if (currentIndex === -1) return;

      focusItemByPath(path);

      if (event.shiftKey && lastSelectedIndex !== -1) {
        // Shift+click: select range from lastSelectedIndex to currentIndex
        const start = Math.min(lastSelectedIndex, currentIndex);
        const end = Math.max(lastSelectedIndex, currentIndex);
        const next = new Set(selectedPaths);

        for (let i = start; i <= end; i++) {
          next.add(allItems[i].path);
        }
        onSelectedPathsChange(next);
        setLastSelectedIndex(currentIndex);
      } else if (event.ctrlKey || event.metaKey) {
        // Ctrl/Cmd+click: toggle selection
        const next = new Set(selectedPaths);
        if (next.has(path)) {
          next.delete(path);
        } else {
          next.add(path);
        }
        onSelectedPathsChange(next);
        setLastSelectedIndex(currentIndex);
      } else {
        // Regular click: single selection
        onSelectedPathsChange(new Set([path]));
        setLastSelectedIndex(currentIndex);
      }
    },
    [
      focusItemByPath,
      selectedPaths,
      onSelectedPathsChange,
      allItems,
      lastSelectedIndex,
    ],
  );

  const handleItemContextMenu = useCallback(
    (event: React.MouseEvent, path: string) => {
      event.preventDefault();
      const currentIndex = allItems.findIndex((item) => item.path === path);
      if (currentIndex === -1) return;

      focusItemByPath(path);
      if (!selectedPaths.has(path)) {
        onSelectedPathsChange(new Set([path]));
      }
      setLastSelectedIndex(currentIndex);
    },
    [focusItemByPath, selectedPaths, onSelectedPathsChange, allItems],
  );

  const handleContainerMouseDown = useCallback(
    (event: React.MouseEvent) => {
      const element = event.target as HTMLElement | null;
      if (element && element.closest("[data-file-card='true']")) {
        return;
      }
      clearSelection();
    },
    [clearSelection],
  );

  const handleFolderClick = (event: React.MouseEvent, path: string) => {
    handleItemSelection(event, path);
  };

  const handleFileClick = (event: React.MouseEvent, path: string) => {
    handleItemSelection(event, path);
  };

  if (!folders.length && !files.length) {
    return <EmptyState />;
  }

  return (
    <Box
      ref={containerRef}
      onMouseDownCapture={handleContainerMouseDown}
      className="custom-scrollbar"
      sx={{
        display: "flex",
        flexDirection: "column",
        gap: 2,
        overflowY: "auto",
        overflowX: "hidden",
        height: "100%",
      }}
    >
      <FoldersList
        folders={folders}
        selectedPaths={selectedPaths}
        viewMode={viewMode}
        onFolderClick={handleFolderClick}
        onOpenDirectory={onOpenDirectory}
        onFolderContextMenu={handleItemContextMenu}
      />

      <FilesList
        files={files}
        selectedPaths={selectedPaths}
        viewMode={viewMode}
        onFileClick={handleFileClick}
        onDownloadFile={onDownloadFile}
        onFileContextMenu={handleItemContextMenu}
      />
    </Box>
  );
};

export default DirectoryListing;
