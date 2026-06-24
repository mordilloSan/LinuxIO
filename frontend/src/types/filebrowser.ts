import type { ResourceStatData as ApiResourceStatData } from "@/api";

export type ViewMode = "card" | "list";

interface ItemMetadata {
  hasPreview?: boolean;
  hidden?: boolean;
  modified?: string;
  modTime?: string;
  name: string;
  showFullPath?: boolean; // Show full directory path (for search results)
  size?: number;
  symlink?: boolean;
  type: string;
}

export type FileItem = ItemMetadata & {
  path: string;
};

// Client-normalized view of a directory resource: the wire `folders` + `files`
// (generated ExtendedFileInfo) are flattened into a single `items` list with
// computed paths. See normalizeResource.
export type FileResource = FileItem & {
  content?: string;
  items?: FileItem[];
};

export type SortField = "name" | "size" | "modTime";
export type SortOrder = "asc" | "desc";

export type ResourceStatData = ApiResourceStatData;

export type MultiStatsItem = Pick<
  FileItem,
  "path" | "name" | "type" | "size"
> & {
  fileCount?: number;
  folderCount?: number;
  aggregateSize?: number;
};

export interface MultiStatsResponse {
  count: number;
  items: MultiStatsItem[];
  totalFiles: number;
  totalFolders: number;
  totalSize: number;
}
