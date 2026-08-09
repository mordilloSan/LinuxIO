import { useContext } from "react";

import { BackgroundTasksStateContext } from "@/contexts/TasksStateContext";

export const useBackgroundTaskState = () => {
  const context = useContext(BackgroundTasksStateContext);
  if (!context) {
    throw new Error(
      "useBackgroundTaskState must be used within BackgroundTasksProvider",
    );
  }
  return context;
};
