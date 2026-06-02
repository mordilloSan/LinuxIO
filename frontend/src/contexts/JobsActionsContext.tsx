import { createContext } from "react";

import type { BackgroundJobsContextValue } from "@/types/backgroundJobs";

export type BackgroundJobsActionsContextValue = Pick<
  BackgroundJobsContextValue,
  | "cancelCompression"
  | "cancelCopy"
  | "cancelDownload"
  | "cancelExtraction"
  | "cancelJob"
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

export const BackgroundJobsActionsContext =
  createContext<BackgroundJobsActionsContextValue | null>(null);
