import { DndContext, type UniqueIdentifier } from "@dnd-kit/core";
import {
  SortableContext,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
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
  Table,
} from "@tanstack/react-table";
import {
  memo,
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent,
  type ReactNode,
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
  targetIsRowControl,
} from "@/components/tables/rowInteraction";
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
import {
  readPersistedState,
  writePersistedState,
} from "@/utils/persistedState";

// The table layer below AppDataTable: chrome, header, cells, and the gesture
// contract from `docs/table-row-gestures.md`. Like the app-dt__* CSS, nothing
// here knows about virtualization — body/row rendering stays in the
// component.

const DETAIL_ANIMATION_CSS = `${TRANSITION_DURATION_STANDARD_MS}ms ${EASING_STANDARD_CSS}`;

export function columnTrack<TData extends RowData>(
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

interface AppTableCellProps<TData extends RowData> {
  cell: Cell<AppTableFeatures, TData>;
  // A snapshot of the definition at render time: TanStack can preserve Column
  // objects while swapping their definitions, so comparing the live
  // `cell.column.columnDef` would miss a renderer swap.
  columnDef: AppDataTableColumnDef<TData>;
  renderKey: AppDataTableCellRenderKey;
  rowIndex?: number;
}

function AppTableCell<TData extends RowData>({
  cell,
  columnDef,
}: AppTableCellProps<TData>) {
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

export const MemoizedAppTableCell = memo(
  AppTableCell,
  (previous, next) =>
    previous.cell.id === next.cell.id &&
    previous.columnDef === next.columnDef &&
    previous.rowIndex === next.rowIndex &&
    areCellRenderKeysEqual(previous.renderKey, next.renderKey),
) as typeof AppTableCell;

interface TableExpandCellProps {
  canExpand: boolean;
  isExpanded: boolean;
  onToggle: () => void;
}

export function TableExpandCell({
  canExpand,
  isExpanded,
  onToggle,
}: TableExpandCellProps) {
  return (
    <div className="app-dt__cell app-dt__cell--expand" role="cell">
      {canExpand && (
        <AppTooltip title={isExpanded ? "Collapse row" : "Expand row"}>
          <AppIconButton
            aria-expanded={isExpanded}
            aria-label={isExpanded ? "Collapse row" : "Expand row"}
            onClick={(event) => {
              event.stopPropagation();
              onToggle();
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
  );
}

export interface AppTableHeaderProps<TData extends RowData> {
  hasDragColumn: boolean;
  hasExpandColumn: boolean;
  headerGroups: HeaderGroup<AppTableFeatures, TData>[];
}

export function AppTableHeader<TData extends RowData>({
  hasDragColumn,
  hasExpandColumn,
  headerGroups,
}: AppTableHeaderProps<TData>) {
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

export function TableEmptyState({ message }: { message: string }) {
  return (
    <div className="app-dt__empty">
      <AppTypography color="text.secondary" variant="body2">
        {message}
      </AppTypography>
    </div>
  );
}

export interface AppTableShellProps {
  ariaLabel: string;
  children: ReactNode;
  className?: string;
  density: "comfortable" | "compact";
  fillAvailable: boolean;
  gridTemplate: string;
  height?: CSSProperties["height"];
  maxHeight?: CSSProperties["maxHeight"];
  style?: CSSProperties;
  variant: "default" | "embedded";
}

export function AppTableShell({
  ariaLabel,
  children,
  className,
  density,
  fillAvailable,
  gridTemplate,
  height,
  maxHeight,
  style,
  variant,
}: AppTableShellProps) {
  const theme = useAppTheme();
  const isDark = theme.palette.mode === "dark";
  const isEmbedded = variant === "embedded";
  const headRowBg = isEmbedded
    ? "transparent"
    : alpha(theme.palette.text.primary, 0.08);
  const selectedBg = alpha(theme.palette.primary.main, isDark ? 0.15 : 0.1);
  const altBg = isEmbedded
    ? "transparent"
    : alpha(theme.palette.text.primary, isDark ? 0.04 : 0.05);

  return (
    <div
      aria-label={ariaLabel}
      className={[
        "app-dt",
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
      {children}
    </div>
  );
}

interface TableDndBoundaryProps {
  children: ReactNode;
  dnd?: {
    contextProps: ReorderableSurfaceDndProps;
    itemIds: UniqueIdentifier[];
  };
}

export function TableDndBoundary({ children, dnd }: TableDndBoundaryProps) {
  if (!dnd) return children;

  return (
    <DndContext {...dnd.contextProps}>
      <SortableContext
        items={dnd.itemIds}
        strategy={verticalListSortingStrategy}
      >
        {children}
      </SortableContext>
    </DndContext>
  );
}

// One localStorage entry per table surface, so clearing or inspecting one
// table's saved expansion never touches another's.
const expandedStorageKey = (key: string) => `linuxio.tableExpanded:${key}`;

// Persisted expansion is only ever the record form: our tables expand rows
// one at a time and collapse with `setExpanded({})`, never the `true`
// (expand-all) form — and only truthy ids are worth storing.
const isExpandedRecord = (value: unknown): value is Record<string, true> =>
  typeof value === "object" &&
  value !== null &&
  !Array.isArray(value) &&
  Object.values(value).every((entry) => entry === true);

function persistableExpanded(expanded: ExpandedState): Record<string, true> {
  if (expanded === true) return {};
  const next: Record<string, true> = {};
  for (const [id, isExpanded] of Object.entries(expanded)) {
    if (isExpanded) next[id] = true;
  }
  return next;
}

export interface UseAppTableInstanceOptions<TData extends RowData, TDnd> {
  columns: AppDataTableColumnDef<TData>[];
  data: TData[];
  dnd?: TDnd;
  enableSorting: boolean;
  getRowCanExpand?: (row: Row<AppTableFeatures, TData>) => boolean;
  getRowId: (
    row: TData,
    index: number,
    parent?: Row<AppTableFeatures, TData>,
  ) => string;
  manualSorting: boolean;
  onSortingChange?: OnChangeFn<SortingState>;
  persistExpandedKey?: string;
  renderExpandedContent?: (row: Row<AppTableFeatures, TData>) => ReactNode;
  sorting?: SortingState;
}

/**
 * The TanStack instance both tables build: internal expansion and sorting,
 * breakpoint-driven column visibility from `meta.hideBelow`, and
 * the sort/reorder exclusion. `persistExpandedKey` mirrors the expansion
 * record to localStorage so it survives navigation and reloads; stale ids in
 * the stored record are harmless because expansion is keyed by row id and an
 * id with no row never renders.
 */
export function useAppTableInstance<TData extends RowData, TDnd>({
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
}: UseAppTableInstanceOptions<TData, TDnd>) {
  const theme = useAppTheme();
  const belowSm = useAppMediaQuery(theme.breakpoints.down("sm"));
  const belowMd = useAppMediaQuery(theme.breakpoints.down("md"));
  const belowLg = useAppMediaQuery(theme.breakpoints.down("lg"));
  const belowXl = useAppMediaQuery(theme.breakpoints.down("xl"));
  // The lazy initializer reads storage once, so the key is fixed for the
  // life of the table — a key that changes identity mid-flight is a bug at
  // the call site, not a supported way to swap stores.
  const [internalExpanded, setInternalExpanded] = useState<ExpandedState>(() =>
    persistExpandedKey
      ? (readPersistedState(
          expandedStorageKey(persistExpandedKey),
          isExpandedRecord,
        ) ?? {})
      : {},
  );
  const [internalSorting, setInternalSorting] = useState<SortingState>([]);

  useEffect(() => {
    if (!persistExpandedKey) return;
    writePersistedState(
      expandedStorageKey(persistExpandedKey),
      persistableExpanded(internalExpanded),
    );
  }, [internalExpanded, persistExpandedKey]);

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

  const resolvedExpanded = internalExpanded;
  const resolvedSorting = sorting ?? internalSorting;
  // A column sort already decides the row order, and a saved manual order would
  // be invisible underneath it. Sorted tables therefore aren't reorderable at
  // all, rather than accepting drags that appear to do nothing.
  const dndOptions = resolvedSorting.length > 0 ? undefined : dnd;

  const handleExpandedChange: OnChangeFn<ExpandedState> = (updater) => {
    setInternalExpanded(updater);
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

  return { dndOptions, table };
}

interface UseTableGestureKeysOptions<TData extends RowData> {
  hasExpandableRows: boolean;
  onClearSelection?: () => void;
  table: Table<AppTableFeatures, TData>;
}

/** The window-level half of the gesture contract: Escape peeling. */
export function useTableGestureKeys<TData extends RowData>({
  hasExpandableRows,
  onClearSelection,
  table,
}: UseTableGestureKeysOptions<TData>) {
  const handleTableKeyDown = useEffectEvent(
    (event: globalThis.KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented) return;
      // A dialog owns the keyboard while it is open, the same guard the
      // filebrowser keyboard hooks use.
      if (document.querySelector(OVERLAY_ROOT_SELECTOR)) return;

      // Escape peels one layer of row state at a time: open detail panels
      // first, then the selection. Pressing it twice therefore gets a table
      // back to rest. A table with nothing expanded skips straight to clearing
      // the selection rather than swallowing a press on nothing.
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
    },
  );

  const isActive = Boolean(hasExpandableRows || onClearSelection);

  useEffect(() => {
    if (!isActive) return;
    window.addEventListener("keydown", handleTableKeyDown);
    return () => window.removeEventListener("keydown", handleTableKeyDown);
  }, [isActive]);
}

interface UseRowClickGesturesOptions<TData extends RowData> {
  onRowClick?: (row: Row<AppTableFeatures, TData>, event: MouseEvent) => void;
  onRowDoubleClick?: (
    row: Row<AppTableFeatures, TData>,
    event: MouseEvent,
  ) => void;
  rowClickExpands: boolean;
  toggleExpanded: (row: Row<AppTableFeatures, TData>) => void;
}

/**
 * The click half of the gesture contract: a row click either expands or runs
 * the table's own action (never both), and only a table that also binds a
 * double click waits out the double-click window before expanding.
 *
 * The table owns the deferred single click, not the row: rows are memoized —
 * and virtualized rows unmount as they scroll — which would drop a pending
 * expand.
 */
export function useRowClickGestures<TData extends RowData>({
  onRowClick,
  onRowDoubleClick,
  rowClickExpands,
  toggleExpanded,
}: UseRowClickGesturesOptions<TData>) {
  const pendingExpandRef = useRef<number | null>(null);
  const cancelPendingExpand = useCallback(() => {
    if (pendingExpandRef.current === null) return;
    window.clearTimeout(pendingExpandRef.current);
    pendingExpandRef.current = null;
  }, []);

  useEffect(() => cancelPendingExpand, [cancelPendingExpand]);

  // Only a table that binds both gestures has to wait out the double-click
  // window; a click-only table expands on the spot.
  const expandClickDelayMs =
    rowClickExpands && onRowDoubleClick ? ROW_DOUBLE_CLICK_MS : 0;

  const handleExpandOnRowClick = useCallback(
    (row: Row<AppTableFeatures, TData>, event: MouseEvent) => {
      if (!row.getCanExpand() || !clickTargetsRowBody(event.target)) return;

      cancelPendingExpand();
      if (!expandClickDelayMs) {
        toggleExpanded(row);
        return;
      }
      // The second click of a double click carries detail 2 — leave the row
      // alone and let the double-click handler have it.
      if (event.detail > 1) return;

      pendingExpandRef.current = window.setTimeout(() => {
        pendingExpandRef.current = null;
        toggleExpanded(row);
      }, expandClickDelayMs);
    },
    [cancelPendingExpand, expandClickDelayMs, toggleExpanded],
  );

  // A double click is how a word gets selected, so the text-selection half of
  // `clickTargetsRowBody` would veto every one of these — only the control
  // check applies here.
  const handleGuardedRowDoubleClick = useCallback(
    (row: Row<AppTableFeatures, TData>, event: MouseEvent) => {
      cancelPendingExpand();
      if (targetIsRowControl(event.target)) return;
      onRowDoubleClick?.(row, event);
    },
    [cancelPendingExpand, onRowDoubleClick],
  );

  return {
    handleRowClick: rowClickExpands ? handleExpandOnRowClick : onRowClick,
    handleRowDoubleClick: onRowDoubleClick
      ? handleGuardedRowDoubleClick
      : undefined,
  };
}
