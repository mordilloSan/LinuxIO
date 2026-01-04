import { useCallback } from "react";

type DroppedEntry = {
  file?: File;
  relativePath: string;
  isDirectory: boolean;
};

const normalizeRelativePath = (path: string) =>
  path
    .split(/[\\/]+/)
    .filter(Boolean)
    .join("/");

const readFileFromEntry = (
  entry: any,
  parentPath: string,
): Promise<DroppedEntry> => {
  return new Promise((resolve, reject) => {
    entry.file(
      (file: File) => {
        const relativePath = parentPath
          ? `${parentPath}/${file.name}`
          : file.name;
        resolve({
          file,
          relativePath: normalizeRelativePath(relativePath),
          isDirectory: false,
        });
      },
      (err: any) => reject(err),
    );
  });
};

const readDirectoryEntry = async (
  entry: any,
  parentPath: string,
  results: DroppedEntry[],
) => {
  const reader = entry.createReader();
  const readEntries = (): Promise<any[]> =>
    new Promise((resolve, reject) => {
      reader.readEntries(resolve, reject);
    });

  const currentPath = parentPath ? `${parentPath}/${entry.name}` : entry.name;
  results.push({
    relativePath: normalizeRelativePath(currentPath),
    isDirectory: true,
  });

  let entries = await readEntries();
  while (entries.length) {
    for (const child of entries) {
      if (child.isFile) {
        results.push(await readFileFromEntry(child, currentPath));
      } else if (child.isDirectory) {
        await readDirectoryEntry(child, currentPath, results);
      }
    }
    entries = await readEntries();
  }
};

const collectFromHandle = async (
  handle: any,
  parentPath: string,
  results: DroppedEntry[],
) => {
  if (!handle) return;
  if (handle.kind === "file" && typeof handle.getFile === "function") {
    const file = await handle.getFile();
    const relativePath = parentPath ? `${parentPath}/${file.name}` : file.name;
    results.push({
      file,
      relativePath: normalizeRelativePath(relativePath),
      isDirectory: false,
    });
    return;
  }

  if (handle.kind === "directory" && typeof handle.entries === "function") {
    const dirPath = parentPath ? `${parentPath}/${handle.name}` : handle.name;
    results.push({
      relativePath: normalizeRelativePath(dirPath),
      isDirectory: true,
    });

    for await (const [, childHandle] of handle.entries()) {
      await collectFromHandle(childHandle, dirPath, results);
    }
  }
};

export const extractDroppedEntries = async (
  dataTransfer: DataTransfer,
): Promise<DroppedEntry[]> => {
  const results: DroppedEntry[] = [];
  const items = Array.from(dataTransfer.items || []).filter(
    (item) => item.kind === "file",
  );

  try {
    const handles = await Promise.all(
      items.map((item: any) => item.getAsFileSystemHandle?.()),
    );
    const validHandles = handles.filter(Boolean);
    if (validHandles.length) {
      for (const handle of validHandles) {
        await collectFromHandle(handle, "", results);
      }
    }
  } catch (err) {
    console.error("Failed to read dropped handles", err);
  }

  const hasEntries = items.some(
    (item) => typeof (item as any).webkitGetAsEntry === "function",
  );

  if (hasEntries) {
    for (const item of items) {
      const entry = (item as any).webkitGetAsEntry?.();
      if (!entry) continue;
      if (entry.isFile) {
        results.push(await readFileFromEntry(entry, ""));
      } else if (entry.isDirectory) {
        await readDirectoryEntry(entry, "", results);
      }
    }
  } else {
    Array.from(dataTransfer.files || []).forEach((file) => {
      const relativePath = normalizeRelativePath(
        (file as any).webkitRelativePath || file.name,
      );
      if (!relativePath) return;
      results.push({ file, relativePath, isDirectory: false });
    });
  }

  if (results.length === 0 && dataTransfer.files?.length) {
    Array.from(dataTransfer.files).forEach((file) => {
      const relativePath = normalizeRelativePath(
        (file as any).webkitRelativePath || file.name,
      );
      if (!relativePath) return;
      results.push({ file, relativePath, isDirectory: false });
    });
  }

  const deduped: DroppedEntry[] = [];
  const seen = new Set<string>();
  for (const dropped of results) {
    const path = normalizeRelativePath(dropped.relativePath);
    if (!path) continue;
    const key = `${path}::${dropped.isDirectory ? "dir" : "file"}`;
    if (seen.has(key)) continue;
    seen.add(key);
    deduped.push({ ...dropped, relativePath: path });
  }

  return deduped;
};

export const useFileDroppedEntries = () =>
  useCallback(
    (dataTransfer: DataTransfer) => extractDroppedEntries(dataTransfer),
    [],
  );

export type { DroppedEntry };
