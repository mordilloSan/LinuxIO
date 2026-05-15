import type { JobSnapshot } from "./linuxio-types";

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
