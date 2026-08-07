import { createContext } from "react";

import type { BackgroundJobsContextValue } from "@/types/backgroundJobs";

export type BackgroundJobsIndexerContextValue = Pick<
  BackgroundJobsContextValue,
  "indexers" | "isIndexerDialogOpen" | "lastIndexerError" | "lastIndexerResult"
>;

export const BackgroundJobsIndexerContext =
  createContext<BackgroundJobsIndexerContextValue | null>(null);

// isIndexing lives in its own context so consumers that only need the boolean
// (e.g. FileBrowserHeader) do not re-render on every indexer progress tick
// carried by the volatile `indexers` array above. null means "no provider".
export const BackgroundJobsIsIndexingContext = createContext<
  BackgroundJobsContextValue["isIndexing"] | null
>(null);
