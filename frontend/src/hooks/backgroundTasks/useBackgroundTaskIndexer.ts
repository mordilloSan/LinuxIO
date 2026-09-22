import { skipToken, useQuery } from "@tanstack/react-query";

import {
  backgroundTaskKeys,
  EMPTY_INDEXER_SUMMARY,
} from "@/api/background-task-cache";
import { useConfigUserId } from "@/hooks/useConfig";

import { useBackgroundTasks } from "./useBackgroundTaskState";

export const useBackgroundTaskIndexer = () => {
  const userId = useConfigUserId();
  const { data } = useQuery({
    queryKey: backgroundTaskKeys.indexer(userId),
    queryFn: skipToken,
    initialData: EMPTY_INDEXER_SUMMARY,
  });
  const indexers = useBackgroundTasks("indexer");
  return { ...(data ?? EMPTY_INDEXER_SUMMARY), indexers };
};
