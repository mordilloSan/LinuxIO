import { useContext } from "react";

import { BackgroundTasksIndexerContext } from "@/contexts/IndexerContext";

export const useBackgroundTaskIndexer = () => {
  const context = useContext(BackgroundTasksIndexerContext);
  if (!context) {
    throw new Error(
      "useBackgroundTaskIndexer must be used within BackgroundTasksProvider",
    );
  }
  return context;
};
