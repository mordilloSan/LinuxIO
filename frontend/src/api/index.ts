/**
 * LinuxIO API - Unified Entry Point
 *
 * JSON API (generated, Go-owned request/response contracts):
 *   await linuxio.system.get_cpu_info()
 *   await linuxio.jobs.cancel(jobId)
 *   linuxio.system.get_cpu_info.useQuery()
 *   linuxio.docker.start_container.useMutation()
 *
 * Streaming API (persistent/long-lived streams):
 *   const stream = openTerminalStream(cols, rows);
 *   stream.onData = (data) => ...;
 */

// === JSON API (generated type-safe endpoints) ===
export { default as linuxio } from "./generated/client";
export { CACHE_TTL_MS } from "./react-query";
export {
  ROUTE_MODES,
  getRouteMode,
  routeName,
} from "./generated/route-metadata";
export type { RouteMode } from "./generated/route-metadata";
export {
  isJobSnapshot,
  isJobLocallyHandled,
  isTerminalJobState,
  jobSnapshotResult,
} from "./jobs";

// === API Error Type ===
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
  openAppUpdateStream,
  openVMConsoleStream,
  openJobAttachStream,
  openJobDataStream,
  openJobEventsStream,
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
