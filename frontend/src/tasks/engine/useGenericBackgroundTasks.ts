import { toast } from "sonner";

import { call, getMutationErrorMessage } from "@/api";

import type { BackgroundTaskRuntime } from "./useBackgroundTaskRuntime";

export function useGenericBackgroundTasks(runtime: BackgroundTaskRuntime) {
  const { set: setBackgroundTasks, read: readTasks } =
    runtime.tasks.backgroundTasks;
  const {
    activeBackgroundTaskIdsRef,
    locallyCanceledTaskIdsRef,
    streamRefsRef,
  } = runtime;

  const removeBackgroundTask = (id: string) => {
    if (!activeBackgroundTaskIdsRef.current.has(id)) {
      return;
    }
    activeBackgroundTaskIdsRef.current.delete(id);
    setBackgroundTasks((prev) => prev.filter((task) => task.id !== id));
    streamRefsRef.current.delete(id);
  };

  const setCancelRequested = (id: string, cancelRequested: boolean) =>
    setBackgroundTasks((prev) =>
      prev.map((task) =>
        task.id === id ? { ...task, cancelRequested } : task,
      ),
    );

  // A job ends when its handler returns, which can take a while: its row
  // stays, marked, and the watch following the job removes it at the end.
  const cancelTask = (id: string) => {
    const task = readTasks().find((item) => item.id === id);
    if (!task || task.cancelRequested) return;
    locallyCanceledTaskIdsRef.current.add(id);
    setCancelRequested(id, true);
    call("tasks.cancel", { taskId: id }).catch((error: unknown) => {
      locallyCanceledTaskIdsRef.current.delete(id);
      setCancelRequested(id, false);
      toast.error(getMutationErrorMessage(error, "Failed to cancel task"));
    });
  };

  return {
    cancelTask,
    recoveryControls: {
      setBackgroundTasks,
      removeBackgroundTask,
    },
  };
}
