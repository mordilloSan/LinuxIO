import { DndContext, type UniqueIdentifier } from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Icon } from "@iconify/react";
import { flexRender, useTable } from "@tanstack/react-table";
import type {
  Cell,
  Column,
  ColumnVisibilityState,
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
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type HTMLAttributes,
  type MouseEvent,
  type ReactNode,
  type Ref,
} from "react";

import { appTableFeatures } from "@/components/tables/AppDataTable.types";
import type {
  AppDataTableBreakpoint,
  AppDataTableCellRenderKey,
  AppDataTableColumnDef,
  AppTableFeatures,
} from "@/components/tables/AppDataTable.types";
import {
  ROW_DOUBLE_CLICK_MS,
  clickTargetsRowBody,
  rowBodyDragListeners,
  targetIsRowControl,
} from "@/components/tables/rowInteraction";
import AppCollapse from "@/components/ui/AppCollapse";
import { OVERLAY_ROOT_SELECTOR } from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { REORDER_HOLD_MS } from "@/constants/reorder";
import type { ReorderableSurfaceDndProps } from "@/hooks/useReorderableSurface";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import {
  EASING_STANDARD_CSS,
  TRANSITION_DURATION_STANDARD_MS,
  shadowSm,
} from "@/theme/constants";
import { alpha } from "@/utils/color";
import { isTypingTarget } from "@/utils/keyboardTarget";
import { mergeRefs } from "@/utils/mergeRefs";

// Shared by both AppDataTable (non-virtualized) and AppVirtualDataTable —
// the app-dt__* prefix and --app-dt-* custom properties are not scoped to
// either table alone.
import "./app-data-table.css";
import "../reorder/reorder.css";

const DETAIL_ANIMATION_CSS = `${TRANSITION_DURATION_STANDARD_MS}ms ${EASING_STANDARD_CSS}`;

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

function columnTrack<TData extends RowData>(
  column: Column<AppTableFeatures, TData>,
) {
  const width = column.columnDef.meta?.width;
  if (typeof width === "number") return `${width}px`;
  if (typeof width === "string" && width.trim()) return width;
  return "minmax(0, 1fr)";
}

function alignToJustify(align?: "left" | "center" | "right") {
  if (align === "center") return "center";
  if (align === "right") return "flex-end";
  return "flex-start";
}

function getColumnDefId<TData extends RowData>(
  column: AppDataTableColumnDef<TData>,
  index: number,
) {
  const candidate = column as {
    accessorKey?: string | number;
    id?: string;
  };

  if (candidate.id) return candidate.id;
  if (candidate.accessorKey !== undefined) return String(candidate.accessorKey);
  return `column-${index}`;
}

function getSortIcon(sortState: false | "asc" | "desc") {
  if (sortState === "asc") return "mdi:chevron-up";
  if (sortState === "desc") return "mdi:chevron-down";
  return "mdi:unfold-more-horizontal";
}

function areCellRenderKeysEqual(
  previous: AppDataTableCellRenderKey,
  next: AppDataTableCellRenderKey,
) {
  if (Object.is(previous, next)) return true;
  if (!Array.isArray(previous) || !Array.isArray(next)) return false;
  if (previous.length !== next.length) return false;
  return previous.every((value, index) => Object.is(value, next[index]));
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

interface AppDataTableCellProps<TData extends RowData> {
  cell: Cell<AppTableFeatures, TData>;
  columnDef: AppDataTableColumnDef<TData>;
  renderKey: AppDataTableCellRenderKey;
  rowIndex: number;
}

type AppDataTableCellModel<TData extends RowData> =
  AppDataTableCellProps<TData>;

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

function AppDataTableCell<TData extends RowData>({
  cell,
  columnDef,
}: AppDataTableCellProps<TData>) {
  const meta = columnDef.meta;

  return (
    <div
      className={["app-dt__cell", meta?.className, meta?.cellClassName]
        .filter(Boolean)
        .join(" ")}
      role="cell"
      style={{
        justifyContent: alignToJustify(meta?.align),
        textAlign: meta?.align,
        ...meta?.style,
        ...meta?.cellStyle,
      }}
    >
      {flexRender(columnDef.cell, cell.getContext())}
    </div>
  );
}

const MemoizedAppDataTableCell = memo(
  AppDataTableCell,
  (previous, next) =>
    previous.cell.id === next.cell.id &&
    previous.columnDef === next.columnDef &&
    previous.rowIndex === next.rowIndex &&
    areCellRenderKeysEqual(previous.renderKey, next.renderKey),
) as typeof AppDataTableCell;

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
        <MemoizedAppDataTableCell
          cell={cell.cell}
          columnDef={cell.columnDef}
          key={cell.cell.id}
          renderKey={cell.renderKey}
          rowIndex={cell.rowIndex}
        />
      ))}
      {hasExpandColumn && (
        <div className="app-dt__cell app-dt__cell--expand" role="cell">
          {canExpand && (
            <AppTooltip title={isExpanded ? "Collapse row" : "Expand row"}>
              <AppIconButton
                aria-expanded={isExpanded}
                aria-label={isExpanded ? "Collapse row" : "Expand row"}
                onClick={(event) => {
                  event.stopPropagation();
                  row.toggleExpanded();
                }}
                size="small"
              >
                <Icon
                  height={22}
                  icon="mdi:chevron-down"
                  style={{
                    transform: isExpanded ? "rotate(180deg)" : "rotate(0deg)",
                    transition: `transform ${DETAIL_ANIMATION_CSS}`,
                  }}
                  width={22}
                />
              </AppIconButton>
            </AppTooltip>
          )}
        </div>
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
  const transformValue = CSS.Transform.toString(transform);
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
    <div className="app-dt__head" role="rowgroup">
      {headerGroups.map((headerGroup) => (
        <div
          className="app-dt__row app-dt__row--head"
          key={headerGroup.id}
          role="row"
        >
          {hasDragColumn && (
            <div
              aria-hidden="true"
              className="app-dt__cell app-dt__cell--head app-dt__cell--drag"
              role="columnheader"
            />
          )}
          {headerGroup.headers.map((header) => {
            const meta = header.column.columnDef.meta;
            const sortState = header.column.getIsSorted();
            const canSort = header.column.getCanSort();

            return (
              <div
                className={[
                  "app-dt__cell",
                  "app-dt__cell--head",
                  meta?.className,
                  meta?.headerClassName,
                ]
                  .filter(Boolean)
                  .join(" ")}
                key={header.id}
                role="columnheader"
                style={{
                  justifyContent: alignToJustify(meta?.align),
                  textAlign: meta?.align,
                  ...meta?.style,
                  ...meta?.headerStyle,
                }}
              >
                {header.isPlaceholder ? null : canSort ? (
                  <button
                    className="app-dt__sort-button"
                    onClick={header.column.getToggleSortingHandler()}
                    type="button"
                  >
                    <span className="app-dt__sort-label">
                      {flexRender(
                        header.column.columnDef.header,
                        header.getContext(),
                      )}
                    </span>
                    <Icon
                      height={16}
                      icon={getSortIcon(sortState)}
                      width={16}
                    />
                  </button>
                ) : (
                  flexRender(
                    header.column.columnDef.header,
                    header.getContext(),
                  )
                )}
              </div>
            );
          })}
          {hasExpandColumn && (
            <div
              aria-hidden="true"
              className="app-dt__cell app-dt__cell--head app-dt__cell--expand"
              role="columnheader"
            />
          )}
        </div>
      ))}
    </div>
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
  const theme = useAppTheme();
  const isDark = theme.palette.mode === "dark";
  const belowSm = useAppMediaQuery(theme.breakpoints.down("sm"));
  const belowMd = useAppMediaQuery(theme.breakpoints.down("md"));
  const belowLg = useAppMediaQuery(theme.breakpoints.down("lg"));
  const belowXl = useAppMediaQuery(theme.breakpoints.down("xl"));
  const [internalExpanded, setInternalExpanded] = useState<ExpandedState>({});
  const [internalSorting, setInternalSorting] = useState<SortingState>([]);
  // The table owns the deferred single click, not the row: rows are memoized and
  // remount when column defs are rebuilt, which would drop a pending expand.
  const pendingExpandRef = useRef<number | null>(null);
  const cancelPendingExpand = useCallback(() => {
    if (pendingExpandRef.current === null) return;
    window.clearTimeout(pendingExpandRef.current);
    pendingExpandRef.current = null;
  }, []);

  useEffect(() => cancelPendingExpand, [cancelPendingExpand]);

  const columnVisibility = useMemo<ColumnVisibilityState>(() => {
    const below: Record<AppDataTableBreakpoint, boolean> = {
      sm: belowSm,
      md: belowMd,
      lg: belowLg,
      xl: belowXl,
    };
    const next: ColumnVisibilityState = {};

    columns.forEach((column, index) => {
      const hideBelow = column.meta?.hideBelow;
      if (!hideBelow) return;
      next[getColumnDefId(column, index)] = !below[hideBelow];
    });

    return next;
  }, [belowLg, belowMd, belowSm, belowXl, columns]);

  const resolvedExpanded = expanded ?? internalExpanded;
  const resolvedSorting = sorting ?? internalSorting;
  // A column sort already decides the row order, and a saved manual order would
  // be invisible underneath it. Sorted tables therefore aren't reorderable at
  // all, rather than accepting drags that appear to do nothing.
  const dndOptions = resolvedSorting.length > 0 ? undefined : dnd;

  const handleExpandedChange: OnChangeFn<ExpandedState> = (updater) => {
    if (expanded === undefined) {
      setInternalExpanded(updater);
    }
    onExpandedChange?.(updater);
  };

  const handleSortingChange: OnChangeFn<SortingState> = (updater) => {
    if (sorting === undefined) {
      setInternalSorting(updater);
    }
    onSortingChange?.(updater);
  };

  const table = useTable({
    features: appTableFeatures,
    autoResetExpanded: false,
    columns,
    data,
    enableSorting,
    enableSortingRemoval: false,
    getRowCanExpand: (row) =>
      Boolean(renderExpandedContent && (getRowCanExpand?.(row) ?? true)),
    getRowId,
    manualSorting,
    onExpandedChange: handleExpandedChange,
    onSortingChange: handleSortingChange,
    state: {
      columnVisibility,
      expanded: resolvedExpanded,
      sorting: resolvedSorting,
    },
  });

  const rows = table.getRowModel().rows;

  const handleTableKeyDown = useEffectEvent(
    (event: globalThis.KeyboardEvent) => {
      if (event.defaultPrevented) return;
      // A dialog owns the keyboard while it is open, the same guard the
      // filebrowser keyboard hooks use.
      if (document.querySelector(OVERLAY_ROOT_SELECTOR)) return;

      // Escape peels one layer of row state at a time: open detail panels
      // first, then the selection. Pressing it twice therefore gets a table
      // back to rest. A table with nothing expanded skips straight to clearing
      // the selection rather than swallowing a press on nothing.
      if (event.key === "Escape") {
        // Read expansion off the table instance rather than a render-time
        // boolean: the listener outlives the render it was created in.
        if (table.getIsSomeRowsExpanded()) {
          table.setExpanded({});
        } else if (onClearSelection) {
          onClearSelection();
        } else {
          return;
        }
        // Claim the press so a nested table, or a page-level handler, leaves
        // it be.
        event.preventDefault();
        return;
      }

      // Ctrl/Cmd-A selects every row the filter and sort leave visible. Shift
      // and Alt are excluded so combos like Ctrl+Shift+A stay inert, and the
      // key is lowercased so CapsLock cannot break the match.
      const isCtrlOrCmd =
        (event.ctrlKey || event.metaKey) && !event.shiftKey && !event.altKey;
      if (!isCtrlOrCmd || event.key.toLowerCase() !== "a") return;
      // Typing keeps its native select-all.
      if (!onSelectAll || isTypingTarget(event)) return;

      event.preventDefault();
      onSelectAll(table.getRowModel().rows.map((row) => row.id));
    },
  );

  useEffect(() => {
    if (!renderExpandedContent && !onClearSelection && !onSelectAll) return;
    window.addEventListener("keydown", handleTableKeyDown);
    return () => window.removeEventListener("keydown", handleTableKeyDown);
  }, [onClearSelection, onSelectAll, renderExpandedContent]);

  const isEmbedded = variant === "embedded";
  const headRowBg = isEmbedded
    ? "transparent"
    : alpha(theme.palette.text.primary, 0.08);
  const selectedBg = alpha(theme.palette.primary.main, isDark ? 0.15 : 0.1);
  const altBg = isEmbedded
    ? "transparent"
    : alpha(theme.palette.text.primary, isDark ? 0.04 : 0.05);
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
  // Only a table that binds both gestures has to wait out the double-click
  // window; a click-only table expands on the spot.
  const expandClickDelayMs =
    rowClickExpands && onRowDoubleClick ? ROW_DOUBLE_CLICK_MS : 0;
  const handleRowClick = rowClickExpands
    ? (row: Row<AppTableFeatures, TData>, event: MouseEvent) => {
        if (!row.getCanExpand() || !clickTargetsRowBody(event.target)) return;

        cancelPendingExpand();
        if (!expandClickDelayMs) {
          row.toggleExpanded();
          return;
        }
        // The second click of a double click carries detail 2 — leave the row
        // alone and let the double-click handler have it.
        if (event.detail > 1) return;

        pendingExpandRef.current = window.setTimeout(() => {
          pendingExpandRef.current = null;
          row.toggleExpanded();
        }, expandClickDelayMs);
      }
    : onRowClick;
  // A double click is how a word gets selected, so the text-selection half of
  // `clickTargetsRowBody` would veto every one of these — only the control check
  // applies here.
  const handleRowDoubleClick = onRowDoubleClick
    ? (row: Row<AppTableFeatures, TData>, event: MouseEvent) => {
        cancelPendingExpand();
        if (targetIsRowControl(event.target)) return;
        onRowDoubleClick(row, event);
      }
    : undefined;
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

  const content = (
    <div
      aria-label={ariaLabel}
      className={[
        "app-dt",
        "app-dt--normal",
        fillAvailable && "app-dt--fill",
        isEmbedded && "app-dt--embedded",
        density === "compact" && "app-dt--compact",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
      role="table"
      style={
        {
          "--app-dt-alt-bg": altBg,
          "--app-dt-grid": gridTemplate,
          "--app-dt-head-bg": headRowBg,
          "--app-dt-selected-bg": selectedBg,
          "--reorder-hold-color": theme.palette.primary.main,
          "--reorder-hold-ms": `${REORDER_HOLD_MS}ms`,
          boxShadow: isEmbedded ? "none" : shadowSm,
          height: height ?? (fillAvailable ? "100%" : undefined),
          maxHeight,
          minHeight: fillAvailable ? 0 : undefined,
          ...style,
        } as CSSProperties
      }
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
          {rows.map((row, rowIndex) => {
            const isExpanded = row.getIsExpanded();
            const isSelected =
              row.id === selectedRowId ||
              (selectedRowIds?.has(row.id) ?? false);
            const canExpand = row.getCanExpand();
            const cells = row.getVisibleCells().map((cell) => ({
              cell,
              columnDef: cell.column.columnDef,
              renderKey: getCellRenderKey(cell, rowIndex),
              rowIndex,
            }));

            return (
              <Fragment key={row.id}>
                {dndOptions ? (
                  <AppDataTableSortableBodyRow
                    canExpand={canExpand}
                    cells={cells}
                    dnd={dndOptions}
                    getRowAttributes={getRowAttributes}
                    hasExpandColumn={hasExpandColumn}
                    isExpanded={isExpanded}
                    isInteractive={
                      isInteractive || (rowClickExpands && canExpand)
                    }
                    isSelected={isSelected}
                    onRowClick={handleRowClick}
                    onRowContextMenu={onRowContextMenu}
                    onRowDoubleClick={handleRowDoubleClick}
                    renderRow={renderRow}
                    row={row}
                    rowIndex={rowIndex}
                  />
                ) : (
                  <MemoizedAppDataTableBodyRow
                    canExpand={canExpand}
                    cells={cells}
                    getRowAttributes={getRowAttributes}
                    hasDragColumn={false}
                    hasExpandColumn={hasExpandColumn}
                    isExpanded={isExpanded}
                    isInteractive={
                      isInteractive || (rowClickExpands && canExpand)
                    }
                    isSelected={isSelected}
                    onRowClick={handleRowClick}
                    onRowContextMenu={onRowContextMenu}
                    onRowDoubleClick={handleRowDoubleClick}
                    renderRow={renderRow}
                    row={row}
                    rowIndex={rowIndex}
                  />
                )}
                {renderExpandedContent && (
                  <MemoizedAppDataTableExpandedRow
                    columnCount={
                      visibleColumns.length + (hasExpandColumn ? 1 : 0)
                    }
                    isExpanded={isExpanded}
                    renderExpandedContent={renderExpandedContent}
                    row={row}
                  />
                )}
              </Fragment>
            );
          })}
        </div>

        {rows.length === 0 && (
          <div className="app-dt__empty">
            <AppTypography color="text.secondary" variant="body2">
              {emptyMessage}
            </AppTypography>
          </div>
        )}
      </div>
    </div>
  );

  if (!dndOptions) return content;

  return (
    <DndContext {...dndOptions.contextProps}>
      <SortableContext
        items={dndOptions.itemIds}
        strategy={verticalListSortingStrategy}
      >
        {content}
      </SortableContext>
    </DndContext>
  );
}

const MemoizedAppDataTable = memo(AppDataTable) as typeof AppDataTable;

export default MemoizedAppDataTable;
