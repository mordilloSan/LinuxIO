export type PackageUpdatePhase = "idle" | "running" | "finishing";

export interface PackageUpdateState {
  error: string | null;
  eventLog: string[];
  phase: PackageUpdatePhase;
  progress: number;
  status: string | null;
  updatingPackage: string | null;
}

export const initialPackageUpdateState: PackageUpdateState = {
  error: null,
  eventLog: [],
  phase: "idle",
  progress: 0,
  status: null,
  updatingPackage: null,
};

type PackageUpdateAction =
  | { type: "start"; packageName: string; status: string; event: string }
  | { type: "progress"; percentage?: number }
  | { type: "package"; packageName: string; status?: string; event?: string }
  | { type: "status"; status: string; event?: string }
  | { type: "finishing" }
  | { type: "complete" }
  | { type: "failed"; error: string }
  | { type: "canceled" }
  | { type: "clearError" };

function appendEvent(
  eventLog: string[],
  message: string | undefined,
): string[] {
  const trimmed = message?.trim();
  if (!trimmed || eventLog.at(-1) === trimmed) return eventLog;
  return [...eventLog, trimmed].slice(-8);
}

/** Pure state machine for the package-update panel. */
export function packageUpdateReducer(
  state: PackageUpdateState,
  action: PackageUpdateAction,
): PackageUpdateState {
  switch (action.type) {
    case "start":
      return {
        error: null,
        eventLog: appendEvent([], action.event),
        phase: "running",
        progress: 0,
        status: action.status,
        updatingPackage: action.packageName,
      };
    case "progress":
      return action.percentage === undefined || action.percentage > 100
        ? state
        : { ...state, progress: Math.max(state.progress, action.percentage) };
    case "package":
      return {
        ...state,
        eventLog: appendEvent(state.eventLog, action.event),
        status: action.status ?? state.status,
        updatingPackage: action.packageName,
      };
    case "status":
      return {
        ...state,
        eventLog: appendEvent(state.eventLog, action.event),
        status: action.status,
      };
    case "finishing":
      return {
        ...state,
        eventLog: appendEvent(state.eventLog, "Finished"),
        phase: "finishing",
        progress: 100,
        status: "Finished",
      };
    case "complete":
      return { ...state, phase: "idle", status: null, updatingPackage: null };
    case "failed":
      return {
        ...state,
        error: action.error,
        phase: "idle",
        status: null,
        updatingPackage: null,
      };
    case "canceled":
      return {
        ...state,
        error: "Update cancelled",
        phase: "idle",
        status: null,
        updatingPackage: null,
      };
    case "clearError":
      return { ...state, error: null };
  }
}
