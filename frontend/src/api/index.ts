/**
 * LinuxIO API - Unified Entry Point
 *
 * JSON API (request/response via bridge streams) → React Query:
 *   linuxio.system.get_cpu_info.useQuery()
 *   linuxio.docker.start_container.useMutation()
 *
 * Streaming API (persistent/long-lived streams):
 *   const stream = openTerminalStream(cols, rows);
 *   stream.onData = (data) => ...;
 */

// === JSON API (React Query type-safe proxy) ===
export { default as linuxio, CACHE_TTL_MS } from "./react-query";

// === Core API (Promise-based, used by React Query internally) ===
export { LinuxIOError } from "./linuxio-core";

// === React Hooks ===
export { useStreamMux, useIsUpdating } from "./linuxio";

// === Connection Utilities ===
export { isConnected, getStatus } from "./linuxio";

// === Stream Openers ===
export {
  openTerminalStream,
  openContainerStream,
  openDockerLogsStream,
  openServiceLogsStream,
  openGeneralLogsStream,
  openDockerComposeStream,
  openDockerIndexerStream,
  openDockerIndexerAttachStream,
  openExecStream,
  openPackageUpdateStream,
  openSmartTestStream,
  openFileUploadStream,
  openFileDownloadStream,
  openFileCompressStream,
  openFileExtractStream,
  openFileIndexerStream,
  openFileIndexerAttachStream,
  openFileCopyStream,
  openFileMoveStream,
} from "./linuxio";

// === Connection Management ===
export {
  initStreamMux,
  closeStreamMux,
  waitForStreamMux,
  getStreamMux,
} from "./StreamMultiplexer";

// === Stream Constants & Encoding ===
export {
  encodeString,
  decodeString,
  STREAM_MULTIPLEXER_CONFIG,
  configureStreamMultiplexer,
} from "./StreamMultiplexer";

// === Streaming Helpers ===
export {
  bindStreamHandlers,
  waitForStreamResult,
  streamWriteChunks,
} from "./stream-helpers";
export type * from "./stream-helpers";

// === Stream Types ===
export type * from "./StreamMultiplexer";

// === Domain Types ===
export type * from "./linuxio-types";
