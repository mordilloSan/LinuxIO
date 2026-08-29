import type { DirectoryListing } from "@/api";

import type { FileItem, FileResource } from "../../types/filebrowser";

export const normalizeResource = (
  data: DirectoryListing,
  requestedPath: string,
): FileResource => {
  const path = requestedPath === "/" ? "/" : requestedPath.replace(/\/+$/, "");
  const basePath = path === "/" ? "/" : `${path}/`;
  const folders = data.folders ?? [];
  const files = data.files ?? [];

  const items: FileItem[] = [
    ...folders.map((item) => ({ ...item, type: "directory" as const })),
    ...files.map((item) => ({ ...item, type: "file" as const })),
  ].map((item) => {
    const nextPath =
      item.type === "directory"
        ? `${basePath}${item.name}/`
        : `${basePath}${item.name}`;
    return {
      ...item,
      path: nextPath.replace(/\/{2,}/g, "/"),
      modTime: item.modified,
      hidden: item.name.startsWith("."),
    };
  });

  return {
    name: path === "/" ? "/" : path.split("/").pop() || path,
    path,
    size: 0,
    type: "directory",
    hidden: false,
    symlink: false,
    isRegularFile: false,
    items,
  };
};

// Mirrors services.MaxTextFileBytes on the backend.
export const MAX_TEXT_FILE_BYTES = 1_000_000;

// Returns why the backend refused text editing for this entry, or null when
// it can be opened. Derived from the listing fields so no extra request is
// needed.
export const getTextEditBlockedReason = (
  resource: Pick<FileResource, "canOpenAsText" | "isRegularFile" | "size">,
): string | null => {
  if (resource.isRegularFile === true && resource.canOpenAsText === true) {
    return null;
  }
  if ((resource.size ?? 0) >= MAX_TEXT_FILE_BYTES) {
    return "Files of 1 MB or larger can't be opened in the text editor. Download the file to edit it.";
  }
  if (resource.isRegularFile === false) {
    return "Only regular files can be opened in the editor.";
  }
  return "This file isn't plain text, so it can't be opened in the editor.";
};

export const isArchiveFile = (name: string) => {
  const lower = name.toLowerCase();
  return (
    lower.endsWith(".zip") ||
    lower.endsWith(".tar.gz") ||
    lower.endsWith(".tgz")
  );
};

export const stripArchiveExtension = (name: string) => {
  const lower = name.toLowerCase();
  if (lower.endsWith(".tar.gz")) {
    return name.slice(0, -7);
  }
  if (lower.endsWith(".tgz")) {
    return name.slice(0, -4);
  }
  if (lower.endsWith(".zip")) {
    return name.slice(0, -4);
  }
  return name;
};

export const ensureZipExtension = (name: string) => {
  const lower = name.toLowerCase();
  if (lower.endsWith(".zip")) {
    return name;
  }
  return `${name}.zip`;
};

export const ensureTarGzExtension = (name: string) => {
  const lower = name.toLowerCase();
  if (lower.endsWith(".tar.gz")) {
    return name;
  }
  return `${name}.tar.gz`;
};
