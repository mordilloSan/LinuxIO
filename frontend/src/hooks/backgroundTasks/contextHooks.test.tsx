import { describe, expect, it, vi } from "vitest";

import {
  BackgroundTasksIndexerContext,
  BackgroundTasksIsIndexingContext,
} from "@/contexts/IndexerContext";
import type { BackgroundTasksIndexerContextValue } from "@/contexts/IndexerContext";
import { BackgroundTasksActionsContext } from "@/contexts/TasksActionsContext";
import type { BackgroundTasksActionsContextValue } from "@/contexts/TasksActionsContext";
import { BackgroundTasksStateContext } from "@/contexts/TasksStateContext";
import type { BackgroundTasksStateContextValue } from "@/contexts/TasksStateContext";
import { useBackgroundTaskActions } from "@/hooks/backgroundTasks/useBackgroundTaskActions";
import { useBackgroundTaskIndexer } from "@/hooks/backgroundTasks/useBackgroundTaskIndexer";
import { useBackgroundTaskState } from "@/hooks/backgroundTasks/useBackgroundTaskState";
import { useIsIndexing } from "@/hooks/backgroundTasks/useIsIndexing";
import { renderHook } from "@/test/render";

const stateValue: BackgroundTasksStateContextValue = {
  backgroundTasks: [],
  compressions: [],
  copies: [],
  downloads: [],
  extractions: [],
  indexers: [],
  isIndexerDialogOpen: false,
  lastIndexerError: null,
  lastIndexerResult: null,
  moves: [],
  transfers: [],
  uploads: [],
};

const actionsValue: BackgroundTasksActionsContextValue = {
  cancelCompression: vi.fn(),
  cancelCopy: vi.fn(),
  cancelDownload: vi.fn(),
  cancelExtraction: vi.fn(),
  cancelTask: vi.fn(),
  cancelMove: vi.fn(),
  cancelUpload: vi.fn(),
  closeIndexerDialog: vi.fn(),
  openIndexerDialog: vi.fn(),
  startCompression: vi.fn(),
  startCopy: vi.fn(),
  startDownload: vi.fn(),
  startExtraction: vi.fn(),
  startIndexer: vi.fn(),
  startMove: vi.fn(),
  startUpload: vi.fn(),
};

const indexerValue: BackgroundTasksIndexerContextValue = {
  indexers: [],
  isIndexerDialogOpen: true,
  lastIndexerError: "failed",
  lastIndexerResult: null,
};

describe("background task context hooks", () => {
  it("throws clear errors outside BackgroundTasksProvider", () => {
    expect(() => renderHook(() => useBackgroundTaskState())).toThrow(
      "useBackgroundTaskState must be used within BackgroundTasksProvider",
    );
    expect(() => renderHook(() => useBackgroundTaskActions())).toThrow(
      "useBackgroundTaskActions must be used within BackgroundTasksProvider",
    );
    expect(() => renderHook(() => useBackgroundTaskIndexer())).toThrow(
      "useBackgroundTaskIndexer must be used within BackgroundTasksProvider",
    );
    expect(() => renderHook(() => useIsIndexing())).toThrow(
      "useIsIndexing must be used within BackgroundTasksProvider",
    );
  });

  it("returns the isIndexing boolean from its own context", () => {
    const isIndexing = renderHook(() => useIsIndexing(), {
      wrapper: ({ children }) => (
        <BackgroundTasksIsIndexingContext.Provider value={false}>
          {children}
        </BackgroundTasksIsIndexingContext.Provider>
      ),
    });

    expect(isIndexing.result.current).toBe(false);
  });

  it("returns state, action, and indexer contexts from providers", () => {
    const state = renderHook(() => useBackgroundTaskState(), {
      wrapper: ({ children }) => (
        <BackgroundTasksStateContext.Provider value={stateValue}>
          {children}
        </BackgroundTasksStateContext.Provider>
      ),
    });
    const actions = renderHook(() => useBackgroundTaskActions(), {
      wrapper: ({ children }) => (
        <BackgroundTasksActionsContext.Provider value={actionsValue}>
          {children}
        </BackgroundTasksActionsContext.Provider>
      ),
    });
    const indexer = renderHook(() => useBackgroundTaskIndexer(), {
      wrapper: ({ children }) => (
        <BackgroundTasksIndexerContext.Provider value={indexerValue}>
          {children}
        </BackgroundTasksIndexerContext.Provider>
      ),
    });

    expect(state.result.current).toBe(stateValue);
    expect(actions.result.current).toBe(actionsValue);
    expect(indexer.result.current).toBe(indexerValue);
  });
});
