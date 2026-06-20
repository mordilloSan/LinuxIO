import type { UniqueIdentifier } from "@dnd-kit/core";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Icon } from "@iconify/react";
import {
  flexRender,
  getCoreRowModel,
  getExpandedRowModel,
  getSortedRowModel,
  useReactTable,
} from "@tanstack/react-table";
import type {
  Cell,
  Column,
  ExpandedState,
  OnChangeFn,
  Row,
  RowData,
  SortingState,
  VisibilityState,
} from "@tanstack/react-table";
import React, { useMemo, useState } from "react";

import type {
  AppDataTableBreakpoint,
  AppDataTableCellRenderKey,
  AppDataTableColumnDef,
} from "@/components/tables/AppDataTable.types";
import AppCollapse from "@/components/ui/AppCollapse";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import {
  EASING_STANDARD_CSS,
  TRANSITION_DURATION_STANDARD_MS,
  shadowSm,
} from "@/constants";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

import "./app-virtual-data-table.css";

const DETAIL_ANIMATION_CSS = `${TRANSITION_DURATION_STANDARD_MS}ms ${EASING_STANDARD_CSS}`;

export type {
  AppDataTableBreakpoint,
  AppDataTableColumnDef,
  AppDataTableColumnMeta,
} from "@/components/tables/AppDataTable.types";

export type AppDataTableRowAttributes = React.HTMLAttributes<HTMLDivElement> & {
  ref?: React.Ref<HTMLDivElement>;
};

export interface AppDataTableRowRenderProps<TData extends RowData> {
  cells: React.ReactNode;
  isSelected: boolean;
  row: Row<TData>;
  rowIndex: number;
  rowProps: AppDataTableRowAttributes;
}

export interface AppDataTableDndOptions<TData extends RowData> {
  enabled?: boolean;
  getItemId: (row: Row<TData>) => UniqueIdentifier;
  handleAriaLabel?: string;
  handleColumnWidth?: string | number;
}

export interface AppDataTableProps<TData extends RowData> {
  ariaLabel?: string;
  className?: string;
  columns: AppDataTableColumnDef<TData, unknown>[];
  data: TData[];
  density?: "comfortable" | "compact";
  dnd?: AppDataTableDndOptions<TData>;
  emptyMessage?: string;
  enableSorting?: boolean;
  expanded?: ExpandedState;
  fillAvailable?: boolean;
  getRowCanExpand?: (row: Row<TData>) => boolean;
  getRowAttributes?: (row: Row<TData>) => AppDataTableRowAttributes;
  getRowId: (row: TData, index: number, parent?: Row<TData>) => string;
  height?: React.CSSProperties["height"];
  manualSorting?: boolean;
  maxHeight?: React.CSSProperties["maxHeight"];
  onExpandedChange?: OnChangeFn<ExpandedState>;
  onRowClick?: (row: Row<TData>, event: React.MouseEvent) => void;
  onRowContextMenu?: (row: Row<TData>, event: React.MouseEvent) => void;
  onRowDoubleClick?: (row: Row<TData>, event: React.MouseEvent) => void;
  onSortingChange?: OnChangeFn<SortingState>;
  renderExpandedContent?: (row: Row<TData>) => React.ReactNode;
  renderRow?: (props: AppDataTableRowRenderProps<TData>) => React.ReactNode;
  selectedRowId?: string | null;
  showHeader?: boolean;
  sorting?: SortingState;
  style?: React.CSSProperties;
  variant?: "default" | "embedded";
}

function columnTrack<TData extends RowData>(column: Column<TData, unknown>) {
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
  column: AppDataTableColumnDef<TData, unknown>,
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

function mergeRefs<T>(
  ...refs: Array<React.Ref<T> | undefined>
): React.RefCallback<T> {
  return (node) => {
    refs.forEach((ref) => {
      if (!ref) return;
      if (typeof ref === "function") {
        ref(node);
        return;
      }
      ref.current = node;
    });
  };
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
  cell: Cell<TData, unknown>,
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
  cell: Cell<TData, unknown>;
  renderKey: AppDataTableCellRenderKey;
}

function AppDataTableCell<TData extends RowData>({
  cell,
}: AppDataTableCellProps<TData>) {
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

const MemoizedAppDataTableCell = React.memo(
  AppDataTableCell,
  (previous, next) =>
    previous.cell.id === next.cell.id &&
    areCellRenderKeysEqual(previous.renderKey, next.renderKey),
) as typeof AppDataTableCell;

interface AppDataTableBodyRowProps<TData extends RowData> {
  cells: React.ReactNode;
  isSelected: boolean;
  renderRow?: (props: AppDataTableRowRenderProps<TData>) => React.ReactNode;
  row: Row<TData>;
  rowIndex: number;
  rowProps: AppDataTableRowAttributes;
}

function AppDataTableBodyRow<TData extends RowData>({
  cells,
  isSelected,
  renderRow,
  row,
  rowIndex,
  rowProps,
}: AppDataTableBodyRowProps<TData>) {
  if (renderRow) {
    return renderRow({
      cells,
      isSelected,
      row,
      rowIndex,
      rowProps,
    });
  }

  return <div {...rowProps}>{cells}</div>;
}

interface AppDataTableSortableBodyRowProps<TData extends RowData> extends Omit<
  AppDataTableBodyRowProps<TData>,
  "cells"
> {
  dnd: AppDataTableDndOptions<TData>;
  renderCells: (handle: React.ReactNode) => React.ReactNode;
}

function AppDataTableSortableBodyRow<TData extends RowData>({
  dnd,
  isSelected,
  renderCells,
  renderRow,
  row,
  rowIndex,
  rowProps,
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
  const transformValue = CSS.Transform.toString(transform);
  const rowTransition = [rowProps.style?.transition, transition]
    .filter(Boolean)
    .join(", ");
  const dragHandle = (
    <span
      {...attributes}
      {...listeners}
      aria-label={dnd.handleAriaLabel ?? "Reorder row"}
      className="app-vdt__drag-handle"
    >
      <Icon height={20} icon="mdi:drag" width={20} />
    </span>
  );

  return (
    <AppDataTableBodyRow
      cells={renderCells(dragHandle)}
      isSelected={isSelected}
      renderRow={renderRow}
      row={row}
      rowIndex={rowIndex}
      rowProps={{
        ...rowProps,
        ref: mergeRefs(rowProps.ref, setNodeRef),
        style: {
          ...rowProps.style,
          opacity: isDragging ? 0.45 : rowProps.style?.opacity,
          transform: transformValue || rowProps.style?.transform,
          transition: rowTransition || undefined,
        },
      }}
    />
  );
}

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
  onExpandedChange,
  onRowClick,
  onRowContextMenu,
  onRowDoubleClick,
  onSortingChange,
  renderExpandedContent,
  renderRow,
  selectedRowId,
  showHeader = true,
  sorting,
  style,
  variant = "default",
}: AppDataTableProps<TData>) {
  "use no memo";

  const theme = useAppTheme();
  const isDark = theme.palette.mode === "dark";
  const belowSm = useAppMediaQuery(theme.breakpoints.down("sm"));
  const belowMd = useAppMediaQuery(theme.breakpoints.down("md"));
  const belowLg = useAppMediaQuery(theme.breakpoints.down("lg"));
  const belowXl = useAppMediaQuery(theme.breakpoints.down("xl"));
  const [internalExpanded, setInternalExpanded] = useState<ExpandedState>({});
  const [internalSorting, setInternalSorting] = useState<SortingState>([]);

  const columnVisibility = useMemo<VisibilityState>(() => {
    const below: Record<AppDataTableBreakpoint, boolean> = {
      sm: belowSm,
      md: belowMd,
      lg: belowLg,
      xl: belowXl,
    };
    const next: VisibilityState = {};

    columns.forEach((column, index) => {
      const hideBelow = column.meta?.hideBelow;
      if (!hideBelow) return;
      next[getColumnDefId(column, index)] = !below[hideBelow];
    });

    return next;
  }, [belowLg, belowMd, belowSm, belowXl, columns]);

  const resolvedExpanded = expanded ?? internalExpanded;
  const resolvedSorting = sorting ?? internalSorting;

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

  // TanStack Table exposes dynamic helper functions that React Compiler cannot memoize safely.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    columns,
    data,
    enableSorting,
    enableSortingRemoval: false,
    getCoreRowModel: getCoreRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
    getRowCanExpand: (row) =>
      Boolean(renderExpandedContent && (getRowCanExpand?.(row) ?? true)),
    getRowId,
    getSortedRowModel: getSortedRowModel(),
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
  const isEmbedded = variant === "embedded";
  const headRowBg = isEmbedded
    ? "transparent"
    : alpha(theme.palette.text.primary, 0.08);
  const selectedBg = alpha(theme.palette.primary.main, isDark ? 0.15 : 0.1);
  const altBg = isEmbedded
    ? "transparent"
    : alpha(theme.palette.text.primary, isDark ? 0.04 : 0.05);
  const hoverBg = alpha(theme.palette.primary.main, 0.08);
  const isInteractive = Boolean(onRowClick || onRowDoubleClick);
  const hasExpandColumn = Boolean(renderExpandedContent);
  const hasDragColumn = Boolean(dnd);
  const visibleColumns = table.getVisibleLeafColumns();
  const gridTemplate = [
    ...(hasDragColumn
      ? [
          typeof dnd?.handleColumnWidth === "number"
            ? `${dnd.handleColumnWidth}px`
            : (dnd?.handleColumnWidth ?? "32px"),
        ]
      : []),
    ...visibleColumns.map((column) => columnTrack(column)),
    ...(hasExpandColumn ? ["40px"] : []),
  ].join(" ");

  return (
    <div
      aria-label={ariaLabel}
      className={[
        "app-vdt",
        "app-vdt--normal",
        fillAvailable && "app-vdt--fill",
        isEmbedded && "app-vdt--embedded",
        density === "compact" && "app-vdt--compact",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
      role="table"
      style={
        {
          "--app-vdt-alt-bg": altBg,
          "--app-vdt-grid": gridTemplate,
          "--app-vdt-head-bg": headRowBg,
          "--app-vdt-hover-bg": hoverBg,
          "--app-vdt-selected-bg": selectedBg,
          boxShadow: isEmbedded ? "none" : shadowSm,
          height: height ?? (fillAvailable ? "100%" : undefined),
          maxHeight,
          minHeight: fillAvailable ? 0 : undefined,
          ...style,
        } as React.CSSProperties
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
              {hasDragColumn && (
                <div
                  aria-hidden="true"
                  className="app-vdt__cell app-vdt__cell--head app-vdt__cell--drag"
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

      <div className="app-vdt__scroll custom-scrollbar" role="presentation">
        <div className="app-vdt__body" role="rowgroup">
          {rows.map((row, rowIndex) => {
            const isExpanded = row.getIsExpanded();
            const isSelected = row.id === selectedRowId;
            const canExpand = row.getCanExpand();
            const rowAttributes = getRowAttributes?.(row);
            const rowAttributeOnClick = rowAttributes?.onClick;
            const rowAttributeOnContextMenu = rowAttributes?.onContextMenu;
            const rowAttributeOnDoubleClick = rowAttributes?.onDoubleClick;
            const renderCells = (dragHandle?: React.ReactNode) => (
              <>
                {hasDragColumn && (
                  <div
                    className="app-vdt__cell app-vdt__cell--drag"
                    role="cell"
                  >
                    {dragHandle}
                  </div>
                )}
                {row.getVisibleCells().map((cell) => (
                  <MemoizedAppDataTableCell
                    cell={cell}
                    key={cell.id}
                    renderKey={getCellRenderKey(cell, rowIndex)}
                  />
                ))}
                {hasExpandColumn && (
                  <div
                    className="app-vdt__cell app-vdt__cell--expand"
                    role="cell"
                  >
                    {canExpand && (
                      <AppTooltip
                        title={isExpanded ? "Collapse row" : "Expand row"}
                      >
                        <AppIconButton
                          aria-expanded={isExpanded}
                          aria-label={
                            isExpanded ? "Collapse row" : "Expand row"
                          }
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
                              transform: isExpanded
                                ? "rotate(180deg)"
                                : "rotate(0deg)",
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
                "app-vdt__row",
                "app-vdt__row--body",
                isInteractive && "app-vdt__row--interactive",
                isSelected && "app-vdt__row--selected",
                rowIndex % 2 === 1 && "app-vdt__row--alt",
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
                if (!event.defaultPrevented) {
                  onRowDoubleClick?.(row, event);
                }
              },
              role: "row",
              style: rowAttributes?.style,
            };

            return (
              <React.Fragment key={row.id}>
                {dnd ? (
                  <AppDataTableSortableBodyRow
                    dnd={dnd}
                    isSelected={isSelected}
                    renderCells={renderCells}
                    renderRow={renderRow}
                    row={row}
                    rowIndex={rowIndex}
                    rowProps={rowProps}
                  />
                ) : (
                  <AppDataTableBodyRow
                    cells={renderCells()}
                    isSelected={isSelected}
                    renderRow={renderRow}
                    row={row}
                    rowIndex={rowIndex}
                    rowProps={rowProps}
                  />
                )}
                {renderExpandedContent && (
                  <AppCollapse in={isExpanded} unmountOnExit>
                    <div className="app-vdt__detail" role="row">
                      <div
                        aria-colspan={
                          visibleColumns.length + (hasExpandColumn ? 1 : 0)
                        }
                        className="app-vdt__detail-cell"
                        role="cell"
                      >
                        {renderExpandedContent(row)}
                      </div>
                    </div>
                  </AppCollapse>
                )}
              </React.Fragment>
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
}

export default AppDataTable;
