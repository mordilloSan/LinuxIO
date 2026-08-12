import { useContext } from "react";

import { BackgroundTasksIsIndexingContext } from "@/contexts/IndexerContext";

export const useIsIndexing = () => {
  const isIndexing = useContext(BackgroundTasksIsIndexingContext);
  if (isIndexing === null) {
    throw new Error(
      "useIsIndexing must be used within BackgroundTasksProvider",
    );
  }
  return isIndexing;
};
