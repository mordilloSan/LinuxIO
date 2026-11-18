import {
  useRef,
  useState,
  type CSSProperties,
  type DragEvent,
  type MouseEvent,
  type RefObject,
} from "react";

import type { TrashEntry } from "@/api";
import AppTypography from "@/components/ui/AppTypography";
import { formatDate, formatFileSize } from "@/utils/formatters";

import { useFileListKeyboardNavigation } from "../-hooks/useFileListKeyboardNavigation";
import { useFileMarqueeSelection } from "../-hooks/useFileMarqueeSelection";
import { setTrashDrag } from "../-internalDrag";
import {
  type TrashSortField,
  trashOriginalFolder,
  trashShownName,
} from "../-trashEntries";
import type { FileItem, SortOrder, ViewMode } from "../-types";
import {
  getFileEntryBackground,
  getFileEntryHoverBackground,
} from "./fileBrowserSurfaces";
import FileCard from "./FileCard";
import FileIcon from "./FileIcon";
import SortBar, { type SortColumn } from "./SortBar";
import { type ItemPosition, VirtualItems } from "./VirtualDirectoryItems";

import "./file-listing.css";

const TRASH_COLUMNS: SortColumn<TrashSortField>[] = [
  { field: "name", label: "Name" },
  { field: "originalPath", label: "Original location" },
  { centered: true, field: "deletedAt", label: "Date deleted" },
  { field: "size", label: "Size" },
];

const TRASH_COLUMN_TEMPLATE =
  "minmax(0, 2fr) minmax(0, 1.5fr) clamp(120px, 20vw, 200px) clamp(72px, 10vw, 110px)";

const ROW_MIN_HEIGHT = 40;

// Folders and files are one list, sorted together: the Trash's items all go
// in the files section.
const noFolders: FileItem[] = [];

const cellStyle: CSSProperties = {
  minWidth: 0,
  overflow: "hidden",
  padding: "var(--app-space-6) var(--app-space-8)",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
};

interface TrashRowProps {
  /** Where the row stands in the whole list, as a listbox's option says. */
  position: ItemPosition;
  disableHover: boolean;
  entry: TrashEntry;
  onClick: (event: MouseEvent) => void;
  onDoubleClick: () => void;
  onDragStart: (event: DragEvent) => void;
  selected: boolean;
}

/** One item as a row of the list, under the Trash's own columns. */
const TrashRow = ({
  disableHover,
  entry,
  onClick,
  onDoubleClick,
  onDragStart,
  position,
  selected,
}: TrashRowProps) => {
  const background = getFileEntryBackground({ selected });
  const shownName = trashShownName(entry);
  const folder = trashOriginalFolder(entry);
  return (
    <div
      aria-posinset={position.posInSet}
      aria-selected={selected}
      aria-setsize={position.setSize}
      className={`file-row hover-lift${disableHover ? " hover-lift--disabled" : ""}`}
      data-file-card="true"
      data-file-path={entry.path}
      draggable
      onClick={(event) => {
        event.stopPropagation();
        onClick(event);
      }}
      onDoubleClick={(event) => {
        event.stopPropagation();
        onDoubleClick();
      }}
      onDragStart={onDragStart}
      role="option"
      style={
        {
          "--file-row-bg": background,
          "--file-row-bg-hover": getFileEntryHoverBackground(background),
          alignItems: "center",
          borderRadius: "var(--app-radius-base)",
          cursor: "pointer",
          display: "grid",
          gridTemplateColumns: TRASH_COLUMN_TEMPLATE,
          minHeight: ROW_MIN_HEIGHT,
          userSelect: "none",
        } as CSSProperties
      }
    >
      <AppTypography
        color="text.primary"
        component="div"
        fontWeight={500}
        style={{
          ...cellStyle,
          alignItems: "center",
          display: "flex",
          gap: "var(--app-space-6)",
        }}
        variant="body1"
      >
        <FileIcon
          className="hover-lift__icon"
          filename={shownName}
          isDirectory={entry.isDir}
          size={24}
        />
        <span
          style={{
            minWidth: 0,
            overflow: "hidden",
            textOverflow: "ellipsis",
          }}
          title={shownName}
        >
          {shownName}
        </span>
      </AppTypography>
      <AppTypography
        color="text.secondary"
        component="div"
        style={cellStyle}
        title={folder || undefined}
        variant="body2"
      >
        {folder || "Unknown"}
      </AppTypography>
      <AppTypography
        color="text.secondary"
        component="div"
        style={{ ...cellStyle, textAlign: "center" }}
        variant="body2"
      >
        {entry.deletedAt ? formatDate(entry.deletedAt) : "Unknown"}
      </AppTypography>
      <AppTypography
        color="text.secondary"
        component="div"
        style={cellStyle}
        variant="body2"
      >
        {/* A trashed folder's size is not listed. */}
        {entry.isDir ? "" : formatFileSize(entry.size, 1, "")}
      </AppTypography>
    </div>
  );
};

interface TrashListProps {
  /** The card view's size step, as the header's slider shows it. */
  cardSize: number;
  entries: TrashEntry[];
  /** Marks the list while files dragged from a folder are over it. */
  isDropTarget?: boolean;
  /** Delete and Shift+Delete: deletes the selection for good, after asking. */
  onDelete: () => void;
  /** Items dragged out of the list were dropped where they restore. */
  onDropped: (paths: string[]) => void;
  /** Backspace and Alt+Up: back to the folders. */
  onLeave: () => void;
  /** Double click and Enter show an item's properties; nothing opens. */
  onOpen: (path: string) => void;
  onSelectionChange: (paths: Set<string>) => void;
  onSortChange: (field: TrashSortField) => void;
  selectedPaths: Set<string>;
  sortField: TrashSortField;
  sortOrder: SortOrder;
  /** Cards or rows, as the folder listing shows its items. */
  viewMode: ViewMode;
}

/**
 * The Trash's items in the folder listing's virtual rows, as cards or as rows
 * under the Trash's columns, with their selection as in a folder: click, Ctrl
 * and Shift click, a marquee, Ctrl+A and the arrow keys. A trashed folder is
 * not a place to browse into, and a trashed file opens no editor: both show
 * their properties. An item is dragged out as a restore, not as a move.
 */
const TrashList = ({
  cardSize,
  entries,
  isDropTarget = false,
  onDelete,
  onDropped,
  onLeave,
  onOpen,
  onSelectionChange,
  onSortChange,
  selectedPaths,
  sortField,
  sortOrder,
  viewMode,
}: TrashListProps) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const anchorIndexRef = useRef(-1);
  const columnCountRef = useRef(1);
  // `source` gates scrolling: the keyboard brings the item it moved to into
  // view, a click never moves the list under the pointer.
  const [focus, setFocus] = useState<{
    index: number;
    source: "keyboard" | "pointer";
  }>({ index: 0, source: "pointer" });

  // The shape the folder listing's rows, keyboard and marquee helpers read.
  const items: FileItem[] = entries.map((entry) => ({
    name: trashShownName(entry),
    path: entry.path,
    size: entry.size,
    type: entry.isDir ? "directory" : "file",
  }));
  const byPath = new Map(entries.map((entry) => [entry.path, entry]));

  useFileListKeyboardNavigation({
    allItems: items,
    anchorIndexRef,
    columnCountRef,
    containerRef: containerRef as RefObject<HTMLDivElement>,
    focusedIndex: focus.index,
    global: true,
    onDelete,
    onDeletePermanently: onDelete,
    onDownloadFile: (item) => onOpen(item.path),
    onFocusChange: (index) => setFocus({ index, source: "keyboard" }),
    onNavigateUp: onLeave,
    onOpenDirectory: onOpen,
    onSelectionChange,
    selectedPaths,
  });

  const { handleMouseDown, isSelecting, selectionBox } =
    useFileMarqueeSelection(containerRef, items, onSelectionChange);

  const select = (event: MouseEvent, path: string) => {
    const index = items.findIndex((item) => item.path === path);
    if (index === -1) return;
    setFocus({ index, source: "pointer" });
    if (event.shiftKey && anchorIndexRef.current !== -1) {
      const start = Math.min(anchorIndexRef.current, index);
      const end = Math.max(anchorIndexRef.current, index);
      const next = new Set(selectedPaths);
      for (let i = start; i <= end; i++) next.add(items[i].path);
      onSelectionChange(next);
    } else if (event.ctrlKey || event.metaKey) {
      const next = new Set(selectedPaths);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      onSelectionChange(next);
    } else {
      onSelectionChange(new Set([path]));
    }
    anchorIndexRef.current = index;
  };

  const rowPath = (target: EventTarget | null) =>
    target instanceof Element
      ? (target
          .closest("[data-file-card='true']")
          ?.getAttribute("data-file-path") ?? null)
      : null;

  // A click on empty space clears the selection; a right click keeps it.
  const handleContainerMouseDown = (event: MouseEvent) => {
    if (event.button === 2 || rowPath(event.target) !== null) return;
    onSelectionChange(new Set());
    setFocus({ index: -1, source: "pointer" });
  };

  // A right click on an item outside the selection selects that item first;
  // the menu itself is the page's, which the event bubbles up to.
  const handleContextMenu = (event: MouseEvent) => {
    const path = rowPath(event.target);
    if (path === null || selectedPaths.has(path)) return;
    onSelectionChange(new Set([path]));
    const index = items.findIndex((item) => item.path === path);
    setFocus({ index, source: "pointer" });
    anchorIndexRef.current = index;
  };

  // Dragging a selected item drags the whole selection; any other item is
  // selected and dragged alone.
  const handleDragStart = (event: DragEvent, path: string) => {
    const paths = selectedPaths.has(path)
      ? entries
          .filter((entry) => selectedPaths.has(entry.path))
          .map((entry) => entry.path)
      : [path];
    if (!selectedPaths.has(path)) onSelectionChange(new Set(paths));
    setTrashDrag(
      event.dataTransfer,
      paths.flatMap((itemPath) => {
        const entry = byPath.get(itemPath);
        return entry ? [{ name: trashShownName(entry), path: itemPath }] : [];
      }),
      event.currentTarget,
    );
    // A drop that took the items, here or in another tab, started their
    // restore: they leave the selection as its other jobs' items do. The
    // end is heard on the dragged row itself: scrolled out of the virtual
    // window meanwhile, the row is out of the document, and its dragend no
    // longer reaches the list.
    event.currentTarget.addEventListener(
      "dragend",
      (end) => {
        if ((end as globalThis.DragEvent).dataTransfer?.dropEffect !== "none") {
          onDropped(paths);
        }
      },
      { once: true },
    );
  };

  const renderItem = (
    item: FileItem,
    _kind: unknown,
    position: ItemPosition,
  ) => {
    const entry = byPath.get(item.path);
    if (!entry) return null;
    const selected = selectedPaths.has(entry.path);
    const onClick = (event: MouseEvent) => select(event, entry.path);
    const onDoubleClick = () => onOpen(entry.path);
    const onDragStart = (event: DragEvent) =>
      handleDragStart(event, entry.path);
    if (viewMode === "list") {
      return (
        <TrashRow
          disableHover={isSelecting}
          entry={entry}
          onClick={onClick}
          onDoubleClick={onDoubleClick}
          onDragStart={onDragStart}
          position={position}
          selected={selected}
        />
      );
    }
    return (
      <FileCard
        caption={trashOriginalFolder(entry) || "Unknown"}
        disableHover={isSelecting}
        isDirectory={entry.isDir}
        modTime={entry.deletedAt || undefined}
        name={item.name}
        onClick={onClick}
        onDoubleClick={onDoubleClick}
        onDragStart={onDragStart}
        path={entry.path}
        option={position}
        selected={selected}
        // A trashed folder's size is not listed.
        size={entry.isDir ? undefined : entry.size}
        type={item.type}
      />
    );
  };

  return (
    <div
      onContextMenu={handleContextMenu}
      style={{
        display: "flex",
        flex: 1,
        flexDirection: "column",
        gap: "var(--app-space-4)",
        minHeight: 0,
      }}
    >
      <SortBar
        columnTemplate={TRASH_COLUMN_TEMPLATE}
        columns={TRASH_COLUMNS}
        onSortChange={onSortChange}
        sortField={sortField}
        sortOrder={sortOrder}
      />
      <div
        style={{
          borderRadius: "var(--app-radius-md)",
          display: "flex",
          flex: 1,
          flexDirection: "column",
          minHeight: 0,
          outline: isDropTarget
            ? "2px dashed var(--app-palette-primary-main)"
            : undefined,
          outlineOffset: -2,
          position: "relative",
        }}
      >
        <VirtualItems
          cardSize={cardSize}
          columnCountRef={columnCountRef}
          containerRef={containerRef}
          files={items}
          folders={noFolders}
          isMarqueeSelecting={isSelecting}
          listboxLabel="Trash"
          onContainerMouseDown={handleContainerMouseDown}
          onMarqueeMouseDown={handleMouseDown}
          renderItem={renderItem}
          revealIndex={focus.source === "keyboard" ? focus.index : -1}
          heading="Items"
          selectionBox={selectionBox}
          viewMode={viewMode}
        />
      </div>
    </div>
  );
};

export default TrashList;
