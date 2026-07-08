import type {
  Stream,
  VMDeleteResult,
  VMPreflight,
  VirtualMachine,
} from "@/api";
import { getMutationErrorMessage } from "@/utils/mutations";

export type VMAction =
  | "start"
  | "shutdown"
  | "reboot"
  | "force_off"
  | "suspend"
  | "resume";

export type ConsoleSession = {
  stream: Stream | null;
  vm: VirtualMachine;
};

export const VM_TOAST = { href: "/vm", label: "Open VMs" };

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
export type VMDialogSourceType = "iso" | "imagePreset";
export type VMDialogImagePresetID = NonNullable<
  ReadyImagePreset["imagePresetId"]
>;
export type VMCreateMode = "iso" | "image";

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

export function normalizeFolderPath(path: string): string {
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

export function isMissingPathError(error: unknown): boolean {
  const message = getMutationErrorMessage(error, "").toLowerCase();
  return (
    message.includes("not found") ||
    message.includes("no such file") ||
    message.includes("does not exist")
  );
}
