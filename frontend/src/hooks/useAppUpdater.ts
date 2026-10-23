import { useContext, useSyncExternalStore } from "react";

import {
  AppUpdateContext,
  type AppUpdateContextValue,
} from "@/contexts/AppUpdateContext";
import {
  isAppUpdateInProgress,
  subscribeAppUpdateInProgress,
} from "@/contexts/appUpdateInProgress";

export const useAppUpdater = (): AppUpdateContextValue => {
  const context = useContext(AppUpdateContext);
  if (!context) {
    throw new Error("AppUpdateContext must be placed within AppUpdateProvider");
  }
  return context;
};

/** Whether a LinuxIO self-update is running; navigation is blocked meanwhile. */
export const useAppUpdateInProgress = (): boolean =>
  useSyncExternalStore(subscribeAppUpdateInProgress, isAppUpdateInProgress);
