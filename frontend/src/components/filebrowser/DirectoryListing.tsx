import {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
  type MouseEvent,
  type RefObject,
} from "react";

import { useFileListKeyboardNavigation } from "@/hooks/filebrowser/useFileListKeyboardNavigation";
import { useFileMarqueeSelection } from "@/hooks/filebrowser/useFileMarqueeSelection";
import { useFileSubfolders } from "@/hooks/filebrowser/useFileSubfolders";
import { useLatestRef } from "@/hooks/useLatestRef";

import EmptyState from "./EmptyState";
import VirtualDirectoryItems from "./VirtualDirectoryItems";
import type {
  FileItem,
  FileResource,
  SortField,
  SortOrder,
  ViewMode,
} from "../../types/filebrowser";

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
  renamePendingPath?: string | null;
  renameProgressPct?: number;
  resource: FileResource;
  selectedPaths: Set<string>;
  showHiddenFiles: boolean;
  sortField: SortField;
  sortOrder: SortOrder;
  viewMode: ViewMode;
}

const DirectoryListing = ({
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
  renamePendingPath,
  renameProgressPct,
  onStartRename,
  onConfirmRename,
  onCancelRename,
}: DirectoryListingProps) => {
  // `source` gates auto-scrolling: keyboard navigation has to reveal the item it
  // moved to, but a mouse click must never move the viewport under the cursor.
  const [focusState, setFocusState] = useState<{
    path: string;
    index: number;
    source: "keyboard" | "pointer";
  }>({
    path: resource.path,
    index: 0,
    source: "pointer",
  });
  const isFocusForCurrentPath = focusState.path === resource.path;
  const focusedIndex = isFocusForCurrentPath ? focusState.index : 0;
  const revealIndex =
    isFocusForCurrentPath && focusState.source === "keyboard"
      ? focusState.index
      : -1;
  const focusIndexFromKeyboard = useCallback(
    (nextIndex: number) => {
      setFocusState({
        path: resource.path,
        index: nextIndex,
        source: "keyboard",
      });
    },
    [resource.path],
  );
  const focusIndexFromPointer = useCallback(
    (nextIndex: number) => {
      setFocusState({
        path: resource.path,
        index: nextIndex,
        source: "pointer",
      });
    },
    [resource.path],
  );
  const lastSelectedIndexRef = useRef(-1);
  const containerRef = useRef<HTMLDivElement>(null);
  const selectedPathsRef = useLatestRef(selectedPaths);

  // Fetch all subfolder sizes in one request
  const { subfoldersMap, isLoading: isLoadingSubfolders } = useFileSubfolders(
    resource.path,
  );

  const clearSelection = useCallback(() => {
    onSelectedPathsChange(new Set());
    focusIndexFromPointer(-1);
  }, [onSelectedPathsChange, focusIndexFromPointer]);

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
    containerRef: containerRef as RefObject<HTMLDivElement>,
    allItems,
    focusedIndex,
    selectedPaths,
    onFocusChange: focusIndexFromKeyboard,
    onSelectionChange: onSelectedPathsChange,
    onDelete: onDelete,
    onRename: onStartRename,
    global: true, // Enable global keyboard navigation
  });

  // Use marquee selection hook
  const { isSelecting, selectionBox, handleMouseDown } =
    useFileMarqueeSelection(containerRef, allItems, onSelectedPathsChange);

  // Handle document click to clear selection
  const handleDocumentMouseDown = useEffectEvent(
    (event: globalThis.MouseEvent) => {
      if (isContextMenuOpen) {
        return;
      }
      if (!containerRef.current) return;
      if (containerRef.current.contains(event.target as Node)) {
        return;
      }
      clearSelection();
    },
  );

  useEffect(() => {
    document.addEventListener("mousedown", handleDocumentMouseDown);
    return () => {
      document.removeEventListener("mousedown", handleDocumentMouseDown);
    };
  }, []);

  const focusItemByPath = useCallback(
    (path: string) => {
      const index = allItems.findIndex((item) => item.path === path);
      if (index === -1) return;
      focusIndexFromPointer(index);
    },
    [allItems, focusIndexFromPointer],
  );

  const handleItemSelection = useCallback(
    (event: MouseEvent, path: string) => {
      const currentIndex = allItems.findIndex((item) => item.path === path);
      if (currentIndex === -1) return;

      focusItemByPath(path);

      if (event.shiftKey && lastSelectedIndexRef.current !== -1) {
        // Shift+click: select range from lastSelectedIndex to currentIndex
        const start = Math.min(lastSelectedIndexRef.current, currentIndex);
        const end = Math.max(lastSelectedIndexRef.current, currentIndex);
        const next = new Set(selectedPathsRef.current);

        for (let i = start; i <= end; i++) {
          next.add(allItems[i].path);
        }
        onSelectedPathsChange(next);
        lastSelectedIndexRef.current = currentIndex;
      } else if (event.ctrlKey || event.metaKey) {
        // Ctrl/Cmd+click: toggle selection
        const next = new Set(selectedPathsRef.current);
        if (next.has(path)) {
          next.delete(path);
        } else {
          next.add(path);
        }
        onSelectedPathsChange(next);
        lastSelectedIndexRef.current = currentIndex;
      } else {
        // Regular click: single selection
        onSelectedPathsChange(new Set([path]));
        lastSelectedIndexRef.current = currentIndex;
      }
    },
    [focusItemByPath, onSelectedPathsChange, allItems, selectedPathsRef],
  );

  const handleItemContextMenu = useCallback(
    (event: MouseEvent, path: string) => {
      event.preventDefault();
      const currentIndex = allItems.findIndex((item) => item.path === path);
      if (currentIndex === -1) return;

      focusItemByPath(path);
      if (!selectedPathsRef.current.has(path)) {
        onSelectedPathsChange(new Set([path]));
      }
      lastSelectedIndexRef.current = currentIndex;
    },
    [focusItemByPath, onSelectedPathsChange, allItems, selectedPathsRef],
  );

  const handleContainerMouseDown = useCallback(
    (event: MouseEvent) => {
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
    (event: MouseEvent, path: string) => {
      handleItemSelection(event, path);
    },
    [handleItemSelection],
  );

  const handleFileClick = useCallback(
    (event: MouseEvent, path: string) => {
      handleItemSelection(event, path);
    },
    [handleItemSelection],
  );

  if (!folders.length && !files.length) {
    return <EmptyState />;
  }

  return (
    <VirtualDirectoryItems
      containerRef={containerRef}
      cutPaths={cutPaths}
      files={files}
      folders={folders}
      isLoadingSubfolders={isLoadingSubfolders}
      isMarqueeSelecting={isSelecting}
      onCancelRename={onCancelRename}
      onConfirmRename={onConfirmRename}
      onContainerMouseDown={handleContainerMouseDown}
      onDownloadFile={onDownloadFile}
      onFileClick={handleFileClick}
      onFileContextMenu={handleItemContextMenu}
      onFolderClick={handleFolderClick}
      onFolderContextMenu={handleItemContextMenu}
      onMarqueeMouseDown={handleMouseDown}
      onOpenDirectory={onOpenDirectory}
      renamingPath={renamingPath}
      renamePendingPath={renamePendingPath}
      renameProgressPct={renameProgressPct}
      revealIndex={revealIndex}
      selectedPaths={selectedPaths}
      selectionBox={selectionBox}
      subfoldersMap={subfoldersMap}
      viewMode={viewMode}
    />
  );
};

export default DirectoryListing;
