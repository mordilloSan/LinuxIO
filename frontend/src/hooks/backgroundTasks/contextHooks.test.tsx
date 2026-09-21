import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import {
  backgroundTaskKeys,
  createBackgroundTaskCache,
} from "@/api/background-task-cache";
import { AuthContext } from "@/contexts/AuthContext";
import { BackgroundTasksProvider } from "@/contexts/BackgroundTasksContext";
import { useBackgroundTaskActions } from "@/hooks/backgroundTasks/useBackgroundTaskActions";
import { useBackgroundTaskIndexer } from "@/hooks/backgroundTasks/useBackgroundTaskIndexer";
import {
  useBackgroundTask,
  useBackgroundTaskList,
  useBackgroundTasks,
} from "@/hooks/backgroundTasks/useBackgroundTaskState";
import { useIsIndexing } from "@/hooks/backgroundTasks/useIsIndexing";
import {
  createAuthContextValue,
  createTestQueryClient,
  seedTaskCache,
} from "@/test/render";
import type { Download } from "@/types/backgroundTasks";

const download = (id: string): Download => ({
  id,
  taskId: id,
  type: "download",
  paths: ["/data/"],
  label: "Preparing",
  progress: 0,
  abortController: new AbortController(),
});

function setup(items: Download[] = []) {
  const queryClient = createTestQueryClient();
  const tasks = seedTaskCache(queryClient, items);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return { queryClient, tasks, wrapper };
}

describe("background task cache subscriptions", () => {
  it("requires a provider only for actions", () => {
    expect(() => renderHook(() => useBackgroundTaskActions())).toThrow(
      "useBackgroundTaskActions must be used within BackgroundTasksProvider",
    );
    const { wrapper } = setup();
    expect(renderHook(() => useIsIndexing(), { wrapper }).result.current).toBe(
      false,
    );
  });

  it("notifies the changed task while preserving sibling, list and flag selections", async () => {
    const { tasks, wrapper, queryClient } = setup([
      download("a"),
      download("b"),
    ]);
    const first = renderHook(() => useBackgroundTask("a"), { wrapper });
    const sibling = renderHook(() => useBackgroundTask("b"), { wrapper });
    const list = renderHook(() => useBackgroundTaskList(), { wrapper });
    const flag = renderHook(() => useIsIndexing(), { wrapper });
    const initialSibling = sibling.result.current;
    const initialList = list.result.current;
    const write = vi.spyOn(queryClient, "setQueryData");
    act(() =>
      tasks.downloads.set((items) =>
        items.map((item) =>
          item.id === "a" ? { ...item, progress: 30 } : item,
        ),
      ),
    );
    await waitFor(() => expect(first.result.current?.progress).toBe(30));
    expect(write).toHaveBeenCalledTimes(1);
    expect(write.mock.calls[0][0]).toEqual(
      backgroundTaskKeys.task("anonymous", "a"),
    );
    expect(sibling.result.current).toBe(initialSibling);
    expect(list.result.current).toBe(initialList);
    expect(flag.result.current).toBe(false);
    expect(
      queryClient.getQueryData(backgroundTaskKeys.task("anonymous", "a")),
    ).toBe(first.result.current);
    act(() =>
      tasks.downloads.set((items) => items.filter((item) => item.id !== "a")),
    );
    await waitFor(() => expect(first.result.current).toBeFalsy());
    expect(list.result.current.map((item) => item.id)).toEqual(["b"]);
  });

  it("isolates users, keeps cache entries alive without observers and rejects late writes after disposal", () => {
    const { queryClient, tasks } = setup([download("a")]);
    const other = createBackgroundTaskCache(queryClient, "other");
    other.downloads.set([{ ...download("a"), progress: 60 }]);
    expect(tasks.get("a")?.progress).toBe(0);
    expect(other.get("a")?.progress).toBe(60);
    expect(
      queryClient
        .getQueryCache()
        .find({ queryKey: backgroundTaskKeys.task("anonymous", "a") })?.gcTime,
    ).toBe(Infinity);
    tasks.dispose();
    tasks.downloads.set([download("late")]);
    tasks.setLastIndexerError("late error");
    expect(
      queryClient.getQueriesData({
        queryKey: backgroundTaskKeys.all("anonymous"),
      }),
    ).toEqual([]);
    expect(other.get("a")?.progress).toBe(60);
  });

  it("moves pending uploads to the server task key without losing their stable row id", async () => {
    const { tasks, wrapper, queryClient } = setup();
    tasks.uploads.set([
      {
        id: "local",
        type: "upload",
        progress: 0,
        label: "Uploading",
        totalFiles: 1,
        completedFiles: 0,
        currentFile: "",
        abortController: new AbortController(),
      },
    ]);
    const row = renderHook(() => useBackgroundTask("local"), { wrapper });
    act(() =>
      tasks.uploads.set((items) =>
        items.map((item) => ({ ...item, taskId: "server", progress: 20 })),
      ),
    );
    await waitFor(() => expect(row.result.current?.progress).toBe(20));
    expect(
      queryClient.getQueryData(backgroundTaskKeys.task("anonymous", "local")),
    ).toBeUndefined();
    expect(
      queryClient.getQueryData(backgroundTaskKeys.task("anonymous", "server")),
    ).toMatchObject({ id: "local", taskId: "server" });
  });

  it("reads indexer progress, summary and flags from the same cache", async () => {
    const { tasks, wrapper } = setup();
    const indexer = renderHook(() => useBackgroundTaskIndexer(), { wrapper });
    const flag = renderHook(() => useIsIndexing(), { wrapper });
    const downloads = renderHook(() => useBackgroundTasks("download"), {
      wrapper,
    });
    act(() => {
      tasks.indexers.set([
        {
          id: "index",
          taskId: "index",
          type: "indexer",
          path: "/",
          progress: 0,
          label: "Indexing",
          currentPath: "",
          phase: "connecting",
          dirsIndexed: 0,
          filesIndexed: 0,
          totalSize: 0,
          durationMs: 0,
          abortController: new AbortController(),
        },
      ]);
      tasks.setIsIndexerDialogOpen(true);
    });
    await waitFor(() => expect(flag.result.current).toBe(true));
    expect(indexer.result.current.indexers[0].id).toBe("index");
    expect(indexer.result.current.isIndexerDialogOpen).toBe(true);
    expect(downloads.result.current).toEqual([]);
    act(() => {
      tasks.setLastIndexerError("failed");
      tasks.indexers.set([]);
    });
    await waitFor(() => expect(flag.result.current).toBe(false));
    expect(indexer.result.current.lastIndexerError).toBe("failed");
  });

  it("keeps all actions identical across parent renders and cache writes, then clears the user cache on unmount", async () => {
    const { queryClient, tasks } = setup();
    const auth = createAuthContextValue();
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={auth}>
          <BackgroundTasksProvider>{children}</BackgroundTasksProvider>
        </AuthContext.Provider>
      </QueryClientProvider>
    );
    const hook = renderHook(() => useBackgroundTaskActions(), { wrapper });
    const first = hook.result.current;
    act(() => {
      first.openIndexerDialog();
      tasks.downloads.set([download("a")]);
    });
    hook.rerender();
    expect(hook.result.current).toBe(first);
    hook.unmount();
    expect(
      queryClient.getQueriesData({
        queryKey: backgroundTaskKeys.all("anonymous"),
      }),
    ).toEqual([]);
  });
});
