import {
  columnVisibilityFeature,
  createExpandedRowModel,
  createSortedRowModel,
  rowExpandingFeature,
  rowSortingFeature,
  sortFn_alphanumeric,
  sortFn_datetime,
  sortFn_text,
  tableFeatures,
} from "@tanstack/react-table";
import type { ColumnDef, RowData } from "@tanstack/react-table";
import type { CSSProperties } from "react";

export type AppDataTableBreakpoint = "sm" | "md" | "lg" | "xl";

/** A single value compared with Object.is, or a readonly array compared element-wise. */
export type AppDataTableCellRenderKey = unknown;

export interface AppDataTableColumnMeta {
  align?: "left" | "center" | "right";
  cellClassName?: string;
  cellStyle?: CSSProperties;
  className?: string;
  /**
   * Narrows cell invalidation to the values the renderer reads. In the virtual
   * table this is the complete render key, so include `rowIndex` when the cell
   * renderer depends on its position.
   */
  getCellRenderKey?: (
    row: unknown,
    rowIndex: number,
  ) => AppDataTableCellRenderKey;
  headerClassName?: string;
  headerStyle?: CSSProperties;
  hideBelow?: AppDataTableBreakpoint;
  style?: CSSProperties;
  width?: string | number;
}

// v9 tree-shakeable feature registry, shared by AppDataTable and
// AppVirtualDataTable: only sorting, expanding, and responsive column
// visibility ship in the bundle. The type-only `columnMeta` slot replaces the
// v8 `declare module` ColumnMeta augmentation.

export const appTableFeatures = tableFeatures({
  columnVisibilityFeature,
  rowExpandingFeature,
  rowSortingFeature,
  expandedRowModel: createExpandedRowModel(),
  sortedRowModel: createSortedRowModel(),
  sortFns: {
    alphanumeric: sortFn_alphanumeric,
    datetime: sortFn_datetime,
    text: sortFn_text,
  },
  columnMeta: {} as AppDataTableColumnMeta,
});

export type AppTableFeatures = typeof appTableFeatures;

export type AppDataTableColumnDef<
  TData extends RowData,
  TValue = unknown,
> = ColumnDef<AppTableFeatures, TData, TValue> & {
  meta?: AppDataTableColumnMeta;
};
