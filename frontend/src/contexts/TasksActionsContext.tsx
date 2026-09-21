import { createContext } from "react";

import type { BackgroundTasksContextValue } from "@/types/backgroundTasks";

export type BackgroundTasksActionsContextValue = BackgroundTasksContextValue;

export const BackgroundTasksActionsContext =
  createContext<BackgroundTasksActionsContextValue | null>(null);
