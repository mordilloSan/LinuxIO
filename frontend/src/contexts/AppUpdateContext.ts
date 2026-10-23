import { createContext } from "react";

export type AppUpdatePhase =
  | "idle"
  | "running"
  | "restarting"
  | "verifying"
  | "done"
  | "failed";

export interface AppUpdateContextValue {
  error: string | null;
  output: string[];
  phase: AppUpdatePhase;
  progress: number;
  resetUpdate: () => void;
  startUpdate: (targetVersion?: string) => void;
  status: string;
  targetVersion: string | null;
  updateComplete: boolean;
  updateSuccess: boolean;
}

export const AppUpdateContext = createContext<AppUpdateContextValue | null>(
  null,
);
AppUpdateContext.displayName = "AppUpdateContext";
