import type { ActiveIndexer, Indexer } from "@/types/backgroundJobs";

export interface IndexerProgressFrame {
  bytes_indexed?: number;
  current_path?: string;
  dirs_indexed?: number;
  files_indexed?: number;
  message?: string;
  operation?: string;
  path?: string;
  phase?: string;
  state?: string;
  status?: string;
}

export interface IndexerResultFrame {
  deleted_entries?: number;
  deleted_indexes?: number;
  dirs_indexed?: number;
  duration_ms?: number;
  files_indexed?: number;
  operation?: string;
  path?: string;
  status?: string;
  total_size?: number;
}

type IndexerDisplay = Pick<
  Indexer,
  "currentPath" | "dirsIndexed" | "filesIndexed" | "message" | "phase"
>;

const phaseLabel = (phase?: string): string => {
  switch (phase) {
    case "connecting":
      return "Connecting to indexer...";
    case "scan":
      return "Scanning filesystem...";
    case "indexing":
      return "Indexing filesystem...";
    case "pre_checkpoint":
      return "Preparing database checkpoint...";
    case "post_checkpoint":
      return "Finishing database checkpoint...";
    default:
      if (phase) {
        const text = phase
          .replace(/[_-]/g, " ")
          .replace(/\b\w/g, (letter) => letter.toUpperCase());
        return `${text}...`;
      }
      return "Processing index...";
  }
};

export function indexerProgressLabel(item: IndexerDisplay): string {
  if (item.phase === "connecting") {
    return phaseLabel(item.phase);
  }
  if (item.message) {
    return item.message;
  }

  const verb = item.phase === "scan" ? "Scanning" : "Indexing";
  if (item.phase === "scan" || item.phase === "indexing") {
    return `${verb}: ${item.filesIndexed} files, ${item.dirsIndexed} dirs`;
  }
  return phaseLabel(item.phase);
}

export function indexerPhaseLabel(item: IndexerDisplay): string {
  if (item.phase === "connecting") {
    return phaseLabel(item.phase);
  }
  if (item.message) {
    return item.message;
  }
  if (item.currentPath) {
    return item.currentPath;
  }
  return phaseLabel(item.phase);
}

export function mergeIndexerProgress(
  item: ActiveIndexer,
  progress: IndexerProgressFrame,
): ActiveIndexer {
  const normalizedStatus = progress.status?.toLowerCase();
  const statusPhase =
    normalizedStatus === "running" || normalizedStatus === "indexing"
      ? "indexing"
      : undefined;
  const phase = progress.phase ?? progress.state ?? statusPhase ?? item.phase;
  const hasPhaseUpdate =
    progress.phase !== undefined ||
    progress.state !== undefined ||
    statusPhase !== undefined;
  const updated = {
    ...item,
    bytesIndexed: progress.bytes_indexed ?? item.bytesIndexed,
    currentPath: progress.current_path ?? progress.path ?? item.currentPath,
    dirsIndexed: progress.dirs_indexed ?? item.dirsIndexed,
    filesIndexed: progress.files_indexed ?? item.filesIndexed,
    message: progress.message ?? (hasPhaseUpdate ? undefined : item.message),
    operation: progress.operation ?? item.operation,
    phase,
    state: progress.state ?? progress.status ?? item.state ?? phase,
  };

  return {
    ...updated,
    label: indexerProgressLabel(updated),
  };
}

export function indexerResultFromFrame(
  requestedPath: string,
  result: IndexerResultFrame | undefined,
): Indexer {
  return {
    deletedEntries: result?.deleted_entries,
    deletedIndexes: result?.deleted_indexes,
    dirsIndexed: result?.dirs_indexed ?? 0,
    durationMs: result?.duration_ms ?? 0,
    filesIndexed: result?.files_indexed ?? 0,
    operation: result?.operation,
    path: result?.path || requestedPath,
    state: result?.status,
    totalSize: result?.total_size ?? 0,
  };
}
