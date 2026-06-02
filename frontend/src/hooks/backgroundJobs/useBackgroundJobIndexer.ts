import { useContext } from "react";

import { BackgroundJobsIndexerContext } from "@/contexts/IndexerContext";

export const useBackgroundJobIndexer = () => {
  const context = useContext(BackgroundJobsIndexerContext);
  if (!context) {
    throw new Error(
      "useBackgroundJobIndexer must be used within BackgroundJobsProvider",
    );
  }
  return context;
};
