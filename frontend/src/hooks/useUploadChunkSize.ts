import { useCallback, useContext } from "react";

import { STREAM_MULTIPLEXER_CONFIG } from "@/api";
import { ConfigAccessorContext, ConfigContext } from "@/contexts/ConfigContext";

const effectiveChunkSize = (chunkSizeMBRaw: unknown): number => {
  const chunkSizeMB = Number(chunkSizeMBRaw ?? 0);
  return chunkSizeMB > 0
    ? chunkSizeMB * 1024 * 1024
    : STREAM_MULTIPLEXER_CONFIG.uploadChunkSize;
};

/**
 * Effective upload chunk size in bytes: the user-configured
 * appSettings.chunkSizeMB when set, otherwise the transport default.
 * Reactive — rerenders on config changes.
 */
export function useUploadChunkSize(): number {
  const configCtx = useContext(ConfigContext);
  return effectiveChunkSize(configCtx?.config.appSettings.chunkSizeMB);
}

/**
 * Identity-stable getter variant for BackgroundTasksProvider: reads the
 * current chunk size through the ref-backed config accessor at upload start,
 * so the provider (and the actions context identity) never rerenders on
 * config changes. Tolerates a missing ConfigProvider (the tasks provider can
 * mount without one).
 */
export function useUploadChunkSizeGetter(): () => number {
  const accessor = useContext(ConfigAccessorContext);
  const getConfig = accessor?.getConfig;
  return useCallback(
    () => effectiveChunkSize(getConfig?.().appSettings.chunkSizeMB),
    [getConfig],
  );
}
