import { useVirtualizer } from "@tanstack/react-virtual";
import React, { useLayoutEffect, useMemo, useState } from "react";

import FileCard from "@/components/cards/FileCard";
import FileListRow from "@/components/filebrowser/FileListRow";
import SelectionBox from "@/components/filebrowser/SelectionBox";
import { SubfolderData } from "@/hooks/filebrowser/useFileSubfolders";
import { useAppTheme } from "@/theme";
import { FileItem, ViewMode } from "@/types/filebrowser";

const CARD_MIN_WIDTH = 260;
const CARD_GAP = 12;
const CARD_PADDING = 4;
const CARD_ROW_ESTIMATE = 88;
const LIST_GAP = 2;
const LIST_ROW_ESTIMATE = 48;
const SECTION_HEADER_ESTIMATE = 28;

interface SelectionBoxState {
  height: number;
  left: number;
  top: number;
  width: number;
}

interface IndexedItem {
  allItemsIndex: number;
  item: FileItem;
}

interface SectionHeaderRow {
  key: string;
  label: string;
  type: "sectionHeader";
}

interface ItemsRow {
  itemKind: "file" | "folder";
  items: IndexedItem[];
  key: string;
  type: "items";
}

type DirectoryVirtualRow = SectionHeaderRow | ItemsRow;

interface VirtualDirectoryItemsProps {
  containerRef: React.RefObject<HTMLDivElement | null>;
  cutPaths: Set<string>;
  files: FileItem[];
  focusedIndex: number;
  folders: FileItem[];
  isLoadingSubfolders: boolean;
  isMarqueeSelecting: boolean;
  onCancelRename: () => void;
  onConfirmRename: (path: string, newName: string) => void;
  onContainerMouseDown: React.MouseEventHandler<HTMLDivElement>;
  onDownloadFile: (item: FileItem) => void;
  onFileClick: (event: React.MouseEvent, path: string) => void;
  onFileContextMenu: (event: React.MouseEvent, path: string) => void;
  onFolderClick: (event: React.MouseEvent, path: string) => void;
  onFolderContextMenu: (event: React.MouseEvent, path: string) => void;
  onMarqueeMouseDown: React.MouseEventHandler<HTMLDivElement>;
  onOpenDirectory: (path: string) => void;
  renamingPath: string | null;
  selectedPaths: Set<string>;
  selectionBox: SelectionBoxState | null;
  subfoldersMap: Map<string, SubfolderData>;
  viewMode: ViewMode;
}

interface DirectoryItemProps {
  cutPaths: Set<string>;
  disableHover: boolean;
  isLoadingSubfolders: boolean;
  item: FileItem;
  itemKind: "file" | "folder";
  onCancelRename: () => void;
  onConfirmRename: (path: string, newName: string) => void;
  onDownloadFile: (item: FileItem) => void;
  onFileClick: (event: React.MouseEvent, path: string) => void;
  onFileContextMenu: (event: React.MouseEvent, path: string) => void;
  onFolderClick: (event: React.MouseEvent, path: string) => void;
  onFolderContextMenu: (event: React.MouseEvent, path: string) => void;
  onOpenDirectory: (path: string) => void;
  renamingPath: string | null;
  selectedPaths: Set<string>;
  subfoldersMap: Map<string, SubfolderData>;
  viewMode: ViewMode;
}

function buildRows({
  columnCount,
  files,
  folders,
  viewMode,
}: {
  columnCount: number;
  files: FileItem[];
  folders: FileItem[];
  viewMode: ViewMode;
}) {
  const rows: DirectoryVirtualRow[] = [];
  const itemsPerRow = viewMode === "card" ? columnCount : 1;

  const appendSection = (
    label: string,
    itemKind: "file" | "folder",
    items: FileItem[],
    itemIndexOffset: number,
  ) => {
    if (items.length === 0) return;

    rows.push({
      key: `${itemKind}-section-header`,
      label,
      type: "sectionHeader",
    });

    for (let index = 0; index < items.length; index += itemsPerRow) {
      const rowItems = items
        .slice(index, index + itemsPerRow)
        .map((item, i) => ({
          allItemsIndex: itemIndexOffset + index + i,
          item,
        }));

      rows.push({
        itemKind,
        items: rowItems,
        key: `${itemKind}-${rowItems.map(({ item }) => item.path).join("|")}`,
        type: "items",
      });
    }
  };

  appendSection("Folders", "folder", folders, 0);
  appendSection("Files", "file", files, folders.length);

  return rows;
}

const SectionHeader: React.FC<{ label: string; viewMode: ViewMode }> =
  React.memo(({ label, viewMode }) => (
    <h6
      style={{
        color: "inherit",
        fontSize: "15px",
        fontWeight: 600,
        margin: viewMode === "list" ? "4px 0" : "4px 0",
        paddingLeft: "4px",
        paddingRight: "4px",
      }}
    >
      {label}
    </h6>
  ));

SectionHeader.displayName = "VirtualDirectorySectionHeader";

const DirectoryItem: React.FC<DirectoryItemProps> = React.memo(
  ({
    item,
    itemKind,
    selectedPaths,
    cutPaths,
    viewMode,
    onFileClick,
    onDownloadFile,
    onFileContextMenu,
    onFolderClick,
    onOpenDirectory,
    onFolderContextMenu,
    renamingPath,
    onConfirmRename,
    onCancelRename,
    disableHover,
    subfoldersMap,
    isLoadingSubfolders,
  }) => {
    const ItemComponent = viewMode === "list" ? FileListRow : FileCard;

    if (itemKind === "file") {
      return (
        <ItemComponent
          disableHover={disableHover}
          hidden={item.hidden}
          isCut={cutPaths.has(item.path)}
          isDirectory={false}
          isRenaming={renamingPath === item.path}
          isSymlink={item.symlink}
          modTime={item.modTime}
          name={item.name}
          onCancelRename={onCancelRename}
          onClick={(event) => onFileClick(event, item.path)}
          onConfirmRename={(newName) => onConfirmRename(item.path, newName)}
          onContextMenu={(event) => onFileContextMenu(event, item.path)}
          onDoubleClick={() => onDownloadFile(item)}
          path={item.path}
          selected={selectedPaths.has(item.path)}
          showFullPath={item.showFullPath}
          size={item.size}
          type={item.type}
        />
      );
    }

    const isSearchResult = item.showFullPath === true;
    const normalizedPath = item.path.endsWith("/")
      ? item.path.slice(0, -1)
      : item.path;
    const subfolderData = item.symlink
      ? null
      : subfoldersMap.get(normalizedPath);
    const size = isSearchResult
      ? typeof item.size === "number"
        ? item.size
        : null
      : subfolderData
        ? subfolderData.size
        : null;
    const shouldShowSize = !item.symlink;
    const sizeIsLoading = shouldShowSize && isLoadingSubfolders;
    const sizeIsUnavailable =
      shouldShowSize && !isLoadingSubfolders && size === null;

    return (
      <ItemComponent
        directorySizeError={null}
        directorySizeLoading={sizeIsLoading}
        directorySizeUnavailable={sizeIsUnavailable}
        disableHover={disableHover}
        hidden={item.hidden}
        isCut={cutPaths.has(item.path)}
        isDirectory={true}
        isRenaming={renamingPath === item.path}
        isSymlink={item.symlink}
        modTime={item.modTime}
        name={item.name}
        onCancelRename={onCancelRename}
        onClick={(event) => onFolderClick(event, item.path)}
        onConfirmRename={(newName) => onConfirmRename(item.path, newName)}
        onContextMenu={(event) => onFolderContextMenu(event, item.path)}
        onDoubleClick={() => onOpenDirectory(item.path)}
        path={item.path}
        selected={selectedPaths.has(item.path)}
        showFullPath={item.showFullPath}
        size={item.symlink ? undefined : size === null ? undefined : size}
        type={item.type}
      />
    );
  },
);

DirectoryItem.displayName = "VirtualDirectoryItem";

const VirtualDirectoryItems: React.FC<VirtualDirectoryItemsProps> = ({
  containerRef,
  cutPaths,
  files,
  focusedIndex,
  folders,
  isLoadingSubfolders,
  isMarqueeSelecting,
  onCancelRename,
  onConfirmRename,
  onContainerMouseDown,
  onDownloadFile,
  onFileClick,
  onFileContextMenu,
  onFolderClick,
  onFolderContextMenu,
  onMarqueeMouseDown,
  onOpenDirectory,
  renamingPath,
  selectedPaths,
  selectionBox,
  subfoldersMap,
  viewMode,
}) => {
  "use no memo";

  const theme = useAppTheme();
  const [viewportWidth, setViewportWidth] = useState(0);
  const horizontalPadding = viewMode === "card" ? CARD_PADDING : 0;
  const rowGap = viewMode === "card" ? CARD_GAP : LIST_GAP;

  useLayoutEffect(() => {
    const node = containerRef.current;
    if (!node) return;

    const measure = () => {
      setViewportWidth(node.clientWidth);
    };

    measure();
    if (typeof ResizeObserver === "undefined") return;

    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [containerRef]);

  const columnCount = useMemo(() => {
    if (viewMode === "list") return 1;

    const availableWidth = Math.max(0, viewportWidth - CARD_PADDING * 2);
    return Math.max(
      1,
      Math.floor((availableWidth + CARD_GAP) / (CARD_MIN_WIDTH + CARD_GAP)),
    );
  }, [viewMode, viewportWidth]);

  const rows = useMemo(
    () => buildRows({ columnCount, files, folders, viewMode }),
    [columnCount, files, folders, viewMode],
  );

  // TanStack Virtual exposes dynamic helper functions that React Compiler cannot memoize safely.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count: rows.length,
    estimateSize: (index) => {
      const row = rows[index];
      if (row?.type === "sectionHeader") {
        return SECTION_HEADER_ESTIMATE + rowGap;
      }
      return (
        (viewMode === "card" ? CARD_ROW_ESTIMATE : LIST_ROW_ESTIMATE) + rowGap
      );
    },
    getItemKey: (index) => rows[index]?.key ?? index,
    getScrollElement: () => containerRef.current,
    overscan: 6,
    useAnimationFrameWithResizeObserver: true,
  });
  const virtualRows = virtualizer.getVirtualItems();

  useLayoutEffect(() => {
    virtualizer.measure();
  }, [columnCount, rows.length, viewMode, virtualizer]);

  useLayoutEffect(() => {
    if (focusedIndex < 0) return;

    const rowIndex = rows.findIndex(
      (row) =>
        row.type === "items" &&
        row.items.some((item) => item.allItemsIndex === focusedIndex),
    );

    if (rowIndex === -1) return;

    virtualizer.scrollToIndex(rowIndex, {
      align: "auto",
    });
  }, [focusedIndex, rows, virtualizer]);

  return (
    <div
      className="custom-scrollbar"
      onMouseDown={onMarqueeMouseDown}
      onMouseDownCapture={onContainerMouseDown}
      ref={containerRef}
      style={{
        height: "100%",
        minHeight: 0,
        minWidth: 0,
        overflowX: "hidden",
        overflowY: "auto",
        position: "relative",
      }}
    >
      <div
        style={{
          height: virtualizer.getTotalSize() + horizontalPadding * 2,
          minWidth: 0,
          position: "relative",
        }}
      >
        {virtualRows.map((virtualRow) => {
          const row = rows[virtualRow.index];
          if (!row) return null;

          return (
            <div
              data-index={virtualRow.index}
              key={virtualRow.key}
              ref={virtualizer.measureElement}
              style={{
                boxSizing: "border-box",
                left: horizontalPadding,
                paddingBottom: rowGap,
                position: "absolute",
                right: horizontalPadding,
                top: 0,
                transform: `translateY(${virtualRow.start + horizontalPadding}px)`,
              }}
            >
              {row.type === "sectionHeader" ? (
                <SectionHeader label={row.label} viewMode={viewMode} />
              ) : (
                <div
                  style={{
                    display: viewMode === "list" ? "flex" : "grid",
                    flexDirection: viewMode === "list" ? "column" : undefined,
                    gap: viewMode === "list" ? theme.spacing(0.25) : CARD_GAP,
                    gridTemplateColumns:
                      viewMode === "card"
                        ? `repeat(${columnCount}, minmax(0, 1fr))`
                        : undefined,
                    minWidth: 0,
                  }}
                >
                  {row.items.map(({ item }) => (
                    <DirectoryItem
                      cutPaths={cutPaths}
                      disableHover={isMarqueeSelecting}
                      isLoadingSubfolders={isLoadingSubfolders}
                      item={item}
                      itemKind={row.itemKind}
                      key={`${item.path}-${item.name}`}
                      onCancelRename={onCancelRename}
                      onConfirmRename={onConfirmRename}
                      onDownloadFile={onDownloadFile}
                      onFileClick={onFileClick}
                      onFileContextMenu={onFileContextMenu}
                      onFolderClick={onFolderClick}
                      onFolderContextMenu={onFolderContextMenu}
                      onOpenDirectory={onOpenDirectory}
                      renamingPath={renamingPath}
                      selectedPaths={selectedPaths}
                      subfoldersMap={subfoldersMap}
                      viewMode={viewMode}
                    />
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </div>

      {isMarqueeSelecting && selectionBox && (
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

export default React.memo(VirtualDirectoryItems);
