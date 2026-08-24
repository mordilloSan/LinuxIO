import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";

import { type AppConfig, STREAM_MULTIPLEXER_CONFIG, linuxio } from "@/api";
import { bridgeConfigQueryKey } from "@/api/config-query";
import { useConfigUserId } from "@/hooks/useConfig";

const effectiveChunkSize = (chunkSizeMBRaw: unknown): number => {
  const chunkSizeMB = Number(chunkSizeMBRaw ?? 0);
  return chunkSizeMB > 0
    ? chunkSizeMB * 1024 * 1024
    : STREAM_MULTIPLEXER_CONFIG.uploadChunkSize;
};

/**
 * Effective upload chunk size in bytes: the user-configured
 * appSettings.chunkSizeMB when set, otherwise the transport default.
 * Reactive — subscribes to the cached bridge config; falls back to the
 * transport default when no snapshot is cached (e.g. outside ConfigProvider).
 */
export function useUploadChunkSize(): number {
  const userId = useConfigUserId();
  const { data } = useQuery({
    ...linuxio.config.get,
    queryKey: bridgeConfigQueryKey(userId),
    enabled: false,
    select: (config: AppConfig) => config.appSettings.chunkSizeMB,
  });
  return effectiveChunkSize(data);
}

/**
 * Identity-stable getter variant for BackgroundTasksProvider: reads the
 * current chunk size straight from the query cache at upload start, so the
 * provider (and the actions context identity) never rerenders on config
 * changes. Tolerates a missing snapshot (the tasks provider can mount
 * without a loaded ConfigProvider).
 */
export function useUploadChunkSizeGetter(): () => number {
  const queryClient = useQueryClient();
  const userId = useConfigUserId();
  return useCallback(
    () =>
      effectiveChunkSize(
        queryClient.getQueryData<AppConfig>(bridgeConfigQueryKey(userId))
          ?.appSettings.chunkSizeMB,
      ),
    [queryClient, userId],
  );
}
