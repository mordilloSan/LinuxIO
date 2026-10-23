import { indexerPhaseText } from "@/tasks/indexerProgress";
import type { Indexer } from "@/tasks/types";

type IndexerDisplay = Pick<
  Indexer,
  "currentPath" | "dirsIndexed" | "filesIndexed" | "message" | "phase"
>;

export function indexerPhaseLabel(item: IndexerDisplay): string {
  if (item.phase === "connecting") {
    return indexerPhaseText(item.phase);
  }
  if (item.message) {
    return item.message;
  }
  if (item.currentPath) {
    return item.currentPath;
  }
  return indexerPhaseText(item.phase);
}
