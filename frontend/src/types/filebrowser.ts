export type ViewMode = "card" | "list";

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
  size?: number;
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
};

export type FileResource = Omit<ApiResource, "files" | "folders"> & {
  items?: FileItem[];
};

export type SortField = "name" | "size" | "modTime";
export type SortOrder = "asc" | "desc";
