import type { TrashEntry } from "@/api";
import { baseName, parentPath } from "@/utils/path";

import type { SortOrder } from "./-types";

export type TrashSortField = "name" | "originalPath" | "deletedAt" | "size";

/**
 * The name an item is shown under: that of where it came from, which a
 * restore puts it back under, or its name in the trash when that is unknown.
 */
export const trashShownName = (entry: TrashEntry): string =>
  (entry.originalPath && baseName(entry.originalPath)) || entry.name;

/** The folder an item came from, or "" when the trash does not say. */
export const trashOriginalFolder = (entry: TrashEntry): string =>
  entry.originalPath ? parentPath(entry.originalPath) : "";

const deletedTime = (entry: TrashEntry) => {
  const time = entry.deletedAt ? Date.parse(entry.deletedAt) : Number.NaN;
  return Number.isNaN(time) ? 0 : time;
};

const compareBy = (field: TrashSortField, a: TrashEntry, b: TrashEntry) => {
  switch (field) {
    case "name":
      return trashShownName(a).localeCompare(trashShownName(b));
    case "originalPath":
      return trashOriginalFolder(a).localeCompare(trashOriginalFolder(b));
    case "deletedAt":
      return deletedTime(a) - deletedTime(b);
    case "size":
      return a.size - b.size;
  }
};

/**
 * The Trash in the order the user picked. Ties keep a stable order by shown
 * name, then by trashed path, so rows do not swap places between reads.
 */
export const sortTrashEntries = (
  entries: readonly TrashEntry[],
  field: TrashSortField,
  order: SortOrder,
): TrashEntry[] =>
  [...entries].sort((a, b) => {
    const primary = compareBy(field, a, b);
    if (primary !== 0) return order === "asc" ? primary : -primary;
    return (
      trashShownName(a).localeCompare(trashShownName(b)) ||
      a.path.localeCompare(b.path)
    );
  });
