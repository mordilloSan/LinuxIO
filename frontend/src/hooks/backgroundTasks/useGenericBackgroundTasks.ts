import { useCallback, useState } from "react";
import { toast } from "sonner";

import { useLatestRef } from "@/hooks/useLatestRef";
import type { BackgroundTask } from "@/types/backgroundTasks";

import type { BackgroundTaskRuntime } from "./useBackgroundTaskRuntime";

export function useGenericBackgroundTasks(runtime: BackgroundTaskRuntime) {
  const [backgroundTasks, setBackgroundTasks] = useState<BackgroundTask[]>([]);
  const backgroundTasksRef = useLatestRef(backgroundTasks);
  const { activeBackgroundTaskIdsRef, streamRefsRef, cancelBridgeTask } =
    runtime;

  const removeBackgroundTask = useCallback(
    (id: string) => {
      if (!activeBackgroundTaskIdsRef.current.has(id)) {
        return;
      }
      activeBackgroundTaskIdsRef.current.delete(id);
      setBackgroundTasks((prev) => prev.filter((task) => task.id !== id));
      streamRefsRef.current.delete(id);
    },
    [activeBackgroundTaskIdsRef, streamRefsRef],
  );

  const cancelTask = useCallback(
    (id: string) => {
      const task = backgroundTasksRef.current.find((item) => item.id === id);
      if (!task) return;
      task.abortController.abort();
      const stream = streamRefsRef.current.get(id) || task.stream;
      if (stream) {
        stream.abort();
        streamRefsRef.current.delete(id);
      }
      cancelBridgeTask(id);
      toast.info("Task cancelled");
      removeBackgroundTask(id);
    },
    [backgroundTasksRef, cancelBridgeTask, removeBackgroundTask, streamRefsRef],
  );

  return {
    backgroundTasks,
    cancelTask,
    recoveryControls: {
      setBackgroundTasks,
      removeBackgroundTask,
    },
  };
}
