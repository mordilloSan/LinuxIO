import type {
  SwapLeft,
  SwapMode,
  SwapModeResult,
  SwapProgress,
  TaskProgress,
} from "@/api";
import { MEMORY_OVERFLOW_DIALOG } from "@/api/dashboard-search";

// What a memory overflow change reports when it ends. The dialog that
// follows the job and the task tray, which reports it when no dialog does,
// say the same.

// A toast fired on another page opens the Memory overflow dialog by its URL.
export const MEMORY_TOAST_META = {
  label: "Open Memory overflow",
  search: { dialog: MEMORY_OVERFLOW_DIALOG },
  to: "/",
} as const;
export const MEMORY_MODE_APPLIED = "Memory overflow setting applied";
export const MEMORY_MODE_FAILED = "Failed to change how memory runs out";
export const MEMORY_DRAINED = "Parked data brought back to memory";
export const MEMORY_DRAIN_FAILED = "Failed to bring parked data back to memory";

/** The four swap file jobs, by the action their task metadata carries. */
export type SwapFileAction = "create" | "resize" | "remove" | "move";

/**
 * What a swap file job reports when it ends, success then failure. A job
 * taken up after a reload may not know its path, and then says "the swap
 * file".
 */
export const swapFileDone = (
  action: SwapFileAction,
  path: string | undefined,
): { failed: string; succeeded: string } => {
  const file = path || "the swap file";
  switch (action) {
    case "create":
      return {
        failed: "Failed to create the swap file",
        succeeded: "Swap file created",
      };
    case "resize":
      return {
        failed: `Failed to resize ${file}`,
        succeeded: `Resized ${file}`,
      };
    case "remove":
      return {
        failed: `Failed to remove ${file}`,
        succeeded: `Removed ${file}`,
      };
    case "move":
      return {
        failed: `Failed to move ${file}`,
        succeeded: `Moved ${file} into LinuxIO's folder`,
      };
  }
};

/** What the task tray calls a swap file job before its first step. */
export const swapFileTaskLabel = (
  action: SwapFileAction,
  path: string | undefined,
): string => {
  const file = path || "a swap file";
  switch (action) {
    case "create":
      return "Creating a swap file";
    case "resize":
      return `Resizing ${file}`;
    case "remove":
      return `Removing ${file}`;
    case "move":
      return `Moving ${file}`;
  }
};

/**
 * The running step of a memory change, naming the swap file it acts on for
 * the steps that act on one; otherwise the backend's message. undefined
 * before the first step.
 */
export const swapProgressText = (
  progress: Pick<TaskProgress, "detail" | "message"> | null | undefined,
): string | undefined => {
  const detail = progress?.detail as Partial<SwapProgress> | undefined;
  const device = detail?.device;
  if (device) {
    switch (detail?.step) {
      case "create_swap_file":
        return `Creating the swap file ${device}`;
      case "make_temporary_swap":
        return `Adding the temporary swap file ${device}`;
      case "resize_swap_file":
        return `Resizing ${device}`;
      case "remove_temporary_swap":
        return `Removing the temporary swap file ${device}`;
      case "remove_swap_file":
        return `Removing ${device}`;
    }
  }
  return progress?.message || undefined;
};

const LATER = "Apply again when more memory is free.";

/** What a mode change left as it was and why, keyed by `what:reason`. */
const leftText = (left: SwapLeft): string | undefined => {
  const file = left.path || "the swap file";
  switch (`${left.what}:${left.reason}`) {
    case "swap_file:not_enough_memory":
      return `Not enough free memory to bring back the data in ${file}, so it stays on. ${LATER}`;
    case "swap_file:zram_not_swapping":
      return `Compressed memory is not running, so ${file} stays on.`;
    case "zram:not_enough_memory":
      return `Not enough free memory to empty compressed memory, so it stays on. ${LATER}`;
    case "zram:not_managed":
      return "Compressed memory is still on: it is not run by a zram tool that LinuxIO manages.";
    case "zram_size:not_enough_memory":
      return `Compressed memory keeps its current size: not enough free memory to restart it. ${LATER}`;
    case "zram_size:not_managed":
      return "Compressed memory keeps its current size: it is not run by a zram tool that LinuxIO manages.";
    case "zram_size:other_size":
      return "Compressed memory came up at another size than planned. Check the zram tool's configuration.";
    case "zram_size:no_other_swap":
      return "Compressed memory keeps its current size until the next restart: restarting it now would leave the host without swap.";
    default:
      return undefined;
  }
};

/**
 * The warning for a mode change that left something as it was, or null when
 * the host reached the mode in full.
 */
export const modeResultWarning = (
  result: SwapModeResult,
  requested: SwapMode,
): string | null => {
  if (result.left.length > 0) {
    return result.left
      .map((left) => leftText(left) ?? "Part of the change was not applied.")
      .join(" ");
  }
  return result.mode === requested
    ? null
    : "The host did not reach this setting. Apply again.";
};
