import { ApiResource, FileItem, FileResource } from "../../types/filebrowser";

export const normalizeResource = (data: ApiResource): FileResource => {
  if (data.type !== "directory") {
    return data;
  }

  const folders = data.folders ?? [];
  const files = data.files ?? [];

  const items: FileItem[] = [...folders, ...files].map((item) => {
    const basePath = data.path === "/" ? "/" : data.path;
    const nextPath =
      item.type === "directory"
        ? `${basePath}${item.name}/`
        : `${basePath}${item.name}`;
    const modTime = item.modTime ?? item.modified;

    return {
      ...item,
      path: nextPath.replace(/\/{2,}/g, "/"),
      modTime,
    };
  });

  return {
    ...data,
    modTime: data.modTime ?? data.modified,
    items,
  };
};

export const buildDownloadUrl = (path: string, inline = false) => {
  const search = new URLSearchParams({ files: path });
  if (inline) search.set("inline", "true");
  return `/navigator/api/raw?${search.toString()}`;
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
