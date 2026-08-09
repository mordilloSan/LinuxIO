import type { TaskSnapshot } from "./generated/linuxio-types";

export function isTerminalTaskState(state: TaskSnapshot["state"]): boolean {
  return state === "completed" || state === "failed" || state === "canceled";
}
