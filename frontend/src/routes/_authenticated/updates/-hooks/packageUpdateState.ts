import {
  type InstallOutputLine,
  MAX_INSTALL_OUTPUT_LINES,
} from "@/components/terminal/InstallOutputPanel";

type PackageUpdatePhase = "idle" | "running";

interface PackageUpdateState {
  error: string | null;
  eventLog: string[];
  output: InstallOutputLine[];
  outputHistoryIncomplete: boolean;
  phase: PackageUpdatePhase;
  progress: number;
  status: string | null;
  updatingPackage: string | null;
}

export const initialPackageUpdateState: PackageUpdateState = {
  error: null,
  eventLog: [],
  output: [],
  outputHistoryIncomplete: false,
  phase: "idle",
  progress: 0,
  status: null,
  updatingPackage: null,
};

type PackageUpdateAction =
  | { type: "start"; packageName: string; status: string; recovered?: boolean }
  | { type: "output"; line: string }
  | { type: "progress"; percentage?: number }
  | { type: "package"; packageName: string; status?: string }
  | { type: "status"; status: string; event?: string }
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
        eventLog: [],
        output: [],
        // A recovered watch replays only the task's recent output.
        outputHistoryIncomplete: action.recovered ?? false,
        phase: "running",
        progress: 0,
        status: action.status,
        updatingPackage: action.packageName,
      };
    case "output": {
      const output = [
        ...state.output,
        {
          id: (state.output.at(-1)?.id ?? -1) + 1,
          stream: "stdout" as const,
          text: action.line,
        },
      ];
      const truncated = output.length > MAX_INSTALL_OUTPUT_LINES;
      return {
        ...state,
        output: truncated ? output.slice(-MAX_INSTALL_OUTPUT_LINES) : output,
        outputHistoryIncomplete: state.outputHistoryIncomplete || truncated,
      };
    }
    case "progress":
      return action.percentage === undefined || action.percentage > 100
        ? state
        : { ...state, progress: Math.max(state.progress, action.percentage) };
    case "package":
      return {
        ...state,
        status: action.status ?? state.status,
        updatingPackage: action.packageName,
      };
    case "status":
      return {
        ...state,
        eventLog: appendEvent(state.eventLog, action.event),
        status: action.status,
      };
    case "complete":
      return {
        ...state,
        phase: "idle",
        progress: 100,
        status: null,
        updatingPackage: null,
      };
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
