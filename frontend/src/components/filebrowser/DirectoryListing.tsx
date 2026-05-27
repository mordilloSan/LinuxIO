import React, {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
} from "react";

import { useFileListKeyboardNavigation } from "@/hooks/useFileListKeyboardNavigation";
import { useFileMarqueeSelection } from "@/hooks/useFileMarqueeSelection";
import { useFileSubfolders } from "@/hooks/useFileSubfolders";
import { useAppTheme } from "@/theme";

import {
  FileItem,
  FileResource,
  SortField,
  SortOrder,
  ViewMode,
} from "../../types/filebrowser";
import EmptyState from "./EmptyState";
import FilesList from "./FilesList";
import FoldersList from "./FoldersList";
import SelectionBox from "./SelectionBox";

interface DirectoryListingProps {
  cutPaths: Set<string>;
  isContextMenuOpen: boolean;
  onCancelRename: () => void;
  onConfirmRename: (path: string, newName: string) => void;
  onDelete?: () => void;
  onDownloadFile: (item: FileItem) => void;
  onOpenDirectory: (path: string) => void;
  onSelectedPathsChange: (paths: Set<string>) => void;
  onStartRename: () => void;
  renamingPath: string | null;
  resource: FileResource;
  selectedPaths: Set<string>;
  showHiddenFiles: boolean;
  sortField: SortField;
  sortOrder: SortOrder;
  viewMode: ViewMode;
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
  cutPaths,
  onSelectedPathsChange,
  isContextMenuOpen,
  onDelete,
  renamingPath,
  onStartRename,
  onConfirmRename,
  onCancelRename,
}) => {
  const theme = useAppTheme();
  const [focusState, setFocusState] = useState<{
    path: string;
    index: number;
  }>({
    path: resource.path,
    index: 0,
  });
  const focusedIndex = focusState.path === resource.path ? focusState.index : 0;
  const setFocusedIndex = useCallback(
    (nextIndex: number) => {
      setFocusState({
        path: resource.path,
        index: nextIndex,
      });
    },
    [resource.path],
  );
  const [lastSelectedIndex, setLastSelectedIndex] = useState<number>(-1);
  const containerRef = useRef<HTMLDivElement>(null);

  // Fetch all subfolder sizes in one request
  const { subfoldersMap, isLoading: isLoadingSubfolders } = useFileSubfolders(
    resource.path,
  );

  const clearSelection = useCallback(() => {
    onSelectedPathsChange(new Set());
    setFocusedIndex(-1);
  }, [onSelectedPathsChange, setFocusedIndex]);

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
    onRename: onStartRename,
    global: true, // Enable global keyboard navigation
  });

  // Use marquee selection hook
  const { isSelecting, selectionBox, handleMouseDown } =
    useFileMarqueeSelection(containerRef, allItems, onSelectedPathsChange);

  // Handle document click to clear selection
  const handleDocumentMouseDown = useEffectEvent((event: MouseEvent) => {
    if (isContextMenuOpen) {
      return;
    }
    if (!containerRef.current) return;
    if (containerRef.current.contains(event.target as Node)) {
      return;
    }
    clearSelection();
  });

  useEffect(() => {
    document.addEventListener("mousedown", handleDocumentMouseDown);
    return () => {
      document.removeEventListener("mousedown", handleDocumentMouseDown);
    };
  }, []);

  useEffect(() => {
    onSelectedPathsChange(new Set());
  }, [resource.path, onSelectedPathsChange]);

  const focusItemByPath = useCallback(
    (path: string) => {
      const index = allItems.findIndex((item) => item.path === path);
      if (index === -1) return;
      setFocusedIndex(index);
    },
    [allItems, setFocusedIndex],
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
      // Don't clear selection on right-click (context menu)
      if (event.button === 2) {
        return;
      }
      clearSelection();
    },
    [clearSelection],
  );

  const handleFolderClick = useCallback(
    (event: React.MouseEvent, path: string) => {
      handleItemSelection(event, path);
    },
    [handleItemSelection],
  );

  const handleFileClick = useCallback(
    (event: React.MouseEvent, path: string) => {
      handleItemSelection(event, path);
    },
    [handleItemSelection],
  );

  if (!folders.length && !files.length) {
    return <EmptyState />;
  }

  return (
    <div
      className="custom-scrollbar"
      onMouseDown={handleMouseDown}
      onMouseDownCapture={handleContainerMouseDown}
      ref={containerRef}
      style={{
        display: "flex",
        flexDirection: "column",
        gap: theme.spacing(2),
        overflowY: "auto",
        overflowX: "hidden",
        height: "100%",
        position: "relative",
      }}
    >
      <FoldersList
        cutPaths={cutPaths}
        folders={folders}
        isLoadingSubfolders={isLoadingSubfolders}
        isMarqueeSelecting={isSelecting}
        onCancelRename={onCancelRename}
        onConfirmRename={onConfirmRename}
        onFolderClick={handleFolderClick}
        onFolderContextMenu={handleItemContextMenu}
        onOpenDirectory={onOpenDirectory}
        renamingPath={renamingPath}
        selectedPaths={selectedPaths}
        subfoldersMap={subfoldersMap}
        viewMode={viewMode}
      />

      <FilesList
        cutPaths={cutPaths}
        files={files}
        isMarqueeSelecting={isSelecting}
        onCancelRename={onCancelRename}
        onConfirmRename={onConfirmRename}
        onDownloadFile={onDownloadFile}
        onFileClick={handleFileClick}
        onFileContextMenu={handleItemContextMenu}
        renamingPath={renamingPath}
        selectedPaths={selectedPaths}
        viewMode={viewMode}
      />

      {isSelecting && selectionBox && (
        <SelectionBox
          height={selectionBox.height}
          left={selectionBox.left}
          top={selectionBox.top}
          width={selectionBox.width}
        />
      )}
    </div>
  );
};

export default DirectoryListing;
