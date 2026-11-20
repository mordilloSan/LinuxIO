import type { UseNavigateResult } from "@tanstack/react-router";
import { toast } from "sonner";

import {
  CAPABILITIES,
  type DockerContainerUpdateResult,
  getMutationErrorMessage,
  type InstallCapabilityResult,
  LinuxIOError,
  type SwapMode,
  type SwapModeResult,
  type TaskSnapshot,
} from "@/api";
import { toastLinkOptions } from "@/api/toast-meta";
import { toastFileError } from "@/utils/linkRefusal";

import {
  DOCKER_STORAGE_APPLIED,
  DOCKER_STORAGE_FAILED,
} from "./dockerStorageFeedback";
import {
  MEMORY_DRAIN_FAILED,
  MEMORY_DRAINED,
  MEMORY_MODE_APPLIED,
  MEMORY_MODE_FAILED,
  MEMORY_TOAST_META,
  modeResultWarning,
} from "./memoryFeedback";
import * as TaskTypes from "./taskTypes";
import { requestString } from "./taskUtils";

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

interface TerminalFeedbackTask {
  id: string;
  type: string;
  metadata: Record<string, unknown>;
}

type TerminalTaskOutcome =
  | { kind: "completed"; result: unknown }
  | { kind: "failed"; error: unknown }
  | { kind: "canceled"; error: unknown };

interface TerminalFeedbackDeps {
  /** Lets a failure toast offer to open the folder a refused link points to. */
  navigate: UseNavigateResult<string>;
  refreshCapabilities: () => Promise<unknown>;
}

interface TerminalFeedbackEntry {
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
  const def = CAPABILITIES.find((capability) => capability.wire === wire);
  const label = def?.label ?? wire;
  // The capability's own page, if it has one, for the toast's "Open …" link.
  const route = def && "route" in def ? def.route : undefined;
  return { label, route };
}

const capabilityInstallError: NonNullable<TerminalFeedbackEntry["onFailed"]> = (
  task,
  error,
  deps,
) => {
  // A failed or canceled install may still have put the package in, as when
  // a step after the package step failed, so the state is read again.
  void deps.refreshCapabilities();
  const { label, route } = capabilityPresentation(task);
  toast.error(
    getMutationErrorMessage(error, `Failed to install ${label}`),
    toastLinkOptions(route),
  );
};

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;
const dockerUpdateError: NonNullable<TerminalFeedbackEntry["onFailed"]> = (
  _task,
  error,
) => {
  toast.error(
    getMutationErrorMessage(error, "Docker container update failed"),
    toastLinkOptions(DOCKER_TOAST_META),
  );
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
        toastLinkOptions(DOCKER_TOAST_META),
      );
    },
    onFailed: dockerUpdateError,
    onCanceled: dockerUpdateError,
  },
  // The Docker settings section can be closed or left while a storage change
  // runs. A section that follows the job reports it and marks it reported;
  // this is for a job nobody follows, as after a reload.
  [TaskTypes.TASK_TYPE_DOCKER_STORAGE]: {
    onCompleted: () => {
      toast.success(DOCKER_STORAGE_APPLIED);
    },
    onFailed: (_task, error) => {
      toast.error(getMutationErrorMessage(error, DOCKER_STORAGE_FAILED));
    },
  },
  // Owned by the global handler (not CapabilityManagerSection) so the toast
  // still fires when the Settings dialog has been closed mid-install. A
  // cancellation is deliberately reported as an error: an install that stopped
  // midway needs attention either way, and both paths have always done so.
  [TaskTypes.TASK_TYPE_SYSTEM_INSTALL_CAPABILITY]: {
    onCompleted: (task, result, deps) => {
      const { label, route } = capabilityPresentation(task);
      // Any successful task result (available or not) refreshes app-wide state.
      void deps.refreshCapabilities();
      const install = result as InstallCapabilityResult | undefined;
      if (install?.available) {
        if (install.warning) {
          toast.warning(
            `${label} installed with warning`,
            toastLinkOptions(route, { description: install.warning }),
          );
        } else {
          toast.success(`${label} installed`, toastLinkOptions(route));
        }
      } else {
        const reason = install?.error ? `: ${install.error}` : ".";
        toast.warning(
          `${label} installed but is still unavailable${reason}`,
          toastLinkOptions(route),
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
  // The Memory overflow dialog can be closed while its change runs. A dialog
  // that follows the job reports it and marks it reported; this is for a job
  // nobody follows, as after a reload. The job's action is the mode asked
  // for, which the warning needs.
  [TaskTypes.TASK_TYPE_MEMORY_SWAP_MODE]: {
    onCompleted: (task, value) => {
      const result = value as SwapModeResult | undefined;
      const requested = requestString(task.metadata, "action") as
        | SwapMode
        | undefined;
      const warning = result
        ? modeResultWarning(result, requested ?? result.mode)
        : null;
      if (warning) {
        toast.warning(warning, toastLinkOptions(MEMORY_TOAST_META));
      } else {
        toast.success(MEMORY_MODE_APPLIED, toastLinkOptions(MEMORY_TOAST_META));
      }
    },
    onFailed: (_task, error) => {
      toast.error(
        getMutationErrorMessage(error, MEMORY_MODE_FAILED),
        toastLinkOptions(MEMORY_TOAST_META),
      );
    },
  },
  [TaskTypes.TASK_TYPE_MEMORY_DRAIN]: {
    onCompleted: () => {
      toast.success(MEMORY_DRAINED, toastLinkOptions(MEMORY_TOAST_META));
    },
    onFailed: (_task, error) => {
      toast.error(
        getMutationErrorMessage(error, MEMORY_DRAIN_FAILED),
        toastLinkOptions(MEMORY_TOAST_META),
      );
    },
  },
  // Page-owned: DiskOverview fires its own scoped toast for SMART tests, so
  // the global handler stays silent for every terminal state.
  [TaskTypes.TASK_TYPE_STORAGE_SMART_TEST]: {},
  // Page-owned the same way: BtrfsDevicesCard toasts the balance outcome.
  [TaskTypes.TASK_TYPE_STORAGE_BTRFS_BALANCE]: {},
  // And BtrfsHealthBanner toasts the scrub outcome.
  [TaskTypes.TASK_TYPE_STORAGE_BTRFS_SCRUB]: {},
};

/**
 * Fallback for task types without a registry entry, used only on the watch
 * path — the events fallback stays registry-only so it cannot double-report a
 * task some page handles locally. A cancel is not a failure, whichever surface
 * asked for it, so only genuine failures toast.
 */
export const GENERIC_TASK_FEEDBACK: TerminalFeedbackEntry = {
  // File tasks picked up after a reload (delete, trash, permissions) fail
  // here, so a refused link gets its "Open the real folder" action.
  onFailed: (_task, error, deps) => {
    toastFileError(
      toast,
      deps.navigate,
      getMutationErrorMessage(error, "Task failed"),
    );
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
    case "interrupted":
      return {
        kind: "failed",
        error: new LinuxIOError(
          task.error?.message || "interrupted",
          task.error?.code,
        ),
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
