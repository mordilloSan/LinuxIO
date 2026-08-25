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

export type AppVirtualTableBreakpoint = "sm" | "md" | "lg" | "xl";

/** A single value compared with Object.is, or a readonly array compared element-wise. */
export type AppVirtualTableCellRenderKey = unknown;

export interface AppVirtualTableColumnMeta {
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
  ) => AppVirtualTableCellRenderKey;
  headerClassName?: string;
  headerStyle?: CSSProperties;
  hideBelow?: AppVirtualTableBreakpoint;
  style?: CSSProperties;
  width?: string | number;
}

// v9 tree-shakeable feature registry for AppVirtualTable: only sorting,
// expanding, and responsive column visibility ship in the bundle. The
// type-only `columnMeta` slot replaces the v8 `declare module` ColumnMeta
// augmentation.

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
  // oxlint-disable-next-line typescript/no-unnecessary-type-assertion -- load-bearing: this assertion is what carries AppVirtualTableColumnMeta into `typeof appTableFeatures`; removing it types the slot as `{}` and erases column meta everywhere.
  columnMeta: {} as AppVirtualTableColumnMeta,
});

export type AppTableFeatures = typeof appTableFeatures;

export type AppVirtualTableColumnDef<
  TData extends RowData,
  TValue = unknown,
> = ColumnDef<AppTableFeatures, TData, TValue> & {
  meta?: AppVirtualTableColumnMeta;
};
