/**
 * LinuxIO API - Unified Entry Point
 *
 * JSON (request/response) → React Query:
 *   linuxio.system.get_cpu_info.useQuery()
 *   linuxio.docker.start_container.useMutation()
 *
 */

// === JSON API (React Query type-safe proxy) ===
export { default as linuxio } from "./react-query";

// === Core API (Promise-based) ===
export { LinuxIOError } from "./linuxio-core";

// === React Hooks ===
export { useStreamMux, useIsUpdating } from "./linuxio";

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
  isConnected,
  getStatus,
} from "./linuxio";

// === Connection Management ===
export {
  initStreamMux,
  closeStreamMux,
  waitForStreamMux,
  getStreamMux,
} from "./StreamMultiplexer";

// === Utilities ===
export {
  encodeString,
  decodeString,
  STREAM_CHUNK_SIZE,
  UPLOAD_WINDOW_SIZE,
} from "./StreamMultiplexer";

// === Streaming Helpers ===
export {
  bindStreamHandlers,
  waitForStreamResult,
  streamWriteChunks,
} from "./stream-helpers";
export type * from "./stream-helpers";

// === Cache Policy ===
export { CACHE_TTL_MS } from "./cache-policy";

// === Stream Types ===
export type * from "./StreamMultiplexer";

// === Domain Types ===
export type * from "./linuxio-types";
