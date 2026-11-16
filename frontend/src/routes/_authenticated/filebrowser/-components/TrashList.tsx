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
import type { FileItem, SortOrder } from "../-types";
import {
  getFileEntryBackground,
  getFileEntryHoverBackground,
} from "./fileBrowserSurfaces";
import FileIcon from "./FileIcon";
import SelectionBox from "./SelectionBox";
import SortBar, { type SortColumn } from "./SortBar";

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

const cellStyle: CSSProperties = {
  minWidth: 0,
  overflow: "hidden",
  padding: "var(--app-space-6) var(--app-space-8)",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
};

interface TrashListProps {
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
}

/**
 * The Trash's items, with their selection as in a folder: click, Ctrl and
 * Shift click, a marquee, Ctrl+A and the arrow keys. A trashed folder is not
 * a place to browse into, and a trashed file opens no editor: both show
 * their properties. A row is dragged out as a restore, not as a move.
 */
const TrashList = ({
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
}: TrashListProps) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const anchorIndexRef = useRef(-1);
  const dragged = useRef<string[]>([]);
  const [focusedIndex, setFocusedIndex] = useState(0);

  // The shape the folder listing's keyboard and marquee helpers read.
  const items: FileItem[] = entries.map((entry) => ({
    name: trashShownName(entry),
    path: entry.path,
    size: entry.size,
    type: entry.isDir ? "directory" : "file",
  }));

  // The list is not virtualized, so the row the keyboard moved to is in the
  // document and is brought into view directly.
  const focusFromKeyboard = (index: number) => {
    setFocusedIndex(index);
    const rows = containerRef.current?.querySelectorAll(
      "[data-file-card='true']",
    );
    rows?.[index]?.scrollIntoView?.({ block: "nearest" });
  };

  useFileListKeyboardNavigation({
    allItems: items,
    anchorIndexRef,
    containerRef: containerRef as RefObject<HTMLDivElement>,
    focusedIndex,
    global: true,
    onDelete,
    onDeletePermanently: onDelete,
    onDownloadFile: (item) => onOpen(item.path),
    onFocusChange: focusFromKeyboard,
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
    setFocusedIndex(index);
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
    setFocusedIndex(-1);
  };

  // A right click on a row outside the selection selects that row first;
  // the menu itself is the page's, which the event bubbles up to.
  const handleContextMenu = (event: MouseEvent) => {
    const path = rowPath(event.target);
    if (path === null || selectedPaths.has(path)) return;
    onSelectionChange(new Set([path]));
    const index = items.findIndex((item) => item.path === path);
    setFocusedIndex(index);
    anchorIndexRef.current = index;
  };

  // Dragging a selected row drags the whole selection; any other row is
  // selected and dragged alone.
  const handleDragStart = (event: DragEvent, path: string) => {
    const paths = selectedPaths.has(path)
      ? entries
          .filter((entry) => selectedPaths.has(entry.path))
          .map((entry) => entry.path)
      : [path];
    if (!selectedPaths.has(path)) onSelectionChange(new Set(paths));
    dragged.current = paths;
    const byPath = new Map(entries.map((entry) => [entry.path, entry]));
    setTrashDrag(
      event.dataTransfer,
      paths.flatMap((itemPath) => {
        const entry = byPath.get(itemPath);
        return entry ? [{ name: trashShownName(entry), path: itemPath }] : [];
      }),
    );
  };
  // A drop that took the items, here or in another tab, started their
  // restore: they leave the selection as its other jobs' items do.
  const handleDragEnd = (event: DragEvent) => {
    if (event.dataTransfer.dropEffect !== "none") onDropped(dragged.current);
    dragged.current = [];
  };

  return (
    <div
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
        aria-label="Trash"
        aria-multiselectable
        onContextMenu={handleContextMenu}
        onMouseDown={handleMouseDown}
        onMouseDownCapture={handleContainerMouseDown}
        ref={containerRef}
        role="listbox"
        style={{
          borderRadius: "var(--app-radius-md)",
          display: "flex",
          flex: 1,
          flexDirection: "column",
          gap: 2,
          minHeight: 0,
          outline: isDropTarget
            ? "2px dashed var(--app-palette-primary-main)"
            : undefined,
          outlineOffset: -2,
          overflowX: "hidden",
          overflowY: "auto",
          position: "relative",
        }}
      >
        {entries.map((entry) => {
          const selected = selectedPaths.has(entry.path);
          const background = getFileEntryBackground({ selected });
          const shownName = trashShownName(entry);
          const folder = trashOriginalFolder(entry);
          return (
            <div
              aria-selected={selected}
              className={`file-row hover-lift${isSelecting ? " hover-lift--disabled" : ""}`}
              data-file-card="true"
              data-file-path={entry.path}
              draggable
              key={entry.path}
              onClick={(event) => {
                event.stopPropagation();
                select(event, entry.path);
              }}
              onDoubleClick={(event) => {
                event.stopPropagation();
                onOpen(entry.path);
              }}
              onDragEnd={handleDragEnd}
              onDragStart={(event) => handleDragStart(event, entry.path)}
              role="option"
              style={
                {
                  "--file-row-bg": background,
                  "--file-row-bg-hover":
                    getFileEntryHoverBackground(background),
                  alignItems: "center",
                  borderRadius: "var(--app-radius-base)",
                  cursor: "pointer",
                  display: "grid",
                  flexShrink: 0,
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
        })}
        {isSelecting && selectionBox && (
          <SelectionBox
            height={selectionBox.height}
            left={selectionBox.left}
            top={selectionBox.top}
            width={selectionBox.width}
          />
        )}
      </div>
    </div>
  );
};

export default TrashList;
