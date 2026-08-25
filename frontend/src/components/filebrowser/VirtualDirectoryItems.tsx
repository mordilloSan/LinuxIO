import { useVirtualizer } from "@tanstack/react-virtual";
import {
  memo,
  useCallback,
  useLayoutEffect,
  useMemo,
  type MouseEvent,
  type MouseEventHandler,
  type RefObject,
} from "react";

import type { SubfolderData } from "@/api";
import {
  DirectoryItem,
  SectionHeader,
} from "@/components/filebrowser/DirectoryRows";
import SelectionBox from "@/components/filebrowser/SelectionBox";
import { useGridColumnCount } from "@/hooks/useGridColumnCount";
import { useAppTheme } from "@/theme";
import type { FileItem, ViewMode } from "@/types/filebrowser";
import { stripTrailingSlash } from "@/utils/path";

const CARD_MIN_WIDTH = 260;
const CARD_GAP = 12;
const CARD_PADDING = 4;
const CARD_ROW_ESTIMATE = 92;
const LIST_GAP = 2;
// FileListRow's resting layout is 40px; include the wrapper's 2px row gap in
// estimateSize below so an unmeasured list row starts at the same 42px stride.
const LIST_ROW_ESTIMATE = 40;
const SECTION_HEADER_ESTIMATE = 28;

interface SelectionBoxState {
  height: number;
  left: number;
  top: number;
  width: number;
}

export interface DirectoryVirtualLayout {
  fileCount: number;
  fileRowCount: number;
  fileSectionHeaderIndex: number;
  folderCount: number;
  folderRowCount: number;
  itemsPerRow: number;
  totalRowCount: number;
}

export type DirectoryVirtualRow =
  | { label: string; type: "sectionHeader" }
  | {
      count: number;
      itemKind: "file" | "folder";
      start: number;
      type: "items";
    };

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
  renameProgressPct?: number;
  /** Item index to scroll into view, or -1 to leave the viewport alone. */
  revealIndex: number;
  selectedPaths: Set<string>;
  selectionBox: SelectionBoxState | null;
  subfoldersMap: Map<string, SubfolderData>;
  viewMode: ViewMode;
}

// Keep the retained row model constant-size. TanStack Virtual asks for rows by
// index, so item groups and headers can be resolved only when they are needed
// instead of allocating one wrapper object and array for every directory item.
export function createDirectoryLayout({
  columnCount,
  fileCount,
  folderCount,
  viewMode,
}: {
  columnCount: number;
  fileCount: number;
  folderCount: number;
  viewMode: ViewMode;
}) {
  const itemsPerRow = viewMode === "card" ? columnCount : 1;
  const folderRowCount = Math.ceil(folderCount / itemsPerRow);
  const fileRowCount = Math.ceil(fileCount / itemsPerRow);
  const folderSectionSize = folderCount > 0 ? 1 + folderRowCount : 0;
  const fileSectionSize = fileCount > 0 ? 1 + fileRowCount : 0;

  return {
    fileCount,
    fileRowCount,
    fileSectionHeaderIndex: fileCount > 0 ? folderSectionSize : -1,
    folderCount,
    folderRowCount,
    itemsPerRow,
    totalRowCount: folderSectionSize + fileSectionSize,
  } satisfies DirectoryVirtualLayout;
}

function isDirectorySectionHeader(
  index: number,
  layout: DirectoryVirtualLayout,
) {
  return (
    (layout.folderRowCount > 0 && index === 0) ||
    (layout.fileRowCount > 0 && index === layout.fileSectionHeaderIndex)
  );
}

export function getDirectoryVirtualRow(
  index: number,
  layout: DirectoryVirtualLayout,
): DirectoryVirtualRow | undefined {
  if (index < 0 || index >= layout.totalRowCount) return undefined;

  if (layout.folderRowCount > 0) {
    if (index === 0) return { label: "Folders", type: "sectionHeader" };
    if (index <= layout.folderRowCount) {
      const start = (index - 1) * layout.itemsPerRow;
      return {
        count: Math.min(layout.itemsPerRow, layout.folderCount - start),
        itemKind: "folder",
        start,
        type: "items",
      };
    }
  }

  if (layout.fileRowCount > 0) {
    if (index === layout.fileSectionHeaderIndex) {
      return { label: "Files", type: "sectionHeader" };
    }
    const start =
      (index - layout.fileSectionHeaderIndex - 1) * layout.itemsPerRow;
    return {
      count: Math.min(layout.itemsPerRow, layout.fileCount - start),
      itemKind: "file",
      start,
      type: "items",
    };
  }

  return undefined;
}

export function getDirectoryVirtualRowKey(
  index: number,
  layout: DirectoryVirtualLayout,
  folders: FileItem[],
  files: FileItem[],
): string | number {
  if (index < 0 || index >= layout.totalRowCount) return index;
  if (layout.folderRowCount > 0 && index === 0) {
    return "folder-section-header";
  }
  if (layout.fileRowCount > 0 && index === layout.fileSectionHeaderIndex) {
    return "file-section-header";
  }

  const isFolderRow = index > 0 && index <= layout.folderRowCount;
  const itemKind = isFolderRow ? "folder" : "file";
  const items = isFolderRow ? folders : files;
  const start = isFolderRow
    ? (index - 1) * layout.itemsPerRow
    : (index - layout.fileSectionHeaderIndex - 1) * layout.itemsPerRow;
  const count = Math.min(layout.itemsPerRow, items.length - start);
  // Preserve the previous full-membership key so replacing any card in a row
  // invalidates its cached measurement. Build it on demand without a row array.
  let paths = "";
  for (let offset = 0; offset < count; offset += 1) {
    if (offset > 0) paths += "|";
    paths += items[start + offset]?.path ?? "";
  }
  return `${itemKind}-${paths}`;
}

export function getDirectoryRevealRowIndex(
  revealIndex: number,
  layout: DirectoryVirtualLayout,
): number {
  if (revealIndex < 0 || revealIndex >= layout.folderCount + layout.fileCount) {
    return -1;
  }
  if (revealIndex < layout.folderCount && layout.folderRowCount > 0) {
    return 1 + Math.floor(revealIndex / layout.itemsPerRow);
  }
  const fileIndex = revealIndex - layout.folderCount;
  if (fileIndex < 0 || layout.fileRowCount === 0) return -1;
  return (
    layout.fileSectionHeaderIndex +
    1 +
    Math.floor(fileIndex / layout.itemsPerRow)
  );
}

// React Compiler skips this whole module: TanStack Virtual's `useVirtualizer()`
// returns unstable functions it refuses to memoize, and the bail is file-wide.
// Manual memoization here stays load-bearing, and anything memo-worthy belongs
// in DirectoryRows.tsx rather than in this file.
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
  renameProgressPct,
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

  const layout = useMemo(
    () =>
      createDirectoryLayout({
        columnCount,
        fileCount: files.length,
        folderCount: folders.length,
        viewMode,
      }),
    [columnCount, files.length, folders.length, viewMode],
  );

  const estimateSize = useCallback(
    (index: number) => {
      if (isDirectorySectionHeader(index, layout)) {
        return SECTION_HEADER_ESTIMATE + rowGap;
      }
      return (
        (viewMode === "card" ? CARD_ROW_ESTIMATE : LIST_ROW_ESTIMATE) + rowGap
      );
    },
    [layout, rowGap, viewMode],
  );
  const getItemKey = useCallback(
    (index: number) => getDirectoryVirtualRowKey(index, layout, folders, files),
    [files, folders, layout],
  );

  // TanStack Virtual exposes dynamic helper functions that React Compiler cannot memoize safely.
  // eslint-disable-next-line react-hooks/incompatible-library
  // oxlint-disable-next-line react/incompatible-library
  const virtualizer = useVirtualizer({
    count: layout.totalRowCount,
    // The virtualizer owns the row wrappers' transform and the container
    // height — scroll and remeasure updates are written straight to the DOM
    // instead of re-rendering. The outer padding rides along as
    // paddingStart/paddingEnd so row starts already include it.
    directDomUpdates: true,
    estimateSize,
    getItemKey,
    getScrollElement: () => containerRef.current,
    overscan: 6,
    paddingEnd: horizontalPadding,
    paddingStart: horizontalPadding,
    useAnimationFrameWithResizeObserver: true,
  });
  const virtualRows = virtualizer.getVirtualItems();

  useLayoutEffect(() => {
    // Row-count/key changes retain valid keyed sizes. Switching layout or the
    // number of cards per row changes every row's actual geometry.
    virtualizer.measure();
  }, [columnCount, viewMode, virtualizer]);

  useLayoutEffect(() => {
    if (revealIndex < 0) return;

    const rowIndex = getDirectoryRevealRowIndex(revealIndex, layout);

    if (rowIndex === -1) return;

    virtualizer.scrollToIndex(rowIndex, {
      align: "auto",
    });
  }, [layout, revealIndex, virtualizer]);

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
          const row = getDirectoryVirtualRow(virtualRow.index, layout);
          if (!row) return null;

          return (
            <div
              data-index={virtualRow.index}
              key={virtualRow.key}
              ref={virtualizer.measureElement}
              style={{
                boxSizing: "border-box",
                left: horizontalPadding,
                minHeight:
                  row.type === "items"
                    ? estimateSize(virtualRow.index)
                    : undefined,
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
                    minHeight:
                      viewMode === "card" ? CARD_ROW_ESTIMATE : undefined,
                    minWidth: 0,
                  }}
                >
                  {Array.from({ length: row.count }, (_, offset) => {
                    const items = row.itemKind === "folder" ? folders : files;
                    const item = items[row.start + offset];
                    if (!item) return null;
                    return (
                      <DirectoryItem
                        disableHover={isMarqueeSelecting}
                        isCut={cutPaths.has(item.path)}
                        isLoadingSubfolders={
                          row.itemKind === "folder" && isLoadingSubfolders
                        }
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
                        renameProgressPct={
                          renamePendingPath === item.path
                            ? renameProgressPct
                            : undefined
                        }
                        selected={selectedPaths.has(item.path)}
                        subfolderData={
                          row.itemKind === "folder" && !item.symlink
                            ? subfoldersMap.get(stripTrailingSlash(item.path))
                            : undefined
                        }
                        viewMode={viewMode}
                      />
                    );
                  })}
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
