import type { JobSnapshot } from "./generated/linuxio-types";

export function isTerminalJobState(state: JobSnapshot["state"]): boolean {
  return state === "completed" || state === "failed" || state === "canceled";
}
