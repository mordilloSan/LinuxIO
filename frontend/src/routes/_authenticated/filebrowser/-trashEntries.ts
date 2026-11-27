import type { TrashEntry, TrashUsageResponse } from "@/api";
import type { SearchInput } from "@/routes/-search";
import { formatFileSize } from "@/utils/formatters";
import { baseName, parentPath } from "@/utils/path";

import type { SortOrder } from "./-types";

const TRASH_SORT_FIELDS = [
  "name",
  "originalPath",
  "deletedAt",
  "size",
] as const;
export type TrashSortField = (typeof TRASH_SORT_FIELDS)[number];

export interface TrashSort {
  field: TrashSortField;
  order: SortOrder;
}

/** Newest deleted first, as the Trash opens. */
export const DEFAULT_TRASH_SORT: TrashSort = {
  field: "deletedAt",
  order: "desc",
};

const isSortField = (value: unknown): value is TrashSortField =>
  TRASH_SORT_FIELDS.includes(value as TrashSortField);
const isSortOrder = (value: unknown): value is SortOrder =>
  value === "asc" || value === "desc";

/**
 * The Trash's sort as the file browser's URL keeps it, `sort` and `order`
 * beside `trash`. An unknown column or order is dropped, so the Trash falls
 * back to its default.
 */
export const validateTrashSort = (
  search: SearchInput,
): { order?: SortOrder; sort?: TrashSortField } => ({
  ...(isSortField(search.sort) ? { sort: search.sort } : {}),
  ...(isSortOrder(search.order) ? { order: search.order } : {}),
});

/** The sort a validated URL names, each part defaulting on its own. */
export const trashSortFromSearch = (search: {
  order?: SortOrder;
  sort?: TrashSortField;
}): TrashSort => ({
  field: search.sort ?? DEFAULT_TRASH_SORT.field,
  order: search.order ?? DEFAULT_TRASH_SORT.order,
});

/** The search parameters for a sort, leaving the defaults out of the URL. */
export const trashSortSearch = (
  sort: TrashSort,
): { order: SortOrder | undefined; sort: TrashSortField | undefined } => ({
  order: sort.order === DEFAULT_TRASH_SORT.order ? undefined : sort.order,
  sort: sort.field === DEFAULT_TRASH_SORT.field ? undefined : sort.field,
});

/**
 * Sorting again by the same column flips the order, as in a folder; another
 * column starts ascending.
 */
export const nextTrashSort = (
  current: TrashSort,
  field: TrashSortField,
): TrashSort => ({
  field,
  order: current.field === field && current.order === "asc" ? "desc" : "asc",
});

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

/**
 * The `files` folder of every trash that holds a trashed folder: the index
 * keeps the sizes of a folder's subfolders, so one read per trash sizes all
 * of them.
 */
export const trashFilesFolders = (entries: readonly TrashEntry[]): string[] => [
  ...new Set(
    entries
      .filter((entry) => entry.isDir)
      .map((entry) => parentPath(entry.path)),
  ),
];

/**
 * The entries with each folder's size filled in from `sizes`, by trashed
 * path, so the list sorts and shows them beside the files' own. A folder
 * `sizes` does not hold keeps the list's 0 and is shown without a size.
 */
export const withFolderSizes = (
  entries: readonly TrashEntry[],
  sizes: ReadonlyMap<string, number>,
): TrashEntry[] =>
  entries.map((entry) => {
    const size = entry.isDir ? sizes.get(entry.path) : undefined;
    return size === undefined ? entry : { ...entry, size };
  });

/**
 * What the Trash takes on disk, as the Trash button and the Trash say it;
 * "" for an empty Trash and while the index gives no size. A trash the index
 * does not hold makes the size a lower bound.
 */
export const trashUsageText = (usage: TrashUsageResponse | undefined): string =>
  usage && usage.size > 0
    ? `${usage.partial ? "at least " : ""}${formatFileSize(usage.size, 1)}`
    : "";
