import { createContext } from "react";

import type { BackgroundTasksContextValue } from "@/types/backgroundTasks";

export type BackgroundTasksStateContextValue = Pick<
  BackgroundTasksContextValue,
  | "backgroundTasks"
  | "compressions"
  | "copies"
  | "downloads"
  | "extractions"
  | "indexers"
  | "isIndexerDialogOpen"
  | "lastIndexerError"
  | "lastIndexerResult"
  | "moves"
  | "transfers"
  | "uploads"
>;

export const BackgroundTasksStateContext =
  createContext<BackgroundTasksStateContextValue | null>(null);
