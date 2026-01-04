import type { DroppedEntry } from "@/hooks/useFileDragAndDrop";

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
