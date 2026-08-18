import type { UniqueIdentifier } from "@dnd-kit/core";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Icon } from "@iconify/react";
import type {
  Cell,
  ExpandedState,
  HeaderGroup,
  OnChangeFn,
  Row,
  RowData,
  SortingState,
} from "@tanstack/react-table";
import {
  Fragment,
  memo,
  useCallback,
  type CSSProperties,
  type HTMLAttributes,
  type MouseEvent,
  type ReactNode,
  type Ref,
} from "react";

import type {
  AppDataTableCellRenderKey,
  AppDataTableColumnDef,
  AppTableFeatures,
} from "@/components/tables/AppDataTable.types";
import { rowBodyDragListeners, targetIsRowControl } from "@/components/tables/rowInteraction";
import {
  AppTableHeader,
  AppTableShell,
  MemoizedAppTableCell,
  TableDndBoundary,
  TableEmptyState,
  TableExpandCell,
  areCellRenderKeysEqual,
  columnTrack,
  useAppTableInstance,
  useRowClickGestures,
  useTableGestureKeys,
} from "@/components/tables/tableShared";
import AppCollapse from "@/components/ui/AppCollapse";
import type { ReorderableSurfaceDndProps } from "@/hooks/useReorderableSurface";
import { mergeRefs } from "@/utils/mergeRefs";

// Shared by both AppDataTable (non-virtualized) and AppVirtualDataTable —
// the app-dt__* prefix and --app-dt-* custom properties are not scoped to
// either table alone.
import "./app-data-table.css";
import "../reorder/reorder.css";

export type {
  AppDataTableBreakpoint,
  AppDataTableColumnDef,
  AppDataTableColumnMeta,
} from "@/components/tables/AppDataTable.types";

export type AppDataTableRowAttributes = HTMLAttributes<HTMLDivElement> & {
  ref?: Ref<HTMLDivElement>;
};

export interface AppDataTableRowRenderProps<TData extends RowData> {
  cells: ReactNode;
  dragHandle?: ReactNode;
  isSelected: boolean;
  row: Row<AppTableFeatures, TData>;
  rowIndex: number;
  rowProps: AppDataTableRowAttributes;
}

export interface AppDataTableDndOptions<TData extends RowData> {
  /**
   * `DndContext` props for the surface. The table mounts the context itself so a
   * reorderable table stays a one-prop change at the call site.
   */
  contextProps: ReorderableSurfaceDndProps;
  getItemId: (row: Row<AppTableFeatures, TData>) => UniqueIdentifier;
  /**
   * Groups adjacent rows under one sortable transform. The row whose
   * `isRowSortable` result is true owns the drag gesture; the other rows stay
   * inert but travel with it. Rows that return no id keep the normal one-row
   * sortable behavior.
   */
  getSortableGroupId?: (
    row: Row<AppTableFeatures, TData>,
  ) => UniqueIdentifier | null | undefined;
  /** Every id in the surface, in saved order — the `SortableContext` items. */
  itemIds: UniqueIdentifier[];
  /**
   * Layout mode is open: rows show a drag handle and stop reacting to clicks.
   * Rows are draggable whenever `dnd` is supplied — this only controls the
   * chrome, because the hold that opens layout mode must reach dnd-kit first.
   */
  editing?: boolean;
  enabled?: boolean;
  handleAriaLabel?: string;
  handleColumnWidth?: string | number;
  /**
   * Rows this returns false for render as plain rows: synthetic rows a caller
   * interleaves with its data — group headers — that must not register with
   * dnd-kit. Omitting it keeps every row sortable.
   */
  isRowSortable?: (row: Row<AppTableFeatures, TData>) => boolean;
  /** Row currently being held, before the hold completes. */
  pendingItemId?: UniqueIdentifier | null;
}

export interface AppDataTableProps<TData extends RowData> {
  ariaLabel?: string;
  className?: string;
  columns: AppDataTableColumnDef<TData>[];
  data: TData[];
  density?: "comfortable" | "compact";
  dnd?: AppDataTableDndOptions<TData>;
  emptyMessage?: string;
  enableSorting?: boolean;
  expanded?: ExpandedState;
  fillAvailable?: boolean;
  getRowCanExpand?: (row: Row<AppTableFeatures, TData>) => boolean;
  getRowAttributes?: (
    row: Row<AppTableFeatures, TData>,
  ) => AppDataTableRowAttributes;
  getRowId: (
    row: TData,
    index: number,
    parent?: Row<AppTableFeatures, TData>,
  ) => string;
  height?: CSSProperties["height"];
  manualSorting?: boolean;
  maxHeight?: CSSProperties["maxHeight"];
  onExpandedChange?: OnChangeFn<ExpandedState>;
  onRowClick?: (row: Row<AppTableFeatures, TData>, event: MouseEvent) => void;
  onRowContextMenu?: (
    row: Row<AppTableFeatures, TData>,
    event: MouseEvent,
  ) => void;
  onRowDoubleClick?: (
    row: Row<AppTableFeatures, TData>,
    event: MouseEvent,
  ) => void;
  /**
   * Clear whatever the table's rows have selected. Supplying it is what opts a
   * table into the second stage of Escape — see `docs/table-row-gestures.md`.
   */
  onClearSelection?: () => void;
  /**
   * Select every row the current filter and sort leave visible. Supplying it is
   * what opts a table into Ctrl/Cmd-A.
   */
  onSelectAll?: (rowIds: string[]) => void;
  onSortingChange?: OnChangeFn<SortingState>;
  renderExpandedContent?: (row: Row<AppTableFeatures, TData>) => ReactNode;
  renderRow?: (props: AppDataTableRowRenderProps<TData>) => ReactNode;
  /** The one open/focused row. Use `selectedRowIds` for multi-selection. */
  selectedRowId?: string | null;
  /**
   * Rows a multi-select table has selected. They carry the same primary-tinted
   * surface as `selectedRowId`, so selection reads the same everywhere.
   */
  selectedRowIds?: ReadonlySet<string>;
  showHeader?: boolean;
  sorting?: SortingState;
  style?: CSSProperties;
  variant?: "default" | "embedded";
}

function areVersionArraysEqual(
  previous: readonly unknown[],
  next: readonly unknown[],
) {
  if (previous.length !== next.length) return false;
  return previous.every((value, index) => Object.is(value, next[index]));
}

function getCellRenderKey<TData extends RowData>(
  cell: Cell<AppTableFeatures, TData>,
  rowIndex: number,
) {
  return (
    cell.column.columnDef.meta?.getCellRenderKey?.(
      cell.row.original,
      rowIndex,
    ) ?? cell.row.original
  );
}

interface AppDataTableCellModel<TData extends RowData> {
  cell: Cell<AppTableFeatures, TData>;
  columnDef: AppDataTableColumnDef<TData>;
  renderKey: AppDataTableCellRenderKey;
  rowIndex: number;
}

function areCellModelsEqual<TData extends RowData>(
  previous: AppDataTableCellModel<TData>[],
  next: AppDataTableCellModel<TData>[],
) {
  if (previous.length !== next.length) return false;
  return previous.every((cell, index) => {
    const nextCell = next[index];
    return (
      cell.cell.id === nextCell.cell.id &&
      cell.columnDef === nextCell.columnDef &&
      cell.rowIndex === nextCell.rowIndex &&
      areCellRenderKeysEqual(cell.renderKey, nextCell.renderKey)
    );
  });
}

interface AppDataTableBodyRowProps<TData extends RowData> {
  canExpand: boolean;
  cells: AppDataTableCellModel<TData>[];
  dragHandle?: ReactNode;
  getRowAttributes?: (
    row: Row<AppTableFeatures, TData>,
  ) => AppDataTableRowAttributes;
  hasDragColumn: boolean;
  hasExpandColumn: boolean;
  isExpanded: boolean;
  isInteractive: boolean;
  isSelected: boolean;
  onRowClick?: (row: Row<AppTableFeatures, TData>, event: MouseEvent) => void;
  onRowContextMenu?: (
    row: Row<AppTableFeatures, TData>,
    event: MouseEvent,
  ) => void;
  onRowDoubleClick?: (
    row: Row<AppTableFeatures, TData>,
    event: MouseEvent,
  ) => void;
  renderRow?: (props: AppDataTableRowRenderProps<TData>) => ReactNode;
  row: Row<AppTableFeatures, TData>;
  rowAttributes?: AppDataTableRowAttributes;
  rowIndex: number;
}

function AppDataTableBodyRow<TData extends RowData>({
  canExpand,
  cells,
  dragHandle,
  getRowAttributes,
  hasDragColumn,
  hasExpandColumn,
  isExpanded,
  isInteractive,
  isSelected,
  onRowClick,
  onRowContextMenu,
  onRowDoubleClick,
  renderRow,
  row,
  rowAttributes: providedRowAttributes,
  rowIndex,
}: AppDataTableBodyRowProps<TData>) {
  const rowAttributes =
    providedRowAttributes ?? getRowAttributes?.(row) ?? undefined;
  const rowAttributeOnClick = rowAttributes?.onClick;
  const rowAttributeOnContextMenu = rowAttributes?.onContextMenu;
  const rowAttributeOnDoubleClick = rowAttributes?.onDoubleClick;
  const rowAttributeOnMouseDown = rowAttributes?.onMouseDown;
  const renderedCells = (
    <>
      {hasDragColumn && (
        <div className="app-dt__cell app-dt__cell--drag" role="cell">
          {dragHandle}
        </div>
      )}
      {cells.map((cell) => (
        <MemoizedAppTableCell
          cell={cell.cell}
          columnDef={cell.columnDef}
          key={cell.cell.id}
          renderKey={cell.renderKey}
          rowIndex={cell.rowIndex}
        />
      ))}
      {hasExpandColumn && (
        <TableExpandCell
          canExpand={canExpand}
          isExpanded={isExpanded}
          onToggle={() => row.toggleExpanded()}
        />
      )}
    </>
  );
  const rowProps: AppDataTableRowAttributes = {
    ...rowAttributes,
    className: [
      "app-dt__row",
      "app-dt__row--body",
      isInteractive && "app-dt__row--interactive",
      isSelected && "app-dt__row--selected",
      rowIndex % 2 === 1 && "app-dt__row--alt",
      rowAttributes?.className,
    ]
      .filter(Boolean)
      .join(" "),
    onClick: (event) => {
      rowAttributeOnClick?.(event);
      if (!event.defaultPrevented) onRowClick?.(row, event);
    },
    onContextMenu: (event) => {
      rowAttributeOnContextMenu?.(event);
      if (!event.defaultPrevented) onRowContextMenu?.(row, event);
    },
    onDoubleClick: (event) => {
      rowAttributeOnDoubleClick?.(event);
      if (!event.defaultPrevented) onRowDoubleClick?.(row, event);
    },
    // The second mousedown of a double click is what starts the browser's word
    // selection. A table that binds a double-click row gesture means something
    // else by it, so that selection is litter left over the row it just acted
    // on. Suppressing it here rather than clearing it afterwards avoids the
    // highlight flashing up at all, and single-click drag-selection — how you
    // copy an id out of a cell — is untouched.
    onMouseDown: (event) => {
      if (
        onRowDoubleClick &&
        event.detail > 1 &&
        !targetIsRowControl(event.target)
      ) {
        event.preventDefault();
      }
      rowAttributeOnMouseDown?.(event);
    },
    role: "row",
    style: rowAttributes?.style,
    "aria-expanded": canExpand ? isExpanded : undefined,
  };

  if (renderRow) {
    return renderRow({
      cells: renderedCells,
      dragHandle,
      isSelected,
      row,
      rowIndex,
      rowProps,
    });
  }

  return <div {...rowProps}>{renderedCells}</div>;
}

const MemoizedAppDataTableBodyRow = memo(
  AppDataTableBodyRow,
  (previous, next) =>
    previous.canExpand === next.canExpand &&
    areCellModelsEqual(previous.cells, next.cells) &&
    previous.dragHandle === next.dragHandle &&
    previous.getRowAttributes === next.getRowAttributes &&
    previous.hasDragColumn === next.hasDragColumn &&
    previous.hasExpandColumn === next.hasExpandColumn &&
    previous.isExpanded === next.isExpanded &&
    previous.isInteractive === next.isInteractive &&
    previous.isSelected === next.isSelected &&
    previous.onRowClick === next.onRowClick &&
    previous.onRowContextMenu === next.onRowContextMenu &&
    previous.onRowDoubleClick === next.onRowDoubleClick &&
    previous.renderRow === next.renderRow &&
    previous.row === next.row &&
    previous.rowAttributes === next.rowAttributes &&
    previous.rowIndex === next.rowIndex,
) as typeof AppDataTableBodyRow;

interface AppDataTableSortableBodyRowProps<TData extends RowData> extends Omit<
  AppDataTableBodyRowProps<TData>,
  "dragHandle" | "hasDragColumn" | "rowAttributes"
> {
  dnd: AppDataTableDndOptions<TData>;
}

interface AppDataTableBodyRowEntry<TData extends RowData> {
  canExpand: boolean;
  cells: AppDataTableCellModel<TData>[];
  isExpanded: boolean;
  isInteractive: boolean;
  isSelected: boolean;
  row: Row<AppTableFeatures, TData>;
  rowIndex: number;
}

function AppDataTableSortableBodyRow<TData extends RowData>({
  canExpand,
  cells,
  dnd,
  getRowAttributes,
  hasExpandColumn,
  isExpanded,
  isInteractive,
  isSelected,
  onRowClick,
  onRowContextMenu,
  onRowDoubleClick,
  renderRow,
  row,
  rowIndex,
}: AppDataTableSortableBodyRowProps<TData>) {
  const {
    attributes,
    isDragging,
    listeners,
    setNodeRef,
    transform,
    transition,
  } = useSortable({
    id: dnd.getItemId(row),
    disabled: dnd.enabled === false,
  });
  const isArmed = dnd.enabled !== false;
  const isEditing = isArmed && (dnd.editing ?? false);
  const isPending =
    dnd.pendingItemId != null && dnd.pendingItemId === dnd.getItemId(row);
  // Table entries can have different heights (an expanded stack beside a
  // single row). dnd-kit's scale component fits the active entry to the drop
  // target and visibly squashes or stretches it, so rows translate only.
  const transformValue = CSS.Translate.toString(transform);
  const rowAttributes = getRowAttributes?.(row);
  const rowTransition = [rowAttributes?.style?.transition, transition]
    .filter(Boolean)
    .join(", ");
  const dragHandle = (
    <span
      {...attributes}
      {...listeners}
      aria-label={dnd.handleAriaLabel ?? "Reorder row"}
      className="app-dt__drag-handle"
    >
      <Icon height={20} icon="mdi:drag" width={20} />
    </span>
  );

  return (
    <AppDataTableBodyRow
      canExpand={canExpand}
      cells={cells}
      dragHandle={isEditing ? dragHandle : undefined}
      hasDragColumn={isEditing}
      hasExpandColumn={hasExpandColumn}
      isExpanded={isExpanded}
      isInteractive={isInteractive}
      isSelected={isSelected}
      onRowClick={onRowClick}
      onRowContextMenu={onRowContextMenu}
      onRowDoubleClick={onRowDoubleClick}
      renderRow={renderRow}
      row={row}
      rowIndex={rowIndex}
      rowAttributes={{
        // The whole row carries the press listeners, not just the handle: the
        // handle only exists once layout mode is open, and the hold is what
        // opens it. A press on one of the row's own controls is exempt.
        ...(isArmed ? rowBodyDragListeners(listeners) : undefined),
        ...rowAttributes,
        className: [
          rowAttributes?.className,
          isPending && "app-dt__row--reorder-pending",
          isEditing && "app-dt__row--reordering",
        ]
          .filter(Boolean)
          .join(" "),
        ref: mergeRefs(rowAttributes?.ref, setNodeRef),
        style: {
          ...rowAttributes?.style,
          opacity: isDragging ? 0.45 : rowAttributes?.style?.opacity,
          transform: transformValue || rowAttributes?.style?.transform,
          transition: rowTransition || undefined,
        },
      }}
    />
  );
}

interface AppDataTableSortableBodyGroupProps<TData extends RowData> {
  columnCount: number;
  dnd: AppDataTableDndOptions<TData>;
  entries: AppDataTableBodyRowEntry<TData>[];
  getRowAttributes?: (
    row: Row<AppTableFeatures, TData>,
  ) => AppDataTableRowAttributes;
  groupId: UniqueIdentifier;
  hasDragColumn: boolean;
  hasExpandColumn: boolean;
  onRowClick?: (row: Row<AppTableFeatures, TData>, event: MouseEvent) => void;
  onRowContextMenu?: (
    row: Row<AppTableFeatures, TData>,
    event: MouseEvent,
  ) => void;
  onRowDoubleClick?: (
    row: Row<AppTableFeatures, TData>,
    event: MouseEvent,
  ) => void;
  renderExpandedContent?: (row: Row<AppTableFeatures, TData>) => ReactNode;
  renderRow?: (props: AppDataTableRowRenderProps<TData>) => ReactNode;
}

/**
 * One sortable node containing a leader row and its inert followers. A table
 * stack uses this so measuring, translating, and fading its header also covers
 * the member-card rows beneath it, while the gesture still starts only from
 * the header.
 */
function AppDataTableSortableBodyGroup<TData extends RowData>({
  columnCount,
  dnd,
  entries,
  getRowAttributes,
  groupId,
  hasDragColumn,
  hasExpandColumn,
  onRowClick,
  onRowContextMenu,
  onRowDoubleClick,
  renderExpandedContent,
  renderRow,
}: AppDataTableSortableBodyGroupProps<TData>) {
  const leaderIndex = entries.findIndex(
    ({ row }) => dnd.isRowSortable?.(row) ?? true,
  );
  const {
    attributes,
    isDragging,
    listeners,
    setNodeRef,
    transform,
    transition,
  } = useSortable({
    id: groupId,
    disabled: dnd.enabled === false || leaderIndex < 0,
  });
  const isArmed = dnd.enabled !== false;
  const isEditing = isArmed && (dnd.editing ?? false);
  const isPending = dnd.pendingItemId === groupId;
  const dragHandle = (
    <span
      {...attributes}
      {...listeners}
      aria-label={dnd.handleAriaLabel ?? "Reorder row"}
      className="app-dt__drag-handle"
    >
      <Icon height={20} icon="mdi:drag" width={20} />
    </span>
  );

  return (
    <div
      className="app-dt__sortable-group"
      ref={setNodeRef}
      role="presentation"
      style={{
        opacity: isDragging ? 0.45 : undefined,
        // Keep the multi-row block at its measured dimensions while it crosses
        // ordinary rows; only its position should change during the drag.
        transform: CSS.Translate.toString(transform),
        transition,
        zIndex: isDragging ? 1 : undefined,
      }}
    >
      {entries.map((entry, index) => {
        const rowAttributes = getRowAttributes?.(entry.row);
        const isLeader = index === leaderIndex;
        const sortableRowAttributes = isLeader
          ? {
              ...(isArmed ? rowBodyDragListeners(listeners) : undefined),
              ...rowAttributes,
              className: [
                rowAttributes?.className,
                isPending && "app-dt__row--reorder-pending",
                isEditing && "app-dt__row--reordering",
              ]
                .filter(Boolean)
                .join(" "),
            }
          : rowAttributes;

        return (
          <Fragment key={entry.row.id}>
            <AppDataTableBodyRow
              canExpand={entry.canExpand}
              cells={entry.cells}
              dragHandle={isLeader && isEditing ? dragHandle : undefined}
              hasDragColumn={hasDragColumn}
              hasExpandColumn={hasExpandColumn}
              isExpanded={entry.isExpanded}
              isInteractive={entry.isInteractive}
              isSelected={entry.isSelected}
              onRowClick={onRowClick}
              onRowContextMenu={onRowContextMenu}
              onRowDoubleClick={onRowDoubleClick}
              renderRow={renderRow}
              row={entry.row}
              rowAttributes={sortableRowAttributes}
              rowIndex={entry.rowIndex}
            />
            {renderExpandedContent && (
              <MemoizedAppDataTableExpandedRow
                columnCount={columnCount}
                isExpanded={entry.isExpanded}
                renderExpandedContent={renderExpandedContent}
                row={entry.row}
              />
            )}
          </Fragment>
        );
      })}
    </div>
  );
}

interface AppDataTableHeaderProps<TData extends RowData> {
  // These version props keep same-ID renderer and sorting changes visible to a
  // memoized header even when TanStack can reuse its header-group objects.
  columnVersion: AppDataTableColumnDef<TData>[];
  hasDragColumn: boolean;
  hasExpandColumn: boolean;
  headerGroups: HeaderGroup<AppTableFeatures, TData>[];
  sortingVersion: SortingState;
}

function AppDataTableHeader<TData extends RowData>({
  hasDragColumn,
  hasExpandColumn,
  headerGroups,
}: AppDataTableHeaderProps<TData>) {
  return (
    <AppTableHeader
      hasDragColumn={hasDragColumn}
      hasExpandColumn={hasExpandColumn}
      headerGroups={headerGroups}
    />
  );
}

const MemoizedAppDataTableHeader = memo(
  AppDataTableHeader,
  (previous, next) =>
    areVersionArraysEqual(previous.columnVersion, next.columnVersion) &&
    previous.hasDragColumn === next.hasDragColumn &&
    previous.hasExpandColumn === next.hasExpandColumn &&
    previous.headerGroups === next.headerGroups &&
    previous.sortingVersion === next.sortingVersion,
) as typeof AppDataTableHeader;

interface AppDataTableExpandedContentProps<TData extends RowData> {
  columnCount: number;
  renderExpandedContent: (row: Row<AppTableFeatures, TData>) => ReactNode;
  row: Row<AppTableFeatures, TData>;
}

function AppDataTableExpandedContent<TData extends RowData>({
  columnCount,
  renderExpandedContent,
  row,
}: AppDataTableExpandedContentProps<TData>) {
  return (
    <div className="app-dt__detail" role="row">
      <div
        aria-colspan={columnCount}
        className="app-dt__detail-cell"
        role="cell"
      >
        {renderExpandedContent(row)}
      </div>
    </div>
  );
}

const MemoizedAppDataTableExpandedContent = memo(
  AppDataTableExpandedContent,
) as typeof AppDataTableExpandedContent;

interface AppDataTableExpandedRowProps<
  TData extends RowData,
> extends AppDataTableExpandedContentProps<TData> {
  isExpanded: boolean;
}

function AppDataTableExpandedRow<TData extends RowData>({
  columnCount,
  isExpanded,
  renderExpandedContent,
  row,
}: AppDataTableExpandedRowProps<TData>) {
  return (
    <AppCollapse in={isExpanded} unmountOnExit>
      <MemoizedAppDataTableExpandedContent
        columnCount={columnCount}
        renderExpandedContent={renderExpandedContent}
        row={row}
      />
    </AppCollapse>
  );
}

const MemoizedAppDataTableExpandedRow = memo(
  AppDataTableExpandedRow,
) as typeof AppDataTableExpandedRow;

function AppDataTable<TData extends RowData>({
  ariaLabel = "Data table",
  className,
  columns,
  data,
  density = "comfortable",
  dnd,
  emptyMessage = "No data available.",
  enableSorting = false,
  expanded,
  fillAvailable = false,
  getRowCanExpand,
  getRowAttributes,
  getRowId,
  height,
  manualSorting = false,
  maxHeight,
  onClearSelection,
  onExpandedChange,
  onRowClick,
  onRowContextMenu,
  onRowDoubleClick,
  onSelectAll,
  onSortingChange,
  renderExpandedContent,
  renderRow,
  selectedRowId,
  selectedRowIds,
  showHeader = true,
  sorting,
  style,
  variant = "default",
}: AppDataTableProps<TData>) {
  const { dndOptions, resolvedSorting, table } = useAppTableInstance({
    columns,
    data,
    dnd,
    enableSorting,
    expanded,
    getRowCanExpand,
    getRowId,
    manualSorting,
    onExpandedChange,
    onSortingChange,
    renderExpandedContent,
    sorting,
  });

  const rows = table.getRowModel().rows;

  useTableGestureKeys({
    hasExpandableRows: Boolean(renderExpandedContent),
    onClearSelection,
    onSelectAll,
    table,
  });

  const isInteractive = Boolean(onRowClick || onRowDoubleClick);
  const hasExpandColumn = Boolean(renderExpandedContent);
  // Only layout mode adds the handle column; an armed-but-idle table keeps its
  // normal column widths.
  const hasDragColumn =
    Boolean(dndOptions?.editing) && dndOptions?.enabled !== false;
  // The whole row is the disclosure control: clicking anywhere on it opens or
  // closes the detail panel, so the chevron reads as a hint rather than the
  // only target. A table that gives a row click another meaning keeps it —
  // its handler wins — and in layout mode the press belongs to the drag.
  const rowClickExpands = hasExpandColumn && !onRowClick && !hasDragColumn;
  const toggleExpanded = useCallback((row: Row<AppTableFeatures, TData>) => {
    row.toggleExpanded();
  }, []);
  const { handleRowClick, handleRowDoubleClick } = useRowClickGestures({
    onRowClick,
    onRowDoubleClick,
    rowClickExpands,
    toggleExpanded,
  });

  const headerGroups = table.getHeaderGroups();
  const visibleColumns = table.getVisibleLeafColumns();
  // TanStack can preserve Column objects while swapping their definitions.
  // Snapshot the definitions so memo comparators observe that version change
  // without invalidating rows merely because this array itself is new.
  const visibleColumnDefinitions = visibleColumns.map(
    (column) => column.columnDef,
  );
  const gridTemplate = [
    ...(hasDragColumn
      ? [
          typeof dndOptions?.handleColumnWidth === "number"
            ? `${dndOptions.handleColumnWidth}px`
            : (dndOptions?.handleColumnWidth ?? "32px"),
        ]
      : []),
    ...visibleColumns.map((column) => columnTrack(column)),
    ...(hasExpandColumn ? ["40px"] : []),
  ].join(" ");
  const bodyRows: AppDataTableBodyRowEntry<TData>[] = rows.map(
    (row, rowIndex) => {
      const isExpanded = row.getIsExpanded();
      const canExpand = row.getCanExpand();
      return {
        canExpand,
        cells: row.getVisibleCells().map((cell) => ({
          cell,
          columnDef: cell.column.columnDef,
          renderKey: getCellRenderKey(cell, rowIndex),
          rowIndex,
        })),
        isExpanded,
        isInteractive: isInteractive || (rowClickExpands && canExpand),
        isSelected:
          row.id === selectedRowId || (selectedRowIds?.has(row.id) ?? false),
        row,
        rowIndex,
      };
    },
  );
  const bodyGroups: {
    entries: AppDataTableBodyRowEntry<TData>[];
    groupId?: UniqueIdentifier;
  }[] = [];
  for (const entry of bodyRows) {
    const groupId = dndOptions?.getSortableGroupId?.(entry.row) ?? undefined;
    const previous = bodyGroups.at(-1);
    if (groupId !== undefined && previous?.groupId === groupId) {
      previous.entries.push(entry);
    } else {
      bodyGroups.push({ entries: [entry], groupId });
    }
  }
  const columnCount = visibleColumns.length + (hasExpandColumn ? 1 : 0);

  return (
    <TableDndBoundary dnd={dndOptions}>
      <AppTableShell
        ariaLabel={ariaLabel}
        className={["app-dt--normal", className].filter(Boolean).join(" ")}
        density={density}
        fillAvailable={fillAvailable}
        gridTemplate={gridTemplate}
        height={height}
        maxHeight={maxHeight}
        style={style}
        variant={variant}
      >
        {showHeader && (
          <MemoizedAppDataTableHeader
            columnVersion={visibleColumnDefinitions}
            hasDragColumn={hasDragColumn}
            hasExpandColumn={hasExpandColumn}
            headerGroups={headerGroups}
            sortingVersion={resolvedSorting}
          />
        )}

        <div className="app-dt__scroll custom-scrollbar" role="presentation">
          <div className="app-dt__body" role="rowgroup">
            {bodyGroups.map(({ entries, groupId }) => {
              if (dndOptions && groupId !== undefined) {
                return (
                  <AppDataTableSortableBodyGroup
                    columnCount={columnCount}
                    dnd={dndOptions}
                    entries={entries}
                    getRowAttributes={getRowAttributes}
                    groupId={groupId}
                    hasDragColumn={hasDragColumn}
                    hasExpandColumn={hasExpandColumn}
                    key={`sortable-group:${String(groupId)}:${entries[0].row.id}`}
                    onRowClick={handleRowClick}
                    onRowContextMenu={onRowContextMenu}
                    onRowDoubleClick={handleRowDoubleClick}
                    renderExpandedContent={renderExpandedContent}
                    renderRow={renderRow}
                  />
                );
              }

              const entry = entries[0];
              const { row } = entry;
              return (
                <Fragment key={row.id}>
                  {dndOptions && (dndOptions.isRowSortable?.(row) ?? true) ? (
                    <AppDataTableSortableBodyRow
                      canExpand={entry.canExpand}
                      cells={entry.cells}
                      dnd={dndOptions}
                      getRowAttributes={getRowAttributes}
                      hasExpandColumn={hasExpandColumn}
                      isExpanded={entry.isExpanded}
                      isInteractive={entry.isInteractive}
                      isSelected={entry.isSelected}
                      onRowClick={handleRowClick}
                      onRowContextMenu={onRowContextMenu}
                      onRowDoubleClick={handleRowDoubleClick}
                      renderRow={renderRow}
                      row={row}
                      rowIndex={entry.rowIndex}
                    />
                  ) : (
                    <MemoizedAppDataTableBodyRow
                      canExpand={entry.canExpand}
                      cells={entry.cells}
                      getRowAttributes={getRowAttributes}
                      // A non-sortable row amid sortable ones still renders the
                      // empty handle cell so its columns stay on the grid.
                      hasDragColumn={hasDragColumn}
                      hasExpandColumn={hasExpandColumn}
                      isExpanded={entry.isExpanded}
                      isInteractive={entry.isInteractive}
                      isSelected={entry.isSelected}
                      onRowClick={handleRowClick}
                      onRowContextMenu={onRowContextMenu}
                      onRowDoubleClick={handleRowDoubleClick}
                      renderRow={renderRow}
                      row={row}
                      rowIndex={entry.rowIndex}
                    />
                  )}
                  {renderExpandedContent && (
                    <MemoizedAppDataTableExpandedRow
                      columnCount={columnCount}
                      isExpanded={entry.isExpanded}
                      renderExpandedContent={renderExpandedContent}
                      row={row}
                    />
                  )}
                </Fragment>
              );
            })}
          </div>

          {rows.length === 0 && <TableEmptyState message={emptyMessage} />}
        </div>
      </AppTableShell>
    </TableDndBoundary>
  );
}

const MemoizedAppDataTable = memo(AppDataTable) as typeof AppDataTable;

export default MemoizedAppDataTable;
