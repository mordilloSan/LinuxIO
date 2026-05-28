import { useContext } from "react";

import { BackgroundJobsActionsContext } from "@/contexts/JobsActionsContext";

export const useBackgroundJobActions = () => {
  const context = useContext(BackgroundJobsActionsContext);
  if (!context) {
    throw new Error(
      "useBackgroundJobActions must be used within BackgroundJobsProvider",
    );
  }
  return context;
};
