import { useContext } from "react";

import { BackgroundTasksActionsContext } from "@/contexts/TasksActionsContext";

export const useBackgroundTaskActions = () => {
  const context = useContext(BackgroundTasksActionsContext);
  if (!context) {
    throw new Error(
      "useBackgroundTaskActions must be used within BackgroundTasksProvider",
    );
  }
  return context;
};
