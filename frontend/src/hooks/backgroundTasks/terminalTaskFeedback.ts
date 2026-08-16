import { toast } from "sonner";

import {
  CAPABILITIES,
  type CapabilityDef,
  type DockerContainerUpdateResult,
  type InstallCapabilityResult,
  type TaskSnapshot,
  LinuxIOError,
} from "@/api";
import * as TaskTypes from "@/constants/backgroundTaskTypes";
import { requestString } from "@/utils/backgroundTasks";
import { getMutationErrorMessage } from "@/utils/mutations";

/**
 * Terminal feedback for background tasks observed by the global handler
 * (useRecoveredTasks). Two delivery paths can report the same terminal event —
 * the watch stream and the task-events fallback — and a page that owns the task
 * may be painting the outcome inline at the same time. This module owns the
 * arbitration so per-type feedback lives in exactly one place:
 *
 * - `TERMINAL_TASK_FEEDBACK` maps task type -> outcome handlers. Adding feedback
 *   for a new type means adding one entry here.
 * - De-duplication across paths is owned by the plumbing (bounded, so a long
 *   session doesn't accrete one id per task forever).
 * - A mounted page that is actively tracking a task claims its type via
 *   `claimTerminalFeedback`; claims register and release synchronously so an
 *   outcome arriving after navigation falls back to the global handler.
 */

export interface TerminalFeedbackTask {
  id: string;
  type: string;
  metadata: Record<string, unknown>;
}

export type TerminalTaskOutcome =
  | { kind: "completed"; result: unknown }
  | { kind: "failed"; error: unknown }
  | { kind: "canceled"; error: unknown };

export interface TerminalFeedbackDeps {
  refreshCapabilities: () => Promise<unknown>;
}

export interface TerminalFeedbackEntry {
  onCompleted?: (
    task: TerminalFeedbackTask,
    result: unknown,
    deps: TerminalFeedbackDeps,
  ) => void;
  onFailed?: (
    task: TerminalFeedbackTask,
    error: unknown,
    deps: TerminalFeedbackDeps,
  ) => void;
  onCanceled?: (
    task: TerminalFeedbackTask,
    error: unknown,
    deps: TerminalFeedbackDeps,
  ) => void;
}

const EMITTED_TASK_RETENTION = 200;
const emittedTaskIds = new Set<string>();

/**
 * Record that terminal feedback for `taskId` has been surfaced. Returns true on
 * the first call for a task, false when it was already handled. Pages call this
 * the moment they paint their own terminal alert, so the global paths stay
 * silent whichever stream's frame lands first; `emitTerminalTaskFeedback` uses
 * it as its dedupe gate.
 */
export function markTerminalFeedbackEmitted(taskId: string): boolean {
  if (emittedTaskIds.has(taskId)) return false;
  emittedTaskIds.add(taskId);
  if (emittedTaskIds.size > EMITTED_TASK_RETENTION) {
    for (const oldest of emittedTaskIds) {
      emittedTaskIds.delete(oldest);
      break;
    }
  }
  return true;
}

const feedbackOwners = new Map<string, number>();

/**
 * Claim terminal feedback ownership of a task type for a mounted page that is
 * actively tracking a task of that type (its own stream paints the outcome
 * inline). Returns an idempotent release. Both directions are synchronous: a
 * claim suppresses the global report from the moment the page starts or adopts
 * a run, and a release hands it back the moment the run settles or the page
 * unmounts — there is no trailing window that could swallow a failure.
 */
export function claimTerminalFeedback(type: string): () => void {
  feedbackOwners.set(type, (feedbackOwners.get(type) ?? 0) + 1);
  let released = false;
  return () => {
    if (released) return;
    released = true;
    const count = feedbackOwners.get(type) ?? 0;
    if (count <= 1) feedbackOwners.delete(type);
    else feedbackOwners.set(type, count - 1);
  };
}

export function hasTerminalFeedbackOwner(type: string): boolean {
  return feedbackOwners.has(type);
}

function capabilityPresentation(task: TerminalFeedbackTask) {
  const wire = requestString(task.metadata, "capability") ?? "capability";
  const def = CAPABILITIES.find((c) => c.wire === wire) as
    | CapabilityDef
    | undefined;
  const label = def?.label ?? wire;
  // Surface an "Open …" action link on the notification for capabilities
  // that have a dedicated page (omitted for ones that don't).
  const opts = def?.route ? { meta: def.route } : undefined;
  return { label, opts };
}

const capabilityInstallError: NonNullable<TerminalFeedbackEntry["onFailed"]> = (
  task,
  error,
) => {
  const { label, opts } = capabilityPresentation(task);
  toast.error(
    getMutationErrorMessage(error, `Failed to install ${label}`),
    opts,
  );
};

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;
const dockerUpdateError: NonNullable<TerminalFeedbackEntry["onFailed"]> = (
  _task,
  error,
) => {
  toast.error(getMutationErrorMessage(error, "Docker container update failed"), {
    meta: DOCKER_TOAST_META,
  });
};

/**
 * What the global handler reports when a task of a given type ends. A handler
 * left undefined means that terminal state is deliberately silent; an empty
 * entry opts the type out of `GENERIC_TASK_FEEDBACK` entirely (page-owned
 * feedback).
 */
export const TERMINAL_TASK_FEEDBACK: Record<string, TerminalFeedbackEntry> = {
  [TaskTypes.TASK_TYPE_DOCKER_UPDATE]: {
    onCompleted: (_task, value) => {
      const result = value as DockerContainerUpdateResult | undefined;
      const name = result?.containerName || "Container";
      toast.success(
        result?.updated ? `${name} updated` : `${name} is already up to date`,
        { meta: DOCKER_TOAST_META },
      );
    },
    onFailed: dockerUpdateError,
    onCanceled: dockerUpdateError,
  },
  // Owned by the global handler (not CapabilityManagerSection) so the toast
  // still fires when the Settings dialog has been closed mid-install. A
  // cancellation is deliberately reported as an error: an install that stopped
  // midway needs attention either way, and both paths have always done so.
  [TaskTypes.TASK_TYPE_SYSTEM_INSTALL_CAPABILITY]: {
    onCompleted: (task, result, deps) => {
      const { label, opts } = capabilityPresentation(task);
      // Any successful task result (available or not) refreshes app-wide state.
      void deps.refreshCapabilities();
      const install = result as InstallCapabilityResult | undefined;
      if (install?.available) {
        toast.success(`${label} installed`, opts);
      } else {
        const reason = install?.error ? `: ${install.error}` : ".";
        toast.warning(
          `${label} installed but is still unavailable${reason}`,
          opts,
        );
      }
    },
    onFailed: capabilityInstallError,
    onCanceled: capabilityInstallError,
  },
  // Success needs no toast (the updates list refreshes via invalidation) and a
  // cancel was user-initiated on some surface — only a genuine failure is news.
  [TaskTypes.TASK_TYPE_PACKAGE_UPDATE]: {
    onFailed: (_task, error) => {
      toast.error(getMutationErrorMessage(error, "Package update failed"));
    },
  },
  // Page-owned: DiskOverview fires its own scoped toast for SMART tests, so
  // the global handler stays silent for every terminal state.
  [TaskTypes.TASK_TYPE_STORAGE_SMART_TEST]: {},
};

/**
 * Fallback for task types without a registry entry, used only on the watch
 * path — the events fallback stays registry-only so it cannot double-report a
 * task some page handles locally. A cancel is not a failure, whichever surface
 * asked for it, so only genuine failures toast.
 */
export const GENERIC_TASK_FEEDBACK: TerminalFeedbackEntry = {
  onFailed: (_task, error) => {
    toast.error(getMutationErrorMessage(error, "Task failed"));
  },
};

/**
 * Single insertion point for global terminal-task feedback. The first call for
 * a task id wins across both delivery paths, and a page that has claimed the
 * type (or already painted its own alert via `markTerminalFeedbackEmitted`)
 * keeps the global report silent. Callers without an explicit `entry` emit for
 * registered types only.
 */
export function emitTerminalTaskFeedback(
  task: TerminalFeedbackTask,
  outcome: TerminalTaskOutcome,
  deps: TerminalFeedbackDeps,
  entry: TerminalFeedbackEntry | undefined = TERMINAL_TASK_FEEDBACK[task.type],
): void {
  if (!entry) return;
  if (!markTerminalFeedbackEmitted(task.id)) return;
  if (hasTerminalFeedbackOwner(task.type)) return;
  switch (outcome.kind) {
    case "completed":
      entry.onCompleted?.(task, outcome.result, deps);
      break;
    case "failed":
      entry.onFailed?.(task, outcome.error, deps);
      break;
    case "canceled":
      entry.onCanceled?.(task, outcome.error, deps);
      break;
  }
}

/**
 * Outcome of a terminal snapshot as seen on the events stream, normalized to
 * the same error shape the watch path rejects with (`waitForStreamResult`
 * builds a LinuxIOError from the result-error frame), so registry handlers and
 * cancellation checks behave identically on both paths. Returns null for
 * non-terminal states.
 */
export function terminalSnapshotOutcome(
  task: TaskSnapshot,
): TerminalTaskOutcome | null {
  switch (task.state) {
    case "completed":
      return { kind: "completed", result: task.result };
    case "failed":
    case "canceled":
      return {
        kind: task.state,
        error: new LinuxIOError(task.error?.message ?? "", task.error?.code),
      };
    default:
      return null;
  }
}

/** Test-only: clears the dedupe and ownership state between cases. */
export function resetTerminalTaskFeedback(): void {
  emittedTaskIds.clear();
  feedbackOwners.clear();
}
