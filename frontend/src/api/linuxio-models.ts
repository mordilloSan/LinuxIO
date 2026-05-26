/**
 * LinuxIO API Domain Model Definitions
 *
 * Shared request/response models consumed by the generated endpoint schema.
 */

export type { ContainerInfo } from "@/types/container";
export type { AppConfig } from "@/types/config";
export type { FilesystemInfo, ResourceStatData } from "@/types/fs";
export type { Update } from "@/types/update";
export type { WireGuardInterface } from "@/types/wireguard";

// ============================================================================
// System Types
// ============================================================================

export interface CPUInfoResponse {
  vendorId: string;
  modelName: string;
  family: string;
  model: string;
  mhz: number;
  cores: number;
  loadAverage: {
    load1: number;
    load5: number;
    load15: number;
  };
  perCoreUsage: number[];
  temperature: Record<string, number>;
}

export interface MemoryInfoResponse {
  system: {
    total: number;
    active: number;
    swapTotal: number;
    swapFree: number;
  };
  docker: {
    used: number;
  };
  zfs: {
    arc: number;
  };
}

export interface GpuDevice {
  address: string;
  actual_freq_mhz?: number;
  boost_freq_mhz?: number;
  boot_vga?: boolean;
  class_name?: string;
  connected_displays?: number;
  current_freq_mhz?: number;
  device_id: string;
  display_names?: string[];
  driver_module?: string;
  driver_version?: string;
  drm_card?: string;
  driver: string;
  fan_percent?: number;
  fan_rpm?: number;
  gtt_total_bytes?: number;
  gtt_used_bytes?: number;
  link_speed?: string;
  link_width?: string;
  max_freq_mhz?: number;
  max_link_speed?: string;
  max_link_width?: string;
  min_freq_mhz?: number;
  memory_free_bytes?: number;
  memory_total_bytes?: number;
  memory_used_bytes?: number;
  model: string;
  numa_node?: number;
  power_draw_watts?: number;
  power_limit_watts?: number;
  power_state?: string;
  programming_interface?: string;
  raw_class?: string;
  requested_freq_mhz?: number;
  revision: string;
  rp0_freq_mhz?: number;
  rp1_freq_mhz?: number;
  rpn_freq_mhz?: number;
  rc6_residency_ms?: number;
  runtime_status?: string;
  subsystem: string;
  subsystem_id: string;
  subclass_name?: string;
  temperature_c?: number;
  utilization_percent?: number;
  vendor: string;
  vendor_id: string;
  visible_memory_total_bytes?: number;
  visible_memory_used_bytes?: number;
}

export interface ApiDisk {
  model: string;
  name: string;
  ro: boolean;
  serial?: string;
  size: string; // e.g. "0B", "953.9G"
  type?: string; // e.g. "nvme", "usb", "sata"
  vendor?: string;
  power?: unknown;
  smart?: unknown;
}

export interface MotherboardInfo {
  baseboard: {
    manufacturer: string;
    model: string;
  };
  bios: {
    vendor: string;
    version: string;
  };
  temperatures?: {
    sensors: Record<string, number>;
  };
}

export interface HostInfo {
  hostname: string;
  os: string;
  platform: string;
  platformVersion: string;
  kernelVersion: string;
  kernelArch: string;
}

export interface SystemInfo {
  chassisType: string;
  productName: string;
  productVersion: string;
  productVendor: string;
  biosVendor: string;
  biosVersion: string;
  biosDate: string;
  cpuSummary: string;
}

export interface SystemLastLogin {
  username: string;
  terminal?: string;
  source?: string;
  time: string;
}

export interface SystemFailedLoginAlert {
  id: string;
  scope?: "user" | "system";
  username: string;
  count: number;
  latestEventId: string;
  latestEvent: AccountUserLogin;
}

export interface SystemHealthSummary {
  failedServicesCount: number;
  failedServices?: string[];
  runningServicesCount: number;
  failedLoginAlert?: SystemFailedLoginAlert | null;
  updatesAvailable: number;
  upToDate: boolean;
  uncleanShutdown: boolean;
  uncleanShutdownBootId?: string;
  lastLogin?: SystemLastLogin | null;
}

export interface PCIDevice {
  class: string;
  model: string;
  vendor: string;
  slot: string;
}

export interface MemoryModule {
  id: string;
  technology: string;
  type: string;
  size: string;
  state: string;
  rank: string;
  speed: string;
}

export type { CapabilitiesResponse } from "./capabilities";

export interface DistroInfo {
  name: string;
  version: string;
  codename: string;
  logo: string;
}

export interface ProcessInfo {
  running: boolean;
  [key: string]: unknown;
}

/** Dashboard network interface stats (simplified) */
export interface InterfaceStats {
  name: string;
  mac: string;
  ipv4: string[] | null;
  rx_speed: number;
  tx_speed: number;
  speed: string;
}

export interface DiskThroughputDevice {
  name: string;
  readBytesPerSec: number;
  writeBytesPerSec: number;
  readOpsPerSec: number;
  writeOpsPerSec: number;
}

export interface DiskThroughputResponse {
  readBytesPerSec: number;
  writeBytesPerSec: number;
  readOpsPerSec: number;
  writeOpsPerSec: number;
  intervalSeconds: number;
  devices: DiskThroughputDevice[];
}

/** Full network interface info (from network.get_network_info) */
export interface NetworkInterface {
  name: string;
  type: string;
  mac: string;
  mtu: number;
  speed: string;
  duplex: string;
  state: number;
  ipv4: string[];
  ipv6: string[];
  rx_speed: number;
  tx_speed: number;
  dns: string[];
  gateway: string;
  ipv4_method?: "auto" | "manual" | "disabled" | "unknown";
}

// ============================================================================
// Docker Types
// ============================================================================

export interface DockerImage {
  Id: string;
  RepoTags: string[];
  Size: number;
  Created: number;
  Containers?: number;
  Labels?: Record<string, string>;
  RepoDigests?: string[];
}

export interface DockerNetwork {
  Id: string;
  Name: string;
  Driver: string;
  Scope: string;
  Internal?: boolean;
  EnableIPv4?: boolean;
  EnableIPv6?: boolean;
  IPAM?: {
    Config?: {
      Subnet: string;
      Gateway: string;
    }[];
  };
  Options?: Record<string, string>;
  Labels?: Record<string, string>;
  Containers?: Record<
    string,
    {
      Name: string;
      IPv4Address?: string;
      IPv6Address?: string;
      MacAddress?: string;
    }
  >;
}

export interface DockerVolume {
  Name: string;
  Driver: string;
  Mountpoint: string;
  CreatedAt?: string;
  Labels?: Record<string, string>;
  Options?: Record<string, string>;
  Scope?: string;
}

export interface DockerSystemInfo {
  // System
  name: string;
  id: string;
  operating_system: string;
  os_type: string;
  architecture: string;
  kernel_version: string;
  system_time: string;
  docker_root_dir: string;
  ncpu: number;
  mem_total: number;
  // Version
  server_version: string;
  api_version: string;
  go_version: string;
  git_commit: string;
  build_time: string;
  experimental: boolean;
  // Configuration
  storage_driver: string;
  logging_driver: string;
  cgroup_driver: string;
  cgroup_version: string;
  init_binary: string;
  default_runtime: string;
  // Network & Proxy
  ipv4_forwarding: boolean;
  http_proxy: string;
  https_proxy: string;
  no_proxy: string;
  // Security & Runtimes
  security_options: string[];
  runtimes: string[];
  // Plugins
  volume_plugins: string[];
  network_plugins: string[];
  log_plugins: string[];
  // Disk
  disk_used: number;
  disk_total: number;
}

export interface ComposeService {
  name: string;
  image: string;
  status: string;
  state: string;
  container_count: number;
  container_ids: string[];
  ports: string[];
}

export interface ComposeProject {
  name: string;
  status: string;
  auto_update?: boolean;
  services: Record<string, ComposeService>;
  config_files: string[];
  working_dir: string;
}

// ============================================================================
// DBus Types
// ============================================================================

export type AutoUpdateFrequency = "hourly" | "daily" | "weekly";
export type AutoUpdateScope = "security" | "updates" | "all";
export type AutoUpdateRebootPolicy =
  | "never"
  | "if_needed"
  | "always"
  | "schedule";

export interface AutoUpdateOptions {
  enabled: boolean;
  frequency: AutoUpdateFrequency;
  scope: AutoUpdateScope;
  download_only: boolean;
  reboot_policy: AutoUpdateRebootPolicy;
  exclude_packages: string[];
}

export interface AutoUpdateState {
  backend: string;
  options: AutoUpdateOptions;
  notes?: string[];
}

export interface Service {
  name: string;
  description?: string;
  load_state: string;
  active_state: string;
  sub_state: string;
  unit_file_state: string;
  active_enter_timestamp: number;
  inactive_enter_timestamp: number;
  [key: string]: unknown;
}

export interface UnitInfo {
  Id?: string;
  Description?: string;
  LoadState?: string;
  ActiveState?: string;
  SubState?: string;
  UnitFileState?: string;
  FragmentPath?: string;
  ActiveEnterTimestamp?: number;
  InactiveEnterTimestamp?: number;
  Requires?: string[];
  Wants?: string[];
  WantedBy?: string[];
  Before?: string[];
  After?: string[];
  Conflicts?: string[];
  PartOf?: string[];
  TriggeredBy?: string[];
  MainPID?: number;
  MemoryCurrent?: number;
  ExecMainStatus?: number;
  NextElapseUSec?: number;
  LastTriggerUSec?: number;
  Unit?: string;
  Listen?: string[];
  NConnections?: number;
  NAccepted?: number;
  [key: string]: unknown;
}

export interface Timer {
  name: string;
  description?: string;
  load_state: string;
  active_state: string;
  sub_state: string;
  unit_file_state: string;
  active_enter_timestamp: number;
  inactive_enter_timestamp: number;
  next_elapse_usec: number;
  last_trigger_usec: number;
  unit: string;
}

export interface Socket {
  name: string;
  description?: string;
  load_state: string;
  active_state: string;
  sub_state: string;
  unit_file_state: string;
  active_enter_timestamp: number;
  inactive_enter_timestamp: number;
  listen: string[];
  n_connections: number;
  n_accepted: number;
}

export interface UpgradeItem {
  package: string;
}

export interface UpdateHistoryRow {
  date: string;
  upgrades: UpgradeItem[];
}

// ============================================================================
// Power Types
// ============================================================================

export interface TunedProfile {
  name: string;
  description?: string;
  active: boolean;
  recommended: boolean;
}

export interface PowerStatus {
  backend: string;
  tuned_available: boolean;
  tuned_active: boolean;
  tuned_activatable: boolean;
  tuned_startable: boolean;
  tuned_unit_available: boolean;
  tuned_unit_file_state: string;
  power_profiles_daemon_active: boolean;
  package_name: string;
  install_command: string;
  active_profile: string;
  recommended_profile: string;
  profiles: TunedProfile[];
  notes?: string[];
  error?: string;
}

// ============================================================================
// Filebrowser Types
// ============================================================================

export interface ApiResource {
  name: string;
  size: number;
  extension: string;
  modified: string;
  mode: string;
  isDir: boolean;
  isSymlink: boolean;
  type: string;
  items?: ApiResource[];
  path: string;
  content?: string;
}

export interface FileResource {
  content?: string;
  items?: ApiResource[];
  path: string;
}

export interface DirectorySizeData {
  path: string;
  size: number;
}

export interface SubfolderData {
  path: string;
  name: string;
  size: number;
  mod_time: string;
}

export interface SubfoldersResponse {
  path: string;
  subfolders: SubfolderData[];
  count: number;
}

export interface SearchResponse {
  query: string;
  results: {
    path: string;
    name: string;
    type?: string;
    isDir?: boolean;
    size: number;
    mod_time?: string;
    modTime?: string;
    modified?: string;
  }[];
  count: number;
}

export interface UsersGroupsResponse {
  users: string[];
  groups: string[];
}

export interface IndexerStatusResponse {
  running: boolean;
  status: string;
  files_indexed: number;
  dirs_indexed: number;
  total_size: number;
  last_indexed?: string;
  warning?: string;
}

// ============================================================================
// Accounts Types
// ============================================================================

export interface AccountUser {
  username: string;
  uid: number;
  gid: number;
  gecos: string;
  homeDir: string;
  shell: string;
  primaryGroup: string;
  groups: string[];
  isSystem: boolean;
  isLocked: boolean;
  lastLogin: string;
}

export interface AccountUserLogin {
  id: string;
  username: string;
  terminal: string;
  source: string;
  time: string;
  startedAt?: string;
  status: "success" | "failed";
}

export interface AccountActiveSession {
  terminal: string;
  startedAt: string;
  idle?: string;
  pid?: number;
  sessionId?: string;
  source?: string;
}

export interface AccountPasswordState {
  locked: boolean;
  hasPassword: boolean;
  lastChanged?: string;
  expires?: string;
  expiresInDays?: number;
  maxDays?: number;
  warningDays?: number;
  error?: string;
}

export interface AccountAdminAccess {
  isAdmin: boolean;
  groups: string[];
}

export interface AccountHomeHealth {
  exists: boolean;
  isDirectory: boolean;
  ownerUid?: number;
  groupGid?: number;
  groupName?: string;
  ownerMatches: boolean;
  mode?: string;
  error?: string;
}

export interface AccountSSHAccess {
  sshDirExists: boolean;
  authorizedKeysExists: boolean;
  authorizedKeysCount: number;
  sshDirMode?: string;
  authorizedKeysMode?: string;
  authorizedKeysOwnerMatches: boolean;
  error?: string;
}

export interface AccountUserProcess {
  pid: number;
  command: string;
  cpu: number;
  memory: number;
}

export interface AccountProcessSummary {
  count: number;
  top: AccountUserProcess[];
  error?: string;
}

export interface AccountUserDetails {
  username: string;
  activeSessions: AccountActiveSession[];
  failedLoginAttempts: number;
  failedLoginAttemptsAvailable: boolean;
  failedLoginAttemptsError?: string;
  password: AccountPasswordState;
  admin: AccountAdminAccess;
  home: AccountHomeHealth;
  ssh: AccountSSHAccess;
  processes: AccountProcessSummary;
}

export interface AccountGroup {
  name: string;
  gid: number;
  members: string[];
  isSystem: boolean;
}

export interface CreateUserRequest {
  username: string;
  password: string;
  fullName?: string;
  homeDir?: string;
  shell?: string;
  groups?: string[];
  createHome?: boolean;
}

export interface ModifyUserRequest {
  username: string;
  fullName?: string;
  homeDir?: string;
  shell?: string;
  groups?: string[];
}

export interface CreateGroupRequest {
  name: string;
  gid?: number;
}

export interface ModifyGroupMembersRequest {
  groupName: string;
  members: string[];
}

// ============================================================================
// Shares Types (NFS Exports & Samba)
// ============================================================================

export interface NFSClient {
  host: string;
  options: string[];
}

export interface NFSExport {
  path: string;
  clients: NFSClient[];
  active: boolean;
}

export interface SambaShare {
  name: string;
  properties: Record<string, string>;
}

// ============================================================================
// Storage Types (LVM & NFS)
// ============================================================================

export interface PhysicalVolume {
  name: string;
  vgName: string;
  size: number;
  free: number;
  attributes: string;
  format: string;
}

export interface VolumeGroup {
  name: string;
  size: number;
  free: number;
  pvCount: number;
  lvCount: number;
  attributes: string;
  pvNames: string[];
}

export interface LogicalVolume {
  name: string;
  vgName: string;
  size: number;
  path: string;
  attributes: string;
  mountpoint: string;
  fsType: string;
  usedPct: number;
}

export interface NFSMount {
  source: string;
  server: string;
  exportPath: string;
  mountpoint: string;
  fsType: string;
  options: string[];
  size: number;
  used: number;
  free: number;
  usedPct: number;
  inFstab: boolean;
  mounted: boolean;
}

// ============================================================================
// Control Types
// ============================================================================

export interface VersionResponse {
  checked_at: string;
  current_version: string;
  latest_version: string;
  update_available: boolean;
}

// ============================================================================
// WireGuard Types
// ============================================================================

export interface Peer {
  name: string;
  public_key: string;
  allowed_ips?: string[];
  endpoint?: string;
  preshared_key?: string;
  persistent_keepalive?: number;
  last_handshake?: string; // RFC3339 or "never"
  last_handshake_unix?: number; // 0 if never
  rx_bytes?: number;
  tx_bytes?: number;
  rx_bps?: number; // bytes/sec
  tx_bps?: number; // bytes/sec
}

export interface PeerConfigDownload {
  filename: string;
  content: string;
}

export interface QRCodeResponse {
  qrcode: string;
}

export interface DeleteStackResult {
  message: string;
  project: string;
  files_deleted: boolean;
  dir_deleted: boolean;
  deleted_path: string;
}

export interface ConfigSetResult {
  message: string;
  path: string;
}

export interface InstallCapabilityResult {
  available: boolean;
  error?: string;
}

export interface IndexerConfig {
  index_path: string;
  index_name: string;
  include_hidden: boolean;
  include_network_mounts: boolean;
  fresh_index: boolean;
  keep_indexes: number;
  db_path: string;
  db_busy_timeout: string;
  db_journal_mode: string;
  db_synchronous: string;
  db_auto_vacuum: string;
  db_max_open_conns: number;
  db_max_idle_conns: number;
  db_conn_max_idle_time: string;
  socket_path: string;
  listen_addr: string;
  interval: string;
}

export interface IndexerConfigSetResult {
  config: IndexerConfig;
  restart_required: boolean;
}

export interface IndexerTimerSetResult {
  config: IndexerConfig;
  interval: string;
  timer_unit: string;
}

export interface IndexerDaemonStatus {
  running: boolean;
  status: string;
  num_dirs: number;
  num_files: number;
  total_size: number;
  last_indexed?: string;
  total_indexes: number;
  total_entries: number;
  database_size: number;
  wal_size: number;
  shm_size: number;
  total_on_disk: number;
  active_operation?: string;
  active_path?: string;
  warning?: string;
}

export interface DirectoryValidationResult {
  valid: boolean;
  exists: boolean;
  canCreate: boolean;
  canWrite: boolean;
  error?: string;
  isDirectory: boolean;
}

export type JobState =
  | "queued"
  | "running"
  | "completed"
  | "failed"
  | "canceled";

export interface JobError {
  message: string;
  code?: number;
}

export interface JobSnapshot {
  id: string;
  type: string;
  args?: string[];
  owner?: {
    session_id?: string;
    username?: string;
    uid?: number;
  };
  state: JobState;
  progress?: unknown;
  result?: unknown;
  error?: JobError;
  created_at: string;
  started_at?: string;
  updated_at: string;
  finished_at?: string;
}

export interface JobEvent {
  type:
    | "job.snapshot"
    | "job.started"
    | "job.progress"
    | "job.result"
    | "job.error"
    | "job.canceled";
  job: JobSnapshot;
  progress?: unknown;
  result?: unknown;
  error?: JobError;
}

// ============================================================================
