import type { TaskSnapshot } from "./generated/linuxio-types";
import { openTaskWatchStream } from "./linuxio";
import { LinuxIOError, request } from "./linuxio-core";
import { waitForStreamResult } from "./stream-helpers";
import { isTerminalTaskState } from "./task-state";

export { isTerminalTaskState };

export function isTaskSnapshot(value: unknown): value is TaskSnapshot {
  return (
    !!value &&
    typeof value === "object" &&
    "id" in value &&
    "state" in value &&
    "created_at" in value
  );
}

export function taskSnapshotResult<T>(value: T | TaskSnapshot): T {
  if (isTaskSnapshot(value)) {
    return value.result as T;
  }
  return value;
}

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

const TASK_POLL_INTERVAL_MS = 1_000;

/**
 * Poll `tasks.get` until the Task reaches a terminal state. Fallback for when
 * the watch stream cannot be opened (mux dropped between Task start and
 * watch); the connection_closed retry policy re-initializes the mux and
 * waits, so this resolves at actual completion instead of failing fast.
 */
async function pollTaskUntilTerminal(taskId: string): Promise<TaskSnapshot> {
  for (;;) {
    const snapshot = await request<TaskSnapshot>(
      "tasks",
      "get",
      { taskId },
      { retryPolicy: "connection_closed" },
    );
    if (isTerminalTaskState(snapshot.state)) {
      return snapshot;
    }
    await new Promise((resolve) => setTimeout(resolve, TASK_POLL_INTERVAL_MS));
  }
}

/**
 * Return a completed snapshot, or throw the Task's structured error
 * (message + code) for a failed/canceled one.
 */
export function terminalSnapshotOrThrow(snapshot: TaskSnapshot): TaskSnapshot {
  if (snapshot.state === "completed") return snapshot;
  throw new LinuxIOError(
    snapshot.error?.message ?? "Task failed",
    snapshot.error?.code,
  );
}

export async function waitForTaskCompletion(
  snapshot: TaskSnapshot,
): Promise<TaskSnapshot> {
  if (isTerminalTaskState(snapshot.state)) {
    return terminalSnapshotOrThrow(snapshot);
  }

  const watch = openTaskWatchStream(snapshot.id);
  if (!watch) {
    // Unlike useTaskStreamAction (which promises live progress and fails fast
    // when it cannot watch), a plain Task action promises completion — so a
    // missed watch falls back to polling rather than resolving mid-Task with
    // an undefined result.
    return terminalSnapshotOrThrow(await pollTaskUntilTerminal(snapshot.id));
  }

  const result = await waitForStreamResult(watch, {
    closeMessage: "Task watch closed before completion",
  });

  try {
    return await request<TaskSnapshot>(
      "tasks",
      "get",
      { taskId: snapshot.id },
      {
        retryPolicy: "connection_closed",
      },
    );
  } catch {
    const now = new Date().toISOString();
    return {
      ...snapshot,
      state: "completed",
      result,
      updated_at: now,
      finished_at: now,
    };
  }
}
