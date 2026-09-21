import { notifyManager, type QueryClient } from "@tanstack/react-query";
import type { SetStateAction } from "react";

import type {
  BackgroundTaskItem,
  Indexer,
  TransferItem,
} from "@/types/backgroundTasks";

export const backgroundTaskKeys = {
  all: (userId: string) => ["background-tasks", "user", userId] as const,
  list: (userId: string) =>
    [...backgroundTaskKeys.all(userId), "list"] as const,
  task: (userId: string, id: string) =>
    [...backgroundTaskKeys.all(userId), "task", id] as const,
  indexer: (userId: string) =>
    [...backgroundTaskKeys.all(userId), "indexer"] as const,
};

// Membership and completed-row metadata only; live progress lives in each task entry.
export interface BackgroundTaskListItem {
  id: string;
  cacheId: string;
  type: BackgroundTaskItem["type"];
  progress: number;
  label?: string;
}

export interface IndexerTaskSummary {
  isIndexerDialogOpen: boolean;
  lastIndexerResult: Indexer | null;
  lastIndexerError: string | null;
}

export const EMPTY_TASK_LIST: BackgroundTaskListItem[] = [];
export const EMPTY_INDEXER_SUMMARY: IndexerTaskSummary = {
  isIndexerDialogOpen: false,
  lastIndexerResult: null,
  lastIndexerError: null,
};

type TaskKind = BackgroundTaskItem["type"] | "transfer";
type TaskOfKind<K extends TaskKind> = K extends "transfer"
  ? TransferItem
  : Extract<BackgroundTaskItem, { type: K }>;

export function createBackgroundTaskCache(
  queryClient: QueryClient,
  userId: string,
) {
  const prefix = backgroundTaskKeys.all(userId);
  // Stream-owned entries must survive periods with no mounted consumers.
  queryClient.setQueryDefaults(prefix, {
    gcTime: Infinity,
    staleTime: Infinity,
    enabled: false,
  });
  const listKey = backgroundTaskKeys.list(userId);
  const summaryKey = backgroundTaskKeys.indexer(userId);
  let active = true;
  const entries = () =>
    queryClient.getQueryData<BackgroundTaskListItem[]>(listKey) ??
    EMPTY_TASK_LIST;
  const get = (id: string) => {
    const entry = entries().find(
      (item) => item.id === id || item.cacheId === id,
    );
    return entry
      ? queryClient.getQueryData<BackgroundTaskItem>(
          backgroundTaskKeys.task(userId, entry.cacheId),
        )
      : undefined;
  };
  const matches = (type: BackgroundTaskItem["type"], kind: TaskKind) =>
    kind === "transfer"
      ? ["compression", "extraction", "copy", "move"].includes(type)
      : type === kind;

  function domain<K extends TaskKind>(kind: K) {
    const read = () =>
      entries()
        .filter((entry) => matches(entry.type, kind))
        .map((entry) =>
          queryClient.getQueryData<BackgroundTaskItem>(
            backgroundTaskKeys.task(userId, entry.cacheId),
          ),
        )
        .filter((item): item is TaskOfKind<K> => item !== undefined);
    const set = (update: SetStateAction<TaskOfKind<K>[]>) => {
      if (!active) return;
      const previous = read();
      const next = typeof update === "function" ? update(previous) : update;
      if (next === previous) return;
      notifyManager.batch(() => {
        const oldEntries = entries();
        const previousById = new Map(previous.map((item) => [item.id, item]));

        for (const item of next) {
          if (previousById.get(item.id) === item) continue;
          queryClient.setQueryData(
            backgroundTaskKeys.task(userId, item.taskId ?? item.id),
            item,
          );
        }
        const nextEntries = next.map((item) => ({
          id: item.id,
          cacheId: item.taskId ?? item.id,
          type: item.type,
          progress: item.progress === 100 ? 100 : 0,
          label: item.progress === 100 ? item.label : undefined,
        }));
        const replacements = new Map(
          nextEntries.map((entry) => [entry.id, entry]),
        );
        const oldIds = new Set(oldEntries.map((entry) => entry.id));
        const list = oldEntries.flatMap((entry) =>
          matches(entry.type, kind)
            ? replacements.get(entry.id)
              ? [replacements.get(entry.id)!]
              : []
            : [entry],
        );
        list.push(...nextEntries.filter((entry) => !oldIds.has(entry.id)));
        if (
          list.length !== oldEntries.length ||
          list.some((entry, i) => {
            const old = oldEntries[i];
            return (
              !old ||
              entry.id !== old.id ||
              entry.cacheId !== old.cacheId ||
              entry.type !== old.type ||
              entry.progress !== old.progress ||
              entry.label !== old.label
            );
          })
        )
          queryClient.setQueryData(listKey, list);
        for (const entry of oldEntries.filter((item) =>
          matches(item.type, kind),
        )) {
          const replacement = replacements.get(entry.id);
          if (!replacement || replacement.cacheId !== entry.cacheId) {
            const key = backgroundTaskKeys.task(userId, entry.cacheId);
            // Notify an observer still mounted during removal before disposing the entry.
            queryClient.setQueryData(key, null);
            queryClient.removeQueries({ queryKey: key, exact: true });
          }
        }
      });
    };
    return { read, set };
  }

  function summarySetter<K extends keyof IndexerTaskSummary>(key: K) {
    return (update: SetStateAction<IndexerTaskSummary[K]>) => {
      if (!active) return;
      queryClient.setQueryData<IndexerTaskSummary>(
        summaryKey,
        (previous = EMPTY_INDEXER_SUMMARY) => ({
          ...previous,
          [key]: typeof update === "function" ? update(previous[key]) : update,
        }),
      );
    };
  }

  return {
    get,
    downloads: domain("download"),
    uploads: domain("upload"),
    transfers: domain("transfer"),
    indexers: domain("indexer"),
    backgroundTasks: domain("task"),
    setIsIndexerDialogOpen: summarySetter("isIndexerDialogOpen"),
    setLastIndexerResult: summarySetter("lastIndexerResult"),
    setLastIndexerError: summarySetter("lastIndexerError"),
    activate: () => {
      active = true;
    },
    dispose: () => {
      active = false;
      queryClient.removeQueries({ queryKey: prefix });
    },
  };
}
