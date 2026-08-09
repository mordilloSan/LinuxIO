import { createContext } from "react";

import type { BackgroundTasksContextValue } from "@/types/backgroundTasks";

export type BackgroundTasksIndexerContextValue = Pick<
  BackgroundTasksContextValue,
  "indexers" | "isIndexerDialogOpen" | "lastIndexerError" | "lastIndexerResult"
>;

export const BackgroundTasksIndexerContext =
  createContext<BackgroundTasksIndexerContextValue | null>(null);

// isIndexing lives in its own context so consumers that only need the boolean
// (e.g. FileBrowserHeader) do not re-render on every indexer progress tick
// carried by the volatile `indexers` array above. null means "no provider".
export const BackgroundTasksIsIndexingContext = createContext<
  BackgroundTasksContextValue["isIndexing"] | null
>(null);
