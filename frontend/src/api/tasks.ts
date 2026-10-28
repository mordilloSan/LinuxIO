import { LinuxIOError } from "./errors";
import { isTerminalTaskState } from "./task-state";

export { isTerminalTaskState };

/** Status code the bridge publishes for a canceled Task. */
export const TASK_CANCELED_CODE = 499;

/**
 * True when a Task ended because it was canceled — by this page, the
 * navbar chip, or another session — rather than because it failed. Cancellation
 * arrives as an ordinary result-error frame, so every terminal-error surface has
 * to tell the two apart before reporting a failure.
 */
export function isTaskCancellationError(error: unknown): boolean {
  return (
    error instanceof LinuxIOError && Number(error.code) === TASK_CANCELED_CODE
  );
}
