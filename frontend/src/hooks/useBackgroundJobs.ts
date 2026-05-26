import { useContext } from "react";

import { BackgroundJobsContext } from "@/contexts/BackgroundJobsContext";

export const useBackgroundJobs = () => {
  const context = useContext(BackgroundJobsContext);
  if (!context) {
    throw new Error(
      "useBackgroundJobs must be used within BackgroundJobsProvider",
    );
  }
  return context;
};

export default useBackgroundJobs;
