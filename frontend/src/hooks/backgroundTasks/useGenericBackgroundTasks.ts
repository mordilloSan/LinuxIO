import { toast } from "sonner";

import type { BackgroundTaskRuntime } from "./useBackgroundTaskRuntime";

export function useGenericBackgroundTasks(runtime: BackgroundTaskRuntime) {
  const { set: setBackgroundTasks, read: readTasks } =
    runtime.tasks.backgroundTasks;
  const { activeBackgroundTaskIdsRef, streamRefsRef, cancelBridgeTask } =
    runtime;

  const removeBackgroundTask = (id: string) => {
    if (!activeBackgroundTaskIdsRef.current.has(id)) {
      return;
    }
    activeBackgroundTaskIdsRef.current.delete(id);
    setBackgroundTasks((prev) => prev.filter((task) => task.id !== id));
    streamRefsRef.current.delete(id);
  };

  const cancelTask = (id: string) => {
    const task = readTasks().find((item) => item.id === id);
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
  };

  return {
    cancelTask,
    recoveryControls: {
      setBackgroundTasks,
      removeBackgroundTask,
    },
  };
}
