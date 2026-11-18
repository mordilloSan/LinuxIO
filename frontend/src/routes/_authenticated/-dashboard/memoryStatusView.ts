import type {
  SwapDrainBlockedReason,
  SwapMode,
  SwapStatus,
  ZramGeneratorDevice,
} from "@/api";
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
  unit_held_off:
    "This swap device is switched off for the next start, so LinuxIO will not empty it",
  others_can_write:
    "This swap device is in a folder that users other than root can change, so LinuxIO will not empty it",
  unmanaged_device:
    "This swap device is not in /etc/fstab, or has no systemd unit LinuxIO can use, so LinuxIO cannot restart it",
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
  unit_unusable: ": it has no systemd unit LinuxIO can use.",
};

/**
 * Why LinuxIO leaves the swap file as it is, said before anything is asked of
 * it; undefined for a swap file it manages. A reason it does not know gets no
 * cause. A file without a systemd unit LinuxIO can use is left to the note on
 * its /etc/fstab entry (fstabEntryNotes), which names it as fstab does.
 */
export function swapFileNote(status: SwapStatus): string | undefined {
  const file = status.swap_file;
  if (
    !file ||
    file.unmanaged_reason === "" ||
    file.unmanaged_reason === "unit_unusable"
  )
    return undefined;
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

/**
 * Each /etc/fstab swap entry LinuxIO leaves alone whatever is chosen, and
 * why, said before anything is asked of it; an entry fstab repeats is said
 * once.
 */
export function fstabEntryNotes(status: SwapStatus): string[] {
  const notes = status.unmanaged_fstab_swaps.map(
    (entry) =>
      `LinuxIO does not manage the swap entry ${entry.source} in /etc/fstab${UNMANAGED_CAUSE[entry.reason] ?? "."} LinuxIO will not turn it on or off, empty it or keep it off after a restart, whatever is chosen here.`,
  );
  return [...new Set(notes)];
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
  // With zram-generator, a device another of its files defines comes back at
  // every start, so the backend refuses to leave zram while there is one.
  const generatorFile =
    status.zram_provider === "zram-generator"
      ? status.zram_generator_devices[0]?.path
      : undefined;
  const zramStays = generatorFile
    ? `${generatorFile} turns compressed memory on at every start. LinuxIO does not change that file. ${generatorTurnOff(generatorFile)}`
    : undefined;
  let zswapDisabled: string | undefined;
  if (!diskSwap) {
    zswapDisabled = needsSwapFile;
  } else if (!status.zswap.available) {
    zswapDisabled = "This kernel has no zswap";
  } else {
    zswapDisabled = zramStays;
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
      disabledReason: diskSwap ? zramStays : needsSwapFile,
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

/** The step that asks before a change, and what Apply then sends with it. */
export interface ConfirmView {
  /** The label of the button that applies the change. */
  action: string;
  /** What the change will do that needs the user's agreement. */
  items: string[];
  /** The configuration file the request agrees may be replaced. */
  replace?: string;
  title: string;
}

/**
 * Turning zram on while the swap file is active also stops the file, but only
 * a file /etc/fstab names has a unit LinuxIO can stop.
 */
const stopsSwapFile = (status: SwapStatus, target: RowMode) =>
  target === "zram" &&
  status.swap_file?.active === true &&
  status.swap_file.managed;

/**
 * The provider's configuration file when it is not LinuxIO's and the change
 * replaces it: on the way to zram with either tool, and with zram-generator
 * also on the way out, which writes a file with no device. zram-tools only
 * disables its service then.
 */
const replacedConfig = (status: SwapStatus, target: RowMode) => {
  const config = status.foreign_zram_config;
  if (!config) return undefined;
  return target === "zram" || status.zram_provider === "zram-generator"
    ? config
    : undefined;
};

/**
 * What a change to target needs confirmed first, in one step; undefined
 * when it applies at once.
 */
export function confirmView(
  status: SwapStatus,
  target: RowMode,
): ConfirmView | undefined {
  const config = replacedConfig(status, target);
  const items: string[] = [];
  if (stopsSwapFile(status, target)) {
    items.push(
      "Compressed memory is turned on.",
      "Data parked in the swap file is brought back to memory.",
      "The swap file is turned off and stays off after a restart.",
      "The file itself is not deleted.",
    );
  } else if (config && target === "zram") {
    items.push("Compressed memory is turned on.");
  }
  if (config) {
    items.push(
      `${config.path} holds zram settings that are not LinuxIO's. It is replaced, and a copy is kept as ${config.copy_path}.`,
    );
  }
  if (items.length === 0) return undefined;
  const zram = target === "zram";
  return {
    action: zram ? "Turn on compressed memory" : "Apply",
    items,
    replace: config?.path,
    title: zram
      ? "Switch to compressed memory?"
      : `Switch to ${rowName(target).toLowerCase()}?`,
  };
}

/** The folder where a link to /dev/null hides a package's drop-in. */
const ETC_DROP_INS = "/etc/systemd/zram-generator.conf.d";

/**
 * How to stop a zram-generator file from setting up its device, as the
 * backend's refusal says it: a package's drop-in under /usr is hidden by a
 * link to /dev/null of its name in /etc; any other is the admin's, or made at
 * runtime, to change.
 */
const generatorTurnOff = (path: string) => {
  const name = path.slice(path.lastIndexOf("/") + 1);
  return path.startsWith("/usr/") &&
    path.endsWith(`/zram-generator.conf.d/${name}`)
    ? `To turn it off, link ${ETC_DROP_INS}/${name} to /dev/null.`
    : "To choose this, remove that file or take the device out of it.";
};

/** The size settings a zram-generator file writes for its device. */
const sizeSettings = (device: ZramGeneratorDevice) =>
  [
    ["zram-size", device.zram_size],
    ["zram-fraction", device.zram_fraction],
    ["max-zram-size", device.max_zram_size],
  ]
    .filter(([, value]) => value)
    .map(([key, value]) => `${key} = ${value}`);

/**
 * The swap devices other zram-generator files define, with the size settings
 * each writes; undefined when there are none. Only with zram-generator as the
 * provider do they win over LinuxIO's own file.
 */
export function generatorFilesNote(
  status: SwapStatus,
): { files: string[]; text: string } | undefined {
  const devices = status.zram_generator_devices;
  if (devices.length === 0) return undefined;
  return {
    files: devices.map((device) => {
      const sizes = sizeSettings(device);
      return sizes.length > 0
        ? `${device.path} sets up ${device.device} with ${sizes.join(", ")}`
        : `${device.path} sets up ${device.device} and sets no size`;
    }),
    text:
      status.zram_provider === "zram-generator"
        ? "zram-generator also reads these files, and what they set wins over the settings LinuxIO writes:"
        : "zram-generator is installed too and sets up compressed memory from these files:",
  };
}

export const presetSwappiness = (status: SwapStatus, mode: RowMode) =>
  status.presets.find((preset) => preset.mode === mode)?.swappiness;
