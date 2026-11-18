import type { SwapLeft, SwapMode, SwapModeResult } from "@/api";

// What a memory overflow change reports when it ends. The dialog that
// follows the job and the task tray, which reports it when no dialog does,
// say the same.

export const MEMORY_TOAST_META = { label: "Open dashboard", to: "/" } as const;
export const MEMORY_MODE_APPLIED = "Memory overflow setting applied";
export const MEMORY_MODE_FAILED = "Failed to change how memory runs out";
export const MEMORY_DRAINED = "Parked data brought back to memory";
export const MEMORY_DRAIN_FAILED = "Failed to bring parked data back to memory";

const LATER = "Apply again when more memory is free.";

/** What a mode change left as it was and why, keyed by `what:reason`. */
const LEFT_TEXT: Partial<
  Record<`${SwapLeft["what"]}:${SwapLeft["reason"]}`, string>
> = {
  "swap_file:not_enough_memory": `Not enough free memory to bring back the data in the swap file, so it stays on. ${LATER}`,
  "swap_file:zram_not_swapping":
    "Compressed memory is not running, so the swap file stays on.",
  "swap_file:not_identified":
    "An active swap file could not be matched to the one in /etc/fstab, so LinuxIO left it as it is.",
  "swap_file:others_can_write":
    "The swap file is in a folder that users other than root can change, so LinuxIO left it on. Move it to a folder only root can write.",
  "swap_file:not_managed":
    "A swap file that LinuxIO does not manage is still on. LinuxIO manages only one swap file in /etc/fstab.",
  "zram:not_enough_memory": `Not enough free memory to empty compressed memory, so it stays on. ${LATER}`,
  "zram:not_managed":
    "Compressed memory is still on: it is not run by a zram tool that LinuxIO manages.",
  "zram_size:not_enough_memory": `Compressed memory keeps its current size: not enough free memory to restart it. ${LATER}`,
  "zram_size:not_managed":
    "Compressed memory keeps its current size: it is not run by a zram tool that LinuxIO manages.",
  "zram_size:other_size":
    "Compressed memory came up at another size than planned. Check the zram tool's configuration.",
  "zram_size:no_other_swap":
    "Compressed memory keeps its current size until the next restart: restarting it now would leave the host without swap.",
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
      .map(
        (left) =>
          LEFT_TEXT[`${left.what}:${left.reason}`] ??
          "Part of the change was not applied.",
      )
      .join(" ");
  }
  return result.mode === requested
    ? null
    : "The host did not reach this setting. Apply again.";
};
