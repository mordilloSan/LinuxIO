import { toast } from "sonner";

import {
  CAPABILITIES,
  type CapabilityDef,
  type InstallCapabilityResult,
  type JobSnapshot,
  LinuxIOError,
} from "@/api";
import * as JobTypes from "@/constants/backgroundJobTypes";
import { requestString } from "@/utils/backgroundJobs";
import { getMutationErrorMessage } from "@/utils/mutations";

/**
 * Terminal feedback for background jobs observed by the global handler
 * (useRecoveredJobs). Two delivery paths can report the same terminal event —
 * the attach stream and the job-events fallback — and a page that owns the job
 * may be painting the outcome inline at the same time. This module owns the
 * arbitration so per-type feedback lives in exactly one place:
 *
 * - `TERMINAL_JOB_FEEDBACK` maps job type -> outcome handlers. Adding feedback
 *   for a new type means adding one entry here.
 * - De-duplication across paths is owned by the plumbing (bounded, so a long
 *   session doesn't accrete one id per job forever).
 * - A mounted page that is actively tracking a job claims its type via
 *   `claimTerminalFeedback`; claims register and release synchronously, unlike
 *   `isJobLocallyHandled` whose 5 s trailing unmark would swallow a failure
 *   that arrives just after navigating away.
 */

export interface TerminalFeedbackJob {
  id: string;
  type: string;
  metadata: Record<string, unknown>;
}

export type TerminalJobOutcome =
  | { kind: "completed"; result: unknown }
  | { kind: "failed"; error: unknown }
  | { kind: "canceled"; error: unknown };

export interface TerminalFeedbackDeps {
  refreshCapabilities: () => Promise<unknown>;
}

export interface TerminalFeedbackEntry {
  onCompleted?: (
    job: TerminalFeedbackJob,
    result: unknown,
    deps: TerminalFeedbackDeps,
  ) => void;
  onFailed?: (
    job: TerminalFeedbackJob,
    error: unknown,
    deps: TerminalFeedbackDeps,
  ) => void;
  onCanceled?: (
    job: TerminalFeedbackJob,
    error: unknown,
    deps: TerminalFeedbackDeps,
  ) => void;
}

const EMITTED_JOB_RETENTION = 200;
const emittedJobIds = new Set<string>();

/**
 * Record that terminal feedback for `jobId` has been surfaced. Returns true on
 * the first call for a job, false when it was already handled. Pages call this
 * the moment they paint their own terminal alert, so the global paths stay
 * silent whichever stream's frame lands first; `emitTerminalJobFeedback` uses
 * it as its dedupe gate.
 */
export function markTerminalFeedbackEmitted(jobId: string): boolean {
  if (emittedJobIds.has(jobId)) return false;
  emittedJobIds.add(jobId);
  if (emittedJobIds.size > EMITTED_JOB_RETENTION) {
    for (const oldest of emittedJobIds) {
      emittedJobIds.delete(oldest);
      break;
    }
  }
  return true;
}

const feedbackOwners = new Map<string, number>();

/**
 * Claim terminal feedback ownership of a job type for a mounted page that is
 * actively tracking a job of that type (its own stream paints the outcome
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

function capabilityPresentation(job: TerminalFeedbackJob) {
  const wire = requestString(job.metadata, "capability") ?? "capability";
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
  job,
  error,
) => {
  const { label, opts } = capabilityPresentation(job);
  toast.error(
    getMutationErrorMessage(error, `Failed to install ${label}`),
    opts,
  );
};

/**
 * What the global handler reports when a job of a given type ends. A handler
 * left undefined means that terminal state is deliberately silent; an empty
 * entry opts the type out of `GENERIC_JOB_FEEDBACK` entirely (page-owned
 * feedback).
 */
export const TERMINAL_JOB_FEEDBACK: Record<string, TerminalFeedbackEntry> = {
  // Owned by the global handler (not CapabilityManagerSection) so the toast
  // still fires when the Settings dialog has been closed mid-install. A
  // cancellation is deliberately reported as an error: an install that stopped
  // midway needs attention either way, and both paths have always done so.
  [JobTypes.JOB_TYPE_SYSTEM_INSTALL_CAPABILITY]: {
    onCompleted: (job, result, deps) => {
      const { label, opts } = capabilityPresentation(job);
      // Any successful job result (available or not) refreshes app-wide state.
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
  [JobTypes.JOB_TYPE_PACKAGE_UPDATE]: {
    onFailed: (_job, error) => {
      toast.error(getMutationErrorMessage(error, "Package update failed"));
    },
  },
  // Page-owned: DiskOverview fires its own scoped toast for SMART tests, so
  // the global handler stays silent for every terminal state.
  [JobTypes.JOB_TYPE_STORAGE_SMART_TEST]: {},
};

/**
 * Fallback for job types without a registry entry, used only on the attach
 * path — the events fallback stays registry-only so it cannot double-report a
 * job some page handles locally. A cancel is not a failure, whichever surface
 * asked for it, so only genuine failures toast.
 */
export const GENERIC_JOB_FEEDBACK: TerminalFeedbackEntry = {
  onFailed: (_job, error) => {
    toast.error(getMutationErrorMessage(error, "Job failed"));
  },
};

/**
 * Single insertion point for global terminal-job feedback. The first call for
 * a job id wins across both delivery paths, and a page that has claimed the
 * type (or already painted its own alert via `markTerminalFeedbackEmitted`)
 * keeps the global report silent. Callers without an explicit `entry` emit for
 * registered types only.
 */
export function emitTerminalJobFeedback(
  job: TerminalFeedbackJob,
  outcome: TerminalJobOutcome,
  deps: TerminalFeedbackDeps,
  entry: TerminalFeedbackEntry | undefined = TERMINAL_JOB_FEEDBACK[job.type],
): void {
  if (!entry) return;
  if (!markTerminalFeedbackEmitted(job.id)) return;
  if (hasTerminalFeedbackOwner(job.type)) return;
  switch (outcome.kind) {
    case "completed":
      entry.onCompleted?.(job, outcome.result, deps);
      break;
    case "failed":
      entry.onFailed?.(job, outcome.error, deps);
      break;
    case "canceled":
      entry.onCanceled?.(job, outcome.error, deps);
      break;
  }
}

/**
 * Outcome of a terminal snapshot as seen on the events stream, normalized to
 * the same error shape the attach path rejects with (`waitForStreamResult`
 * builds a LinuxIOError from the result-error frame), so registry handlers and
 * cancellation checks behave identically on both paths. Returns null for
 * non-terminal states.
 */
export function terminalSnapshotOutcome(
  job: JobSnapshot,
): TerminalJobOutcome | null {
  switch (job.state) {
    case "completed":
      return { kind: "completed", result: job.result };
    case "failed":
    case "canceled":
      return {
        kind: job.state,
        error: new LinuxIOError(job.error?.message ?? "", job.error?.code),
      };
    default:
      return null;
  }
}

/** Test-only: clears the dedupe and ownership state between cases. */
export function resetTerminalJobFeedback(): void {
  emittedJobIds.clear();
  feedbackOwners.clear();
}
