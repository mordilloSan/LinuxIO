import { describe, expect, it, vi } from "vitest";

import { BackgroundJobsIndexerContext } from "@/contexts/IndexerContext";
import type { BackgroundJobsIndexerContextValue } from "@/contexts/IndexerContext";
import { BackgroundJobsActionsContext } from "@/contexts/JobsActionsContext";
import type { BackgroundJobsActionsContextValue } from "@/contexts/JobsActionsContext";
import { BackgroundJobsStateContext } from "@/contexts/JobsStateContext";
import type { BackgroundJobsStateContextValue } from "@/contexts/JobsStateContext";
import { useBackgroundJobActions } from "@/hooks/backgroundJobs/useBackgroundJobActions";
import { useBackgroundJobIndexer } from "@/hooks/backgroundJobs/useBackgroundJobIndexer";
import { useBackgroundJobState } from "@/hooks/backgroundJobs/useBackgroundJobState";
import { renderHook } from "@/test/render";

const stateValue: BackgroundJobsStateContextValue = {
  backgroundJobs: [],
  compressions: [],
  copies: [],
  downloads: [],
  extractions: [],
  indexers: [],
  isIndexerDialogOpen: false,
  isIndexing: false,
  lastIndexerError: null,
  lastIndexerResult: null,
  moves: [],
  transfers: [],
  uploads: [],
};

const actionsValue: BackgroundJobsActionsContextValue = {
  cancelCompression: vi.fn(),
  cancelCopy: vi.fn(),
  cancelDownload: vi.fn(),
  cancelExtraction: vi.fn(),
  cancelJob: vi.fn(),
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

const indexerValue: BackgroundJobsIndexerContextValue = {
  indexers: [],
  isIndexerDialogOpen: true,
  isIndexing: true,
  lastIndexerError: "failed",
  lastIndexerResult: null,
};

describe("background job context hooks", () => {
  it("throws clear errors outside BackgroundJobsProvider", () => {
    expect(() => renderHook(() => useBackgroundJobState())).toThrow(
      "useBackgroundJobState must be used within BackgroundJobsProvider",
    );
    expect(() => renderHook(() => useBackgroundJobActions())).toThrow(
      "useBackgroundJobActions must be used within BackgroundJobsProvider",
    );
    expect(() => renderHook(() => useBackgroundJobIndexer())).toThrow(
      "useBackgroundJobIndexer must be used within BackgroundJobsProvider",
    );
  });

  it("returns state, action, and indexer contexts from providers", () => {
    const state = renderHook(() => useBackgroundJobState(), {
      wrapper: ({ children }) => (
        <BackgroundJobsStateContext.Provider value={stateValue}>
          {children}
        </BackgroundJobsStateContext.Provider>
      ),
    });
    const actions = renderHook(() => useBackgroundJobActions(), {
      wrapper: ({ children }) => (
        <BackgroundJobsActionsContext.Provider value={actionsValue}>
          {children}
        </BackgroundJobsActionsContext.Provider>
      ),
    });
    const indexer = renderHook(() => useBackgroundJobIndexer(), {
      wrapper: ({ children }) => (
        <BackgroundJobsIndexerContext.Provider value={indexerValue}>
          {children}
        </BackgroundJobsIndexerContext.Provider>
      ),
    });

    expect(state.result.current).toBe(stateValue);
    expect(actions.result.current).toBe(actionsValue);
    expect(indexer.result.current).toBe(indexerValue);
  });
});
