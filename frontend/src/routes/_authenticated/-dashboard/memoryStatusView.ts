import type { SwapDrainBlockedReason, SwapMode, SwapStatus } from "@/api";
import { formatFileSize } from "@/utils/formatters";

/** The three choices the dialog offers; none and mixed only describe a host. */
export type RowMode = "zswap" | "zram" | "disk";

export interface RowView {
  /** Warning-coloured note under the description. */
  caution?: string;
  /** The zram row shows a Capabilities link where its radio would be. */
  capabilitiesLink: boolean;
  current: boolean;
  description: string;
  /** Why the row cannot be chosen; absent when it can. */
  disabledReason?: string;
  mode: RowMode;
  name: string;
  recommended: boolean;
  /** The technical name, shown once as a secondary caption. */
  technical: string;
}

export type BannerAction =
  | { kind: "none" }
  | { kind: "enable_zram" }
  | { kind: "capabilities" }
  | { kind: "switch_zram" }
  | { kind: "drain"; blockedReason: string | undefined; canDrain: boolean };

export interface BannerView {
  action: BannerAction;
  severity: "warning" | "info" | "success";
  text: string;
}

const NEEDS_SWAP_FILE = "Needs a swap file on the system drive";
const SWAP_FILE_NOT_STARTED =
  "LinuxIO will not turn on the swap file: users other than root can change its folder";

export const ROW_MODES: readonly RowMode[] = ["zswap", "zram", "disk"];

export const isRowMode = (mode: SwapMode): mode is RowMode =>
  ROW_MODES.includes(mode as RowMode);

export const rowName = (mode: RowMode): string =>
  ({
    disk: "Disk only",
    zram: "Compressed memory only",
    zswap: "Compressed memory with the disk as backup",
  })[mode];

const DRAIN_BLOCKED_TEXT: Partial<Record<SwapDrainBlockedReason, string>> = {
  not_enough_memory: "Not enough free memory right now",
  unit_masked:
    "This swap device is switched off for the next start, so LinuxIO will not empty it",
  others_can_write:
    "This swap device is in a folder that users other than root can change, so LinuxIO will not empty it",
  unmanaged_device:
    "This swap device is not in /etc/fstab, so LinuxIO cannot restart it",
};

/** Bytes parked on disk swap: zram holds its data in memory. */
const parkedBytes = (status: SwapStatus) =>
  status.devices.reduce(
    (sum, device) => (device.kind === "zram" ? sum : sum + device.used),
    0,
  );

const canChooseZram = (status: SwapStatus) =>
  status.zram_unavailable_reason === "";

/**
 * Disk swap a mode can count on, as the backend's plan counts it: a swap file
 * LinuxIO can turn on, or an active swap file or partition.
 */
const hasDiskSwap = (status: SwapStatus) =>
  status.swap_file?.managed === true ||
  status.devices.some((device) => device.kind !== "zram");

const UNMANAGED_CAUSE: Partial<
  Record<NonNullable<SwapStatus["swap_file"]>["unmanaged_reason"], string>
> = {
  not_in_fstab: ": /etc/fstab does not name it.",
  others_can_write: ": users other than root can change a folder on its path.",
};

/**
 * Why LinuxIO leaves the swap file as it is, said before anything is asked of
 * it; undefined for a swap file it manages. A reason it does not know gets no
 * cause.
 */
export function swapFileNote(status: SwapStatus): string | undefined {
  const file = status.swap_file;
  if (!file || file.unmanaged_reason === "") return undefined;
  const cause = UNMANAGED_CAUSE[file.unmanaged_reason] ?? ".";
  const left = file.active
    ? "LinuxIO will not turn it off or empty it, whatever is chosen here."
    : "LinuxIO will not turn it on, whatever is chosen here.";
  const remedy =
    file.unmanaged_reason === "others_can_write"
      ? " Move it to a folder only root can write."
      : "";
  return `LinuxIO does not manage the swap file ${file.path}${cause} ${left}${remedy}`;
}

export function bannerView(status: SwapStatus): BannerView {
  if (status.mode === "none") {
    let action: BannerAction = { kind: "enable_zram" };
    if (status.zram_unavailable_reason === "package_missing") {
      action = { kind: "capabilities" };
    } else if (status.zram_unavailable_reason === "swap_partition_active") {
      action = { kind: "none" };
    }
    return {
      action,
      severity: "warning",
      text: "If memory runs out, Linux will close programs.",
    };
  }
  if (status.mode === "mixed") {
    return {
      action: { kind: "none" },
      severity: "warning",
      text: "Compressed memory and the swap file are both active. Choose one below and apply to finish.",
    };
  }
  const parked = parkedBytes(status);
  if (parked > 0) {
    return {
      action: {
        blockedReason: status.can_drain
          ? undefined
          : DRAIN_BLOCKED_TEXT[status.drain_blocked_reason],
        canDrain: status.can_drain,
        kind: "drain",
      },
      severity: "info",
      text: `${formatFileSize(parked)} of memory is parked on the system drive. Programs that need it are slower until it comes back.`,
    };
  }
  if (status.swap_file?.active && status.swap_file.fs_type === "btrfs") {
    return {
      // Only a file /etc/fstab names can be stopped, so only then would the
      // switch remove it.
      action:
        canChooseZram(status) && status.swap_file.managed
          ? { kind: "switch_zram" }
          : { kind: "none" },
      severity: "warning",
      text: "The swap file blocks snapshots of the system drive.",
    };
  }
  return {
    action: { kind: "none" },
    severity: "success",
    text: "Memory is fine. Nothing is parked.",
  };
}

export function rowViews(status: SwapStatus): RowView[] {
  const diskSwap = hasDiskSwap(status);
  const planned = formatFileSize(status.zram_planned_size);
  const memoryUsed = formatFileSize(Math.round(status.zram_planned_size / 3));
  let zramDisabled: string | undefined;
  if (status.zram_unavailable_reason === "swap_partition_active") {
    zramDisabled =
      "A swap partition is active. LinuxIO only manages swap files.";
  }
  const needsSwapFile =
    status.swap_file?.unmanaged_reason === "others_can_write"
      ? SWAP_FILE_NOT_STARTED
      : NEEDS_SWAP_FILE;
  let zswapDisabled: string | undefined;
  if (!diskSwap) {
    zswapDisabled = needsSwapFile;
  } else if (!status.zswap.available) {
    zswapDisabled = "This kernel has no zswap";
  }
  const base = (mode: RowMode) => ({
    current: status.mode === mode,
    mode,
    name: rowName(mode),
    recommended: status.recommended_mode === mode,
  });
  return [
    {
      ...base("zswap"),
      capabilitiesLink: false,
      description:
        "Squeezes rarely used data to about a third of its size and keeps it in memory. When the compressed area is full the oldest data spills to the swap file.",
      disabledReason: zswapDisabled,
      technical: "zswap",
    },
    {
      ...base("zram"),
      capabilitiesLink: status.zram_unavailable_reason === "package_missing",
      caution:
        "No backup. If it fills up, Linux closes the hungriest program, and a VM would stop like a power cut.",
      description: `The same, with no swap file. Writes nothing to disk and snapshots of the system drive work. Holds about ${planned} using about ${memoryUsed} of memory.`,
      disabledReason: zramDisabled,
      // The tool that would set zram up, since each writes its own file.
      technical:
        status.zram_provider === "" ? "zram" : `zram (${status.zram_provider})`,
    },
    {
      ...base("disk"),
      capabilitiesLink: false,
      caution: "Blocks snapshots of the system drive while in use.",
      description:
        "Rarely used data goes straight to the swap file on the system drive. Slowest.",
      disabledReason: diskSwap ? undefined : needsSwapFile,
      technical: "swap file",
    },
  ];
}

/** The current mode when it is a row, otherwise the recommended row. */
export function initialSelection(status: SwapStatus): RowMode {
  if (isRowMode(status.mode)) return status.mode;
  return isRowMode(status.recommended_mode) ? status.recommended_mode : "zram";
}

/** Apply is for a different row, or for finishing a switch left mixed. */
export function canApply(status: SwapStatus, selected: RowMode): boolean {
  const row = rowViews(status).find((view) => view.mode === selected);
  if (!row || row.disabledReason || row.capabilitiesLink) return false;
  return status.mode === "mixed" || selected !== status.mode;
}

/**
 * Turning zram on while the swap file is active also stops the file, but only
 * a file /etc/fstab names has a unit LinuxIO can stop.
 */
export const needsSwapFileConfirm = (status: SwapStatus, target: RowMode) =>
  target === "zram" &&
  status.swap_file?.active === true &&
  status.swap_file.managed;

export const presetSwappiness = (status: SwapStatus, mode: RowMode) =>
  status.presets.find((preset) => preset.mode === mode)?.swappiness;
