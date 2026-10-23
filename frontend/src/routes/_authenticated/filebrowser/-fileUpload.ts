export interface DroppedEntry {
  file?: File;
  isDirectory: boolean;
  relativePath: string;
}

/**
 * Normalize upload relative path by removing redundant slashes and empty segments
 */
export const normalizeUploadRelativePath = (path: string): string =>
  path
    .split(/[\\/]+/)
    .filter(Boolean)
    .join("/");

/**
 * Split filename into base name and extension
 * @example splitName("file.txt") => { base: "file", ext: ".txt" }
 * @example splitName("archive.tar.gz") => { base: "archive.tar", ext: ".gz" }
 */
export const splitName = (name: string): { base: string; ext: string } => {
  const idx = name.lastIndexOf(".");
  if (idx > 0) {
    return {
      base: name.slice(0, idx),
      ext: name.slice(idx),
    };
  }
  return { base: name, ext: "" };
};

/**
 * Strip numeric suffix from a file/folder name
 * @example stripNumericSuffix("file (2)") => { root: "file", suffix: 2 }
 * @example stripNumericSuffix("file") => { root: "file", suffix: null }
 */
export const stripNumericSuffix = (
  base: string,
): { root: string; suffix: number | null } => {
  const match = /^(.+?)(?: \((\d+)\))?$/.exec(base);
  if (!match) {
    return { root: base, suffix: null };
  }
  return {
    root: match[1],
    suffix: match[2] ? parseInt(match[2], 10) : null,
  };
};

/**
 * Build DroppedEntry array from FileList
 * Extracts files and infers directory structure from webkitRelativePath
 */
export const buildEntriesFromFileList = (
  files: FileList | null,
): DroppedEntry[] => {
  if (!files?.length) return [];
  const directories = new Set<string>();
  const entries: DroppedEntry[] = [];

  Array.from(files).forEach((file) => {
    const relativePath = normalizeUploadRelativePath(
      (file as any).webkitRelativePath || file.name,
    );
    if (!relativePath) return;

    entries.push({
      file,
      relativePath,
      isDirectory: false,
    });

    // Infer directory structure from file path
    const segments = relativePath.split("/");
    segments.pop(); // Remove filename
    let current = "";
    segments.forEach((segment) => {
      if (!segment) return;
      current = current ? `${current}/${segment}` : segment;
      const normalized = normalizeUploadRelativePath(current);
      if (normalized) {
        directories.add(normalized);
      }
    });
  });

  const directoryEntries = Array.from(directories).map((relativePath) => ({
    relativePath,
    isDirectory: true,
  }));

  return [...directoryEntries, ...entries];
};

/**
 * Merge two arrays of DroppedEntry, removing duplicates and sorting
 * Directories come before files, then sorted alphabetically
 */
export const mergeDroppedEntries = (
  existing: DroppedEntry[],
  additions: DroppedEntry[],
): DroppedEntry[] => {
  if (!additions.length) return existing;
  const map = new Map<string, DroppedEntry>();

  [...existing, ...additions].forEach((entry) => {
    const normalized = normalizeUploadRelativePath(entry.relativePath);
    if (!normalized) return;
    const key = `${entry.isDirectory ? "dir" : "file"}::${normalized}`;
    map.set(key, {
      ...entry,
      relativePath: normalized,
    });
  });

  return Array.from(map.values()).sort((a, b) => {
    if (a.isDirectory !== b.isDirectory) {
      return a.isDirectory ? -1 : 1;
    }
    return a.relativePath.localeCompare(b.relativePath);
  });
};

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
          relativePath: normalizeUploadRelativePath(relativePath),
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
    relativePath: normalizeUploadRelativePath(currentPath),
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
      relativePath: normalizeUploadRelativePath(relativePath),
      isDirectory: false,
    });
    return;
  }

  if (handle.kind === "directory" && typeof handle.entries === "function") {
    const dirPath = parentPath ? `${parentPath}/${handle.name}` : handle.name;
    results.push({
      relativePath: normalizeUploadRelativePath(dirPath),
      isDirectory: true,
    });

    for await (const [, childHandle] of handle.entries()) {
      await collectFromHandle(childHandle, dirPath, results);
    }
  }
};

// Dropped DataTransfer reading: FileSystemHandles first, then webkit entries,
// then the flat file list.
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
      const relativePath = normalizeUploadRelativePath(
        (file as any).webkitRelativePath || file.name,
      );
      if (!relativePath) return;
      results.push({ file, relativePath, isDirectory: false });
    });
  }

  if (results.length === 0 && dataTransfer.files?.length) {
    Array.from(dataTransfer.files).forEach((file) => {
      const relativePath = normalizeUploadRelativePath(
        (file as any).webkitRelativePath || file.name,
      );
      if (!relativePath) return;
      results.push({ file, relativePath, isDirectory: false });
    });
  }

  const deduped: DroppedEntry[] = [];
  const seen = new Set<string>();
  for (const dropped of results) {
    const path = normalizeUploadRelativePath(dropped.relativePath);
    if (!path) continue;
    const key = `${path}::${dropped.isDirectory ? "dir" : "file"}`;
    if (seen.has(key)) continue;
    seen.add(key);
    deduped.push({ ...dropped, relativePath: path });
  }

  return deduped;
};
