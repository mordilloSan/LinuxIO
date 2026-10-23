import type { IndexerProgress, IndexerResult } from "@/api";

import type { ActiveIndexer, Indexer } from "./types";

type IndexerDisplay = Pick<
  Indexer,
  "currentPath" | "dirsIndexed" | "filesIndexed" | "message" | "phase"
>;

export const indexerPhaseText = (phase?: string): string => {
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

function indexerProgressLabel(item: IndexerDisplay): string {
  if (item.phase === "connecting") {
    return indexerPhaseText(item.phase);
  }
  if (item.message) {
    return item.message;
  }

  const verb = item.phase === "scan" ? "Scanning" : "Indexing";
  if (item.phase === "scan" || item.phase === "indexing") {
    return `${verb}: ${item.filesIndexed} files, ${item.dirsIndexed} dirs`;
  }
  return indexerPhaseText(item.phase);
}

export function mergeIndexerProgress(
  item: ActiveIndexer,
  progress: IndexerProgress,
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
  result: IndexerResult | undefined,
): Indexer {
  return {
    deletedEntries: result?.deleted_entries,
    dirsIndexed: result?.dirs_indexed ?? 0,
    durationMs: result?.duration_ms ?? 0,
    filesIndexed: result?.files_indexed ?? 0,
    operation: result?.operation,
    path: result?.path || requestedPath,
    state: result?.status,
    totalSize: result?.total_size ?? 0,
  };
}
