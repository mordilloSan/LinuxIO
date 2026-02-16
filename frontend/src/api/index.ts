/**
 * LinuxIO API - Unified Entry Point
 *
 * JSON (request/response) → React Query:
 *   linuxio.system.get_cpu_info.useQuery()
 *   linuxio.docker.start_container.useMutation()
 *
 * Non-JSON (streaming) → openStream():
 *   const stream = openStream("terminal", "bash", ["120", "32"], "terminal");
 *
 * Module SDK:
 *   linuxio.useCall(), linuxio.useMutate()
 */

// === JSON API (React Query type-safe proxy) ===
export { default as linuxio } from "./react-query";

// === Core API (Promise-based) ===
export {
  call,
  spawn,
  openStream,
  LinuxIOError,
  SpawnedProcess,
} from "./linuxio-core";
export type { CallOptions, SpawnOptions } from "./linuxio-core";

// === React Hooks ===
export { useStreamMux, useIsUpdating } from "./linuxio";

// === Payload Builders ===
export {
  terminalPayload,
  dockerLogsPayload,
  serviceLogsPayload,
  generalLogsPayload,
  containerPayload,
  uploadPayload,
  downloadPayload,
  compressPayload,
  extractPayload,
  packageUpdatePayload,
  execPayload,
  smartTestPayload,
  dockerComposePayload,
  dockerReindexPayload,
  fileReindexPayload,
  fileCopyPayload,
  fileMovePayload,
  openTerminalStream,
  openContainerStream,
  openDockerLogsStream,
  openServiceLogsStream,
  openGeneralLogsStream,
  openDockerComposeStream,
  openDockerReindexStream,
  openExecStream,
  openPackageUpdateStream,
  openSmartTestStream,
  openFileUploadStream,
  openFileDownloadStream,
  openFileCompressStream,
  openFileExtractStream,
  openFileReindexStream,
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
export type {
  StreamEventHandlers,
  WaitForStreamResultOptions,
  WriteStreamChunksOptions,
} from "./stream-helpers";

// === Cache Policy ===
export { CACHE_TTL_MS } from "./cache-policy";

// === Stream Types ===
export type {
  Stream,
  ProgressFrame,
  ResultFrame,
  MuxStatus,
  StreamType,
  StreamStatus,
} from "./StreamMultiplexer";

// === Domain Types ===
export type {
  LinuxIOSchema,
  HandlerName,
  CommandName,
  CommandArgs,
  CommandResult,
  CPUInfoResponse,
  MemoryInfoResponse,
  GpuDevice,
  ApiDisk,
  MotherboardInfo,
  HostInfo,
  CapabilitiesResponse,
  DistroInfo,
  ProcessInfo,
  InterfaceStats,
  NetworkInterface,
  DockerImage,
  DockerNetwork,
  DockerVolume,
  ComposeService,
  ComposeProject,
  AutoUpdateFrequency,
  AutoUpdateScope,
  AutoUpdateRebootPolicy,
  AutoUpdateOptions,
  AutoUpdateState,
  Service,
  UpgradeItem,
  UpdateHistoryRow,
  ApiResource,
  FileResource,
  DirectorySizeData,
  SubfolderData,
  SubfoldersResponse,
  SearchResponse,
  UsersGroupsResponse,
  FileDownloadResult,
  ArchiveDownloadResult,
  CompressResult,
  ExtractResult,
  AccountUser,
  AccountGroup,
  CreateUserRequest,
  ModifyUserRequest,
  CreateGroupRequest,
  ModifyGroupMembersRequest,
  PhysicalVolume,
  VolumeGroup,
  LogicalVolume,
  NFSMount,
  VersionResponse,
  Peer,
  PeerConfigDownload,
  QRCodeResponse,
  DeleteStackResult,
  ConfigSettings,
  ConfigSetResult,
  DockerConfigSetResult,
  DirectoryValidationResult,
} from "./linuxio-types";
