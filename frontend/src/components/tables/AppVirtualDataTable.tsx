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

import { appTableFeatures } from "@/components/tables/AppDataTable.types";
import type {
  AppDataTableBreakpoint,
  AppDataTableCellRenderKey,
  AppDataTableColumnDef,
  AppDataTableColumnMeta,
  AppTableFeatures,
} from "@/components/tables/AppDataTable.types";
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

import "./app-virtual-data-table.css";
import "../reorder/reorder.css";

const DETAIL_ANIMATION_CSS = `${TRANSITION_DURATION_STANDARD_MS}ms ${EASING_STANDARD_CSS}`;

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
  pendingItemId?: UniqueIdentifier | null;
}

export interface AppVirtualDataTableProps<TData extends RowData> {
  ariaLabel?: string;
  className?: string;
  columns: AppVirtualDataTableColumnDef<TData>[];
  data: TData[];
  density?: "comfortable" | "compact";
  /**
   * Hold-to-reorder wiring from `useReorderableTableDnd`. Virtualized rows drag
   * from the row body — there is no handle column, so the column template stays
   * identical whether or not layout mode is open.
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
  column: AppVirtualDataTableColumnDef<TData>,
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

function getCellRenderKey<TData extends RowData>(
  cell: Cell<AppTableFeatures, TData>,
  rowIndex: number,
) {
  const getExplicitRenderKey = cell.column.columnDef.meta?.getCellRenderKey;
  return getExplicitRenderKey
    ? getExplicitRenderKey(cell.row.original, rowIndex)
    : [cell.row.original, rowIndex];
}

interface AppVirtualDataTableCellProps<TData extends RowData> {
  cell: Cell<AppTableFeatures, TData>;
  renderKey: AppDataTableCellRenderKey;
}

function AppVirtualDataTableCell<TData extends RowData>({
  cell,
}: AppVirtualDataTableCellProps<TData>) {
  const meta = cell.column.columnDef.meta;

  return (
    <div
      className={["app-vdt__cell", meta?.className, meta?.cellClassName]
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
      {flexRender(cell.column.columnDef.cell, cell.getContext())}
    </div>
  );
}

const MemoizedAppVirtualDataTableCell = memo(
  AppVirtualDataTableCell,
  (previous, next) =>
    previous.cell.id === next.cell.id &&
    previous.cell.column.columnDef === next.cell.column.columnDef &&
    areCellRenderKeysEqual(previous.renderKey, next.renderKey),
) as typeof AppVirtualDataTableCell;

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
  const { isDragging, listeners, setNodeRef, transform, transition } =
    useSortable({ id: itemId ?? row.id, disabled: !isArmed });

  if (!dnd || !isArmed) {
    return {
      isReorderEditing: false,
      isReorderPending: false,
      reorderListeners: undefined,
      reorderStyle: undefined,
      setReorderNodeRef: undefined,
    } as const;
  }

  return {
    isReorderEditing: dnd.editing ?? false,
    isReorderPending: dnd.pendingItemId != null && dnd.pendingItemId === itemId,
    reorderListeners: listeners,
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
  hasExpandColumn: boolean;
  isExpanded: boolean;
  isInteractive: boolean;
  isSelected: boolean;
  measureElement: (node: HTMLDivElement | null) => void;
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
  virtualIndex: number;
}

function AppVirtualDataTableBodyRow<TData extends RowData>({
  canExpand,
  dnd,
  getRowAttributes,
  hasExpandColumn,
  isExpanded,
  isInteractive,
  isSelected,
  measureElement,
  onExpand,
  onRowClick,
  onRowContextMenu,
  onRowDoubleClick,
  row,
  rowIndex,
  virtualIndex,
}: AppVirtualDataTableBodyRowProps<TData>) {
  const rowAttributes = getRowAttributes?.(row);
  const {
    isReorderEditing,
    isReorderPending,
    reorderListeners,
    reorderStyle,
    setReorderNodeRef,
  } = useVirtualRowReorder(dnd, row);
  const rowAttributeOnClick = rowAttributes?.onClick;
  const rowAttributeOnContextMenu = rowAttributes?.onContextMenu;
  const rowAttributeOnDoubleClick = rowAttributes?.onDoubleClick;

  return (
    <div
      className="app-vdt__virtual-row"
      data-index={virtualIndex}
      ref={measureElement}
    >
      <div
        {...reorderListeners}
        {...rowAttributes}
        ref={setReorderNodeRef}
        className={[
          "app-vdt__row",
          "app-vdt__row--body",
          isInteractive && "app-vdt__row--interactive",
          isSelected && "app-vdt__row--selected",
          rowIndex % 2 === 1 && "app-vdt__row--alt",
          isReorderPending && "app-vdt__row--reorder-pending",
          isReorderEditing && "app-vdt__row--reordering",
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
        role="row"
        style={{ ...rowAttributes?.style, ...reorderStyle }}
      >
        {row.getVisibleCells().map((cell) => (
          <MemoizedAppVirtualDataTableCell
            cell={cell}
            key={cell.id}
            renderKey={getCellRenderKey(cell, rowIndex)}
          />
        ))}
        {hasExpandColumn && (
          <div className="app-vdt__cell app-vdt__cell--expand" role="cell">
            {canExpand && (
              <AppTooltip title={isExpanded ? "Collapse row" : "Expand row"}>
                <AppIconButton
                  aria-expanded={isExpanded}
                  aria-label={isExpanded ? "Collapse row" : "Expand row"}
                  onClick={(event) => {
                    event.stopPropagation();
                    onExpand(row);
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
      </div>
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

  const theme = useAppTheme();
  const isDark = theme.palette.mode === "dark";
  const belowSm = useAppMediaQuery(theme.breakpoints.down("sm"));
  const belowMd = useAppMediaQuery(theme.breakpoints.down("md"));
  const belowLg = useAppMediaQuery(theme.breakpoints.down("lg"));
  const belowXl = useAppMediaQuery(theme.breakpoints.down("xl"));

  const [internalExpanded, setInternalExpanded] = useState<ExpandedState>({});
  const [internalSorting, setInternalSorting] = useState<SortingState>([]);
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

  const columnVisibility = useMemo<ColumnVisibilityState>(() => {
    const below: Record<AppVirtualDataTableBreakpoint, boolean> = {
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
  // A column sort already owns the row order; see AppDataTable for the reasoning.
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
  const expandedRowIds = useMemo(() => {
    const next = new Set<string>();
    for (const row of rows) {
      if (row.getIsExpanded()) {
        next.add(row.id);
      }
    }
    return next;
  }, [rows]);

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
  const visibleColumns = table.getVisibleLeafColumns();
  const gridTemplate = [
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

  useEffect(() => {
    if (scrollToIndex === null || scrollToIndex === undefined) return;
    if (scrollToIndex < 0 || scrollToIndex >= rows.length) return;
    virtualizer.scrollToIndex(scrollToIndex, { align: "auto" });
  }, [rows.length, scrollToIndex, virtualizer]);

  const content = (
    <div
      className={[
        "app-vdt",
        fillAvailable && "app-vdt--fill",
        isEmbedded && "app-vdt--embedded",
        density === "compact" && "app-vdt--compact",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
      role="table"
      aria-label={ariaLabel}
      style={
        {
          "--app-vdt-alt-bg": altBg,
          "--app-vdt-grid": gridTemplate,
          "--app-vdt-head-bg": headRowBg,
          "--app-vdt-selected-bg": selectedBg,
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
        <div className="app-vdt__head" role="rowgroup">
          {table.getHeaderGroups().map((headerGroup) => (
            <div
              className="app-vdt__row app-vdt__row--head"
              key={headerGroup.id}
              role="row"
            >
              {headerGroup.headers.map((header) => {
                const meta = header.column.columnDef.meta;
                const sortState = header.column.getIsSorted();
                const canSort = header.column.getCanSort();

                return (
                  <div
                    className={[
                      "app-vdt__cell",
                      "app-vdt__cell--head",
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
                        className="app-vdt__sort-button"
                        onClick={header.column.getToggleSortingHandler()}
                        type="button"
                      >
                        <span className="app-vdt__sort-label">
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
                  className="app-vdt__cell app-vdt__cell--head app-vdt__cell--expand"
                  role="columnheader"
                />
              )}
            </div>
          ))}
        </div>
      )}

      <div
        className="app-vdt__scroll custom-scrollbar"
        onScroll={onScroll}
        ref={scrollRef}
        role="presentation"
      >
        <div
          className="app-vdt__body"
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
                  className="app-vdt__virtual-row app-vdt__virtual-row--detail"
                  data-index={virtualRow.index}
                  key={entry.key}
                  ref={virtualizer.measureElement}
                >
                  <div
                    className="app-vdt__detail"
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
                      className="app-vdt__detail-cell"
                      ref={(node) => setDetailContentRef(row.id, node)}
                      role="cell"
                    >
                      {renderExpandedContent?.(row)}
                    </div>
                  </div>
                </div>
              );
            }

            return (
              <MemoizedAppVirtualDataTableBodyRow
                canExpand={row.getCanExpand()}
                columnVersion={columns}
                dnd={dndOptions}
                getRowAttributes={getRowAttributes}
                hasExpandColumn={hasExpandColumn}
                isExpanded={isExpanded}
                isInteractive={isInteractive}
                isSelected={row.id === selectedRowId}
                key={entry.key}
                measureElement={virtualizer.measureElement}
                onExpand={handleExpandRow}
                onRowClick={onRowClick}
                onRowContextMenu={onRowContextMenu}
                onRowDoubleClick={onRowDoubleClick}
                row={row}
                rowIndex={entry.rowIndex}
                virtualIndex={virtualRow.index}
              />
            );
          })}
        </div>

        {rows.length === 0 && (
          <div className="app-vdt__empty">
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

const MemoizedAppVirtualDataTable = memo(
  AppVirtualDataTable,
) as typeof AppVirtualDataTable;

export default MemoizedAppVirtualDataTable;
