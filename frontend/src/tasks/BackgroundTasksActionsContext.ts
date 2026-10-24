import { createContext } from "react";

import type { BackgroundTaskActions } from "./types";

export const BackgroundTasksActionsContext =
  createContext<BackgroundTaskActions | null>(null);
