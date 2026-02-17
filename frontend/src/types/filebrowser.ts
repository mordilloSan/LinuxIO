export type ViewMode = "card" | "list";

interface ItemMetadata {
  name: string;
  type: string;
  size?: number;
  modTime?: string;
  modified?: string;
  hidden?: boolean;
  hasPreview?: boolean;
  symlink?: boolean;
  showFullPath?: boolean; // Show full directory path (for search results)
}

type ItemWithPath = ItemMetadata & {
  path: string;
};

export type ApiItem = ItemMetadata;

interface DirectoryListing {
  files?: ApiItem[];
  folders?: ApiItem[];
  parentDirItems?: ApiItem[];
}

export type ApiResource = ItemWithPath &
  DirectoryListing & {
    content?: string;
  };

export type FileItem = ItemWithPath;

export type FileResource = Omit<ApiResource, "files" | "folders"> & {
  items?: FileItem[];
};

export type SortField = "name" | "size" | "modTime";
export type SortOrder = "asc" | "desc";

export interface ResourceStatData {
  mode: string;
  owner: string;
  group: string;
  size: number;
  modified: string;
  raw: string;
  permissions: string;
  path: string;
  realPath: string;
  name: string;
}

export type MultiStatsItem = Pick<
  FileItem,
  "path" | "name" | "type" | "size"
> & {
  fileCount?: number;
  folderCount?: number;
  aggregateSize?: number;
};

export interface MultiStatsResponse {
  totalSize: number;
  totalFiles: number;
  totalFolders: number;
  items: MultiStatsItem[];
  count: number;
}
