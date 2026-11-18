import { describe, expect, it } from "vitest";

import {
  DEFAULT_TRASH_SORT,
  nextTrashSort,
  trashSortFromSearch,
  trashSortSearch,
  validateTrashSort,
} from "./-trashEntries";

describe("the Trash's sort in the URL", () => {
  it("keeps a known column and order and drops anything else", () => {
    expect(validateTrashSort({ order: "asc", sort: "name" })).toEqual({
      order: "asc",
      sort: "name",
    });
    expect(validateTrashSort({ order: "up", sort: "modTime" })).toEqual({});
    expect(validateTrashSort({ order: 1, sort: ["size"] })).toEqual({});
    expect(validateTrashSort({})).toEqual({});
  });

  it("reads newest deleted first when the URL names no sort", () => {
    expect(trashSortFromSearch({})).toEqual(DEFAULT_TRASH_SORT);
    expect(DEFAULT_TRASH_SORT).toEqual({ field: "deletedAt", order: "desc" });
    expect(trashSortFromSearch({ sort: "size" })).toEqual({
      field: "size",
      order: "desc",
    });
    expect(trashSortFromSearch({ order: "asc" })).toEqual({
      field: "deletedAt",
      order: "asc",
    });
  });

  it("leaves the defaults out of the URL", () => {
    expect(trashSortSearch(DEFAULT_TRASH_SORT)).toEqual({
      order: undefined,
      sort: undefined,
    });
    expect(trashSortSearch({ field: "name", order: "asc" })).toEqual({
      order: "asc",
      sort: "name",
    });
    // What it writes reads back as the same sort.
    for (const sort of [
      { field: "name", order: "desc" },
      { field: "deletedAt", order: "asc" },
      { field: "originalPath", order: "asc" },
    ] as const) {
      expect(trashSortFromSearch(trashSortSearch(sort))).toEqual(sort);
    }
  });

  it("flips the order on the same column and starts another ascending", () => {
    expect(nextTrashSort(DEFAULT_TRASH_SORT, "deletedAt")).toEqual({
      field: "deletedAt",
      order: "asc",
    });
    expect(nextTrashSort({ field: "name", order: "asc" }, "name")).toEqual({
      field: "name",
      order: "desc",
    });
    expect(nextTrashSort({ field: "name", order: "desc" }, "size")).toEqual({
      field: "size",
      order: "asc",
    });
  });
});
