import { useContext } from "react";

import { BackgroundJobsStateContext } from "@/contexts/JobsStateContext";

export const useBackgroundJobState = () => {
  const context = useContext(BackgroundJobsStateContext);
  if (!context) {
    throw new Error(
      "useBackgroundJobState must be used within BackgroundJobsProvider",
    );
  }
  return context;
};
