import { useContext } from "react";

import { BackgroundJobsIsIndexingContext } from "@/contexts/IndexerContext";

export const useIsIndexing = () => {
  const isIndexing = useContext(BackgroundJobsIsIndexingContext);
  if (isIndexing === null) {
    throw new Error("useIsIndexing must be used within BackgroundJobsProvider");
  }
  return isIndexing;
};
