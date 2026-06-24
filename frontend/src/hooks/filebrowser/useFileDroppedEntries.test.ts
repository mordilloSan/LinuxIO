import { describe, expect, it } from "vitest";

import { extractDroppedEntries } from "@/hooks/filebrowser/useFileDroppedEntries";

function file(name: string, webkitRelativePath?: string): File {
  const item = new File(["content"], name);
  Object.defineProperty(item, "webkitRelativePath", {
    configurable: true,
    value: webkitRelativePath ?? "",
  });
  return item;
}

function transfer({
  files = [],
  items = [],
}: {
  files?: File[];
  items?: unknown[];
}): DataTransfer {
  const fileList: Record<number, File> & {
    item: (index: number) => File | null;
    length: number;
  } = {
    length: files.length,
    item: (index: number) => files[index] ?? null,
  };
  files.forEach((file, index) => {
    fileList[index] = file;
  });

  return {
    files: fileList,
    items,
  } as unknown as DataTransfer;
}

describe("extractDroppedEntries", () => {
  it("falls back to DataTransfer files and normalizes relative paths", async () => {
    const entries = await extractDroppedEntries(
      transfer({
        files: [
          file("compose.yaml", "stacks\\app//compose.yaml"),
          file("README.md"),
        ],
      }),
    );

    expect(
      entries.map((entry) => [entry.relativePath, entry.isDirectory]),
    ).toEqual([
      ["stacks/app/compose.yaml", false],
      ["README.md", false],
    ]);
  });

  it("extracts recursive webkit directory entries", async () => {
    const compose = file("compose.yaml");
    const fileEntry = {
      isFile: true,
      isDirectory: false,
      file: (success: (file: File) => void) => success(compose),
    };
    const directoryEntry = {
      isFile: false,
      isDirectory: true,
      name: "stack",
      createReader: () => {
        const batches = [[fileEntry], []];
        return {
          readEntries: (success: (entries: unknown[]) => void) =>
            success(batches.shift() ?? []),
        };
      },
    };

    const entries = await extractDroppedEntries(
      transfer({
        items: [
          {
            kind: "file",
            webkitGetAsEntry: () => directoryEntry,
          },
        ],
      }),
    );

    expect(
      entries.map((entry) => [entry.relativePath, entry.isDirectory]),
    ).toEqual([
      ["stack", true],
      ["stack/compose.yaml", false],
    ]);
  });

  it("deduplicates entries by normalized path and kind", async () => {
    const duplicate = file("compose.yaml", "stack/compose.yaml");

    const entries = await extractDroppedEntries(
      transfer({
        files: [duplicate, duplicate],
      }),
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      isDirectory: false,
      relativePath: "stack/compose.yaml",
    });
  });
});
