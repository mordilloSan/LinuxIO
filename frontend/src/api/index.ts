/**
 * LinuxIO API - Unified Entry Point
 *
 * JSON API (generated, Go-owned request/response contracts):
 *   useQuery(linuxio.system.get_cpu_info)
 *   useCallMutation(linuxio.docker.start_container, { success, error })
 *   linuxio.docker.compose.useTaskStreamAction({ onProgress })
 *
 * Streaming API (persistent/long-lived streams):
 *   const stream = openTerminalStream(cols, rows);
 *   stream.onData = (data) => ...;
 */

// === JSON API (generated type-safe endpoints) ===
export { default as linuxio } from "./generated/client";
export {
  CACHE_TTL_MS,
  invalidateOperationQueries,
  useCallMutation,
  type ActionConfig,
  type CallDefinition,
  type CallDescriptor,
  type CallFactory,
  type CallQueryOptions,
} from "./call-react-query";
export { call } from "./calls";
export type {
  TaskStreamActionConfig,
  TaskStreamActionResult,
} from "./task-react-query";
export {
  ROUTE_MODES,
  getRouteMode,
  routeName,
} from "./generated/route-metadata";
export type { RouteMode } from "./generated/route-metadata";
export {
  isTaskCancellationError,
  isTaskSnapshot,
  isTerminalTaskState,
  taskSnapshotResult,
  TASK_CANCELED_CODE,
} from "./tasks";

// === API Error Type ===
export {
  LinuxIOError,
  ensureLoaderRequestReady,
  isConnectionLossError,
} from "./linuxio-core";
export type { ConnectionLossCode } from "./linuxio-core";

// === React Hooks ===
export { useStreamMux } from "./linuxio";
export { isRequestAvailable, subscribeRequestAvailability } from "./linuxio";

// === Connection Utilities ===
export { isConnected, getStatus } from "./linuxio";

// === Stream Openers ===
export {
  openChannel,
  openTerminalStream,
  openContainerStream,
  openAppUpdateStream,
  openVMConsoleStream,
  openTaskDataStream,
  openTaskEventsStream,
  openTaskWatchStream,
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
export { uploadContent } from "./uploads";
export type { UploadContentOptions } from "./uploads";
export {
  createStreamMessageChannel,
  StreamMessageChannel,
} from "./stream-channel";
export type * from "./stream-channel";

// === Stream Types ===
export type * from "./StreamMultiplexer";

// === Domain/API Types ===
export type * from "./generated/linuxio-types";

// === Capabilities (manifest, types, helpers) ===
export type {
  CapabilityDef,
  CapabilityErrorKey,
  CapabilityKey,
  CapabilityState,
  CapabilityValueKey,
  CapabilityWire,
} from "./capabilities";
export {
  CAPABILITIES,
  CAPABILITY_KEYS,
  capabilityStateFromWire,
  emptyCapabilityState,
  parseCapabilityState,
  pickCapabilityState,
} from "./capabilities";
