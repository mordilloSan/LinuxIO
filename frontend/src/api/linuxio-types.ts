/**
 * LinuxIO API Type Definitions
 *
 * Auto-complete friendly, fully typed API definitions.
 * Each handler namespace contains its commands with typed args and return values.
 *
 * NOTE: These types are derived from actual backend responses as used by frontend components.
 */

import type { ContainerInfo } from "@/types/container";
import type { FilesystemInfo, ResourceStatData } from "@/types/fs";
import type { Update } from "@/types/update";
import type { WireGuardInterface } from "@/types/wireguard";
import type {
  ModuleInfo,
  ModuleDetailsInfo,
  ValidationResult,
  InstallResult,
  UninstallResult,
} from "@/types/module";

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
  temperature: { [core: string]: number };
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
  device_id: string;
  driver: string;
  model: string;
  revision: string;
  subsystem: string;
  subsystem_id: string;
  vendor: string;
  vendor_id: string;
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
    socket: number[];
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

/** Full network interface info (from dbus GetNetworkInfo) */
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
    Config?: Array<{
      Subnet: string;
      Gateway: string;
    }>;
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
  [key: string]: unknown;
}

export interface UpgradeItem {
  package: string;
}

export interface UpdateHistoryRow {
  date: string;
  upgrades: UpgradeItem[];
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
  results: Array<{
    path: string;
    name: string;
    isDir: boolean;
    size: number;
    modified: string;
  }>;
  count: number;
}

export interface UsersGroupsResponse {
  users: string[];
  groups: string[];
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

// ============================================================================
// API Schema Definition
// ============================================================================

/**
 * Complete API schema mapping handlers to commands to types.
 * Format: { handler: { command: { args: ArgsType, result: ResultType } } }
 */
export interface LinuxIOSchema {
  system: {
    get_cpu_info: { args: []; result: CPUInfoResponse };
    get_sensor_info: { args: []; result: unknown };
    get_motherboard_info: { args: []; result: MotherboardInfo };
    get_memory_info: { args: []; result: MemoryInfoResponse };
    get_host_info: { args: []; result: HostInfo };
    get_uptime: { args: []; result: number };
    get_fs_info: { args: []; result: FilesystemInfo[] };
    get_processes: { args: []; result: ProcessInfo[] };
    get_gpu_info: { args: []; result: GpuDevice[] };
    get_updates_fast: { args: []; result: Update[] };
    get_network_info: { args: []; result: InterfaceStats[] };
  };

  docker: {
    list_containers: { args: []; result: ContainerInfo[] };
    start_container: { args: [containerId: string]; result: void };
    stop_container: { args: [containerId: string]; result: void };
    get_container_logs: { args: [containerId: string]; result: string };
    remove_container: { args: [containerId: string]; result: void };
    restart_container: { args: [containerId: string]; result: void };
    list_images: { args: []; result: DockerImage[] };
    delete_image: { args: [imageId: string]; result: void };
    list_networks: { args: []; result: DockerNetwork[] };
    create_network: { args: [name: string]; result: void };
    delete_network: { args: [id: string]; result: void };
    list_volumes: { args: []; result: DockerVolume[] };
    create_volume: { args: [name: string]; result: void };
    delete_volume: { args: [name: string]; result: void };
    list_compose_projects: { args: []; result: ComposeProject[] };
    get_compose_project: {
      args: [projectName: string];
      result: ComposeProject;
    };
    compose_up: { args: [projectName: string]; result: any };
    compose_down: { args: [projectName: string]; result: any };
    compose_stop: { args: [projectName: string]; result: any };
    compose_restart: { args: [projectName: string]; result: any };
    get_docker_folder: { args: []; result: { folder: string } };
    validate_compose: {
      args: [content: string];
      result: {
        valid: boolean;
        errors: Array<{
          line?: number;
          column?: number;
          field?: string;
          message: string;
          type: "error" | "warning";
        }>;
      };
    };
    get_compose_file_path: {
      args: [stackName: string];
      result: { path: string; exists: boolean; directory: string };
    };
  };

  dbus: {
    Reboot: { args: []; result: void };
    PowerOff: { args: []; result: void };
    GetUpdates: { args: []; result: Update[] };
    GetUpdatesBasic: { args: []; result: Update[] };
    GetUpdateDetail: { args: [packageId: string]; result: Update };
    InstallPackage: { args: [packageId: string]; result: void };
    GetAutoUpdates: { args: []; result: AutoUpdateState };
    SetAutoUpdates: {
      args: [options: AutoUpdateOptions];
      result: AutoUpdateState;
    };
    ApplyOfflineUpdates: {
      args: [];
      result: { status?: string; error?: string };
    };
    GetUpdateHistory: { args: []; result: UpdateHistoryRow[] };
    ListServices: { args: []; result: Service[] };
    GetServiceInfo: { args: [serviceName: string]; result: Service };
    GetServiceLogs: {
      args: [serviceName: string, lines: string];
      result: string[];
    };
    StartService: { args: [serviceName: string]; result: void };
    StopService: { args: [serviceName: string]; result: void };
    RestartService: { args: [serviceName: string]; result: void };
    ReloadService: { args: [serviceName: string]; result: void };
    EnableService: { args: [serviceName: string]; result: void };
    DisableService: { args: [serviceName: string]; result: void };
    MaskService: { args: [serviceName: string]; result: void };
    UnmaskService: { args: [serviceName: string]; result: void };
    GetNetworkInfo: { args: []; result: NetworkInterface[] };
    SetIPv4Manual: {
      args: [iface: string, address: string, gateway: string, dns: string];
      result: void;
    };
    SetIPv4: { args: [iface: string, method: string]; result: void };
    SetIPv6: { args: [iface: string, method: string]; result: void };
    SetMTU: { args: [iface: string, mtu: string]; result: void };
    EnableConnection: { args: [iface: string]; result: void };
    DisableConnection: { args: [iface: string]; result: void };
  };

  filebrowser: {
    resource_get: {
      args: [path: string, unused?: string, getContent?: string];
      result: ApiResource;
    };
    resource_stat: { args: [path: string]; result: ResourceStatData };
    resource_delete: { args: [path: string]; result: void };
    resource_post: { args: [path: string, action?: string]; result: void };
    resource_patch: {
      args: [action: string, src: string, dst: string];
      result: void;
    };
    dir_size: { args: [path: string]; result: DirectorySizeData };
    subfolders: { args: [path: string]; result: SubfoldersResponse };
    search: {
      args: [query: string, limit?: string, basePath?: string];
      result: SearchResponse;
    };
    indexer_status: {
      args: [];
      result: { running: boolean; progress: number };
    };
    chmod: {
      args: [
        path: string,
        mode: string,
        owner?: string,
        group?: string,
        recursive?: string,
      ];
      result: void;
    };
    users_groups: { args: []; result: UsersGroupsResponse };
    file_update_from_temp: {
      args: [tempPath: string, targetPath: string];
      result: void;
    };
    download: { args: [path: string]; result: Uint8Array };
    archive: { args: [path: string]; result: void };
    compress: {
      args: [outputPath: string, format: string, ...files: string[]];
      result: void;
    };
    extract: { args: [archivePath: string, destPath: string]; result: void };
  };

  config: {
    theme_get: { args: []; result: string };
    theme_set: { args: [theme: string]; result: void };
  };

  control: {
    version: { args: []; result: VersionResponse };
    update: { args: []; result: void };
    shutdown: { args: []; result: void };
  };

  modules: {
    GetModules: { args: []; result: ModuleInfo[] };
    GetModuleDetails: {
      args: [moduleName: string];
      result: ModuleDetailsInfo;
    };
    ValidateModule: { args: [path: string]; result: ValidationResult };
    InstallModule: {
      args: [sourcePath: string, targetName?: string, createSymlink?: string];
      result: InstallResult;
    };
    UninstallModule: { args: [moduleName: string]; result: UninstallResult };
  };

  wireguard: {
    list_interfaces: { args: []; result: WireGuardInterface[] };
    add_interface: { args: [name: string]; result: void };
    remove_interface: { args: [name: string]; result: void };
    list_peers: { args: [interfaceName: string]; result: Peer[] };
    add_peer: { args: [interfaceName: string]; result: void };
    remove_peer: {
      args: [interfaceName: string, publicKey: string];
      result: void;
    };
    peer_qrcode: {
      args: [interfaceName: string, publicKey: string];
      result: QRCodeResponse;
    };
    peer_config_download: {
      args: [interfaceName: string, publicKey: string];
      result: PeerConfigDownload;
    };
    get_keys: { args: []; result: { publicKey: string; privateKey: string } };
    up_interface: { args: [name: string]; result: void };
    down_interface: { args: [name: string]; result: void };
    enable_interface: { args: [name: string]; result: void };
    disable_interface: { args: [name: string]; result: void };
  };

  terminal: {
    list_shells: {
      args: [containerId: string];
      result: string[];
    };
  };

  accounts: {
    // User management
    list_users: { args: []; result: AccountUser[] };
    get_user: { args: [username: string]; result: AccountUser };
    create_user: { args: [request: string]; result: void };
    delete_user: { args: [username: string]; result: void };
    modify_user: { args: [request: string]; result: void };
    change_password: {
      args: [username: string, password: string];
      result: void;
    };
    lock_user: { args: [username: string]; result: void };
    unlock_user: { args: [username: string]; result: void };
    // Group management
    list_groups: { args: []; result: AccountGroup[] };
    get_group: { args: [groupName: string]; result: AccountGroup };
    create_group: { args: [request: string]; result: void };
    delete_group: { args: [groupName: string]; result: void };
    modify_group_members: { args: [request: string]; result: void };
    // Utility
    list_shells: { args: []; result: string[] };
  };

  storage: {
    // LVM Read
    list_pvs: { args: []; result: PhysicalVolume[] };
    list_vgs: { args: []; result: VolumeGroup[] };
    list_lvs: { args: []; result: LogicalVolume[] };

    // LVM Write
    create_lv: {
      args: [vgName: string, lvName: string, size: string];
      result: { success: boolean; path: string };
    };
    delete_lv: {
      args: [vgName: string, lvName: string];
      result: { success: boolean };
    };
    resize_lv: {
      args: [vgName: string, lvName: string, newSize: string];
      result: { success: boolean };
    };
    // Drive
    get_drive_info: { args: []; result: ApiDisk[] };
    run_smart_test: {
      args: [device: string, testType: string];
      result: {
        success: boolean;
        device: string;
        test: string;
        message: string;
      };
    };
    // NFS
    list_nfs_mounts: { args: []; result: NFSMount[] };
    list_nfs_exports: { args: [server: string]; result: string[] };
    mount_nfs: {
      args: [
        server: string,
        exportPath: string,
        mountpoint: string,
        options: string,
        persist: string,
      ];
      result: { success: boolean; mountpoint?: string; warning?: string };
    };
    unmount_nfs: {
      args: [mountpoint: string, removeFstab: string];
      result: { success: boolean; warning?: string };
    };
    remount_nfs: {
      args: [mountpoint: string, options: string, updateFstab: string];
      result: { success: boolean; mountpoint?: string; warning?: string };
    };
  };
}

// ============================================================================
// Type Utilities
// ============================================================================

/** Extract handler names from schema */
export type HandlerName = keyof LinuxIOSchema;

/** Extract command names for a given handler */
export type CommandName<H extends HandlerName> = keyof LinuxIOSchema[H];

/** Extract args type for a handler/command pair */
export type CommandArgs<
  H extends HandlerName,
  C extends CommandName<H>,
> = LinuxIOSchema[H][C] extends { args: infer A } ? A : never;

/** Extract result type for a handler/command pair */
export type CommandResult<
  H extends HandlerName,
  C extends CommandName<H>,
> = LinuxIOSchema[H][C] extends { result: infer R } ? R : never;
