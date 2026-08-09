import type { FileExtractRequest, Stream } from "@/api";

// A copy/move operates on a whole selection in one batch task. `destination` is
// the target directory; each source keeps its basename.
export interface CopyMoveStartOptions {
  sources: string[];
  destination: string;
  overwrite?: boolean;
  onComplete?: () => void;
}

export type ExtractionStartOptions = FileExtractRequest & {
  onComplete?: () => void;
};

interface TrackedTaskItem<TType extends string> {
  abortController: AbortController;
  id: string;
  taskId?: string;
  label: string;
  progress: number;
  stream?: Stream | null;
  type: TType;
}

interface ByteTrackedTaskItem<
  TType extends string,
> extends TrackedTaskItem<TType> {
  bytes?: number;
  speed?: number;
  total?: number;
}

export interface Download extends ByteTrackedTaskItem<"download"> {
  paths: string[];
}

export interface Upload extends TrackedTaskItem<"upload"> {
  completedFiles: number;
  currentFile: string;
  displayName?: string;
  speed?: number;
  totalFiles: number;
}

export interface Compression extends ByteTrackedTaskItem<"compression"> {
  archiveName: string;
  destination: string;
  paths: string[];
}

export interface Extraction extends ByteTrackedTaskItem<"extraction"> {
  archivePath: string;
  destination: string;
}

export interface Indexer {
  abortController?: AbortController;
  bytesIndexed?: number;
  currentPath?: string;
  deletedEntries?: number;
  deletedIndexes?: number;
  dirsIndexed: number;
  durationMs: number;
  filesIndexed: number;
  id?: string;
  label?: string;
  message?: string;
  operation?: string;
  path: string;
  phase?: string;
  progress?: number;
  state?: string;
  stream?: Stream | null;
  totalSize: number;
  type?: "indexer";
}

export type ActiveIndexer = Indexer &
  TrackedTaskItem<"indexer"> & {
    currentPath: string;
    phase: string;
  };

export interface Copy extends ByteTrackedTaskItem<"copy"> {
  destination: string;
  source: string;
}

export interface Move extends ByteTrackedTaskItem<"move"> {
  destination: string;
  source: string;
}

export interface BackgroundTask extends TrackedTaskItem<"task"> {
  indeterminate?: boolean;
  taskType: string;
  processed?: number;
}

/** The four bridge-task transfers driven by one descriptor-based engine. */
export type TransferItem = Compression | Extraction | Copy | Move;

export type BackgroundTaskItem =
  | Download
  | Upload
  | TransferItem
  | ActiveIndexer
  | BackgroundTask;

export interface BackgroundTasksContextValue {
  backgroundTasks: BackgroundTask[];
  cancelCompression: (id: string) => void;
  cancelCopy: (id: string) => void;
  cancelDownload: (id: string) => void;
  cancelExtraction: (id: string) => void;
  cancelTask: (id: string) => void;
  cancelMove: (id: string) => void;
  cancelUpload: (id: string) => void;
  closeIndexerDialog: () => void;
  compressions: Compression[];
  copies: Copy[];
  downloads: Download[];
  extractions: Extraction[];
  indexers: ActiveIndexer[];
  isIndexerDialogOpen: boolean;
  isIndexing: boolean;
  lastIndexerError: string | null;
  lastIndexerResult: Indexer | null;
  moves: Move[];
  openIndexerDialog: () => void;
  startCompression: (options: {
    paths: string[];
    archiveName: string;
    destination: string;
    onComplete?: () => void;
  }) => Promise<void>;
  startCopy: (options: CopyMoveStartOptions) => Promise<void>;
  startDownload: (paths: string[]) => Promise<void>;
  startExtraction: (options: ExtractionStartOptions) => Promise<void>;
  startIndexer: (options: {
    path?: string;
    onComplete?: (result: Indexer) => void;
  }) => Promise<void>;
  startMove: (options: CopyMoveStartOptions) => Promise<void>;
  startUpload: (
    entries: { file?: File; relativePath: string; isDirectory: boolean }[],
    targetPath: string,
    overwrite?: boolean,
  ) => Promise<{
    uploaded: number;
    failures: { path: string; message: string }[];
  }>;
  transfers: BackgroundTaskItem[];
  uploads: Upload[];
}
