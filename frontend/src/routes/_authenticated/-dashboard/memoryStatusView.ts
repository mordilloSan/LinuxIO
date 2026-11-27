import type {
  SwapDrainBlockedReason,
  SwapFileCreateReason,
  SwapFileLockedReason,
  SwapFileResizeReason,
  SwapFileRow,
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
  /** A plain note under the description: what else choosing the row does. */
  note?: string;
  recommended: boolean;
  /**
   * Warning-coloured note on the current zswap or disk row while the next
   * start would not bring the mode back: Apply is then for this row too.
   */
  unsaved?: string;
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

const MIB = 1024 ** 2;

/** The smallest swap file, should the status not say it. */
const SMALLEST_SWAP_FILE = 256 * MIB;

export const ROW_MODES: readonly RowMode[] = ["zswap", "zram", "disk"];

export const isRowMode = (mode: SwapMode): mode is RowMode =>
  ROW_MODES.includes(mode as RowMode);

export const rowName = (mode: RowMode): string =>
  ({
    disk: "Disk only",
    zram: "Compressed memory only",
    zswap: "Compressed memory with the disk as backup",
  })[mode];

/** "a", "a and b", "a, b and c". */
const listOf = (names: string[]) =>
  names.length < 2
    ? (names[0] ?? "")
    : `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;

/** The folder a path is in. */
const folderOf = (path: string) => path.slice(0, path.lastIndexOf("/")) || "/";

/** Bytes parked on disk swap: zram holds its data in memory. */
const parkedBytes = (status: SwapStatus) =>
  status.devices.reduce(
    (sum, device) => (device.kind === "zram" ? sum : sum + device.used),
    0,
  );

const canChooseZram = (status: SwapStatus) =>
  status.zram_unavailable_reason === "";

/** The swap files that block snapshots: a file does only while it swaps. */
const snapshotBlockers = (status: SwapStatus) =>
  status.swap_files.filter((row) => row.active && row.blocks_snapshots);

/**
 * Whether a switch to compressed memory takes every blocking file away: it
 * removes only the files LinuxIO acts on.
 */
const zramUnblocksSnapshots = (blocking: SwapFileRow[]) =>
  blocking.length > 0 && blocking.every((row) => row.locked_reason === "");

/** The swap files LinuxIO resizes and removes, and a switch to zram removes. */
export const removableFiles = (status: SwapStatus) =>
  status.swap_files.filter((row) => row.locked_reason === "");

/**
 * The swap that stays on beside compressed memory, since LinuxIO never turns
 * it off: an active swap partition, or an active swap file it does not act on.
 */
const swapThatStays = (status: SwapStatus) => [
  ...status.devices
    .filter((device) => device.kind === "partition")
    .map((device) => device.path),
  ...status.swap_files
    .filter((row) => row.active && row.locked_reason !== "")
    .map((row) => row.path),
];

const staysBesideZram = (status: SwapStatus) => {
  const stays = swapThatStays(status);
  if (stays.length === 0) return undefined;
  return stays.length === 1
    ? `${stays[0]} stays on and takes what compressed memory cannot hold.`
    : `${listOf(stays)} stay on and take what compressed memory cannot hold.`;
};

/**
 * Why the parked data cannot be brought back. For other_swap it names the
 * devices that hold data and that LinuxIO does not turn off.
 */
const drainBlockedText = (
  status: SwapStatus,
  reason: SwapDrainBlockedReason,
): string | undefined => {
  switch (reason) {
    case "not_enough_memory":
      return "Not enough free memory right now";
    case "other_swap": {
      const removable = new Set(removableFiles(status).map((row) => row.path));
      const holders = status.devices
        .filter(
          (device) =>
            device.kind !== "zram" &&
            device.used > 0 &&
            !removable.has(device.path),
        )
        .map((device) => device.path);
      return holders.length > 0
        ? `The data is on ${listOf(holders)}, which LinuxIO does not turn off, so it stays there`
        : "The data is on swap that LinuxIO does not turn off, so it stays there";
    }
    default:
      return undefined;
  }
};

export function bannerView(status: SwapStatus): BannerView {
  if (status.mode === "none") {
    return {
      action:
        status.zram_unavailable_reason === "package_missing"
          ? { kind: "capabilities" }
          : { kind: "enable_zram" },
      severity: "warning",
      text: "If memory runs out, Linux will close programs.",
    };
  }
  if (status.mode === "mixed") {
    return {
      action: { kind: "none" },
      severity: "warning",
      text: "Compressed memory and a swap file are both active. Choose one below and apply to finish.",
    };
  }
  const parked = parkedBytes(status);
  if (parked > 0) {
    return {
      action: {
        blockedReason: status.can_drain
          ? undefined
          : drainBlockedText(status, status.drain_blocked_reason),
        canDrain: status.can_drain,
        kind: "drain",
      },
      severity: "info",
      text: `${formatFileSize(parked)} of memory is parked on the system drive. Programs that need it are slower until it comes back.`,
    };
  }
  const blocking = snapshotBlockers(status);
  if (blocking.length > 0) {
    return {
      // Offered only when the switch would take every blocking file away.
      action:
        canChooseZram(status) && zramUnblocksSnapshots(blocking)
          ? { kind: "switch_zram" }
          : { kind: "none" },
      severity: "warning",
      text:
        blocking.length === 1
          ? `${blocking[0].path} blocks snapshots of the subvolume it is in.`
          : `${listOf(blocking.map((row) => row.path))} block snapshots of the subvolumes they are in.`,
    };
  }
  return {
    action: { kind: "none" },
    severity: "success",
    text: "Memory is fine. Nothing is parked.",
  };
}

/** Why LinuxIO cannot create its swap file, as the end of a sentence. */
const createBlockedCause = (status: SwapStatus): string | undefined => {
  const create = status.create;
  const folder = folderOf(create.path);
  const causes: Record<Exclude<SwapFileCreateReason, "">, string> = {
    exists: `${create.path} is already there`,
    unsupported_filesystem: create.fs_type
      ? `${folder} is on ${create.fs_type}, and LinuxIO makes swap files only on ext4, xfs and btrfs`
      : `LinuxIO makes swap files only on ext4, xfs and btrfs`,
    read_only: `the filesystem of ${folder} is read-only`,
    btrfs_layout: `the btrfs filesystem of ${folder} has more than one device or data that is not single`,
    not_enough_space: `the disk has ${formatFileSize(create.free_space)} free, and a swap file of at least ${formatFileSize(minSize(status))} must still leave ${formatFileSize(create.reserve)} free`,
    folder_not_root_only: `a folder on ${create.path} is a link or can be changed by users other than root`,
    path_taken: `something other than its swap file is in ${folder}`,
    hibernation: `the host hibernates to a swap file on the disk ${folder} is on, where LinuxIO changes no swap file`,
  };
  return create.blocked_reason === ""
    ? undefined
    : (causes[create.blocked_reason] ?? "LinuxIO cannot use its folder");
};

/** How the host stands for a mode that needs disk swap. */
type DiskSwap =
  | { kind: "active" }
  /** fstab names a swap file that is off, which LinuxIO does not turn on. */
  | { kind: "off"; row: SwapFileRow }
  /** None: the change creates LinuxIO's swap file. */
  | { kind: "create" }
  | { kind: "blocked"; cause: string };

const diskSwap = (status: SwapStatus): DiskSwap => {
  if (status.devices.some((device) => device.kind !== "zram")) {
    return { kind: "active" };
  }
  const off = status.swap_files.find((row) => row.in_fstab);
  if (off) return { kind: "off", row: off };
  const cause = createBlockedCause(status);
  return cause ? { kind: "blocked", cause } : { kind: "create" };
};

const needsDiskSwapReason = (state: DiskSwap): string | undefined => {
  switch (state.kind) {
    case "off":
      if (state.row.resize_blocked_reason === "missing") {
        return state.row.locked_reason === ""
          ? `/etc/fstab names ${state.row.path}, and nothing is at its path. Remove it under Swap files to choose this.`
          : `/etc/fstab names ${state.row.path}, and nothing is at its path.`;
      }
      return state.row.locked_reason === ""
        ? `${state.row.path} is in /etc/fstab and off. LinuxIO does not turn it on: remove it under Swap files to choose this.`
        : `${state.row.path} is in /etc/fstab and off, and LinuxIO does not turn it on.`;
    case "blocked":
      return `Needs a swap file, and LinuxIO cannot create one: ${state.cause}.`;
    default:
      return undefined;
  }
};

// The mode is read from what the server does now. zswap switched by hand, or
// a file of LinuxIO's that was removed, leaves a mode nothing brings back.
const UNSAVED_TEXT = "On now, but not set for the next start.";
const UNSAVED_APPLY_TEXT = "Apply to keep it after a restart.";

export function rowViews(status: SwapStatus): RowView[] {
  const disk = diskSwap(status);
  const planned = formatFileSize(status.zram_planned_size);
  const memoryUsed = formatFileSize(Math.round(status.zram_planned_size / 3));
  // With zram-generator, a device another of its files defines comes back at
  // every start, so the backend refuses to leave zram while there is one.
  const generatorFile =
    status.zram_provider === "zram-generator"
      ? status.zram_generator_devices[0]?.path
      : undefined;
  const zramStays = generatorFile
    ? `${generatorFile} turns compressed memory on at every start. LinuxIO does not change that file. ${generatorTurnOff(generatorFile)}`
    : undefined;
  const needsDiskSwap = needsDiskSwapReason(disk);
  let zswapDisabled: string | undefined;
  if (needsDiskSwap) {
    zswapDisabled = needsDiskSwap;
  } else if (!status.zswap.available) {
    zswapDisabled = "This kernel has no zswap";
  } else {
    zswapDisabled = zramStays;
  }
  // Said only on a row that can be chosen.
  const createsFile = (disabled: string | undefined) =>
    disk.kind === "create" && !disabled
      ? `Creates the swap file ${status.create.path} first, at a size you choose.`
      : undefined;
  const diskDisabled = needsDiskSwap ?? zramStays;
  // Only an active swap file on btrfs outside LinuxIO's folder blocks
  // snapshots. Compressed memory changes that only when it removes every
  // such file, so a locked one keeps the promise off its row.
  const blocking = snapshotBlockers(status);
  const blocksSnapshots = blocking.length > 0;
  const base = (mode: RowMode) => ({
    current: status.mode === mode,
    mode,
    name: rowName(mode),
    recommended: status.recommended_mode === mode,
  });
  // Apply is named only on a row that can be applied.
  const unsaved = (mode: RowMode, disabled: string | undefined) => {
    if (status.mode !== mode || status.mode_saved_for_boot) return undefined;
    return disabled ? UNSAVED_TEXT : `${UNSAVED_TEXT} ${UNSAVED_APPLY_TEXT}`;
  };
  return [
    {
      ...base("zswap"),
      capabilitiesLink: false,
      description:
        "Squeezes rarely used data to about a third of its size and keeps it in memory. When the compressed area is full the oldest data spills to the swap file.",
      disabledReason: zswapDisabled,
      note: createsFile(zswapDisabled),
      technical: "zswap",
      unsaved: unsaved("zswap", zswapDisabled),
    },
    {
      ...base("zram"),
      capabilitiesLink: status.zram_unavailable_reason === "package_missing",
      caution:
        "No backup. If it fills up, Linux closes the hungriest program, and a VM would stop like a power cut.",
      description: `The same, with no swap file. ${zramUnblocksSnapshots(blocking) ? "Writes nothing to disk and snapshots of the system drive work." : "Writes nothing to disk."} Holds about ${planned} using about ${memoryUsed} of memory.`,
      note: staysBesideZram(status),
      // The tool that would set zram up, since each writes its own file.
      technical:
        status.zram_provider === "" ? "zram" : `zram (${status.zram_provider})`,
    },
    {
      ...base("disk"),
      capabilitiesLink: false,
      caution: blocksSnapshots
        ? "Blocks snapshots of the system drive while in use."
        : undefined,
      description:
        "Rarely used data goes straight to the swap file on the system drive. Slowest.",
      disabledReason: diskDisabled,
      note: createsFile(diskDisabled),
      technical: "swap file",
      unsaved: unsaved("disk", diskDisabled),
    },
  ];
}

/** The current mode when it is a row, otherwise the recommended row. */
export function initialSelection(status: SwapStatus): RowMode {
  if (isRowMode(status.mode)) return status.mode;
  return isRowMode(status.recommended_mode) ? status.recommended_mode : "zram";
}

/**
 * Apply is for a different row, for finishing a switch left mixed, or for
 * the current row while the next start would not bring it back.
 */
export function canApply(status: SwapStatus, selected: RowMode): boolean {
  const row = rowViews(status).find((view) => view.mode === selected);
  if (!row || row.disabledReason || row.capabilitiesLink) return false;
  return (
    status.mode === "mixed" ||
    selected !== status.mode ||
    !status.mode_saved_for_boot
  );
}

/** A size field's limits and starting value, in whole MiB. */
export interface SizeLimits {
  max: number;
  min: number;
  start: number;
}

const minSize = (status: SwapStatus) =>
  status.create.min_size || SMALLEST_SWAP_FILE;

const limits = (
  status: SwapStatus,
  maxBytes: number,
  startBytes: number,
): SizeLimits => {
  const min = Math.ceil(minSize(status) / MIB);
  const max = Math.floor(maxBytes / MIB);
  return {
    max,
    min,
    start: Math.min(Math.max(Math.round(startBytes / MIB), min), max),
  };
};

/** The size field of Create: up to what the disk allows, from the default. */
export const createLimits = (status: SwapStatus): SizeLimits =>
  limits(status, status.create.max_size, status.create.default_size);

/**
 * The size field of a resize: up to what the disk allows for the file, from
 * its current size. A file that swaps reports one page less than its length,
 * so the current size is rounded to whole MiB.
 */
export const resizeLimits = (
  status: SwapStatus,
  row: SwapFileRow,
): SizeLimits => {
  const field = limits(status, row.max_size, row.size);
  return { ...field, start: Math.round(row.size / MIB) };
};

export interface SizeCheck {
  /** The size in bytes when the text is one the request may carry. */
  bytes?: number;
  /** What is wrong with the text; absent when it is fine. */
  error?: string;
  /** What the field accepts, shown under it while nothing is wrong. */
  help: string;
}

/** What the size field holds, read against its limits. */
export function checkSize(text: string, field: SizeLimits): SizeCheck {
  if (field.max < field.min) {
    return {
      error: `The disk has no room for a swap file of ${field.min} MiB.`,
      help: "",
    };
  }
  const help = `From ${field.min} to ${field.max} MiB. ${field.max} MiB is the largest size the disk allows.`;
  const trimmed = text.trim();
  if (!/^\d+$/.test(trimmed)) {
    return { error: "Enter a whole number of MiB.", help };
  }
  const mib = Number(trimmed);
  if (mib < field.min) return { error: `At least ${field.min} MiB.`, help };
  if (mib > field.max) {
    return {
      error: `At most ${field.max} MiB, the largest size the disk allows.`,
      help,
    };
  }
  return { bytes: mib * MIB, help };
}

const LOCKED_TEXT: Record<Exclude<SwapFileLockedReason, "">, string> = {
  not_in_fstab:
    "something other than /etc/fstab turns it on, so LinuxIO cannot keep it off.",
  not_root_only: "another user could put a file of their own at this name.",
  hibernation: "changing this file would break hibernation.",
};

/** What a file that cannot be resized reads, for the reasons that are not a state. */
const resizeBlockedText = (
  row: SwapFileRow,
  reason: SwapFileResizeReason,
): string | undefined => {
  switch (reason) {
    case "unsupported_filesystem":
      return row.fs_type
        ? `It cannot be resized: it is on ${row.fs_type}, and LinuxIO resizes swap files only on ext4, xfs and btrfs.`
        : "It cannot be resized: LinuxIO resizes swap files only on ext4, xfs and btrfs.";
    case "read_only":
      return "It cannot be resized: its filesystem is read-only.";
    case "btrfs_layout":
      return "It cannot be resized: its btrfs filesystem has more than one device or data that is not single.";
    default:
      return undefined;
  }
};

/** The size a row shows: a missing file has none. */
const sizeText = (row: SwapFileRow) =>
  row.resize_blocked_reason === "missing"
    ? "missing"
    : formatFileSize(row.size);

const usedText = (size: number, used: number) =>
  `${formatFileSize(size)}, ${used > 0 ? `${formatFileSize(used)} used` : "none used"}`;

/** One swap file under "Swap files", with what may be done to it. */
export interface SwapFileView {
  /** Offered when the file can be moved into LinuxIO's folder. */
  move: boolean;
  /** Lines under the path: what the file is and why an action is missing. */
  notes: string[];
  path: string;
  remove: boolean;
  /** Resize is offered. */
  resize: boolean;
  /** The file as the status reports it, which its steps are built from. */
  row: SwapFileRow;
  /** The btrfs line: the file blocks snapshots. */
  snapshots?: string;
  /** Its size and use, or the state it is in. */
  summary: string;
}

export function swapFileViews(status: SwapStatus): SwapFileView[] {
  return status.swap_files.map((row) => {
    const locked = row.locked_reason !== "";
    const reason = row.resize_blocked_reason;
    const notes: string[] = [];
    let summary: string;
    if (row.active) {
      summary = usedText(row.size, row.used);
    } else if (reason === "missing") {
      summary = "Missing";
    } else if (reason === "no_swap_header") {
      summary = `Not a swap file, ${formatFileSize(row.size)}`;
    } else {
      summary = `Off, ${formatFileSize(row.size)}`;
    }
    if (row.temporary) {
      notes.push(
        "A temporary swap file a resize left on. Remove turns it off and deletes it.",
      );
    }
    if (reason === "missing") {
      notes.push(
        "/etc/fstab names it, and nothing is at its path. Remove takes its line out of /etc/fstab.",
      );
    } else if (reason === "no_swap_header") {
      notes.push("It is off and holds no swap signature.");
    } else if (!row.active && row.in_fstab) {
      notes.push("/etc/fstab names it, and LinuxIO does not turn it on.");
    }
    if (locked) {
      const cause = LOCKED_TEXT[row.locked_reason as keyof typeof LOCKED_TEXT];
      notes.push(
        cause
          ? `LinuxIO does not resize or remove it: ${cause}`
          : "LinuxIO does not resize or remove it.",
      );
    } else if (!row.temporary) {
      const blocked = resizeBlockedText(row, reason);
      if (blocked) notes.push(blocked);
    }
    const snapshots =
      row.active && row.blocks_snapshots
        ? `${row.path} blocks snapshots of the subvolume it is in.${row.can_move ? " LinuxIO can make it again in a subvolume of its own, at the same size." : ""}`
        : undefined;
    return {
      move: !locked && row.can_move === true,
      notes,
      path: row.path,
      remove: !locked,
      resize: !locked && !row.temporary && reason === "",
      row,
      snapshots,
      summary,
    };
  });
}

/** A swap device that is not a swap file, listed with no actions. */
export interface OtherSwapView {
  kind: string;
  path: string;
  summary: string;
}

export function otherSwapViews(status: SwapStatus): OtherSwapView[] {
  return status.devices
    .filter((device) => device.kind !== "file")
    .map((device) => ({
      kind: device.kind === "zram" ? "Compressed memory" : "Partition",
      path: device.path,
      summary: usedText(device.size, device.used),
    }));
}

/** "Create a swap file" when LinuxIO can make one, or why it cannot. */
export type CreateView =
  | { kind: "offer"; size: SizeLimits }
  | { kind: "blocked"; reason: string }
  /** LinuxIO's swap file is there already, as a row. */
  | { kind: "none" };

export function createView(status: SwapStatus): CreateView {
  if (status.create.blocked_reason === "exists") return { kind: "none" };
  const cause = createBlockedCause(status);
  return cause
    ? {
        kind: "blocked",
        reason: `LinuxIO cannot create a swap file: ${cause}.`,
      }
    : { kind: "offer", size: createLimits(status) };
}

/** A step of the dialog that asks before a change, or sets its size. */
export interface StepView {
  /** The label of the button that starts the change. */
  action: string;
  /** What the change will do. */
  items: string[];
  /** The size field, when the change makes or resizes a swap file. */
  size?: SizeLimits;
  /** A resize to this many MiB changes nothing, so it is not offered. */
  sameSize?: number;
  title: string;
}

/** The step that asks before a change of mode, and what it sends. */
export interface ConfirmView extends StepView {
  /** The swap files the change removes, sent as confirm_remove. */
  remove?: string[];
  /** The configuration file the request agrees may be replaced. */
  replace?: string;
}

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
 * What a change to target needs confirmed or sized first, in one step;
 * undefined when it applies at once. A switch to zram removes every swap
 * file LinuxIO acts on and names each; a switch to disk or zswap on a host
 * with no disk swap creates LinuxIO's swap file at a size the user sets.
 */
export function confirmView(
  status: SwapStatus,
  target: RowMode,
): ConfirmView | undefined {
  const config = replacedConfig(status, target);
  const zram = target === "zram";
  const removed = zram ? removableFiles(status) : [];
  const creates = !zram && diskSwap(status).kind === "create";
  const items: string[] = [];
  if (zram && (removed.length > 0 || config)) {
    items.push("Compressed memory is turned on.");
  }
  for (const row of removed) {
    items.push(
      row.resize_blocked_reason === "missing"
        ? `Nothing is at ${row.path}. Its line is taken out of /etc/fstab.`
        : `${row.path}, ${sizeText(row)}, is removed.`,
    );
  }
  if (removed.some((row) => row.active)) {
    items.push(
      "Data parked in a swap file is brought back to memory before the file is turned off and deleted.",
    );
  }
  if (removed.length > 0) {
    const stays = staysBesideZram(status);
    if (stays) items.push(stays);
  }
  if (creates) {
    items.push(
      `LinuxIO creates the swap file ${status.create.path}, adds it to /etc/fstab and turns it on.`,
    );
  }
  if (config) {
    items.push(
      `${config.path} holds zram settings that are not LinuxIO's. It is replaced, and a copy is kept as ${config.copy_path}.`,
    );
  }
  if (items.length === 0) return undefined;
  return {
    action: zram ? "Turn on compressed memory" : "Apply",
    items,
    remove: removed.length > 0 ? removed.map((row) => row.path) : undefined,
    replace: config?.path,
    size: creates ? createLimits(status) : undefined,
    title: zram
      ? "Switch to compressed memory?"
      : `Switch to ${rowName(target).toLowerCase()}?`,
  };
}

/** The step that sets the size of LinuxIO's new swap file. */
export const createStepView = (status: SwapStatus): StepView => ({
  action: "Create",
  items: [
    `LinuxIO creates the swap file ${status.create.path}, adds it to /etc/fstab and turns it on.`,
  ],
  size: createLimits(status),
  title: "Create a swap file",
});

/** The step that sets a swap file's new size. */
export const resizeStepView = (
  status: SwapStatus,
  row: SwapFileRow,
): StepView => {
  const size = resizeLimits(status, row);
  return {
    action: "Resize",
    items: [
      `${row.path} is ${formatFileSize(row.size)}.`,
      "To change its size, LinuxIO turns the file off and on again. Data parked in it is brought back to memory first.",
    ],
    sameSize: size.start,
    size,
    title: `Resize ${row.path}`,
  };
};

/** Whether row is the only swap the host has on, zram included. */
const lastActiveSwap = (status: SwapStatus, row: SwapFileRow) =>
  row.active && status.devices.every((device) => device.path === row.path);

/** The step that asks before a swap file is removed. */
export const removeStepView = (
  status: SwapStatus,
  row: SwapFileRow,
): StepView => {
  const items: string[] = [];
  if (row.temporary) {
    items.push(
      `The temporary swap file ${row.path}, ${sizeText(row)}, is turned off and deleted.`,
    );
  } else if (row.resize_blocked_reason === "missing") {
    items.push(
      `Nothing is at ${row.path}. Its line is taken out of /etc/fstab.`,
    );
  } else if (row.active) {
    items.push(
      `${row.path}, ${sizeText(row)}, is turned off, taken out of /etc/fstab and deleted.`,
    );
  } else {
    items.push(
      `${row.path}, ${sizeText(row)}, is taken out of /etc/fstab and deleted.`,
    );
  }
  if (row.active) {
    items.push("Data parked in it is brought back to memory first.");
  }
  if (lastActiveSwap(status, row)) {
    items.push(
      "The host then has no swap. If memory runs out, Linux will close programs.",
    );
  }
  return { action: "Remove", items, title: `Remove ${row.path}?` };
};

/** The step that asks before a swap file is made again in LinuxIO's folder. */
export const moveStepView = (
  status: SwapStatus,
  row: SwapFileRow,
): StepView => ({
  action: "Move",
  items: [
    `LinuxIO creates the swap file ${status.create.path}, ${formatFileSize(row.size)}, in a subvolume of its own, adds it to /etc/fstab and turns it on.`,
    row.active
      ? `Then ${row.path} is turned off, taken out of /etc/fstab and deleted. Data parked in it is brought back to memory first.`
      : `Then ${row.path} is taken out of /etc/fstab and deleted.`,
  ],
  title: `Move ${row.path}?`,
});

/** What a step does to a swap file, or to LinuxIO's new one. */
export type FileStepKind = "create" | "resize" | "remove" | "move";

/** A step built from the status as it is now. */
export interface FileStepView extends StepView {
  /** The step can no longer be taken: its file changed meanwhile. */
  changed?: boolean;
}

/**
 * The step of a swap file action, built from the status as it is now, so its
 * limits and warnings follow the host while it is open. When the file is
 * gone, or no longer offers the action, the step says so and cannot start.
 */
export function fileStepView(
  status: SwapStatus,
  kind: FileStepKind,
  path: string,
): FileStepView {
  if (kind === "create") {
    return createView(status).kind === "offer"
      ? createStepView(status)
      : {
          action: "Create",
          changed: true,
          items: [
            "LinuxIO can no longer create a swap file here, so go back to see why.",
          ],
          title: "Create a swap file",
        };
  }
  const file = swapFileViews(status).find((view) => view.path === path);
  if (file?.[kind]) {
    const build = {
      move: moveStepView,
      remove: removeStepView,
      resize: resizeStepView,
    }[kind];
    return build(status, file.row);
  }
  return {
    action: { move: "Move", remove: "Remove", resize: "Resize" }[kind],
    changed: true,
    items: [
      `${path} changed while this was open, so go back to see what can be done with it now.`,
    ],
    title: {
      move: `Move ${path}?`,
      remove: `Remove ${path}?`,
      resize: `Resize ${path}`,
    }[kind],
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
