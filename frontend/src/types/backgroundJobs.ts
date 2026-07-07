import type { FileExtractRequest, Stream } from "@/api";

// A copy/move operates on a whole selection in one batch job. `destination` is
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

interface TrackedJobItem<TType extends string> {
  abortController: AbortController;
  id: string;
  jobId?: string;
  label: string;
  progress: number;
  stream?: Stream | null;
  type: TType;
}

interface ByteTrackedJobItem<
  TType extends string,
> extends TrackedJobItem<TType> {
  bytes?: number;
  speed?: number;
  total?: number;
}

export interface Download extends ByteTrackedJobItem<"download"> {
  paths: string[];
}

export interface Upload extends TrackedJobItem<"upload"> {
  completedFiles: number;
  currentFile: string;
  displayName?: string;
  speed?: number;
  totalFiles: number;
}

export interface Compression extends ByteTrackedJobItem<"compression"> {
  archiveName: string;
  destination: string;
  paths: string[];
}

export interface Extraction extends ByteTrackedJobItem<"extraction"> {
  archivePath: string;
  destination: string;
}

export interface Indexer {
  abortController?: AbortController;
  currentPath?: string;
  dirsIndexed: number;
  durationMs: number;
  filesIndexed: number;
  id?: string;
  label?: string;
  path: string;
  phase?: string;
  progress?: number;
  stream?: Stream | null;
  totalSize: number;
  type?: "indexer";
}

export type ActiveIndexer = Indexer &
  TrackedJobItem<"indexer"> & {
    currentPath: string;
    phase: string;
  };

export interface Copy extends ByteTrackedJobItem<"copy"> {
  destination: string;
  source: string;
}

export interface Move extends ByteTrackedJobItem<"move"> {
  destination: string;
  source: string;
}

export interface BackgroundJob extends TrackedJobItem<"job"> {
  indeterminate?: boolean;
  jobType: string;
  processed?: number;
}

/** The four bridge-job transfers driven by one descriptor-based engine. */
export type TransferItem = Compression | Extraction | Copy | Move;

export type BackgroundJobItem =
  Download | Upload | TransferItem | ActiveIndexer | BackgroundJob;

export interface BackgroundJobsContextValue {
  backgroundJobs: BackgroundJob[];
  cancelCompression: (id: string) => void;
  cancelCopy: (id: string) => void;
  cancelDownload: (id: string) => void;
  cancelExtraction: (id: string) => void;
  cancelJob: (id: string) => void;
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
  transfers: BackgroundJobItem[];
  uploads: Upload[];
}
