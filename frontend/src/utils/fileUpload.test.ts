import { describe, expect, it } from "vitest";

import {
  buildEntriesFromFileList,
  mergeDroppedEntries,
  normalizeUploadRelativePath,
  splitName,
  stripNumericSuffix,
} from "@/utils/fileUpload";

function file(name: string, webkitRelativePath?: string): File {
  const item = new File(["content"], name);
  Object.defineProperty(item, "webkitRelativePath", {
    configurable: true,
    value: webkitRelativePath ?? "",
  });
  return item;
}

function fileList(files: File[]): FileList {
  const listLike: Record<number, File> & {
    item: (index: number) => File | null;
    length: number;
  } = {
    length: files.length,
    item: (index: number) => files[index] ?? null,
  };
  files.forEach((item, index) => {
    listLike[index] = item;
  });
  return listLike as unknown as FileList;
}

describe("file upload utilities", () => {
  it("normalizes uploaded relative paths across slash styles", () => {
    expect(normalizeUploadRelativePath("\\folder//nested/file.txt")).toBe(
      "folder/nested/file.txt",
    );
    expect(normalizeUploadRelativePath("///")).toBe("");
  });

  it("splits filenames at the final extension", () => {
    expect(splitName("archive.tar.gz")).toEqual({
      base: "archive.tar",
      ext: ".gz",
    });
    expect(splitName(".env")).toEqual({ base: ".env", ext: "" });
    expect(splitName("Dockerfile")).toEqual({ base: "Dockerfile", ext: "" });
  });

  it("strips numeric duplicate suffixes", () => {
    expect(stripNumericSuffix("file (2)")).toEqual({
      root: "file",
      suffix: 2,
    });
    expect(stripNumericSuffix("file")).toEqual({
      root: "file",
      suffix: null,
    });
  });

  it("builds directory and file entries from a FileList", () => {
    const entries = buildEntriesFromFileList(
      fileList([
        file("compose.yaml", "stacks/app/compose.yaml"),
        file("README.md", "stacks/README.md"),
      ]),
    );

    expect(
      entries.map((entry) => [entry.relativePath, entry.isDirectory]),
    ).toEqual([
      ["stacks", true],
      ["stacks/app", true],
      ["stacks/app/compose.yaml", false],
      ["stacks/README.md", false],
    ]);
  });

  it("merges dropped entries by normalized path with directories first", () => {
    const merged = mergeDroppedEntries(
      [
        { relativePath: "folder//old.txt", isDirectory: false },
        { relativePath: "folder", isDirectory: true },
      ],
      [
        { relativePath: "folder/new.txt", isDirectory: false },
        { relativePath: "folder\\old.txt", isDirectory: false },
      ],
    );

    expect(
      merged.map((entry) => [entry.relativePath, entry.isDirectory]),
    ).toEqual([
      ["folder", true],
      ["folder/new.txt", false],
      ["folder/old.txt", false],
    ]);
  });
});
