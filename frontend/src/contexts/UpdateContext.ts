import { createContext } from "react";

export type UpdatePhase =
  | "idle"
  | "running"
  | "restarting"
  | "verifying"
  | "done"
  | "failed";

export interface UpdateContextValue {
  canNavigate: boolean;
  error: string | null;
  isUpdating: boolean;
  output: string[];
  phase: UpdatePhase;
  progress: number;
  resetUpdate: () => void;
  startUpdate: (targetVersion?: string) => void;
  status: string;
  targetVersion: string | null;
  updateComplete: boolean;
  updateSuccess: boolean;
}

export const UpdateContext = createContext<UpdateContextValue | null>(null);
UpdateContext.displayName = "UpdateContext";

export const UpdateNavigationContext = createContext<boolean | null>(null);
UpdateNavigationContext.displayName = "UpdateNavigationContext";

let liveUpdateBlockerOwner: object | null = null;
let liveUpdateBlocked = false;

export const isLiveUpdateBlocked = () => liveUpdateBlocked;

export const publishLiveUpdateBlocked = (owner: object, blocked: boolean) => {
  liveUpdateBlockerOwner = owner;
  liveUpdateBlocked = blocked;

  return () => {
    if (liveUpdateBlockerOwner !== owner) return;
    liveUpdateBlockerOwner = null;
    liveUpdateBlocked = false;
  };
};
