import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Stream, TaskSnapshot } from "@/api";
import { backgroundTaskKeys } from "@/api/background-task-cache";
import {
  TASK_TYPE_FILE_COMPRESS,
  TASK_TYPE_FILE_INDEXER,
  TASK_TYPE_DOCKER_UPDATE,
  TASK_TYPE_FILE_UPLOAD_BATCH,
} from "@/constants/backgroundTaskTypes";
import { createTestQueryClient } from "@/test/render";

import { useBackgroundTaskRuntime } from "./useBackgroundTaskRuntime";
import { useGenericBackgroundTasks } from "./useGenericBackgroundTasks";
import { useIndexerTasks } from "./useIndexerTasks";
import { useRecoveredTasks } from "./useRecoveredTasks";
import { useTransferTasks } from "./useTransferTasks";
import { useUploadTasks } from "./useUploadTasks";

const mocks = vi.hoisted(() => ({
  upload: vi.fn(),
  data: vi.fn(),
  compress: vi.fn(),
  index: vi.fn(),
  events: vi.fn(),
  watch: vi.fn(),
  cancel: vi.fn(),
  refreshCapabilities: vi.fn(),
}));
vi.mock("@/api", async (importOriginal) => {
  const api = await importOriginal<typeof import("@/api")>();
  return {
    ...api,
    isConnected: () => true,
    useStreamMux: () => ({ status: "open" }),
    openTaskEventsStream: mocks.events,
    openTaskWatchStream: mocks.watch,
    openTaskDataStream: mocks.data,
    call: mocks.cancel,
    linuxio: {
      ...api.linuxio,
      filebrowser: {
        ...api.linuxio.filebrowser,
        upload_batch: mocks.upload,
        compress: mocks.compress,
        index: mocks.index,
      },
    },
  };
});
vi.mock("@/hooks/useAuth", () => ({
  default: () => ({ refreshCapabilities: mocks.refreshCapabilities }),
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}));

function stream(): Stream {
  return {
    id: 1,
    type: "request",
    status: "open",
    onProgress: null,
    onResult: null,
    onClose: null,
    onData: null,
    abort: vi.fn(),
    close: vi.fn(),
    resize: vi.fn(),
    write: vi.fn(),
  };
}
function snapshot(
  id: string,
  type: string,
  metadata: TaskSnapshot["metadata"] = {},
): TaskSnapshot {
  return {
    id,
    type,
    state: "running",
    metadata,
    created_at: "2026-09-21T00:00:00Z",
    updated_at: "2026-09-21T00:00:00Z",
  };
}
function setup() {
  const queryClient = createTestQueryClient();
  const events = stream();
  const watches = new Map<string, Stream>();
  mocks.events.mockReturnValue(events);
  mocks.watch.mockImplementation((id: string) => {
    const watch = stream();
    watches.set(id, watch);
    return watch;
  });
  mocks.cancel.mockResolvedValue(undefined);
  const hook = renderHook(
    () => {
      const runtime = useBackgroundTaskRuntime();
      const transfers = useTransferTasks(runtime);
      const uploads = useUploadTasks(runtime, () => 1024);
      const indexers = useIndexerTasks(runtime);
      const generic = useGenericBackgroundTasks(runtime);
      useRecoveredTasks(runtime, {
        recoverTransfer: transfers.recoverTransfer,
        indexers: indexers.recoveryControls,
        genericTasks: generic.recoveryControls,
      });
      return { runtime, transfers, indexers, generic, uploads };
    },
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      ),
    },
  );
  return { ...hook, queryClient, events, watches };
}
const frames = new Map<number, FrameRequestCallback>();
const flushFrames = () => {
  const callbacks = [...frames.values()];
  frames.clear();
  callbacks.forEach((callback) => callback(0));
};

describe("task streams write the shared cache", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    let frameId = 0;
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      frames.set(++frameId, callback);
      return frameId;
    });
    vi.stubGlobal("cancelAnimationFrame", (id: number) => frames.delete(id));
  });
  afterEach(() => {
    frames.clear();
    vi.unstubAllGlobals();
  });

  it("uses identical entries and progress handling for live and recovered transfers", async () => {
    const { result, queryClient, events, watches } = setup();
    const task = snapshot("archive", TASK_TYPE_FILE_COMPRESS, {
      path: "/data/archive.zip",
    });
    mocks.compress.mockResolvedValue(task);
    await act(async () =>
      result.current.transfers.startCompression({
        paths: [],
        archiveName: "archive.zip",
        destination: "/data",
      }),
    );
    // A real selection is required before the bridge task is started.
    await act(async () =>
      result.current.transfers.startCompression({
        paths: ["/source"],
        archiveName: "archive.zip",
        destination: "/data",
      }),
    );
    const key = backgroundTaskKeys.task("anonymous", task.id);
    expect(queryClient.getQueryData(key)).toMatchObject({
      id: task.id,
      type: "compression",
      progress: 0,
    });
    act(() => {
      watches.get(task.id)!.onProgress?.({
        percentage: 40,
        detail: { pct: 40, bytes: 40, total: 100 },
      } as never);
      flushFrames();
    });
    expect(queryClient.getQueryData(key)).toMatchObject({
      progress: 40,
      label: "Compressing archive.zip (40%)",
    });
    await act(async () => watches.get(task.id)!.onResult?.({ status: "ok" }));
    expect(queryClient.getQueryData(key)).toBeUndefined();
    await act(async () =>
      events.onProgress?.({
        task: {
          ...task,
          progress: {
            percentage: 40,
            detail: { pct: 40, bytes: 40, total: 100 },
          },
        },
        type: "task.progress",
      } as never),
    );
    expect(queryClient.getQueryData(key)).toMatchObject({
      id: task.id,
      taskId: task.id,
      type: "compression",
      progress: 40,
      label: "Compressing archive.zip (40%)",
    });
    const watchCount = mocks.watch.mock.calls.length;
    await act(async () =>
      events.onProgress?.({ task, type: "task.progress" } as never),
    );
    expect(mocks.watch).toHaveBeenCalledTimes(watchCount);
    await act(async () => result.current.transfers.cancelTransfer(task.id));
    expect(mocks.cancel).toHaveBeenCalledWith("tasks.cancel", {
      taskId: task.id,
    });
    expect(queryClient.getQueryData(key)).toBeUndefined();
    act(flushFrames);
    expect(queryClient.getQueryData(key)).toBeUndefined();
  });

  it("keeps a completed upload under its bridge key and suppresses recovery while its row remains visible", async () => {
    const { result, queryClient, events } = setup();
    const data = stream();
    mocks.data.mockReturnValue(data);
    const task = snapshot("upload", TASK_TYPE_FILE_UPLOAD_BATCH);
    mocks.upload.mockResolvedValue(task);
    let completion:
      | ReturnType<typeof result.current.uploads.startUpload>
      | undefined;
    await act(async () => {
      completion = result.current.uploads.startUpload(
        [{ relativePath: "folder", isDirectory: true }],
        "/data",
      );
    });
    const item = result.current.runtime.tasks.uploads.read()[0];
    expect(item.taskId).toBe(task.id);
    const key = backgroundTaskKeys.task("anonymous", task.id);
    expect(queryClient.getQueryData(key)).toMatchObject({
      id: item.id,
      type: "upload",
    });
    await act(async () => {
      data.onResult?.({ status: "ok", data: { succeeded: 1 } });
      await completion;
    });
    expect(queryClient.getQueryData(key)).toMatchObject({
      progress: 100,
      taskId: task.id,
      type: "upload",
    });
    await act(async () =>
      events.onProgress?.({ task, type: "task.progress" } as never),
    );
    expect(mocks.watch).not.toHaveBeenCalled();
    expect(result.current.runtime.tasks.backgroundTasks.read()).toEqual([]);
    act(() => result.current.uploads.cancelUpload(item.id));
    expect(mocks.cancel).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(key)).toBeUndefined();
  });

  it("seeds recovered indexer progress and keeps its terminal summary in the cache", async () => {
    const { queryClient, events, watches } = setup();
    const task = {
      ...snapshot("indexer", TASK_TYPE_FILE_INDEXER, { path: "/data" }),
      progress: {
        detail: { files_indexed: 12, bytes_indexed: 1024, phase: "indexing" },
      },
    };
    await act(async () =>
      events.onProgress?.({ task, type: "task.progress" } as never),
    );
    expect(
      queryClient.getQueryData(backgroundTaskKeys.task("anonymous", task.id)),
    ).toMatchObject({ type: "indexer", filesIndexed: 12, bytesIndexed: 1024 });
    await act(async () =>
      watches.get(task.id)!.onResult?.({
        status: "ok",
        data: {
          files_indexed: 12,
          dirs_indexed: 2,
          total_size: 1024,
          duration_ms: 10,
        },
      }),
    );
    expect(
      queryClient.getQueryData(backgroundTaskKeys.task("anonymous", task.id)),
    ).toBeUndefined();
    expect(
      queryClient.getQueryData(backgroundTaskKeys.indexer("anonymous")),
    ).toMatchObject({
      lastIndexerResult: { filesIndexed: 12, dirsIndexed: 2 },
      lastIndexerError: null,
    });
  });

  it("updates and removes generic recovered tasks without provider state", async () => {
    const { queryClient, events, watches } = setup();
    const task = snapshot("docker", TASK_TYPE_DOCKER_UPDATE);
    await act(async () =>
      events.onProgress?.({ task, type: "task.progress" } as never),
    );
    const key = backgroundTaskKeys.task("anonymous", task.id);
    act(() =>
      watches
        .get(task.id)!
        .onProgress?.({ percentage: 65, message: "Pulling image" } as never),
    );
    expect(queryClient.getQueryData(key)).toMatchObject({
      type: "task",
      progress: 65,
      label: "Pulling image",
    });
    await act(async () =>
      watches
        .get(task.id)!
        .onResult?.({ status: "error", error: "Pull failed" }),
    );
    expect(queryClient.getQueryData(key)).toBeUndefined();
  });
});
