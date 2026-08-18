import type { UniqueIdentifier } from "@dnd-kit/core";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Icon } from "@iconify/react";
import type {
  Cell,
  ExpandedState,
  OnChangeFn,
  Row,
  RowData,
  SortingState,
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
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
  type RefObject,
  type UIEventHandler,
} from "react";

import type {
  AppDataTableBreakpoint,
  AppDataTableColumnDef,
  AppDataTableColumnMeta,
  AppTableFeatures,
} from "@/components/tables/AppDataTable.types";
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
import { TRANSITION_DURATION_STANDARD_MS } from "@/theme/constants";

// Shared with the non-virtualized AppDataTable too — see the same note
// over there.
import "./app-data-table.css";
import "../reorder/reorder.css";

export type AppVirtualDataTableBreakpoint = AppDataTableBreakpoint;
export type AppVirtualDataTableColumnMeta = AppDataTableColumnMeta;
export type AppVirtualDataTableColumnDef<
  TData extends RowData,
  TValue = unknown,
> = AppDataTableColumnDef<TData, TValue>;

export interface AppVirtualDataTableDndOptions<TData extends RowData> {
  contextProps: ReorderableSurfaceDndProps;
  getItemId: (row: Row<AppTableFeatures, TData>) => UniqueIdentifier;
  itemIds: UniqueIdentifier[];
  editing?: boolean;
  enabled?: boolean;
  handleAriaLabel?: string;
  handleColumnWidth?: string | number;
  pendingItemId?: UniqueIdentifier | null;
}

export interface AppVirtualDataTableProps<TData extends RowData> {
  ariaLabel?: string;
  className?: string;
  columns: AppVirtualDataTableColumnDef<TData>[];
  data: TData[];
  density?: "comfortable" | "compact";
  /**
   * Hold-to-reorder wiring from `useReorderableTableDnd`. Rows drag from the
   * row body, and layout mode adds the visible handle column — the same
   * chrome as the non-virtualized table.
   */
  dnd?: AppVirtualDataTableDndOptions<TData>;
  emptyMessage?: string;
  estimateExpandedRowHeight?: number;
  enableSorting?: boolean;
  estimateRowHeight?: number;
  expanded?: ExpandedState;
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
  onExpandedChange?: OnChangeFn<ExpandedState>;
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
  renderExpandedContent?: (row: Row<AppTableFeatures, TData>) => ReactNode;
  scrollElementRef?: RefObject<HTMLDivElement | null>;
  scrollToIndex?: number | null;
  selectedRowId?: string | null;
  showHeader?: boolean;
  sorting?: SortingState;
  style?: CSSProperties;
  variant?: "default" | "embedded";
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
  dnd: AppVirtualDataTableDndOptions<TData> | undefined,
  row: Row<AppTableFeatures, TData>,
) {
  const isArmed = Boolean(dnd) && dnd?.enabled !== false;
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
    // The visible handle takes the raw activator, the way the non-virtualized
    // table wires its handle; the row body keeps the guarded listeners below.
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

interface AppVirtualDataTableBodyRowProps<TData extends RowData> {
  canExpand: boolean;
  dnd?: AppVirtualDataTableDndOptions<TData>;
  // Invalidate a memoized row when same-ID columns replace their renderer or
  // metadata; the row reads the actual visible cells from TanStack Table.
  columnVersion: AppVirtualDataTableColumnDef<TData>[];
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
  row: Row<AppTableFeatures, TData>;
  rowIndex: number;
}

function AppVirtualDataTableBodyRow<TData extends RowData>({
  canExpand,
  dnd,
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
  row,
  rowIndex,
}: AppVirtualDataTableBodyRowProps<TData>) {
  "use no memo";
  const rowAttributes = getRowAttributes?.(row);
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

  return (
    <div
      {...reorderListeners}
      {...rowAttributes}
      aria-expanded={canExpand ? isExpanded : undefined}
      ref={setReorderNodeRef}
      className={[
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
        .join(" ")}
      onClick={(event) => {
        rowAttributeOnClick?.(event);
        if (!event.defaultPrevented) onRowClick?.(row, event);
      }}
      onContextMenu={(event) => {
        rowAttributeOnContextMenu?.(event);
        if (!event.defaultPrevented) onRowContextMenu?.(row, event);
      }}
      onDoubleClick={(event) => {
        rowAttributeOnDoubleClick?.(event);
        if (!event.defaultPrevented) onRowDoubleClick?.(row, event);
      }}
      // See AppDataTable: the second mousedown of a double click is what
      // starts the browser's word selection, which is litter on a table that
      // means something else by a double click.
      onMouseDown={(event) => {
        if (
          onRowDoubleClick &&
          event.detail > 1 &&
          !targetIsRowControl(event.target)
        ) {
          event.preventDefault();
        }
        reorderOnMouseDown?.(event);
        rowAttributeOnMouseDown?.(event);
      }}
      role="row"
      style={{ ...rowAttributes?.style, ...reorderStyle }}
    >
      {hasDragColumn && (
        <div className="app-dt__cell app-dt__cell--drag" role="cell">
          {handleListeners && (
            <span
              {...handleAttributes}
              {...handleListeners}
              aria-label={dnd?.handleAriaLabel ?? "Reorder row"}
              className="app-dt__drag-handle"
            >
              <Icon height={20} icon="mdi:drag" width={20} />
            </span>
          )}
        </div>
      )}
      {row.getVisibleCells().map((cell) => (
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
          onToggle={() => onExpand(row)}
        />
      )}
    </div>
  );
}

const MemoizedAppVirtualDataTableBodyRow = memo(
  AppVirtualDataTableBodyRow,
) as typeof AppVirtualDataTableBodyRow;

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
// oxlint-disable-next-line react/react-compiler
function AppVirtualDataTable<TData extends RowData>({
  ariaLabel = "Data table",
  className,
  columns,
  data,
  density = "comfortable",
  dnd,
  emptyMessage = "No data available.",
  estimateExpandedRowHeight = 0,
  enableSorting = false,
  estimateRowHeight = 48,
  expanded,
  fillAvailable = true,
  getRowCanExpand,
  getRowAttributes,
  getRowId,
  height,
  manualSorting = false,
  maxHeight,
  onClearSelection,
  onExpandedChange,
  onScroll,
  onRowClick,
  onRowContextMenu,
  onRowDoubleClick,
  onSortingChange,
  overscan = 12,
  renderExpandedContent,
  scrollElementRef,
  scrollToIndex,
  selectedRowId,
  showHeader = true,
  sorting,
  style,
  variant = "default",
}: AppVirtualDataTableProps<TData>) {
  "use no memo";

  const internalScrollRef = useRef<HTMLDivElement>(null);
  const scrollRef = scrollElementRef ?? internalScrollRef;
  const expandedRowIdsRef = useRef<Set<string>>(new Set());
  const measureFrameRef = useRef<number | null>(null);
  const detailAnimationFrameRefs = useRef<Map<string, number>>(new Map());
  const detailContentHeightsRef = useRef<Map<string, number>>(new Map());
  const detailContentObserverRefs = useRef<Map<string, ResizeObserver>>(
    new Map(),
  );
  const detailNodeRefs = useRef<Map<string, HTMLDivElement>>(new Map());
  const detailSizesRef = useRef<Map<string, number>>(new Map());
  const latestVirtualEntriesRef = useRef<Array<VirtualTableEntry<TData>>>([]);
  const [mountedDetailRowIds, setMountedDetailRowIds] = useState<Set<string>>(
    () => new Set(),
  );

  const { dndOptions, resolvedExpanded, table } = useAppTableInstance({
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
  const expandedRowIds = useMemo(() => {
    const next = new Set<string>();
    for (const row of rows) {
      if (row.getIsExpanded()) {
        next.add(row.id);
      }
    }
    return next;
  }, [rows]);

  useTableGestureKeys({
    hasExpandableRows: Boolean(renderExpandedContent),
    onClearSelection,
    table,
  });

  const virtualEntries = useMemo<Array<VirtualTableEntry<TData>>>(() => {
    const entries: Array<VirtualTableEntry<TData>> = [];

    rows.forEach((row, rowIndex) => {
      entries.push({
        kind: "row",
        key: `${row.id}:row`,
        row,
        rowIndex,
      });

      if (
        renderExpandedContent &&
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
  }, [mountedDetailRowIds, renderExpandedContent, rows]);

  const virtualizer = useVirtualizer({
    count: virtualEntries.length,
    // The virtualizer owns the row wrappers' transform and the body height —
    // scroll and remeasure updates (including the detail-row height animation,
    // which calls resizeItem every frame) are written straight to the DOM
    // instead of re-rendering. Rows must not set their own translateY.
    directDomUpdates: true,
    estimateSize: (index) => {
      const entry = virtualEntries[index];
      if (entry?.kind === "detail") {
        return (
          detailSizesRef.current.get(entry.row.id) ?? estimateExpandedRowHeight
        );
      }
      return estimateRowHeight;
    },
    getItemKey: (index) => virtualEntries[index]?.key ?? index,
    getScrollElement: () => scrollRef.current,
    overscan,
    useAnimationFrameWithResizeObserver: true,
  });

  useLayoutEffect(() => {
    expandedRowIdsRef.current = expandedRowIds;
  }, [expandedRowIds]);

  useLayoutEffect(() => {
    latestVirtualEntriesRef.current = virtualEntries;
  }, [virtualEntries]);

  const scheduleMeasure = useCallback(() => {
    if (measureFrameRef.current !== null) return;

    measureFrameRef.current = window.requestAnimationFrame(() => {
      measureFrameRef.current = null;
      virtualizer.measure();
    });
  }, [virtualizer]);

  const setDetailSize = useCallback(
    (rowId: string, size: number) => {
      const normalizedSize = Math.max(0, Math.round(size));
      detailSizesRef.current.set(rowId, normalizedSize);

      const node = detailNodeRefs.current.get(rowId);
      if (node) {
        node.style.height = `${normalizedSize}px`;
      }

      const detailIndex = latestVirtualEntriesRef.current.findIndex(
        (entry) => entry.kind === "detail" && entry.row.id === rowId,
      );
      if (detailIndex >= 0) {
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

      const startSize =
        detailSizesRef.current.get(rowId) ??
        (expandedRowIdsRef.current.has(rowId) ? 0 : targetSize);
      const normalizedTargetSize = Math.max(0, Math.round(targetSize));

      if (startSize === normalizedTargetSize) {
        setDetailSize(rowId, normalizedTargetSize);
        if (removeWhenComplete && !expandedRowIdsRef.current.has(rowId)) {
          setMountedDetailRowIds((current) => {
            if (!current.has(rowId)) return current;
            const next = new Set(current);
            next.delete(rowId);
            return next;
          });
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
          scheduleMeasure();
        }
      };

      const frame = window.requestAnimationFrame(step);
      detailAnimationFrameRefs.current.set(rowId, frame);
    },
    [scheduleMeasure, setDetailSize],
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
    scheduleMeasure();
  }, [resolvedExpanded, scheduleMeasure]);

  useLayoutEffect(() => {
    if (!renderExpandedContent) return;

    for (const rowId of mountedDetailRowIds) {
      if (expandedRowIds.has(rowId)) {
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
    expandedRowIds,
    mountedDetailRowIds,
    renderExpandedContent,
  ]);

  useEffect(
    () => () => {
      if (measureFrameRef.current !== null) {
        window.cancelAnimationFrame(measureFrameRef.current);
      }
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
  // See AppDataTable: the row itself is the disclosure control unless the table
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
                  <MemoizedAppVirtualDataTableBodyRow
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
                    row={row}
                    rowIndex={entry.rowIndex}
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

const MemoizedAppVirtualDataTable = memo(
  AppVirtualDataTable,
) as typeof AppVirtualDataTable;

export default MemoizedAppVirtualDataTable;
