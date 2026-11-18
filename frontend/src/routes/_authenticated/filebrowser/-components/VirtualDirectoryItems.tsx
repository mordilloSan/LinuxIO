import { useVirtualizer } from "@tanstack/react-virtual";
import {
  Fragment,
  memo,
  useCallback,
  useLayoutEffect,
  useMemo,
  type DragEvent,
  type MouseEvent,
  type MouseEventHandler,
  type ReactNode,
  type RefObject,
} from "react";

import type { SubfolderData } from "@/api";
import { useGridColumnCount } from "@/hooks/useGridColumnCount";
import { stripTrailingSlash } from "@/utils/path";

import {
  CARD_GAP,
  CARD_PADDING,
  CardIconSizeContext,
  cardGeometry,
} from "../-cardSize";
import type { FileItem, ViewMode } from "../-types";
import { DirectoryItem, SectionHeader } from "./DirectoryRows";
import SelectionBox from "./SelectionBox";

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

interface DirectoryVirtualLayout {
  fileCount: number;
  fileRowCount: number;
  /** The files' heading row, or -1 without one. */
  fileSectionHeaderIndex: number;
  /** The first row of files. */
  fileRowsStart: number;
  folderCount: number;
  folderRowCount: number;
  /** The folders' heading row, or -1 without one. */
  folderSectionHeaderIndex: number;
  /** The first row of folders. */
  folderRowsStart: number;
  itemsPerRow: number;
  totalRowCount: number;
}

type DirectoryVirtualRow =
  | { label: string; type: "sectionHeader" }
  | {
      count: number;
      itemKind: "file" | "folder";
      start: number;
      type: "items";
    };

type ItemKind = "file" | "folder";

/** Where an item stands in the whole list, as a listbox's options say. */
export interface ItemPosition {
  posInSet: number;
  setSize: number;
}

interface VirtualItemsProps {
  /** The card view's size step (`cardGeometry`); the header's slider sets it. */
  cardSize: number;
  /** Receives the number of items per row (1 in list view). */
  columnCountRef?: RefObject<number>;
  containerRef: RefObject<HTMLDivElement | null>;
  files: FileItem[];
  folders: FileItem[];
  /** In a listbox, the heading above it. */
  heading?: string;
  isMarqueeSelecting: boolean;
  /**
   * Makes the list a listbox of that name, as the Trash's is. It then holds
   * only its items, as options, without the sections' heading rows; the
   * heading sits above it.
   */
  listboxLabel?: string;
  onContainerMouseDown: MouseEventHandler<HTMLDivElement>;
  onMarqueeMouseDown: MouseEventHandler<HTMLDivElement>;
  /** One item, as a card or a list row as `viewMode` has it. */
  renderItem: (
    item: FileItem,
    itemKind: ItemKind,
    position: ItemPosition,
  ) => ReactNode;
  /** Item index to scroll into view, or -1 to leave the viewport alone. */
  revealIndex: number;
  selectionBox: SelectionBoxState | null;
  viewMode: ViewMode;
}

interface VirtualDirectoryItemsProps {
  /** The card view's size step (`cardGeometry`); the header's slider sets it. */
  cardSize: number;
  /** Receives the number of items per row (1 in list view). */
  columnCountRef?: RefObject<number>;
  containerRef: RefObject<HTMLDivElement | null>;
  cutPaths: Set<string>;
  dropTargetPath?: string | null;
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
  onFolderDragLeave?: (event: DragEvent, path: string) => void;
  onFolderDragOver?: (event: DragEvent, path: string) => void;
  onFolderDrop?: (event: DragEvent, path: string) => void;
  onItemDragStart?: (event: DragEvent, path: string) => void;
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
// Without headers the sections run on with no heading rows between them.
export function createDirectoryLayout({
  columnCount,
  fileCount,
  folderCount,
  headers = true,
  viewMode,
}: {
  columnCount: number;
  fileCount: number;
  folderCount: number;
  headers?: boolean;
  viewMode: ViewMode;
}) {
  const itemsPerRow = viewMode === "card" ? columnCount : 1;
  const headerRows = headers ? 1 : 0;
  const folderRowCount = Math.ceil(folderCount / itemsPerRow);
  const fileRowCount = Math.ceil(fileCount / itemsPerRow);
  const folderSectionSize = folderCount > 0 ? headerRows + folderRowCount : 0;
  const fileSectionSize = fileCount > 0 ? headerRows + fileRowCount : 0;

  return {
    fileCount,
    fileRowCount,
    fileRowsStart: folderSectionSize + (fileCount > 0 ? headerRows : 0),
    fileSectionHeaderIndex: fileCount > 0 && headers ? folderSectionSize : -1,
    folderCount,
    folderRowCount,
    folderRowsStart: folderCount > 0 ? headerRows : 0,
    folderSectionHeaderIndex: folderCount > 0 && headers ? 0 : -1,
    itemsPerRow,
    totalRowCount: folderSectionSize + fileSectionSize,
  } satisfies DirectoryVirtualLayout;
}

function isDirectorySectionHeader(
  index: number,
  layout: DirectoryVirtualLayout,
) {
  return (
    index >= 0 &&
    (index === layout.folderSectionHeaderIndex ||
      index === layout.fileSectionHeaderIndex)
  );
}

export function getDirectoryVirtualRow(
  index: number,
  layout: DirectoryVirtualLayout,
): DirectoryVirtualRow | undefined {
  if (index < 0 || index >= layout.totalRowCount) return undefined;
  if (index === layout.folderSectionHeaderIndex) {
    return { label: "Folders", type: "sectionHeader" };
  }
  if (index === layout.fileSectionHeaderIndex) {
    return { label: "Files", type: "sectionHeader" };
  }

  const folderRow = index - layout.folderRowsStart;
  if (folderRow >= 0 && folderRow < layout.folderRowCount) {
    const start = folderRow * layout.itemsPerRow;
    return {
      count: Math.min(layout.itemsPerRow, layout.folderCount - start),
      itemKind: "folder",
      start,
      type: "items",
    };
  }

  const fileRow = index - layout.fileRowsStart;
  if (fileRow >= 0 && fileRow < layout.fileRowCount) {
    const start = fileRow * layout.itemsPerRow;
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
  const row = getDirectoryVirtualRow(index, layout);
  if (!row) return index;
  if (row.type === "sectionHeader") {
    return index === layout.folderSectionHeaderIndex
      ? "folder-section-header"
      : "file-section-header";
  }

  const items = row.itemKind === "folder" ? folders : files;
  // Preserve the previous full-membership key so replacing any card in a row
  // invalidates its cached measurement. Build it on demand without a row array.
  let paths = "";
  for (let offset = 0; offset < row.count; offset += 1) {
    if (offset > 0) paths += "|";
    paths += items[row.start + offset]?.path ?? "";
  }
  return `${row.itemKind}-${paths}`;
}

export function getDirectoryRevealRowIndex(
  revealIndex: number,
  layout: DirectoryVirtualLayout,
): number {
  if (revealIndex < 0 || revealIndex >= layout.folderCount + layout.fileCount) {
    return -1;
  }
  if (revealIndex < layout.folderCount) {
    return (
      layout.folderRowsStart + Math.floor(revealIndex / layout.itemsPerRow)
    );
  }
  const fileIndex = revealIndex - layout.folderCount;
  return layout.fileRowsStart + Math.floor(fileIndex / layout.itemsPerRow);
}

// React Compiler skips this whole module: TanStack Virtual's `useVirtualizer()`
// returns unstable functions it refuses to memoize, and the bail is file-wide.
// Manual memoization here stays load-bearing, and anything memo-worthy belongs
// in DirectoryRows.tsx rather than in this file.

/**
 * The folder listing's virtual rows and card grid, for any list of items in a
 * folders section and a files section: the folder listing's and the Trash's.
 * Only the rows in view are in the document, and each item in them is what
 * `renderItem` makes of it. As a listbox (`listboxLabel`) it holds only the
 * items, placed in the whole list, under a heading outside it.
 */
const VirtualItemsList = ({
  cardSize,
  columnCountRef,
  containerRef,
  files,
  folders,
  heading,
  isMarqueeSelecting,
  listboxLabel,
  onContainerMouseDown,
  onMarqueeMouseDown,
  renderItem,
  revealIndex,
  selectionBox,
  viewMode,
}: VirtualItemsProps) => {
  "use no memo";

  const horizontalPadding = viewMode === "card" ? CARD_PADDING : 0;
  const rowGap = viewMode === "card" ? CARD_GAP : LIST_GAP;

  // A size step sets the cards' width, and with it their icon and height.
  const card = cardGeometry(cardSize);
  const cardColumnCount = useGridColumnCount(containerRef, {
    gap: CARD_GAP,
    minItemWidth: card.minWidth,
    padding: CARD_PADDING,
  });
  const columnCount = viewMode === "list" ? 1 : cardColumnCount;

  useLayoutEffect(() => {
    if (columnCountRef) columnCountRef.current = columnCount;
  }, [columnCount, columnCountRef]);

  const layout = useMemo(
    () =>
      createDirectoryLayout({
        columnCount,
        fileCount: files.length,
        folderCount: folders.length,
        // A listbox owns only its options: its heading sits outside it.
        headers: !listboxLabel,
        viewMode,
      }),
    [columnCount, files.length, folders.length, listboxLabel, viewMode],
  );

  const estimateSize = useCallback(
    (index: number) => {
      if (isDirectorySectionHeader(index, layout)) {
        return SECTION_HEADER_ESTIMATE + rowGap;
      }
      return (viewMode === "card" ? card.height : LIST_ROW_ESTIMATE) + rowGap;
    },
    [card.height, layout, rowGap, viewMode],
  );
  const getItemKey = useCallback(
    (index: number) => getDirectoryVirtualRowKey(index, layout, folders, files),
    [files, folders, layout],
  );

  // TanStack Virtual exposes dynamic helper functions that React Compiler cannot memoize safely.
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
    // Row-count/key changes retain valid keyed sizes. Switching layout, the
    // number of cards per row or the cards' height changes every row's
    // actual geometry.
    virtualizer.measure();
  }, [card.height, columnCount, viewMode, virtualizer]);

  // The item the keyboard last moved to stays in view when the layout
  // changes, a card-size step included: a step can change the rows' height
  // without changing the columns.
  useLayoutEffect(() => {
    if (revealIndex < 0) return;

    const rowIndex = getDirectoryRevealRowIndex(revealIndex, layout);

    if (rowIndex === -1) return;

    virtualizer.scrollToIndex(rowIndex, {
      align: "auto",
    });
  }, [card.height, layout, revealIndex, virtualizer]);

  const setSize = folders.length + files.length;

  const list = (
    <div
      aria-label={listboxLabel}
      aria-multiselectable={listboxLabel ? true : undefined}
      onMouseDown={onMarqueeMouseDown}
      onMouseDownCapture={onContainerMouseDown}
      ref={containerRef}
      role={listboxLabel ? "listbox" : undefined}
      style={{
        flex: listboxLabel ? 1 : undefined,
        height: listboxLabel ? undefined : "100%",
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
        <CardIconSizeContext value={card.iconSize}>
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
                      gap: viewMode === "list" ? 1 : CARD_GAP,
                      gridTemplateColumns:
                        viewMode === "card"
                          ? `repeat(${columnCount}, minmax(0, 1fr))`
                          : undefined,
                      minHeight: viewMode === "card" ? card.height : undefined,
                      minWidth: 0,
                    }}
                  >
                    {Array.from({ length: row.count }, (_, offset) => {
                      const items = row.itemKind === "folder" ? folders : files;
                      const index = row.start + offset;
                      const item = items[index];
                      if (!item) return null;
                      // Folders come first in the whole list.
                      const posInSet =
                        (row.itemKind === "folder" ? 0 : folders.length) +
                        index +
                        1;
                      return (
                        <Fragment key={`${item.path}-${item.name}`}>
                          {renderItem(item, row.itemKind, {
                            posInSet,
                            setSize,
                          })}
                        </Fragment>
                      );
                    })}
                  </div>
                )}
              </div>
            );
          })}
        </CardIconSizeContext>
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

  if (!listboxLabel) return list;
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
        minWidth: 0,
      }}
    >
      {heading ? (
        <div style={{ paddingInline: horizontalPadding }}>
          <SectionHeader label={heading} viewMode={viewMode} />
        </div>
      ) : null}
      {list}
    </div>
  );
};

export const VirtualItems = memo(VirtualItemsList);

/** The folder listing's items, as cards or list rows. */
const VirtualDirectoryItems = ({
  cardSize,
  columnCountRef,
  containerRef,
  cutPaths,
  dropTargetPath,
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
  onFolderDragLeave,
  onFolderDragOver,
  onFolderDrop,
  onItemDragStart,
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

  const renderItem = useCallback(
    (item: FileItem, itemKind: ItemKind) => (
      <DirectoryItem
        disableHover={isMarqueeSelecting}
        isCut={cutPaths.has(item.path)}
        isDropTarget={dropTargetPath === item.path}
        isLoadingSubfolders={itemKind === "folder" && isLoadingSubfolders}
        isRenaming={renamingPath === item.path}
        isRenamePending={renamePendingPath === item.path}
        item={item}
        itemKind={itemKind}
        onCancelRename={onCancelRename}
        onConfirmRename={onConfirmRename}
        onDownloadFile={onDownloadFile}
        onFileClick={onFileClick}
        onFileContextMenu={onFileContextMenu}
        onFolderClick={onFolderClick}
        onFolderContextMenu={onFolderContextMenu}
        onFolderDragLeave={onFolderDragLeave}
        onFolderDragOver={onFolderDragOver}
        onFolderDrop={onFolderDrop}
        onItemDragStart={onItemDragStart}
        onOpenDirectory={onOpenDirectory}
        renameProgressPct={
          renamePendingPath === item.path ? renameProgressPct : undefined
        }
        selected={selectedPaths.has(item.path)}
        subfolderData={
          itemKind === "folder" && !item.symlink
            ? subfoldersMap.get(stripTrailingSlash(item.path))
            : undefined
        }
        viewMode={viewMode}
      />
    ),
    [
      cutPaths,
      dropTargetPath,
      isLoadingSubfolders,
      isMarqueeSelecting,
      onCancelRename,
      onConfirmRename,
      onDownloadFile,
      onFileClick,
      onFileContextMenu,
      onFolderClick,
      onFolderContextMenu,
      onFolderDragLeave,
      onFolderDragOver,
      onFolderDrop,
      onItemDragStart,
      onOpenDirectory,
      renamePendingPath,
      renameProgressPct,
      renamingPath,
      selectedPaths,
      subfoldersMap,
      viewMode,
    ],
  );

  return (
    <VirtualItems
      cardSize={cardSize}
      columnCountRef={columnCountRef}
      containerRef={containerRef}
      files={files}
      folders={folders}
      isMarqueeSelecting={isMarqueeSelecting}
      onContainerMouseDown={onContainerMouseDown}
      onMarqueeMouseDown={onMarqueeMouseDown}
      renderItem={renderItem}
      revealIndex={revealIndex}
      selectionBox={selectionBox}
      viewMode={viewMode}
    />
  );
};

export default memo(VirtualDirectoryItems);
