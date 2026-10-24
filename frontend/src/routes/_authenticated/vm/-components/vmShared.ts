import type { VMDeleteResult, VMPreflight, VirtualMachine } from "@/api";
import { getDistroIcon } from "@/icons/distro";

export type VMAction =
  | "start"
  | "shutdown"
  | "reboot"
  | "force_off"
  | "suspend"
  | "resume";

export const VM_TOAST = { label: "Open VMs", to: "/vm" } as const;

export const IMAGE_PRESETS = [
  {
    diskGB: "32",
    id: "home-assistant-os",
    imagePresetId: "home-assistant-os",
    label: "Home Assistant OS",
    memoryMB: "4096",
    minDiskGB: 32,
    sourceType: "imagePreset",
    start: true,
    vcpus: "2",
    bridgedPreferred: true,
  },
  {
    diskGB: "20",
    id: "debian-server",
    imagePresetId: "debian-server",
    label: "Debian Server",
    memoryMB: "2048",
    minDiskGB: 12,
    sourceType: "imagePreset",
    start: true,
    vcpus: "2",
    bridgedPreferred: false,
  },
  {
    diskGB: "24",
    id: "ubuntu-server",
    imagePresetId: "ubuntu-server",
    label: "Ubuntu Server LTS",
    memoryMB: "2048",
    minDiskGB: 12,
    sourceType: "imagePreset",
    start: true,
    vcpus: "2",
    bridgedPreferred: false,
  },
  {
    diskGB: "24",
    id: "fedora-cloud",
    imagePresetId: "fedora-cloud",
    label: "Fedora Cloud",
    memoryMB: "2048",
    minDiskGB: 12,
    sourceType: "imagePreset",
    start: true,
    vcpus: "2",
    bridgedPreferred: false,
  },
] as const;

export const CLOUD_INIT_IMAGE_PRESETS = new Set<string>([
  "debian-server",
  "ubuntu-server",
  "fedora-cloud",
]);

export const DEFAULT_MANAGED_ROOT_PATH = "/var/lib/libvirt/images/linuxio";
export const DEFAULT_MANAGED_ISO_PATH = `${DEFAULT_MANAGED_ROOT_PATH}/isos`;
export const DEFAULT_MANAGED_CLOUD_PATH = `${DEFAULT_MANAGED_ROOT_PATH}/cloud-images`;

export type ReadyImagePreset = (typeof IMAGE_PRESETS)[number];
export type VMPresetID = ReadyImagePreset["id"] | "custom";
export type VMDialogSourceType = "iso" | "imagePreset" | "import";
export type VMDialogImagePresetID = NonNullable<
  ReadyImagePreset["imagePresetId"]
>;
export type VMCreateMode = "iso" | "image" | "import";

export function formatMemory(memoryMB: number): string {
  if (!memoryMB) return "-";
  if (memoryMB >= 1024) {
    const gb = memoryMB / 1024;
    return `${Number.isInteger(gb) ? gb.toFixed(0) : gb.toFixed(1)} GB`;
  }
  return `${memoryMB} MB`;
}

export function normalizeVMDeleteResult(
  result: Partial<VMDeleteResult> | null | undefined,
): VMDeleteResult {
  return {
    preserved: Array.isArray(result?.preserved) ? result.preserved : [],
    removed: Array.isArray(result?.removed) ? result.removed : [],
  };
}

export function formatDisk(diskGB: number): string {
  return diskGB > 0 ? `${diskGB} GB` : "-";
}

export function vmIPAddresses(vm: VirtualMachine): string[] {
  return (vm.nics ?? []).flatMap((nic) => nic.ipAddresses ?? []);
}

export function normalizeState(state: string): string {
  return state ? state.replaceAll("_", " ") : "unknown";
}

export function stateChipColor(
  state: string,
): "success" | "warning" | "error" | "default" {
  switch (state) {
    case "running":
      return "success";
    case "paused":
    case "pmsuspended":
      return "warning";
    case "shutoff":
    case "shutdown":
    case "crashed":
      return state === "crashed" ? "error" : "default";
    default:
      return "default";
  }
}

export function preflightReady(preflight: VMPreflight | undefined): boolean {
  return Boolean(preflight && (preflight.errors ?? []).length === 0);
}

export function isISOPath(path: string): boolean {
  return path.toLowerCase().endsWith(".iso");
}

export function isImportDiskPath(path: string): boolean {
  return /\.(ova|qcow2|vmdk|vdi|img|raw)$/i.test(path);
}

function normalizeFolderPath(path: string): string {
  const trimmed = path.trim();
  if (!trimmed) return "";
  if (/^\/+$/.test(trimmed)) return "/";
  return trimmed.replace(/\/+$/, "");
}

export function parentDirectory(path: string): string {
  const normalized = normalizeFolderPath(path);
  if (!normalized || normalized === "/") return "";
  const slashIndex = normalized.lastIndexOf("/");
  if (slashIndex <= 0) return "/";
  return normalized.slice(0, slashIndex);
}

export function folderFromISOPathText(path: string): string {
  const trimmed = path.trim();
  if (!trimmed.startsWith("/")) return "";
  if (isISOPath(trimmed)) return parentDirectory(trimmed);
  return normalizeFolderPath(trimmed);
}

// libosinfo ids that the distro map does not spell the same way.
const VM_OS_ICONS: Record<string, string> = {
  archlinux: "simple-icons:archlinux",
  "centos-stream": "simple-icons:centos",
  freebsd: "simple-icons:freebsd",
  homeassistant: "simple-icons:homeassistant",
};

/** OS ids the edit dialog offers; their logos come from vmOSIcon. */
export const VM_OS_OPTIONS = [
  { id: "almalinux", label: "AlmaLinux" },
  { id: "alpine", label: "Alpine Linux" },
  { id: "archlinux", label: "Arch Linux" },
  { id: "centos-stream", label: "CentOS Stream" },
  { id: "debian", label: "Debian" },
  { id: "fedora", label: "Fedora" },
  { id: "freebsd", label: "FreeBSD" },
  { id: "homeassistant", label: "Home Assistant OS" },
  { id: "kali", label: "Kali Linux" },
  { id: "linuxmint", label: "Linux Mint" },
  { id: "manjaro", label: "Manjaro" },
  { id: "nixos", label: "NixOS" },
  { id: "opensuse", label: "openSUSE" },
  { id: "rhel", label: "Red Hat Enterprise Linux" },
  { id: "rocky", label: "Rocky Linux" },
  { id: "ubuntu", label: "Ubuntu" },
  { id: "win", label: "Windows" },
] as const;

/** A readable name for a VM's OS id, or "Unknown". */
export function vmOSLabel(os?: string): string {
  if (!os) return "Unknown";
  return VM_OS_OPTIONS.find((option) => option.id === os)?.label ?? os;
}

/** The logo for a VM's OS id; a plain screen when the OS is unknown. */
export function vmOSIcon(os?: string): string {
  if (!os) return "mdi:monitor";
  if (os.startsWith("win")) return "mdi:microsoft-windows";
  return VM_OS_ICONS[os] ?? getDistroIcon(os);
}
