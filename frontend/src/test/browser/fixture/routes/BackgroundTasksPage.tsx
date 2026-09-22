import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import { createBackgroundTaskCache } from "@/api/background-task-cache";
import AppButton from "@/components/ui/AppButton";
import {
  useBackgroundTask,
  useBackgroundTaskList,
} from "@/hooks/backgroundTasks/useBackgroundTaskState";
import { useIsIndexing } from "@/hooks/backgroundTasks/useIsIndexing";

function TaskRow({ id }: { id: string }) {
  const task = useBackgroundTask(id);
  const ref = useRef<HTMLOutputElement>(null);
  useEffect(() => {
    if (ref.current)
      ref.current.dataset.commits = String(
        Number(ref.current.dataset.commits ?? 0) + 1,
      );
  });
  return (
    <output ref={ref} data-testid={id}>
      {task?.progress ?? "removed"}
    </output>
  );
}

function TaskList() {
  const tasks = useBackgroundTaskList();
  const ref = useRef<HTMLOutputElement>(null);
  useEffect(() => {
    if (ref.current)
      ref.current.dataset.commits = String(
        Number(ref.current.dataset.commits ?? 0) + 1,
      );
  });
  return (
    <output ref={ref} data-testid="list">
      {tasks.map((task) => task.id).join(",")}
    </output>
  );
}

function IndexingFlag() {
  const indexing = useIsIndexing();
  const ref = useRef<HTMLOutputElement>(null);
  useEffect(() => {
    if (ref.current)
      ref.current.dataset.commits = String(
        Number(ref.current.dataset.commits ?? 0) + 1,
      );
  });
  return (
    <output ref={ref} data-testid="indexing">
      {String(indexing)}
    </output>
  );
}

export default function BackgroundTasksPage() {
  const [queryClient] = useState(() => new QueryClient());
  const [tasks] = useState(() =>
    createBackgroundTaskCache(queryClient, "anonymous"),
  );
  useEffect(() => {
    tasks.activate();
    tasks.downloads.set(
      ["first", "second"].map((id) => ({
        id,
        taskId: id,
        type: "download",
        paths: ["/data/"],
        progress: 0,
        label: "Preparing",
        abortController: new AbortController(),
      })),
    );
    return () => tasks.dispose();
  }, [tasks]);
  return (
    <QueryClientProvider client={queryClient}>
      <AppButton
        onClick={() =>
          tasks.downloads.set((items) =>
            items.map((item) =>
              item.id === "first"
                ? { ...item, progress: item.progress + 10 }
                : item,
            ),
          )
        }
      >
        Advance first task
      </AppButton>
      <AppButton
        onClick={() =>
          tasks.downloads.set((items) =>
            items.filter((item) => item.id !== "first"),
          )
        }
      >
        Remove first task
      </AppButton>
      <TaskList />
      <TaskRow id="first" />
      <TaskRow id="second" />
      <IndexingFlag />
    </QueryClientProvider>
  );
}
