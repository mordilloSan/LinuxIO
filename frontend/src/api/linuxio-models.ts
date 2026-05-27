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
  cores: number;
  family: string;
  loadAverage: {
    load1: number;
    load5: number;
    load15: number;
  };
  mhz: number;
  model: string;
  modelName: string;
  perCoreUsage: number[];
  temperature: Record<string, number>;
  vendorId: string;
}

export interface MemoryInfoResponse {
  docker: {
    used: number;
  };
  system: {
    total: number;
    active: number;
    swapTotal: number;
    swapFree: number;
  };
  zfs: {
    arc: number;
  };
}

export interface GpuDevice {
  actual_freq_mhz?: number;
  address: string;
  boost_freq_mhz?: number;
  boot_vga?: boolean;
  class_name?: string;
  connected_displays?: number;
  current_freq_mhz?: number;
  device_id: string;
  display_names?: string[];
  driver: string;
  driver_module?: string;
  driver_version?: string;
  drm_card?: string;
  fan_percent?: number;
  fan_rpm?: number;
  gtt_total_bytes?: number;
  gtt_used_bytes?: number;
  link_speed?: string;
  link_width?: string;
  max_freq_mhz?: number;
  max_link_speed?: string;
  max_link_width?: string;
  memory_free_bytes?: number;
  memory_total_bytes?: number;
  memory_used_bytes?: number;
  min_freq_mhz?: number;
  model: string;
  numa_node?: number;
  power_draw_watts?: number;
  power_limit_watts?: number;
  power_state?: string;
  programming_interface?: string;
  raw_class?: string;
  rc6_residency_ms?: number;
  requested_freq_mhz?: number;
  revision: string;
  rp0_freq_mhz?: number;
  rp1_freq_mhz?: number;
  rpn_freq_mhz?: number;
  runtime_status?: string;
  subclass_name?: string;
  subsystem: string;
  subsystem_id: string;
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
  power?: unknown;
  ro: boolean;
  serial?: string;
  size: string; // e.g. "0B", "953.9G"
  smart?: unknown;
  type?: string; // e.g. "nvme", "usb", "sata"
  vendor?: string;
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
  kernelArch: string;
  kernelVersion: string;
  os: string;
  platform: string;
  platformVersion: string;
}

export interface SystemInfo {
  biosDate: string;
  biosVendor: string;
  biosVersion: string;
  chassisType: string;
  cpuSummary: string;
  productName: string;
  productVendor: string;
  productVersion: string;
}

export interface SystemLastLogin {
  source?: string;
  terminal?: string;
  time: string;
  username: string;
}

export interface SystemFailedLoginAlert {
  count: number;
  id: string;
  latestEvent: AccountUserLogin;
  latestEventId: string;
  scope?: "user" | "system";
  username: string;
}

export interface SystemHealthSummary {
  failedLoginAlert?: SystemFailedLoginAlert | null;
  failedServices?: string[];
  failedServicesCount: number;
  lastLogin?: SystemLastLogin | null;
  runningServicesCount: number;
  uncleanShutdown: boolean;
  uncleanShutdownBootId?: string;
  updatesAvailable: number;
  upToDate: boolean;
}

export interface PCIDevice {
  class: string;
  model: string;
  slot: string;
  vendor: string;
}

export interface MemoryModule {
  id: string;
  rank: string;
  size: string;
  speed: string;
  state: string;
  technology: string;
  type: string;
}

export type { CapabilitiesResponse } from "./capabilities";

export interface DistroInfo {
  codename: string;
  logo: string;
  name: string;
  version: string;
}

export interface ProcessInfo {
  [key: string]: unknown;
  running: boolean;
}

/** Dashboard network interface stats (simplified) */
export interface InterfaceStats {
  ipv4: string[] | null;
  mac: string;
  name: string;
  rx_speed: number;
  speed: string;
  tx_speed: number;
}

export interface DiskThroughputDevice {
  name: string;
  readBytesPerSec: number;
  readOpsPerSec: number;
  writeBytesPerSec: number;
  writeOpsPerSec: number;
}

export interface DiskThroughputResponse {
  devices: DiskThroughputDevice[];
  intervalSeconds: number;
  readBytesPerSec: number;
  readOpsPerSec: number;
  writeBytesPerSec: number;
  writeOpsPerSec: number;
}

/** Full network interface info (from network.get_network_info) */
export interface NetworkInterface {
  dns: string[];
  duplex: string;
  gateway: string;
  ipv4: string[];
  ipv4_method?: "auto" | "manual" | "disabled" | "unknown";
  ipv6: string[];
  mac: string;
  mtu: number;
  name: string;
  rx_speed: number;
  speed: string;
  state: number;
  tx_speed: number;
  type: string;
}

// ============================================================================
// Docker Types
// ============================================================================

export interface DockerImage {
  Containers?: number;
  Created: number;
  Id: string;
  Labels?: Record<string, string>;
  RepoDigests?: string[];
  RepoTags: string[];
  Size: number;
}

export interface DockerNetwork {
  Containers?: Record<
    string,
    {
      Name: string;
      IPv4Address?: string;
      IPv6Address?: string;
      MacAddress?: string;
    }
  >;
  Driver: string;
  EnableIPv4?: boolean;
  EnableIPv6?: boolean;
  Id: string;
  Internal?: boolean;
  IPAM?: {
    Config?: {
      Subnet: string;
      Gateway: string;
    }[];
  };
  Labels?: Record<string, string>;
  Name: string;
  Options?: Record<string, string>;
  Scope: string;
}

export interface DockerVolume {
  CreatedAt?: string;
  Driver: string;
  Labels?: Record<string, string>;
  Mountpoint: string;
  Name: string;
  Options?: Record<string, string>;
  Scope?: string;
}

export interface DockerSystemInfo {
  api_version: string;
  architecture: string;
  build_time: string;
  cgroup_driver: string;
  cgroup_version: string;
  default_runtime: string;
  disk_total: number;
  // Disk
  disk_used: number;
  docker_root_dir: string;
  experimental: boolean;
  git_commit: string;
  go_version: string;
  http_proxy: string;
  https_proxy: string;
  id: string;
  init_binary: string;
  // Network & Proxy
  ipv4_forwarding: boolean;
  kernel_version: string;
  log_plugins: string[];
  logging_driver: string;
  mem_total: number;
  // System
  name: string;
  ncpu: number;
  network_plugins: string[];
  no_proxy: string;
  operating_system: string;
  os_type: string;
  runtimes: string[];
  // Security & Runtimes
  security_options: string[];
  // Version
  server_version: string;
  // Configuration
  storage_driver: string;
  system_time: string;
  // Plugins
  volume_plugins: string[];
}

export interface ComposeService {
  container_count: number;
  container_ids: string[];
  image: string;
  name: string;
  ports: string[];
  state: string;
  status: string;
}

export interface ComposeProject {
  auto_update?: boolean;
  config_files: string[];
  name: string;
  services: Record<string, ComposeService>;
  status: string;
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
  download_only: boolean;
  enabled: boolean;
  exclude_packages: string[];
  frequency: AutoUpdateFrequency;
  reboot_policy: AutoUpdateRebootPolicy;
  scope: AutoUpdateScope;
}

export interface AutoUpdateState {
  backend: string;
  notes?: string[];
  options: AutoUpdateOptions;
}

export interface Service {
  [key: string]: unknown;
  active_enter_timestamp: number;
  active_state: string;
  description?: string;
  inactive_enter_timestamp: number;
  load_state: string;
  name: string;
  sub_state: string;
  unit_file_state: string;
}

export interface UnitInfo {
  [key: string]: unknown;
  ActiveEnterTimestamp?: number;
  ActiveState?: string;
  After?: string[];
  Before?: string[];
  Conflicts?: string[];
  Description?: string;
  ExecMainStatus?: number;
  FragmentPath?: string;
  Id?: string;
  InactiveEnterTimestamp?: number;
  LastTriggerUSec?: number;
  Listen?: string[];
  LoadState?: string;
  MainPID?: number;
  MemoryCurrent?: number;
  NAccepted?: number;
  NConnections?: number;
  NextElapseUSec?: number;
  PartOf?: string[];
  Requires?: string[];
  SubState?: string;
  TriggeredBy?: string[];
  Unit?: string;
  UnitFileState?: string;
  WantedBy?: string[];
  Wants?: string[];
}

export interface Timer {
  active_enter_timestamp: number;
  active_state: string;
  description?: string;
  inactive_enter_timestamp: number;
  last_trigger_usec: number;
  load_state: string;
  name: string;
  next_elapse_usec: number;
  sub_state: string;
  unit: string;
  unit_file_state: string;
}

export interface Socket {
  active_enter_timestamp: number;
  active_state: string;
  description?: string;
  inactive_enter_timestamp: number;
  listen: string[];
  load_state: string;
  n_accepted: number;
  n_connections: number;
  name: string;
  sub_state: string;
  unit_file_state: string;
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
  active: boolean;
  description?: string;
  name: string;
  recommended: boolean;
}

export interface PowerStatus {
  active_profile: string;
  backend: string;
  error?: string;
  install_command: string;
  notes?: string[];
  package_name: string;
  power_profiles_daemon_active: boolean;
  profiles: TunedProfile[];
  recommended_profile: string;
  tuned_activatable: boolean;
  tuned_active: boolean;
  tuned_available: boolean;
  tuned_startable: boolean;
  tuned_unit_available: boolean;
  tuned_unit_file_state: string;
}

// ============================================================================
// Filebrowser Types
// ============================================================================

export interface ApiResource {
  content?: string;
  extension: string;
  isDir: boolean;
  isSymlink: boolean;
  items?: ApiResource[];
  mode: string;
  modified: string;
  name: string;
  path: string;
  size: number;
  type: string;
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
  mod_time: string;
  name: string;
  path: string;
  size: number;
}

export interface SubfoldersResponse {
  count: number;
  path: string;
  subfolders: SubfolderData[];
}

export interface SearchResponse {
  count: number;
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
}

export interface UsersGroupsResponse {
  groups: string[];
  users: string[];
}

export interface IndexerStatusResponse {
  dirs_indexed: number;
  files_indexed: number;
  last_indexed?: string;
  running: boolean;
  status: string;
  total_size: number;
  warning?: string;
}

// ============================================================================
// Accounts Types
// ============================================================================

export interface AccountUser {
  gecos: string;
  gid: number;
  groups: string[];
  homeDir: string;
  isLocked: boolean;
  isSystem: boolean;
  lastLogin: string;
  primaryGroup: string;
  shell: string;
  uid: number;
  username: string;
}

export interface AccountUserLogin {
  id: string;
  source: string;
  startedAt?: string;
  status: "success" | "failed";
  terminal: string;
  time: string;
  username: string;
}

export interface AccountActiveSession {
  idle?: string;
  pid?: number;
  sessionId?: string;
  source?: string;
  startedAt: string;
  terminal: string;
}

export interface AccountPasswordState {
  error?: string;
  expires?: string;
  expiresInDays?: number;
  hasPassword: boolean;
  lastChanged?: string;
  locked: boolean;
  maxDays?: number;
  warningDays?: number;
}

export interface AccountAdminAccess {
  groups: string[];
  isAdmin: boolean;
}

export interface AccountHomeHealth {
  error?: string;
  exists: boolean;
  groupGid?: number;
  groupName?: string;
  isDirectory: boolean;
  mode?: string;
  ownerMatches: boolean;
  ownerUid?: number;
}

export interface AccountSSHAccess {
  authorizedKeysCount: number;
  authorizedKeysExists: boolean;
  authorizedKeysMode?: string;
  authorizedKeysOwnerMatches: boolean;
  error?: string;
  sshDirExists: boolean;
  sshDirMode?: string;
}

export interface AccountUserProcess {
  command: string;
  cpu: number;
  memory: number;
  pid: number;
}

export interface AccountProcessSummary {
  count: number;
  error?: string;
  top: AccountUserProcess[];
}

export interface AccountUserDetails {
  activeSessions: AccountActiveSession[];
  admin: AccountAdminAccess;
  failedLoginAttempts: number;
  failedLoginAttemptsAvailable: boolean;
  failedLoginAttemptsError?: string;
  home: AccountHomeHealth;
  password: AccountPasswordState;
  processes: AccountProcessSummary;
  ssh: AccountSSHAccess;
  username: string;
}

export interface AccountGroup {
  gid: number;
  isSystem: boolean;
  members: string[];
  name: string;
}

export interface CreateUserRequest {
  createHome?: boolean;
  fullName?: string;
  groups?: string[];
  homeDir?: string;
  password: string;
  shell?: string;
  username: string;
}

export interface ModifyUserRequest {
  fullName?: string;
  groups?: string[];
  homeDir?: string;
  shell?: string;
  username: string;
}

export interface CreateGroupRequest {
  gid?: number;
  name: string;
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
  active: boolean;
  clients: NFSClient[];
  path: string;
}

export interface SambaShare {
  name: string;
  properties: Record<string, string>;
}

// ============================================================================
// Storage Types (LVM & NFS)
// ============================================================================

export interface PhysicalVolume {
  attributes: string;
  format: string;
  free: number;
  name: string;
  size: number;
  vgName: string;
}

export interface VolumeGroup {
  attributes: string;
  free: number;
  lvCount: number;
  name: string;
  pvCount: number;
  pvNames: string[];
  size: number;
}

export interface LogicalVolume {
  attributes: string;
  fsType: string;
  mountpoint: string;
  name: string;
  path: string;
  size: number;
  usedPct: number;
  vgName: string;
}

export interface NFSMount {
  exportPath: string;
  free: number;
  fsType: string;
  inFstab: boolean;
  mounted: boolean;
  mountpoint: string;
  options: string[];
  server: string;
  size: number;
  source: string;
  used: number;
  usedPct: number;
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
  allowed_ips?: string[];
  endpoint?: string;
  last_handshake?: string; // RFC3339 or "never"
  last_handshake_unix?: number; // 0 if never
  name: string;
  persistent_keepalive?: number;
  preshared_key?: string;
  public_key: string;
  rx_bps?: number; // bytes/sec
  rx_bytes?: number;
  tx_bps?: number; // bytes/sec
  tx_bytes?: number;
}

export interface PeerConfigDownload {
  content: string;
  filename: string;
}

export interface QRCodeResponse {
  qrcode: string;
}

export interface DeleteStackResult {
  deleted_path: string;
  dir_deleted: boolean;
  files_deleted: boolean;
  message: string;
  project: string;
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
  db_auto_vacuum: string;
  db_busy_timeout: string;
  db_conn_max_idle_time: string;
  db_journal_mode: string;
  db_max_idle_conns: number;
  db_max_open_conns: number;
  db_path: string;
  db_synchronous: string;
  fresh_index: boolean;
  include_hidden: boolean;
  include_network_mounts: boolean;
  index_name: string;
  index_path: string;
  interval: string;
  keep_indexes: number;
  listen_addr: string;
  socket_path: string;
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
  active_operation?: string;
  active_path?: string;
  database_size: number;
  last_indexed?: string;
  num_dirs: number;
  num_files: number;
  running: boolean;
  shm_size: number;
  status: string;
  total_entries: number;
  total_indexes: number;
  total_on_disk: number;
  total_size: number;
  wal_size: number;
  warning?: string;
}

export interface DirectoryValidationResult {
  canCreate: boolean;
  canWrite: boolean;
  error?: string;
  exists: boolean;
  isDirectory: boolean;
  valid: boolean;
}

export type JobState =
  | "queued"
  | "running"
  | "completed"
  | "failed"
  | "canceled";

export interface JobError {
  code?: number;
  message: string;
}

export interface JobSnapshot {
  args?: string[];
  created_at: string;
  error?: JobError;
  finished_at?: string;
  id: string;
  owner?: {
    session_id?: string;
    username?: string;
    uid?: number;
  };
  progress?: unknown;
  result?: unknown;
  started_at?: string;
  state: JobState;
  type: string;
  updated_at: string;
}

export interface JobEvent {
  error?: JobError;
  job: JobSnapshot;
  progress?: unknown;
  result?: unknown;
  type:
    | "job.snapshot"
    | "job.started"
    | "job.progress"
    | "job.result"
    | "job.error"
    | "job.canceled";
}

// ============================================================================
