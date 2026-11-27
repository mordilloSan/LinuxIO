import { describe, expect, it } from "vitest";

import type { TrashEntry } from "@/api";

import {
  DEFAULT_TRASH_SORT,
  nextTrashSort,
  sortTrashEntries,
  trashFilesFolders,
  trashUsageText,
  trashSortFromSearch,
  trashSortSearch,
  validateTrashSort,
  withFolderSizes,
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

describe("a trashed folder's size", () => {
  const HOME = "/home/alice/.local/share/Trash/files";
  const DISK = "/mnt/data/.Trash-1000/files";
  const entry = (path: string, isDir: boolean, size = 0): TrashEntry => ({
    confirmRestore: false,
    deletedAt: "",
    isDir,
    name: path.slice(path.lastIndexOf("/") + 1),
    originalPath: "",
    path,
    size,
  });
  const entries = [
    entry(`${HOME}/go-build`, true),
    entry(`${HOME}/notes.txt`, false, 10),
    entry(`${DISK}/gitea`, true),
    entry(`${HOME}/photos`, true),
  ];

  it("is asked for once per trash that holds a folder", () => {
    expect(trashFilesFolders(entries)).toEqual([HOME, DISK]);
    expect(trashFilesFolders([entry(`${HOME}/notes.txt`, false)])).toEqual([]);
  });

  it("is filled in from the index and sorts with the files' sizes", () => {
    const sizes = new Map([
      [`${HOME}/go-build`, 36_507_222_016],
      [`${DISK}/gitea`, 4096],
    ]);
    const sized = withFolderSizes(entries, sizes);
    expect(sized.map((item) => item.size)).toEqual([
      36_507_222_016, 10, 4096, 0,
    ]);
    // A folder the index holds nothing for keeps its place among the smallest.
    expect(
      sortTrashEntries(sized, "size", "desc").map((item) => item.name),
    ).toEqual(["go-build", "gitea", "notes.txt", "photos"]);
    // The list's entries are the query's own: they are not written to.
    expect(entries[0].size).toBe(0);
  });
});

describe("what the Trash takes on disk", () => {
  it("is said only when the index holds a size for it", () => {
    expect(trashUsageText(undefined)).toBe("");
    expect(trashUsageText({ partial: false, pending: false, size: 0 })).toBe(
      "",
    );
    expect(
      trashUsageText({ partial: false, pending: false, size: 36_507_222_016 }),
    ).toBe("34 GB");
  });

  it("is a lower bound when a trash is not in the index", () => {
    expect(trashUsageText({ partial: true, pending: false, size: 2048 })).toBe(
      "at least 2 KB",
    );
  });
});
