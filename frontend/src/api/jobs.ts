import type { JobSnapshot } from "./generated/linuxio-types";
import { isTerminalJobState } from "./job-state";
import { openJobAttachStream } from "./linuxio";
import { LinuxIOError, request } from "./linuxio-core";
import { waitForStreamResult } from "./stream-helpers";

export { isTerminalJobState };

export function isJobSnapshot(value: unknown): value is JobSnapshot {
  return (
    !!value &&
    typeof value === "object" &&
    "id" in value &&
    "state" in value &&
    "created_at" in value
  );
}

export function jobSnapshotResult<T>(value: T | JobSnapshot): T {
  if (isJobSnapshot(value)) {
    return value.result as T;
  }
  return value;
}

/** Status code the bridge publishes for a canceled job (markCanceledLocked). */
export const JOB_CANCELED_CODE = 499;

/**
 * True when a job stream ended because the job was canceled — by this page, the
 * navbar chip, or another session — rather than because it failed. Cancellation
 * arrives as an ordinary result-error frame, so every terminal-error surface has
 * to tell the two apart before reporting a failure.
 */
export function isJobCancellationError(error: unknown): boolean {
  return (
    error instanceof LinuxIOError && Number(error.code) === JOB_CANCELED_CODE
  );
}

const JOB_POLL_INTERVAL_MS = 1_000;

/**
 * Poll `jobs.get` until the job reaches a terminal state. Fallback for when
 * the attach stream cannot be opened (mux dropped between job start and
 * attach); the connection_closed retry policy re-initializes the mux and
 * waits, so this resolves at actual completion instead of failing fast.
 */
async function pollJobUntilTerminal(jobId: string): Promise<JobSnapshot> {
  for (;;) {
    const snapshot = await request<JobSnapshot>(
      "jobs",
      "get",
      { jobId },
      { retryPolicy: "connection_closed" },
    );
    if (isTerminalJobState(snapshot.state)) {
      return snapshot;
    }
    await new Promise((resolve) => setTimeout(resolve, JOB_POLL_INTERVAL_MS));
  }
}

function terminalSnapshotOrThrow(snapshot: JobSnapshot): JobSnapshot {
  if (snapshot.state === "completed") return snapshot;
  throw new LinuxIOError(
    snapshot.error?.message ?? "Job failed",
    snapshot.error?.code,
  );
}

export async function waitForJobCompletion(
  snapshot: JobSnapshot,
): Promise<JobSnapshot> {
  if (isTerminalJobState(snapshot.state)) {
    return terminalSnapshotOrThrow(snapshot);
  }

  const attach = openJobAttachStream(snapshot.id);
  if (!attach) {
    // Unlike useJobStreamAction (which promises live progress and fails fast
    // when it cannot attach), a plain job action promises completion — so a
    // missed attach falls back to polling rather than resolving mid-job with
    // an undefined result.
    return terminalSnapshotOrThrow(await pollJobUntilTerminal(snapshot.id));
  }

  const result = await waitForStreamResult(attach, {
    closeMessage: "Job stream closed before completion",
  });

  try {
    return await request<JobSnapshot>(
      "jobs",
      "get",
      { jobId: snapshot.id },
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
