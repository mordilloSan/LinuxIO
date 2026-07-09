import type { ColumnDef, RowData } from "@tanstack/react-table";
import type { CSSProperties } from "react";

export type AppDataTableBreakpoint = "sm" | "md" | "lg" | "xl";

export type AppDataTableCellRenderKey = unknown | readonly unknown[];

export interface AppDataTableColumnMeta {
  align?: "left" | "center" | "right";
  cellClassName?: string;
  cellStyle?: CSSProperties;
  className?: string;
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

/* eslint-disable @typescript-eslint/no-empty-interface, @typescript-eslint/no-empty-object-type, @typescript-eslint/no-unused-vars */
declare module "@tanstack/react-table" {
  interface ColumnMeta<
    TData extends RowData,
    TValue,
  > extends AppDataTableColumnMeta {}
}
/* eslint-enable @typescript-eslint/no-empty-interface, @typescript-eslint/no-empty-object-type, @typescript-eslint/no-unused-vars */

export type AppDataTableColumnDef<TData, TValue = unknown> = ColumnDef<
  TData,
  TValue
> & {
  meta?: AppDataTableColumnMeta;
};
