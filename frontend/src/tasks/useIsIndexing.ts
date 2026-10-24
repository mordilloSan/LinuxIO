import { useBackgroundTaskList } from "./useBackgroundTaskState";

export const useIsIndexing = () =>
  useBackgroundTaskList((items) =>
    items.some((item) => item.type === "indexer"),
  );
