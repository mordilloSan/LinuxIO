import { useCallback, useState } from "react";
import { toast } from "sonner";

import type { BackgroundJob } from "@/types/backgroundJobs";

import type { BackgroundJobRuntime } from "./useBackgroundJobRuntime";

export function useGenericBackgroundJobs(runtime: BackgroundJobRuntime) {
  const [backgroundJobs, setBackgroundJobs] = useState<BackgroundJob[]>([]);
  const { activeBackgroundJobIdsRef, streamRefsRef, cancelBridgeJob } = runtime;

  const removeBackgroundJob = useCallback(
    (id: string) => {
      if (!activeBackgroundJobIdsRef.current.has(id)) {
        return;
      }
      activeBackgroundJobIdsRef.current.delete(id);
      setBackgroundJobs((prev) => prev.filter((job) => job.id !== id));
      streamRefsRef.current.delete(id);
    },
    [activeBackgroundJobIdsRef, streamRefsRef],
  );

  const cancelJob = useCallback(
    (id: string) => {
      const job = backgroundJobs.find((item) => item.id === id);
      if (!job) return;
      job.abortController.abort();
      const stream = streamRefsRef.current.get(id) || job.stream;
      if (stream) {
        stream.abort();
        streamRefsRef.current.delete(id);
      }
      cancelBridgeJob(id);
      toast.info("Job cancelled");
      removeBackgroundJob(id);
    },
    [backgroundJobs, cancelBridgeJob, removeBackgroundJob, streamRefsRef],
  );

  return {
    backgroundJobs,
    cancelJob,
    recoveryControls: {
      setBackgroundJobs,
      removeBackgroundJob,
    },
  };
}
