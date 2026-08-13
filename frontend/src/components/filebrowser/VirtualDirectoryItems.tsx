import { useVirtualizer } from "@tanstack/react-virtual";
import {
  memo,
  useLayoutEffect,
  useMemo,
  type MouseEvent,
  type MouseEventHandler,
  type RefObject,
} from "react";

import type { SubfolderData } from "@/api";
import SelectionBox from "@/components/filebrowser/SelectionBox";
import {
  DirectoryItem,
  SectionHeader,
} from "@/components/filebrowser/VirtualDirectoryRows";
import { useGridColumnCount } from "@/hooks/useGridColumnCount";
import { useAppTheme } from "@/theme";
import type { FileItem, ViewMode } from "@/types/filebrowser";
import { stripTrailingSlash } from "@/utils/path";

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
  containerRef: RefObject<HTMLDivElement | null>;
  cutPaths: Set<string>;
  files: FileItem[];
  folders: FileItem[];
  isLoadingSubfolders: boolean;
  isMarqueeSelecting: boolean;
  onCancelRename: () => void;
  onConfirmRename: (path: string, newName: string) => void;
  onContainerMouseDown: MouseEventHandler<HTMLDivElement>;
  onDownloadFile: (item: FileItem) => void;
  onFileClick: (event: MouseEvent, path: string) => void;
  onFileContextMenu: (event: MouseEvent, path: string) => void;
  onFolderClick: (event: MouseEvent, path: string) => void;
  onFolderContextMenu: (event: MouseEvent, path: string) => void;
  onMarqueeMouseDown: MouseEventHandler<HTMLDivElement>;
  onOpenDirectory: (path: string) => void;
  renamingPath: string | null;
  renamePendingPath?: string | null;
  /** Item index to scroll into view, or -1 to leave the viewport alone. */
  revealIndex: number;
  selectedPaths: Set<string>;
  selectionBox: SelectionBoxState | null;
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

// React Compiler skips this whole module: TanStack Virtual's `useVirtualizer()`
// returns unstable functions it refuses to memoize, and the bail is file-wide.
// Manual memoization here stays load-bearing, and anything memo-worthy belongs
// in VirtualDirectoryRows.tsx rather than in this file.
const VirtualDirectoryItems = ({
  containerRef,
  cutPaths,
  files,
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
  renamePendingPath,
  revealIndex,
  selectedPaths,
  selectionBox,
  subfoldersMap,
  viewMode,
}: VirtualDirectoryItemsProps) => {
  "use no memo";

  const theme = useAppTheme();
  const horizontalPadding = viewMode === "card" ? CARD_PADDING : 0;
  const rowGap = viewMode === "card" ? CARD_GAP : LIST_GAP;

  const cardColumnCount = useGridColumnCount(containerRef, {
    gap: CARD_GAP,
    minItemWidth: CARD_MIN_WIDTH,
    padding: CARD_PADDING,
  });
  const columnCount = viewMode === "list" ? 1 : cardColumnCount;

  const rows = useMemo(
    () => buildRows({ columnCount, files, folders, viewMode }),
    [columnCount, files, folders, viewMode],
  );

  // TanStack Virtual exposes dynamic helper functions that React Compiler cannot memoize safely.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count: rows.length,
    // The virtualizer owns the row wrappers' transform and the container
    // height — scroll and remeasure updates are written straight to the DOM
    // instead of re-rendering. The outer padding rides along as
    // paddingStart/paddingEnd so row starts already include it.
    directDomUpdates: true,
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
    paddingEnd: horizontalPadding,
    paddingStart: horizontalPadding,
    useAnimationFrameWithResizeObserver: true,
  });
  const virtualRows = virtualizer.getVirtualItems();

  useLayoutEffect(() => {
    virtualizer.measure();
  }, [columnCount, rows.length, viewMode, virtualizer]);

  useLayoutEffect(() => {
    if (revealIndex < 0) return;

    const rowIndex = rows.findIndex(
      (row) =>
        row.type === "items" &&
        row.items.some((item) => item.allItemsIndex === revealIndex),
    );

    if (rowIndex === -1) return;

    virtualizer.scrollToIndex(rowIndex, {
      align: "auto",
    });
  }, [revealIndex, rows, virtualizer]);

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
        ref={virtualizer.containerRef}
        style={{
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
                      disableHover={isMarqueeSelecting}
                      isCut={cutPaths.has(item.path)}
                      isLoadingSubfolders={isLoadingSubfolders}
                      isRenaming={renamingPath === item.path}
                      isRenamePending={renamePendingPath === item.path}
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
                      selected={selectedPaths.has(item.path)}
                      subfolderData={
                        row.itemKind === "folder" && !item.symlink
                          ? subfoldersMap.get(stripTrailingSlash(item.path))
                          : undefined
                      }
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

export default memo(VirtualDirectoryItems);
