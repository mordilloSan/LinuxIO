import { describe, expect, it } from "vitest";

import {
  indexerResultFromFrame,
  mergeIndexerProgress,
} from "@/hooks/backgroundTasks/indexerProgress";
import type { ActiveIndexer } from "@/types/backgroundTasks";

const activeIndexer = (): ActiveIndexer => ({
  abortController: new AbortController(),
  currentPath: "",
  dirsIndexed: 0,
  durationMs: 0,
  filesIndexed: 0,
  id: "task-1",
  taskId: "task-1",
  label: "Connecting to indexer...",
  path: "/",
  phase: "connecting",
  progress: 0,
  totalSize: 0,
  type: "indexer",
});

describe("indexer progress", () => {
  it("preserves the upstream phase, message, counters, and bytes", () => {
    const next = mergeIndexerProgress(activeIndexer(), {
      bytes_indexed: 512,
      current_path: "/srv/photos",
      dirs_indexed: 2,
      files_indexed: 10,
      message: "Scanning filesystem",
      operation: "index",
      phase: "scan",
    });

    expect(next).toMatchObject({
      bytesIndexed: 512,
      currentPath: "/srv/photos",
      dirsIndexed: 2,
      filesIndexed: 10,
      label: "Scanning filesystem",
      message: "Scanning filesystem",
      operation: "index",
      phase: "scan",
      state: "scan",
    });
  });

  it("accepts state as a phase alias", () => {
    const next = mergeIndexerProgress(activeIndexer(), {
      message: "Checkpointing database",
      state: "post_checkpoint",
    });

    expect(next.phase).toBe("post_checkpoint");
    expect(next.state).toBe("post_checkpoint");
    expect(next.label).toBe("Checkpointing database");
  });

  it("maps the complete event into the indexer summary", () => {
    expect(
      indexerResultFromFrame("/", {
        deleted_entries: 3,
        deleted_indexes: 1,
        dirs_indexed: 20,
        duration_ms: 150,
        files_indexed: 100,
        operation: "index",
        path: "/srv",
        status: "complete",
        total_size: 5000,
      }),
    ).toMatchObject({
      deletedEntries: 3,
      deletedIndexes: 1,
      dirsIndexed: 20,
      durationMs: 150,
      filesIndexed: 100,
      operation: "index",
      path: "/srv",
      state: "complete",
      totalSize: 5000,
    });
  });
});
