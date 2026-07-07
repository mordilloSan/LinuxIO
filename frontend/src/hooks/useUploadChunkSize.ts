import { useContext } from "react";

import { STREAM_MULTIPLEXER_CONFIG } from "@/api";
import { ConfigContext } from "@/contexts/ConfigContext";

/**
 * Effective upload chunk size in bytes: the user-configured
 * appSettings.chunkSizeMB when set, otherwise the transport default.
 * Tolerates a missing ConfigProvider (BackgroundJobsProvider can mount
 * without one).
 */
export function useUploadChunkSize(): number {
  const configCtx = useContext(ConfigContext);
  const chunkSizeMB = Number(configCtx?.config.appSettings.chunkSizeMB ?? 0);
  return chunkSizeMB > 0
    ? chunkSizeMB * 1024 * 1024
    : STREAM_MULTIPLEXER_CONFIG.uploadChunkSize;
}
