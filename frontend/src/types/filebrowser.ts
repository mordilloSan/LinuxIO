export type ViewMode = "list" | "compact" | "normal" | "gallery";

export type ApiItem = {
  name: string;
  type: string;
  size?: number;
  modTime?: string;
  modified?: string;
  hidden?: boolean;
  hasPreview?: boolean;
  symlink?: boolean;
};

export type ApiResource = {
  name: string;
  path: string;
  type: string;
  source: string;
  files?: ApiItem[];
  folders?: ApiItem[];
  content?: string;
  parentDirItems?: ApiItem[];
  modTime?: string;
  modified?: string;
  hidden?: boolean;
  hasPreview?: boolean;
  symlink?: boolean;
};

export type FileItem = ApiItem & {
  path: string;
  source: string;
};

export type FileResource = Omit<ApiResource, "files" | "folders"> & {
  items?: FileItem[];
};

export type SortField = "name" | "size" | "modTime";
export type SortOrder = "asc" | "desc";
