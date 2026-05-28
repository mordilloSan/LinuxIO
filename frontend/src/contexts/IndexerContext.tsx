import { createContext } from "react";

import type { BackgroundJobsContextValue } from "@/types/backgroundJobs";

export type BackgroundJobsIndexerContextValue = Pick<
  BackgroundJobsContextValue,
  | "indexers"
  | "isIndexerDialogOpen"
  | "isIndexing"
  | "lastIndexerError"
  | "lastIndexerResult"
>;

export const BackgroundJobsIndexerContext =
  createContext<BackgroundJobsIndexerContextValue | null>(null);
