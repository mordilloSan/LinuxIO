import { createContext } from "react";

import type { BackgroundTasksContextValue } from "@/types/backgroundTasks";

export type BackgroundTasksActionsContextValue = Pick<
  BackgroundTasksContextValue,
  | "cancelCompression"
  | "cancelCopy"
  | "cancelDownload"
  | "cancelExtraction"
  | "cancelTask"
  | "cancelMove"
  | "cancelUpload"
  | "closeIndexerDialog"
  | "openIndexerDialog"
  | "startCompression"
  | "startCopy"
  | "startDownload"
  | "startExtraction"
  | "startIndexer"
  | "startMove"
  | "startUpload"
>;

export const BackgroundTasksActionsContext =
  createContext<BackgroundTasksActionsContextValue | null>(null);
