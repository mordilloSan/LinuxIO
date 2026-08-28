import type { UniqueIdentifier } from "@dnd-kit/core";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Icon } from "@iconify/react";
import type {
  Cell,
  OnChangeFn,
  Row,
  RowData,
  SortingState,
} from "@tanstack/react-table";
import { useVirtualizer, type Virtualizer } from "@tanstack/react-virtual";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  memo,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type HTMLAttributes,
  type MouseEvent,
  type ReactNode,
  type Ref,
  type RefObject,
  type UIEventHandler,
} from "react";

import type {
  AppVirtualTableColumnDef,
  AppTableFeatures,
} from "@/components/tables/AppVirtualTable.types";
import {
  rowBodyDragListeners,
  targetIsRowControl,
} from "@/components/tables/rowInteraction";
import {
  AppTableHeader,
  AppTableShell,
  MemoizedAppTableCell,
  TableDndBoundary,
  TableEmptyState,
  TableExpandCell,
  columnTrack,
  useAppTableInstance,
  useRowClickGestures,
  useTableGestureKeys,
} from "@/components/tables/tableShared";
import type { ReorderableSurfaceDndProps } from "@/hooks/useReorderableSurface";
import { useAppMediaQuery } from "@/theme";
import {
  TABLE_ROW_MIN_HEIGHT,
  TRANSITION_DURATION_STANDARD_MS,
} from "@/theme/constants";

// The app-dt__* chrome; see the note at the top of the stylesheet.
import "./app-virtual-table.css";
import "../reorder/reorder.css";

export interface AppVirtualTableDndOptions<TData extends RowData> {
  contextProps: ReorderableSurfaceDndProps;
  getItemId: (row: Row<AppTableFeatures, TData>) => UniqueIdentifier;
  /**
   * Groups adjacent rows under one sortable transform — and, here, under one
   * virtualized entry, so the whole block is measured and translated together.
   * The row whose `isRowSortable` result is true owns the drag gesture; the
   * other rows stay inert but travel with it.
   */
  getSortableGroupId?: (
    row: Row<AppTableFeatures, TData>,
  ) => UniqueIdentifier | null | undefined;
  itemIds: UniqueIdentifier[];
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
  pendingItemId?: UniqueIdentifier | null;
}

export type AppVirtualTableRowAttributes = HTMLAttributes<HTMLDivElement> & {
  ref?: Ref<HTMLDivElement>;
};

export interface AppVirtualTableRowRenderProps<TData extends RowData> {
  cells: ReactNode;
  dragHandle?: ReactNode;
  isSelected: boolean;
  row: Row<AppTableFeatures, TData>;
  rowIndex: number;
  rowProps: AppVirtualTableRowAttributes;
}

export interface AppVirtualTableProps<TData extends RowData> {
  ariaLabel?: string;
  className?: string;
  columns: AppVirtualTableColumnDef<TData>[];
  /** Nullish renders `emptyMessage`; see EMPTY_TABLE_ROWS in tableShared. */
  data: TData[] | null | undefined;
  density?: "comfortable" | "compact";
  /**
   * Hold-to-reorder wiring from `useReorderableTableDnd`. Rows drag from the
   * row body, and layout mode adds the visible handle column.
   */
  dnd?: AppVirtualTableDndOptions<TData>;
  emptyMessage?: string;
  estimateExpandedRowHeight?: number;
  enableSorting?: boolean;
  estimateRowHeight?: number;
  /**
   * Fill the parent height and make the body the scroll viewport.
   * Defaults to true for app-page data tables; set false for compact embedded tables.
   */
  fillAvailable?: boolean;
  getRowCanExpand?: (row: Row<AppTableFeatures, TData>) => boolean;
  getRowAttributes?: (
    row: Row<AppTableFeatures, TData>,
  ) => HTMLAttributes<HTMLDivElement>;
  getRowId: (
    row: TData,
    index: number,
    parent?: Row<AppTableFeatures, TData>,
  ) => string;
  height?: CSSProperties["height"];
  manualSorting?: boolean;
  maxHeight?: CSSProperties["maxHeight"];
  onScroll?: UIEventHandler<HTMLDivElement>;
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
  onSortingChange?: OnChangeFn<SortingState>;
  overscan?: number;
  /**
   * Persist which rows are expanded to localStorage under this app-unique
   * key, so expansion survives navigation and reloads. Only for tables whose
   * `getRowId` is stable across sessions (a name, an id) — an index or a
   * per-session synthetic id would re-expand the wrong row next load.
   */
  persistExpandedKey?: string;
  renderExpandedContent?: (row: Row<AppTableFeatures, TData>) => ReactNode;
  renderRow?: (props: AppVirtualTableRowRenderProps<TData>) => ReactNode;
  scrollElementRef?: RefObject<HTMLDivElement | null>;
  scrollToIndex?: number | null;
  selectedRowId?: string | null;
  showHeader?: boolean;
  sorting?: SortingState;
  style?: CSSProperties;
  variant?: "default" | "embedded";
}

interface VirtualGroupMember<TData extends RowData> {
  row: Row<AppTableFeatures, TData>;
  rowIndex: number;
}

type VirtualTableEntry<TData extends RowData> =
  | {
      kind: "row";
      key: string;
      row: Row<AppTableFeatures, TData>;
      rowIndex: number;
    }
  | {
      kind: "detail";
      key: string;
      row: Row<AppTableFeatures, TData>;
      rowIndex: number;
    }
  // A sortable group is one virtual entry: the virtualizer measures and
  // positions the whole block, so its rows share one dnd transform. Rows
  // inside a group don't get detail entries — no table combines groups with
  // `renderExpandedContent`.
  | {
      kind: "group";
      key: string;
      groupId: UniqueIdentifier;
      members: VirtualGroupMember<TData>[];
    };

function getCellRenderKey<TData extends RowData>(
  cell: Cell<AppTableFeatures, TData>,
  rowIndex: number,
) {
  const getExplicitRenderKey = cell.column.columnDef.meta?.getCellRenderKey;
  return getExplicitRenderKey
    ? getExplicitRenderKey(cell.row.original, rowIndex)
    : [cell.row.original, rowIndex];
}

/**
 * Sortable wiring for one virtualized row. The virtualizer owns the outer
 * wrapper's translateY, so the drag transform goes on the inner row instead of
 * fighting it.
 *
 * Only mounted rows are drop targets — dnd-kit cannot see what virtualization
 * has unmounted — so long lists are reordered by dragging in steps, scrolling
 * as you go.
 */
function useVirtualRowReorder<TData extends RowData>(
  dnd: AppVirtualTableDndOptions<TData> | undefined,
  row: Row<AppTableFeatures, TData>,
) {
  const isSortableRow = dnd?.isRowSortable?.(row) ?? true;
  const isArmed = Boolean(dnd) && dnd?.enabled !== false && isSortableRow;
  const itemId = dnd?.getItemId(row);
  const {
    attributes,
    isDragging,
    listeners,
    setNodeRef,
    transform,
    transition,
  } = useSortable({ id: itemId ?? row.id, disabled: !isArmed });

  if (!dnd || !isArmed) {
    return {
      handleAttributes: undefined,
      handleListeners: undefined,
      isReorderEditing: false,
      isReorderPending: false,
      reorderListeners: undefined,
      reorderStyle: undefined,
      setReorderNodeRef: undefined,
    } as const;
  }

  return {
    // The visible handle takes the raw activator; the row body keeps the
    // guarded listeners below.
    handleAttributes: attributes,
    handleListeners: listeners,
    isReorderEditing: dnd.editing ?? false,
    isReorderPending: dnd.pendingItemId != null && dnd.pendingItemId === itemId,
    // A press on one of the row's own controls is not a drag.
    reorderListeners: rowBodyDragListeners(listeners),
    reorderStyle: {
      opacity: isDragging ? 0.45 : undefined,
      transform: CSS.Transform.toString(transform) || undefined,
      transition: transition || undefined,
    } as CSSProperties,
    setReorderNodeRef: setNodeRef,
  } as const;
}

interface AppVirtualTableBodyRowProps<TData extends RowData> {
  canExpand: boolean;
  dnd?: AppVirtualTableDndOptions<TData>;
  // Invalidate a memoized row when same-ID columns replace their renderer or
  // metadata; the row reads the actual visible cells from TanStack Table.
  columnVersion: AppVirtualTableColumnDef<TData>[];
  /** A group leader's handle, when the group owns the sortable transform. */
  dragHandle?: ReactNode;
  getRowAttributes?: (
    row: Row<AppTableFeatures, TData>,
  ) => HTMLAttributes<HTMLDivElement>;
  hasDragColumn: boolean;
  hasExpandColumn: boolean;
  isExpanded: boolean;
  isInteractive: boolean;
  isSelected: boolean;
  onExpand: (row: Row<AppTableFeatures, TData>) => void;
  onRowClick?: (row: Row<AppTableFeatures, TData>, event: MouseEvent) => void;
  onRowContextMenu?: (
    row: Row<AppTableFeatures, TData>,
    event: MouseEvent,
  ) => void;
  onRowDoubleClick?: (
    row: Row<AppTableFeatures, TData>,
    event: MouseEvent,
  ) => void;
  renderRow?: (props: AppVirtualTableRowRenderProps<TData>) => ReactNode;
  row: Row<AppTableFeatures, TData>;
  /** Group-supplied attributes; when present, `getRowAttributes` stands down. */
  rowAttributes?: AppVirtualTableRowAttributes;
  rowIndex: number;
  /** Snapshot of the visible cells for this render, used by the row memo boundary. */
  visibleCells: Cell<AppTableFeatures, TData>[];
}

function AppVirtualTableBodyRow<TData extends RowData>({
  canExpand,
  dnd,
  dragHandle,
  getRowAttributes,
  hasDragColumn,
  hasExpandColumn,
  isExpanded,
  isInteractive,
  isSelected,
  onExpand,
  onRowClick,
  onRowContextMenu,
  onRowDoubleClick,
  renderRow,
  row,
  rowAttributes: providedRowAttributes,
  rowIndex,
  visibleCells,
}: AppVirtualTableBodyRowProps<TData>) {
  "use no memo";
  const rowAttributes = providedRowAttributes ?? getRowAttributes?.(row);
  const {
    handleAttributes,
    handleListeners,
    isReorderEditing,
    isReorderPending,
    reorderListeners,
    reorderStyle,
    setReorderNodeRef,
  } = useVirtualRowReorder(dnd, row);
  const rowAttributeOnClick = rowAttributes?.onClick;
  const rowAttributeOnContextMenu = rowAttributes?.onContextMenu;
  const rowAttributeOnDoubleClick = rowAttributes?.onDoubleClick;
  // Both spreads can carry a mousedown — the reorder activator and the caller's
  // own attributes — so the suppression below has to delegate
  const rowAttributeOnMouseDown = rowAttributes?.onMouseDown;
  const reorderOnMouseDown = reorderListeners?.onMouseDown as
    | ((event: MouseEvent) => void)
    | undefined;
  const ownDragHandle = handleListeners ? (
    <span
      {...handleAttributes}
      {...handleListeners}
      aria-label={dnd?.handleAriaLabel ?? "Reorder row"}
      className="app-dt__drag-handle"
    >
      <Icon height={20} icon="mdi:drag" width={20} />
    </span>
  ) : undefined;
  const resolvedDragHandle =
    dragHandle ?? (isReorderEditing ? ownDragHandle : undefined);

  const renderedCells = (
    <>
      {hasDragColumn && (
        <div className="app-dt__cell app-dt__cell--drag" role="cell">
          {resolvedDragHandle}
        </div>
      )}
      {visibleCells.map((cell) => (
        <MemoizedAppTableCell
          cell={cell}
          columnDef={cell.column.columnDef}
          key={cell.id}
          renderKey={getCellRenderKey(cell, rowIndex)}
        />
      ))}
      {hasExpandColumn && (
        <TableExpandCell
          canExpand={canExpand}
          isExpanded={isExpanded}
          onToggle={() => onExpand(row.table.getRow(row.id))}
        />
      )}
    </>
  );

  const rowProps: AppVirtualTableRowAttributes = {
    ...reorderListeners,
    ...rowAttributes,
    "aria-expanded": canExpand ? isExpanded : undefined,
    className: [
      "app-dt__row",
      "app-dt__row--body",
      isInteractive && "app-dt__row--interactive",
      isSelected && "app-dt__row--selected",
      rowIndex % 2 === 1 && "app-dt__row--alt",
      isReorderPending && "app-dt__row--reorder-pending",
      isReorderEditing && "app-dt__row--reordering",
      rowAttributes?.className,
    ]
      .filter(Boolean)
      .join(" "),
    onClick: (event) => {
      rowAttributeOnClick?.(event);
      if (!event.defaultPrevented) {
        onRowClick?.(row.table.getRow(row.id), event);
      }
    },
    onContextMenu: (event) => {
      rowAttributeOnContextMenu?.(event);
      if (!event.defaultPrevented) {
        onRowContextMenu?.(row.table.getRow(row.id), event);
      }
    },
    onDoubleClick: (event) => {
      rowAttributeOnDoubleClick?.(event);
      if (!event.defaultPrevented) {
        onRowDoubleClick?.(row.table.getRow(row.id), event);
      }
    },
    // See AppVirtualTable: the second mousedown of a double click is what
    // starts the browser's word selection, which is litter on a table that
    // means something else by a double click.
    onMouseDown: (event) => {
      if (
        onRowDoubleClick &&
        event.detail > 1 &&
        !targetIsRowControl(event.target)
      ) {
        event.preventDefault();
      }
      reorderOnMouseDown?.(event);
      rowAttributeOnMouseDown?.(event);
    },
    ref: setReorderNodeRef,
    role: "row",
    style: { ...rowAttributes?.style, ...reorderStyle },
  };

  if (renderRow) {
    return renderRow({
      cells: renderedCells,
      dragHandle: resolvedDragHandle,
      isSelected,
      row,
      rowIndex,
      rowProps,
    });
  }

  return <div {...rowProps}>{renderedCells}</div>;
}

function haveSameVisibleCells<TData extends RowData>(
  previous: Cell<AppTableFeatures, TData>[],
  next: Cell<AppTableFeatures, TData>[],
) {
  return (
    previous.length === next.length &&
    previous.every((cell, index) => cell.id === next[index]?.id)
  );
}

function haveSameRowDndState<TData extends RowData>(
  previousDnd: AppVirtualTableDndOptions<TData> | undefined,
  nextDnd: AppVirtualTableDndOptions<TData> | undefined,
  previousRow: Row<AppTableFeatures, TData>,
  nextRow: Row<AppTableFeatures, TData>,
) {
  if (previousDnd === nextDnd) return true;
  if (!previousDnd || !nextDnd) return false;

  const previousItemId = previousDnd.getItemId(previousRow);
  const nextItemId = nextDnd.getItemId(nextRow);
  const previousArmed =
    previousDnd.enabled !== false &&
    (previousDnd.isRowSortable?.(previousRow) ?? true);
  const nextArmed =
    nextDnd.enabled !== false && (nextDnd.isRowSortable?.(nextRow) ?? true);

  return (
    previousItemId === nextItemId &&
    previousArmed === nextArmed &&
    (!previousArmed ||
      ((previousDnd.editing ?? false) === (nextDnd.editing ?? false) &&
        (previousDnd.pendingItemId === previousItemId) ===
          (nextDnd.pendingItemId === nextItemId) &&
        previousDnd.handleAriaLabel === nextDnd.handleAriaLabel))
  );
}

function areAppVirtualTableBodyRowPropsEqual<TData extends RowData>(
  previous: AppVirtualTableBodyRowProps<TData>,
  next: AppVirtualTableBodyRowProps<TData>,
) {
  return (
    previous.canExpand === next.canExpand &&
    previous.columnVersion === next.columnVersion &&
    previous.dragHandle === next.dragHandle &&
    previous.getRowAttributes === next.getRowAttributes &&
    previous.hasDragColumn === next.hasDragColumn &&
    previous.hasExpandColumn === next.hasExpandColumn &&
    previous.isExpanded === next.isExpanded &&
    previous.isInteractive === next.isInteractive &&
    previous.isSelected === next.isSelected &&
    previous.onExpand === next.onExpand &&
    previous.onRowClick === next.onRowClick &&
    previous.onRowContextMenu === next.onRowContextMenu &&
    previous.onRowDoubleClick === next.onRowDoubleClick &&
    previous.renderRow === next.renderRow &&
    previous.rowAttributes === next.rowAttributes &&
    previous.rowIndex === next.rowIndex &&
    previous.row.table === next.row.table &&
    previous.row.id === next.row.id &&
    previous.row.index === next.row.index &&
    previous.row.depth === next.row.depth &&
    previous.row.parentId === next.row.parentId &&
    previous.row.original === next.row.original &&
    haveSameVisibleCells(previous.visibleCells, next.visibleCells) &&
    haveSameRowDndState(previous.dnd, next.dnd, previous.row, next.row)
  );
}

const MemoizedAppVirtualTableBodyRow = memo(
  AppVirtualTableBodyRow,
  areAppVirtualTableBodyRowPropsEqual,
) as typeof AppVirtualTableBodyRow;

interface AppVirtualTableSortableBodyGroupProps<TData extends RowData> {
  columnVersion: AppVirtualTableColumnDef<TData>[];
  dnd: AppVirtualTableDndOptions<TData>;
  getRowAttributes?: (
    row: Row<AppTableFeatures, TData>,
  ) => HTMLAttributes<HTMLDivElement>;
  groupId: UniqueIdentifier;
  hasDragColumn: boolean;
  hasExpandColumn: boolean;
  isInteractive: boolean;
  members: VirtualGroupMember<TData>[];
  onExpand: (row: Row<AppTableFeatures, TData>) => void;
  onRowClick?: (row: Row<AppTableFeatures, TData>, event: MouseEvent) => void;
  onRowContextMenu?: (
    row: Row<AppTableFeatures, TData>,
    event: MouseEvent,
  ) => void;
  onRowDoubleClick?: (
    row: Row<AppTableFeatures, TData>,
    event: MouseEvent,
  ) => void;
  renderRow?: (props: AppVirtualTableRowRenderProps<TData>) => ReactNode;
  rowClickExpands: boolean;
  selectedRowId?: string | null;
}

/**
 * One sortable node containing a leader row and its inert followers. The
 * group is a single virtual entry, so the virtualizer measures the block
 * while dnd-kit translates it; member rows render with their own indexes and
 * surfaces.
 */
function AppVirtualTableSortableBodyGroup<TData extends RowData>({
  columnVersion,
  dnd,
  getRowAttributes,
  groupId,
  hasDragColumn,
  hasExpandColumn,
  isInteractive,
  members,
  onExpand,
  onRowClick,
  onRowContextMenu,
  onRowDoubleClick,
  renderRow,
  rowClickExpands,
  selectedRowId,
}: AppVirtualTableSortableBodyGroupProps<TData>) {
  "use no memo";
  const leaderIndex = members.findIndex(
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
      {members.map((member, index) => {
        const { row, rowIndex } = member;
        const rowAttributes = getRowAttributes?.(row);
        const isLeader = index === leaderIndex;
        const sortableRowAttributes = isLeader
          ? {
              // The whole leader row carries the press listeners, not just the
              // handle — the hold that opens layout mode must reach dnd-kit
              // before the handle exists.
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
        const canExpand = row.getCanExpand();

        return (
          <MemoizedAppVirtualTableBodyRow
            canExpand={canExpand}
            columnVersion={columnVersion}
            dragHandle={isLeader && isEditing ? dragHandle : undefined}
            hasDragColumn={hasDragColumn}
            hasExpandColumn={hasExpandColumn}
            isExpanded={row.getIsExpanded()}
            isInteractive={isInteractive || (rowClickExpands && canExpand)}
            isSelected={row.id === selectedRowId}
            key={row.id}
            onExpand={onExpand}
            onRowClick={onRowClick}
            onRowContextMenu={onRowContextMenu}
            onRowDoubleClick={onRowDoubleClick}
            renderRow={renderRow}
            row={row}
            rowAttributes={sortableRowAttributes}
            rowIndex={rowIndex}
            visibleCells={row.getVisibleCells()}
          />
        );
      })}
    </div>
  );
}

function easeStandard(progress: number) {
  return progress < 0.5
    ? 2 * progress * progress
    : 1 - Math.pow(-2 * progress + 2, 2) / 2;
}

function observeDetailContent(
  node: HTMLDivElement,
  onResize: ResizeObserverCallback,
) {
  const observer = new ResizeObserver(onResize);
  observer.observe(node);
  return observer;
}

// React Compiler skips this component because of @tanstack/react-virtual
// (no compiler-compatible release exists); Table itself is v9 and fine.
// Manual memoization stays load-bearing here.
function AppVirtualTable<TData extends RowData>({
  ariaLabel = "Data table",
  className,
  columns,
  data,
  density = "comfortable",
  dnd,
  emptyMessage = "No data available.",
  estimateExpandedRowHeight = 0,
  enableSorting = false,
  estimateRowHeight = TABLE_ROW_MIN_HEIGHT,
  fillAvailable = true,
  getRowCanExpand,
  getRowAttributes,
  getRowId,
  height,
  manualSorting = false,
  maxHeight,
  onClearSelection,
  onScroll,
  onRowClick,
  onRowContextMenu,
  onRowDoubleClick,
  onSortingChange,
  overscan = 12,
  persistExpandedKey,
  renderExpandedContent,
  renderRow,
  scrollElementRef,
  scrollToIndex,
  selectedRowId,
  showHeader = true,
  sorting,
  style,
  variant = "default",
}: AppVirtualTableProps<TData>) {
  "use no memo";

  const prefersReducedMotion = useAppMediaQuery(
    "(prefers-reduced-motion: reduce)",
  );
  const internalScrollRef = useRef<HTMLDivElement>(null);
  const scrollRef = scrollElementRef ?? internalScrollRef;
  const expandedRowIdsRef = useRef<Set<string>>(new Set());
  const detailAnimationFrameRefs = useRef<Map<string, number>>(new Map());
  const detailContentHeightsRef = useRef<Map<string, number>>(new Map());
  const detailContentObserverRefs = useRef<Map<string, ResizeObserver>>(
    new Map(),
  );
  const detailNodeRefs = useRef<Map<string, HTMLDivElement>>(new Map());
  const detailSizesRef = useRef<Map<string, number>>(new Map());
  const latestDetailIndicesRef = useRef<Map<string, number>>(new Map());
  const [mountedDetailRowIds, setMountedDetailRowIds] = useState<Set<string>>(
    () => new Set(),
  );

  const { dndOptions, table } = useAppTableInstance({
    columns,
    data,
    dnd,
    enableSorting,
    getRowCanExpand,
    getRowId,
    manualSorting,
    onSortingChange,
    persistExpandedKey,
    renderExpandedContent,
    sorting,
  });

  const rows = table.getRowModel().rows;
  const expandedRowIds = useMemo(() => {
    const next = new Set<string>();
    for (const row of rows) {
      if (row.getIsExpanded()) {
        next.add(row.id);
      }
    }
    return next;
  }, [rows]);
  const expandedRowIdsKey = useMemo(
    () => JSON.stringify([...expandedRowIds].sort()),
    [expandedRowIds],
  );

  useTableGestureKeys({
    hasExpandableRows: Boolean(renderExpandedContent),
    onClearSelection,
    table,
  });

  const hasExpandedContent = Boolean(renderExpandedContent);
  const resolvedEstimateRowHeight = Math.max(
    TABLE_ROW_MIN_HEIGHT,
    estimateRowHeight,
  );

  const virtualEntries = useMemo<Array<VirtualTableEntry<TData>>>(() => {
    const entries: Array<VirtualTableEntry<TData>> = [];
    const getSortableGroupId = dndOptions?.getSortableGroupId;

    rows.forEach((row, rowIndex) => {
      const groupId = getSortableGroupId?.(row) ?? undefined;
      if (groupId !== undefined) {
        const previous = entries.at(-1);
        if (previous?.kind === "group" && previous.groupId === groupId) {
          previous.members.push({ row, rowIndex });
        } else {
          entries.push({
            kind: "group",
            key: `group:${String(groupId)}:${row.id}`,
            groupId,
            members: [{ row, rowIndex }],
          });
        }
        return;
      }

      entries.push({
        kind: "row",
        key: `${row.id}:row`,
        row,
        rowIndex,
      });

      if (
        hasExpandedContent &&
        (row.getIsExpanded() || mountedDetailRowIds.has(row.id))
      ) {
        entries.push({
          kind: "detail",
          key: `${row.id}:detail`,
          row,
          rowIndex,
        });
      }
    });

    return entries;
  }, [dndOptions, hasExpandedContent, mountedDetailRowIds, rows]);

  const virtualMeasurementInputs = useMemo(
    () => ({
      estimateExpandedRowHeight,
      estimateRowHeight: resolvedEstimateRowHeight,
      virtualEntries,
    }),
    [estimateExpandedRowHeight, resolvedEstimateRowHeight, virtualEntries],
  );

  const estimateVirtualItemSize = useCallback(
    (index: number) => {
      const entry = virtualMeasurementInputs.virtualEntries[index];
      if (entry?.kind === "detail") {
        return (
          detailSizesRef.current.get(entry.row.id) ??
          virtualMeasurementInputs.estimateExpandedRowHeight
        );
      }
      if (entry?.kind === "group") {
        return (
          entry.members.length * virtualMeasurementInputs.estimateRowHeight
        );
      }
      return virtualMeasurementInputs.estimateRowHeight;
    },
    [virtualMeasurementInputs],
  );

  const getVirtualItemKey = useCallback(
    (index: number) =>
      virtualMeasurementInputs.virtualEntries[index]?.key ?? index,
    // TanStack keys its offset calculation by this callback, but not by
    // estimateSize. Both callbacks share one input snapshot so estimate changes
    // recalculate unmeasured offsets without discarding measured row sizes.
    [virtualMeasurementInputs],
  );

  const shouldAdjustScrollPositionOnItemSizeChange = useCallback<
    NonNullable<
      Virtualizer<
        HTMLDivElement,
        Element
      >["shouldAdjustScrollPositionOnItemSizeChange"]
    >
  >((item, delta, instance) => {
    const cachedSize = instance.itemSizeCache.get(item.key);
    const scrollOffset =
      (instance.scrollOffset ?? 0) + instance.scrollAdjustments;

    // Preserve TanStack Virtual's default first-measure behavior.
    if (cachedSize === undefined) return item.start < scrollOffset;

    const entirelyAboveViewport = item.start + cachedSize <= scrollOffset;
    const isCollapsingDetail =
      delta < 0 &&
      typeof item.key === "string" &&
      item.key.endsWith(":detail") &&
      !expandedRowIdsRef.current.has(item.key.slice(0, -":detail".length));

    // A controlled detail collapse animates through several resizeItem calls.
    // Preserve the visible anchor even when the virtualizer interprets its own
    // compensating scroll writes as backward scrolling. Ordinary measurements
    // retain the library's backward-scroll guard.
    return (
      entirelyAboveViewport &&
      (isCollapsingDetail || instance.scrollDirection !== "backward")
    );
  }, []);

  // oxlint-disable-next-line react/incompatible-library
  const virtualizer = useVirtualizer({
    count: virtualEntries.length,
    // The virtualizer owns the row wrappers' transform and the body height —
    // scroll and remeasure updates (including the detail-row height animation,
    // which calls resizeItem every frame) are written straight to the DOM
    // instead of re-rendering. Rows must not set their own translateY.
    directDomUpdates: true,
    estimateSize: estimateVirtualItemSize,
    getItemKey: getVirtualItemKey,
    getScrollElement: () => scrollRef.current,
    overscan,
    useAnimationFrameWithResizeObserver: true,
  });

  useLayoutEffect(() => {
    const previous = virtualizer.shouldAdjustScrollPositionOnItemSizeChange;
    virtualizer.shouldAdjustScrollPositionOnItemSizeChange =
      shouldAdjustScrollPositionOnItemSizeChange;

    return () => {
      if (
        virtualizer.shouldAdjustScrollPositionOnItemSizeChange ===
        shouldAdjustScrollPositionOnItemSizeChange
      ) {
        virtualizer.shouldAdjustScrollPositionOnItemSizeChange = previous;
      }
    };
  }, [shouldAdjustScrollPositionOnItemSizeChange, virtualizer]);

  useLayoutEffect(() => {
    expandedRowIdsRef.current = expandedRowIds;
  }, [expandedRowIds]);

  useLayoutEffect(() => {
    const detailIndices = latestDetailIndicesRef.current;
    detailIndices.clear();
    virtualEntries.forEach((entry, index) => {
      if (entry.kind === "detail") {
        detailIndices.set(entry.row.id, index);
      }
    });
  }, [virtualEntries]);

  const setDetailSize = useCallback(
    (rowId: string, size: number) => {
      const normalizedSize = Math.max(0, Math.round(size));
      const node = detailNodeRefs.current.get(rowId);
      if (detailSizesRef.current.get(rowId) === normalizedSize) {
        if (node) node.style.height = `${normalizedSize}px`;
        return;
      }
      detailSizesRef.current.set(rowId, normalizedSize);

      if (node) {
        node.style.height = `${normalizedSize}px`;
      }

      const detailIndex = latestDetailIndicesRef.current.get(rowId);
      if (detailIndex !== undefined) {
        virtualizer.resizeItem(detailIndex, normalizedSize);
      }
    },
    [virtualizer],
  );

  const animateDetailSize = useCallback(
    (rowId: string, targetSize: number, removeWhenComplete = false) => {
      const existingFrame = detailAnimationFrameRefs.current.get(rowId);
      if (existingFrame !== undefined) {
        window.cancelAnimationFrame(existingFrame);
        detailAnimationFrameRefs.current.delete(rowId);
      }

      const normalizedTargetSize = Math.max(0, Math.round(targetSize));
      const startSize = prefersReducedMotion
        ? normalizedTargetSize
        : (detailSizesRef.current.get(rowId) ??
          (expandedRowIdsRef.current.has(rowId) ? 0 : targetSize));

      if (startSize === normalizedTargetSize) {
        setDetailSize(rowId, normalizedTargetSize);
        if (removeWhenComplete && !expandedRowIdsRef.current.has(rowId)) {
          setMountedDetailRowIds((current) => {
            if (!current.has(rowId)) return current;
            const next = new Set(current);
            next.delete(rowId);
            return next;
          });
          detailSizesRef.current.delete(rowId);
          detailContentHeightsRef.current.delete(rowId);
        }
        return;
      }

      const startedAt = window.performance.now();

      const step = (now: number) => {
        const elapsed = now - startedAt;
        const progress = Math.min(1, elapsed / TRANSITION_DURATION_STANDARD_MS);
        const easedProgress = easeStandard(progress);
        const nextSize =
          startSize + (normalizedTargetSize - startSize) * easedProgress;

        setDetailSize(rowId, nextSize);

        if (progress < 1) {
          const frame = window.requestAnimationFrame(step);
          detailAnimationFrameRefs.current.set(rowId, frame);
          return;
        }

        detailAnimationFrameRefs.current.delete(rowId);
        setDetailSize(rowId, normalizedTargetSize);

        if (removeWhenComplete && !expandedRowIdsRef.current.has(rowId)) {
          setMountedDetailRowIds((current) => {
            if (!current.has(rowId)) return current;
            const next = new Set(current);
            next.delete(rowId);
            return next;
          });
          detailSizesRef.current.delete(rowId);
          detailContentHeightsRef.current.delete(rowId);
        }
      };

      const frame = window.requestAnimationFrame(step);
      detailAnimationFrameRefs.current.set(rowId, frame);
    },
    [prefersReducedMotion, setDetailSize],
  );

  const measureDetailContent = useCallback(
    (rowId: string, node: HTMLElement) => {
      const measuredHeight = Math.ceil(node.getBoundingClientRect().height);
      const previousHeight = detailContentHeightsRef.current.get(rowId);

      if (previousHeight === measuredHeight) return;
      detailContentHeightsRef.current.set(rowId, measuredHeight);

      if (expandedRowIdsRef.current.has(rowId)) {
        animateDetailSize(rowId, measuredHeight);
      }
    },
    [animateDetailSize],
  );

  const setDetailContentRef = useCallback(
    (rowId: string, node: HTMLDivElement | null) => {
      const existingObserver = detailContentObserverRefs.current.get(rowId);
      if (existingObserver) {
        existingObserver.disconnect();
        detailContentObserverRefs.current.delete(rowId);
      }

      if (!node) return;

      if (expandedRowIdsRef.current.has(rowId)) {
        setMountedDetailRowIds((current) => {
          if (current.has(rowId)) return current;
          const next = new Set(current);
          next.add(rowId);
          return next;
        });
      }

      measureDetailContent(rowId, node);
      if (typeof ResizeObserver === "undefined") return;

      const observer = observeDetailContent(node, () => {
        measureDetailContent(rowId, node);
      });
      detailContentObserverRefs.current.set(rowId, observer);

      return () => {
        observer.disconnect();
        if (detailContentObserverRefs.current.get(rowId) === observer) {
          detailContentObserverRefs.current.delete(rowId);
        }
      };
    },
    [measureDetailContent],
  );

  useLayoutEffect(() => {
    if (!hasExpandedContent) return;

    for (const rowId of mountedDetailRowIds) {
      if (expandedRowIdsRef.current.has(rowId)) {
        animateDetailSize(
          rowId,
          detailContentHeightsRef.current.get(rowId) ?? 0,
        );
      } else {
        animateDetailSize(rowId, 0, true);
      }
    }
  }, [
    animateDetailSize,
    expandedRowIdsKey,
    hasExpandedContent,
    mountedDetailRowIds,
  ]);

  useEffect(
    () => () => {
      for (const frame of detailAnimationFrameRefs.current.values()) {
        window.cancelAnimationFrame(frame);
      }
      for (const observer of detailContentObserverRefs.current.values()) {
        observer.disconnect();
      }
    },
    [],
  );

  const isInteractive = Boolean(onRowClick || onRowDoubleClick);
  const hasExpandColumn = Boolean(renderExpandedContent);
  // See AppVirtualTable: the row itself is the disclosure control unless the table
  // gives a row click another meaning, or layout mode has claimed the press.
  const isReorderEditing =
    Boolean(dndOptions?.editing) && dndOptions?.enabled !== false;
  const rowClickExpands = hasExpandColumn && !onRowClick && !isReorderEditing;
  const visibleColumns = table.getVisibleLeafColumns();
  // Only layout mode adds the handle column; an armed-but-idle table keeps its
  // normal column widths.
  const gridTemplate = [
    ...(isReorderEditing
      ? [
          typeof dndOptions?.handleColumnWidth === "number"
            ? `${dndOptions.handleColumnWidth}px`
            : (dndOptions?.handleColumnWidth ?? "32px"),
        ]
      : []),
    ...visibleColumns.map((column) => columnTrack(column)),
    ...(hasExpandColumn ? ["40px"] : []),
  ].join(" ");
  const virtualItems = virtualizer.getVirtualItems();

  const handleExpandRow = useCallback((row: Row<AppTableFeatures, TData>) => {
    setMountedDetailRowIds((current) => {
      if (current.has(row.id)) return current;
      const next = new Set(current);
      next.add(row.id);
      return next;
    });
    row.toggleExpanded();
  }, []);

  const { handleRowClick, handleRowDoubleClick } = useRowClickGestures({
    onRowClick,
    onRowDoubleClick,
    rowClickExpands,
    toggleExpanded: handleExpandRow,
  });

  useEffect(() => {
    if (scrollToIndex === null || scrollToIndex === undefined) return;
    if (scrollToIndex < 0 || scrollToIndex >= rows.length) return;
    virtualizer.scrollToIndex(scrollToIndex, { align: "auto" });
  }, [rows.length, scrollToIndex, virtualizer]);

  return (
    <TableDndBoundary dnd={dndOptions}>
      <AppTableShell
        ariaLabel={ariaLabel}
        className={className}
        density={density}
        fillAvailable={fillAvailable}
        gridTemplate={gridTemplate}
        height={height}
        maxHeight={maxHeight}
        style={style}
        variant={variant}
      >
        {showHeader && (
          <AppTableHeader
            hasDragColumn={isReorderEditing}
            hasExpandColumn={hasExpandColumn}
            headerGroups={table.getHeaderGroups()}
          />
        )}

        <div
          className="app-dt__scroll custom-scrollbar"
          onScroll={onScroll}
          ref={scrollRef}
          role="presentation"
        >
          <div
            className="app-dt__body"
            ref={virtualizer.containerRef}
            role="rowgroup"
          >
            {virtualItems.map((virtualRow) => {
              const entry = virtualEntries[virtualRow.index];

              if (entry.kind === "group") {
                // Group entries only exist when dnd supplies
                // getSortableGroupId, so dndOptions is present here.
                if (!dndOptions) return null;
                return (
                  <div
                    className="app-dt__virtual-row"
                    data-index={virtualRow.index}
                    key={entry.key}
                    ref={virtualizer.measureElement}
                  >
                    <AppVirtualTableSortableBodyGroup
                      columnVersion={columns}
                      dnd={dndOptions}
                      getRowAttributes={getRowAttributes}
                      groupId={entry.groupId}
                      hasDragColumn={isReorderEditing}
                      hasExpandColumn={hasExpandColumn}
                      isInteractive={isInteractive}
                      members={entry.members}
                      onExpand={handleExpandRow}
                      onRowClick={handleRowClick}
                      onRowContextMenu={onRowContextMenu}
                      onRowDoubleClick={handleRowDoubleClick}
                      renderRow={renderRow}
                      rowClickExpands={rowClickExpands}
                      selectedRowId={selectedRowId}
                    />
                  </div>
                );
              }

              const row = entry.row;
              const isExpanded = row.getIsExpanded();

              if (entry.kind === "detail") {
                return (
                  <div
                    className="app-dt__virtual-row app-dt__virtual-row--detail"
                    data-index={virtualRow.index}
                    key={entry.key}
                    ref={virtualizer.measureElement}
                  >
                    <div
                      className="app-dt__detail"
                      ref={(node) => {
                        if (node) {
                          detailNodeRefs.current.set(row.id, node);
                          node.style.height = `${
                            detailSizesRef.current.get(row.id) ?? 0
                          }px`;
                        } else {
                          detailNodeRefs.current.delete(row.id);
                        }
                      }}
                      role="row"
                    >
                      <div
                        aria-colspan={
                          visibleColumns.length + (hasExpandColumn ? 1 : 0)
                        }
                        className="app-dt__detail-cell"
                        ref={(node) => setDetailContentRef(row.id, node)}
                        role="cell"
                      >
                        {renderExpandedContent?.(row)}
                      </div>
                    </div>
                  </div>
                );
              }

              const canExpand = row.getCanExpand();

              // The measured wrapper lives out here so an index shift alone —
              // a detail row mounting above — never re-renders the row beneath.
              return (
                <div
                  className="app-dt__virtual-row"
                  data-index={virtualRow.index}
                  key={entry.key}
                  ref={virtualizer.measureElement}
                >
                  <MemoizedAppVirtualTableBodyRow
                    canExpand={canExpand}
                    columnVersion={columns}
                    dnd={dndOptions}
                    getRowAttributes={getRowAttributes}
                    hasDragColumn={isReorderEditing}
                    hasExpandColumn={hasExpandColumn}
                    isExpanded={isExpanded}
                    isInteractive={
                      isInteractive || (rowClickExpands && canExpand)
                    }
                    isSelected={row.id === selectedRowId}
                    onExpand={handleExpandRow}
                    onRowClick={handleRowClick}
                    onRowContextMenu={onRowContextMenu}
                    onRowDoubleClick={handleRowDoubleClick}
                    renderRow={renderRow}
                    row={row}
                    rowIndex={entry.rowIndex}
                    visibleCells={row.getVisibleCells()}
                  />
                </div>
              );
            })}
          </div>

          {rows.length === 0 && <TableEmptyState message={emptyMessage} />}
        </div>
      </AppTableShell>
    </TableDndBoundary>
  );
}

const MemoizedAppVirtualTable = memo(AppVirtualTable) as typeof AppVirtualTable;

export default MemoizedAppVirtualTable;
