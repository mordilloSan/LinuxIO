import type { Stream } from "@/api";

export interface Download {
  abortController: AbortController;
  bytes?: number;
  id: string;
  jobId?: string;
  label: string;
  paths: string[];
  progress: number;
  speed?: number;
  stream?: Stream | null; // For stream-based downloads
  total?: number;
  type: "download";
}

export interface Upload {
  abortController: AbortController;
  completedFiles: number;
  currentFile: string;
  displayName?: string;
  id: string;
  jobId?: string;
  label: string;
  progress: number;
  speed?: number;
  stream?: Stream | null; // For stream-based uploads
  totalFiles: number;
  type: "upload";
}

export interface Compression {
  abortController: AbortController;
  archiveName: string;
  bytes?: number;
  destination: string;
  id: string;
  label: string;
  paths: string[];
  progress: number;
  speed?: number;
  stream?: Stream | null;
  total?: number;
  type: "compression";
}

export interface Extraction {
  abortController: AbortController;
  archivePath: string;
  bytes?: number;
  destination: string;
  id: string;
  label: string;
  progress: number;
  speed?: number;
  stream?: Stream | null;
  total?: number;
  type: "extraction";
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

export type ActiveIndexer = Indexer & {
  id: string;
  type: "indexer";
  currentPath: string;
  phase: string;
  progress: number;
  label: string;
  abortController: AbortController;
};

export interface Copy {
  abortController: AbortController;
  bytes?: number;
  destination: string;
  id: string;
  label: string;
  progress: number;
  source: string;
  speed?: number;
  stream?: Stream | null;
  total?: number;
  type: "copy";
}

export interface Move {
  abortController: AbortController;
  bytes?: number;
  destination: string;
  id: string;
  label: string;
  progress: number;
  source: string;
  speed?: number;
  stream?: Stream | null;
  total?: number;
  type: "move";
}

export interface BackgroundJob {
  abortController: AbortController;
  id: string;
  indeterminate?: boolean;
  jobType: string;
  label: string;
  processed?: number;
  progress: number;
  stream?: Stream | null;
  type: "job";
}

export type BackgroundJobItem =
  | Download
  | Upload
  | Compression
  | Extraction
  | ActiveIndexer
  | Copy
  | Move
  | BackgroundJob;

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
  startCopy: (options: {
    source: string;
    destination: string;
    onComplete?: () => void;
  }) => Promise<void>;
  startDownload: (paths: string[]) => Promise<void>;
  startExtraction: (options: {
    archivePath: string;
    destination?: string;
    onComplete?: () => void;
  }) => Promise<void>;
  startIndexer: (options: {
    path?: string;
    onComplete?: (result: Indexer) => void;
  }) => Promise<void>;
  startMove: (options: {
    source: string;
    destination: string;
    onComplete?: () => void;
  }) => Promise<void>;
  startUpload: (
    entries: { file?: File; relativePath: string; isDirectory: boolean }[],
    targetPath: string,
    override?: boolean,
  ) => Promise<{
    conflicts: {
      file?: File;
      relativePath: string;
      isDirectory: boolean;
    }[];
    uploaded: number;
    failures: { path: string; message: string }[];
  }>;
  transfers: BackgroundJobItem[];
  uploads: Upload[];
}
