import { skipToken, useQueries, useQuery } from "@tanstack/react-query";

import {
  backgroundTaskKeys,
  EMPTY_TASK_LIST,
  type BackgroundTaskListItem,
} from "@/api/background-task-cache";
import { useConfigUserId } from "@/hooks/useConfig";
import type { BackgroundTaskItem } from "@/types/backgroundTasks";

export function useBackgroundTaskList<T = BackgroundTaskListItem[]>(
  select?: (items: BackgroundTaskListItem[]) => T,
) {
  const userId = useConfigUserId();
  return useQuery({
    queryKey: backgroundTaskKeys.list(userId),
    queryFn: skipToken,
    initialData: EMPTY_TASK_LIST,
    select,
  }).data!;
}

export function useBackgroundTask(id: string) {
  const userId = useConfigUserId();
  const cacheId = useBackgroundTaskList(
    (items) => items.find((item) => item.id === id)?.cacheId,
  );
  return useQuery<BackgroundTaskItem | null>({
    queryKey: backgroundTaskKeys.task(userId, cacheId ?? id),
    queryFn: skipToken,
  }).data;
}

/** Subscribe only to entries of the requested kind; rows should use useBackgroundTask. */
export function useBackgroundTasks<K extends BackgroundTaskItem["type"]>(
  kind?: K,
) {
  const userId = useConfigUserId();
  const entries = useBackgroundTaskList((items) =>
    kind ? items.filter((item) => item.type === kind) : items,
  );
  return useQueries({
    queries: entries.map((entry) => ({
      queryKey: backgroundTaskKeys.task(userId, entry.cacheId),
      queryFn: skipToken,
    })),
    combine: (results) =>
      results
        .map((result) => result.data)
        .filter(
          (item): item is Extract<BackgroundTaskItem, { type: K }> => !!item,
        ),
  });
}
