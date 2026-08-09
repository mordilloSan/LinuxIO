import type { Row, RowData } from "@tanstack/react-table";
import { useMemo } from "react";

import type { AppDataTableDndOptions } from "@/components/tables/AppDataTable";
import type { AppTableFeatures } from "@/components/tables/AppDataTable.types";
import type { AppVirtualDataTableDndOptions } from "@/components/tables/AppVirtualDataTable";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";

interface ReorderableTableDndOptions<TData extends RowData, TItem> {
  surface: ReorderableSurface<TItem>;
  /**
   * Row id, when it differs from the table's own `getRowId`. Must be stable —
   * the returned object is memoized on it.
   */
  getItemId?: (row: Row<AppTableFeatures, TData>) => string;
  handleAriaLabel?: string;
  handleColumnWidth?: string | number;
}

const defaultGetItemId = <TData extends RowData>(
  row: Row<AppTableFeatures, TData>,
) => row.id;

/**
 * Builds the `dnd` prop for an `AppDataTable` from a reorderable surface. Kept
 * memoized because the table is memoized: a fresh object each render would
 * rerender every row.
 */
export function useReorderableTableDnd<TData extends RowData, TItem>({
  getItemId = defaultGetItemId,
  handleAriaLabel,
  handleColumnWidth,
  surface,
}: ReorderableTableDndOptions<TData, TItem>): AppDataTableDndOptions<TData> {
  return useMemo(
    () => ({
      contextProps: surface.dndContextProps,
      editing: surface.editMode,
      getItemId,
      handleAriaLabel,
      handleColumnWidth,
      itemIds: surface.ids,
      pendingItemId: surface.pendingId,
    }),
    [
      getItemId,
      handleAriaLabel,
      handleColumnWidth,
      surface.dndContextProps,
      surface.editMode,
      surface.ids,
      surface.pendingId,
    ],
  );
}

/**
 * The `AppVirtualDataTable` variant. Virtualized rows drag from the row body, so
 * there is no handle column to configure.
 */
export function useVirtualReorderableTableDnd<TData extends RowData, TItem>({
  getItemId = defaultGetItemId,
  surface,
}: Omit<
  ReorderableTableDndOptions<TData, TItem>,
  "handleAriaLabel" | "handleColumnWidth"
>): AppVirtualDataTableDndOptions<TData> {
  return useMemo(
    () => ({
      contextProps: surface.dndContextProps,
      editing: surface.editMode,
      getItemId,
      itemIds: surface.ids,
      pendingItemId: surface.pendingId,
    }),
    [
      getItemId,
      surface.dndContextProps,
      surface.editMode,
      surface.ids,
      surface.pendingId,
    ],
  );
}
