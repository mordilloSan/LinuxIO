import {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
  type DragEvent,
  type MouseEvent,
  type RefObject,
} from "react";

import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import { useLatestRef } from "@/hooks/useLatestRef";

import EmptyState from "./EmptyState";
import VirtualDirectoryItems from "./VirtualDirectoryItems";
import { useFileListKeyboardNavigation } from "../-hooks/useFileListKeyboardNavigation";
import { useFileMarqueeSelection } from "../-hooks/useFileMarqueeSelection";
import { useFileSubfolders } from "../-hooks/useFileSubfolders";
import {
  hasInternalDrag,
  readInternalDrag,
  setInternalDrag,
} from "../-internalDrag";
import type {
  FileItem,
  FileResource,
  SortField,
  SortOrder,
  ViewMode,
} from "../-types";

interface DirectoryListingProps {
  cutPaths: Set<string>;
  isContextMenuOpen: boolean;
  onCancelRename: () => void;
  onConfirmRename: (path: string, newName: string) => void;
  onDelete?: () => void;
  onDeletePermanently?: () => void;
  onDownloadFile: (item: FileItem) => void;
  onDropItems: (paths: string[], destinationDir: string, copy: boolean) => void;
  onNavigateUp: () => void;
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
  onDropItems,
  onNavigateUp,
  selectedPaths,
  cutPaths,
  onSelectedPathsChange,
  isContextMenuOpen,
  onDelete,
  onDeletePermanently,
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
  const columnCountRef = useRef(1);
  const containerRef = useRef<HTMLDivElement>(null);
  const draggedPathsRef = useRef<string[]>([]);
  const [dropTargetPath, setDropTargetPath] = useState<string | null>(null);
  const selectedPathsRef = useLatestRef(selectedPaths);

  // Fetch all subfolder sizes in one request
  const {
    error: subfoldersError,
    isLoading: isLoadingSubfolders,
    isUnavailable: subfoldersUnavailable,
    subfoldersMap,
  } = useFileSubfolders(resource.path);

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
  const allItemsRef = useLatestRef(allItems);

  // Use keyboard navigation hook
  useFileListKeyboardNavigation({
    containerRef: containerRef as RefObject<HTMLDivElement>,
    allItems,
    anchorIndexRef: lastSelectedIndexRef,
    columnCountRef,
    focusedIndex,
    selectedPaths,
    onFocusChange: focusIndexFromKeyboard,
    onSelectionChange: onSelectedPathsChange,
    onDelete: onDelete,
    onDeletePermanently,
    onDownloadFile,
    onNavigateUp,
    onOpenDirectory,
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
      const index = allItemsRef.current.findIndex((item) => item.path === path);
      if (index === -1) return;
      focusIndexFromPointer(index);
    },
    [allItemsRef, focusIndexFromPointer],
  );

  const handleItemSelection = useCallback(
    (event: MouseEvent, path: string) => {
      const currentItems = allItemsRef.current;
      const currentIndex = currentItems.findIndex((item) => item.path === path);
      if (currentIndex === -1) return;

      focusItemByPath(path);

      if (event.shiftKey && lastSelectedIndexRef.current !== -1) {
        // Shift+click: select range from lastSelectedIndex to currentIndex
        const start = Math.min(lastSelectedIndexRef.current, currentIndex);
        const end = Math.max(lastSelectedIndexRef.current, currentIndex);
        const next = new Set(selectedPathsRef.current);

        for (let i = start; i <= end; i++) {
          next.add(currentItems[i].path);
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
    [allItemsRef, focusItemByPath, onSelectedPathsChange, selectedPathsRef],
  );

  const handleItemContextMenu = useCallback(
    (event: MouseEvent, path: string) => {
      event.preventDefault();
      const currentIndex = allItemsRef.current.findIndex(
        (item) => item.path === path,
      );
      if (currentIndex === -1) return;

      focusItemByPath(path);
      if (!selectedPathsRef.current.has(path)) {
        onSelectedPathsChange(new Set([path]));
      }
      lastSelectedIndexRef.current = currentIndex;
    },
    [allItemsRef, focusItemByPath, onSelectedPathsChange, selectedPathsRef],
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

  // Dragging a selected row drags the whole selection; any other row is
  // selected and dragged alone.
  const handleItemDragStart = useCallback(
    (event: DragEvent, path: string) => {
      let paths = Array.from(selectedPathsRef.current);
      if (!selectedPathsRef.current.has(path)) {
        paths = [path];
        onSelectedPathsChange(new Set(paths));
        focusItemByPath(path);
      }
      draggedPathsRef.current = paths;
      setInternalDrag(event.dataTransfer, paths);
    },
    [focusItemByPath, onSelectedPathsChange, selectedPathsRef],
  );

  const handleFolderDragOver = useCallback((event: DragEvent, path: string) => {
    // A folder cannot be dropped into itself.
    if (
      !hasInternalDrag(event.dataTransfer) ||
      draggedPathsRef.current.includes(path)
    ) {
      return;
    }
    event.preventDefault();
    event.dataTransfer.dropEffect = event.ctrlKey ? "copy" : "move";
    setDropTargetPath(path);
  }, []);

  const handleFolderDragLeave = useCallback(
    (event: DragEvent, path: string) => {
      if (event.currentTarget.contains(event.relatedTarget as Node | null)) {
        return;
      }
      setDropTargetPath((current) => (current === path ? null : current));
    },
    [],
  );

  const handleFolderDrop = useCallback(
    (event: DragEvent, path: string) => {
      if (!hasInternalDrag(event.dataTransfer)) return;
      // The page's upload drop handler must never see an internal drag.
      event.preventDefault();
      event.stopPropagation();
      setDropTargetPath(null);
      const paths = readInternalDrag(event.dataTransfer);
      if (paths.length === 0 || paths.includes(path)) return;
      onDropItems(paths, path, event.ctrlKey);
    },
    [onDropItems],
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
    <>
      {subfoldersUnavailable && (
        <AppAlert severity="warning">
          <AppAlertTitle>Directory sizes unavailable</AppAlertTitle>
          {subfoldersError?.message ??
            "The indexer is unavailable; cached directory sizes are stale."}
        </AppAlert>
      )}
      <VirtualDirectoryItems
        columnCountRef={columnCountRef}
        containerRef={containerRef}
        cutPaths={cutPaths}
        dropTargetPath={dropTargetPath}
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
        onFolderDragLeave={handleFolderDragLeave}
        onFolderDragOver={handleFolderDragOver}
        onFolderDrop={handleFolderDrop}
        onItemDragStart={handleItemDragStart}
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
    </>
  );
};

export default DirectoryListing;
