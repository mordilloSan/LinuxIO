import type { Row, RowData } from "@tanstack/react-table";
import { useMemo } from "react";

import type { AppDataTableDndOptions } from "@/components/tables/AppDataTable";
import type { AppTableFeatures } from "@/components/tables/AppDataTable.types";
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
 * Builds the `dnd` prop for a data table from a reorderable surface — both
 * table primitives accept it. Kept memoized because the tables are memoized:
 * a fresh object each render would rerender every row.
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
