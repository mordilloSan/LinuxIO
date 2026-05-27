import type { JobSnapshot } from "./generated/linuxio-types";

import { isTerminalJobState } from "./job-state";
import { openJobAttachStream } from "./linuxio";
import { call, LinuxIOError } from "./linuxio-core";
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

const LOCAL_HANDLER_RETENTION_MS = 5_000;
const locallyHandledJobIds = new Set<string>();

export function markJobLocallyHandled(id: string): void {
  locallyHandledJobIds.add(id);
}

export function unmarkJobLocallyHandled(id: string): void {
  setTimeout(() => locallyHandledJobIds.delete(id), LOCAL_HANDLER_RETENTION_MS);
}

export function isJobLocallyHandled(id: string): boolean {
  return locallyHandledJobIds.has(id);
}

export async function waitForJobCompletion(
  snapshot: JobSnapshot,
): Promise<JobSnapshot> {
  if (isTerminalJobState(snapshot.state)) {
    if (snapshot.state === "completed") return snapshot;
    throw new LinuxIOError(
      snapshot.error?.message ?? "Job failed",
      snapshot.error?.code,
    );
  }

  const attach = openJobAttachStream(snapshot.id);
  if (!attach) {
    return snapshot;
  }

  markJobLocallyHandled(snapshot.id);
  try {
    const result = await waitForStreamResult(attach, {
      closeMessage: "Job stream closed before completion",
    });

    try {
      return await call<JobSnapshot>("jobs", "get", [snapshot.id], {
        retryPolicy: "connection_closed",
      });
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
  } finally {
    unmarkJobLocallyHandled(snapshot.id);
  }
}
