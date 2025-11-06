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
      source: data.source,
      modTime,
    };
  });

  return {
    ...data,
    modTime: data.modTime ?? data.modified,
    items,
  };
};

export const buildDownloadUrl = (source: string, path: string, inline = false) => {
  const filesParam = `${encodeURIComponent(source)}::${encodeURIComponent(path)}`;
  const search = new URLSearchParams({ files: filesParam });
  if (inline) search.set("inline", "true");
  return `/navigator/api/raw?${search.toString()}`;
};
