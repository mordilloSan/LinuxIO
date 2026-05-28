import { createContext } from "react";

import type { BackgroundJobsContextValue } from "@/types/backgroundJobs";

export type BackgroundJobsStateContextValue = Pick<
  BackgroundJobsContextValue,
  | "backgroundJobs"
  | "compressions"
  | "copies"
  | "downloads"
  | "extractions"
  | "indexers"
  | "isIndexerDialogOpen"
  | "isIndexing"
  | "lastIndexerError"
  | "lastIndexerResult"
  | "moves"
  | "transfers"
  | "uploads"
>;

export const BackgroundJobsStateContext =
  createContext<BackgroundJobsStateContextValue | null>(null);
