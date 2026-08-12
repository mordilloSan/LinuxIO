import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { PkgUpdateProgress, Stream, TaskProgress } from "@/api";
import type { TaskStreamActionMockConfig } from "@/test/taskStreamAction";

const apiMocks = vi.hoisted(() => ({
  cancelTask: vi.fn(),
  listTasks: vi.fn(),
  openTaskWatchStream: vi.fn(),
  updatePackages: vi.fn(),
}));

const streamMocks = vi.hoisted(() => ({
  runStream: vi.fn(),
  streamActionConfigs: [] as (TaskStreamActionMockConfig | undefined)[],
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  const { createTaskStreamActionMock } =
    await import("@/test/taskStreamAction");
  return {
    ...actual,
    call: (route: string, request?: unknown) =>
      route === "tasks.list"
        ? apiMocks.listTasks(request)
        : actual.call(route as never, request as never),
    useCallMutation: () => ({
      isPending: false,
      mutate: (request: unknown) => apiMocks.cancelTask(request),
      mutateAsync: apiMocks.cancelTask,
    }),
    openTaskWatchStream: apiMocks.openTaskWatchStream,
    linuxio: {
      ...actual.linuxio,
      tasks: {
        ...actual.linuxio.tasks,
        list: apiMocks.listTasks,
        cancel: actual.linuxio.tasks.cancel,
      },
      packages: {
        ...actual.linuxio.packages,
        update: {
          useTaskStreamAction: (config?: TaskStreamActionMockConfig) => {
            streamMocks.streamActionConfigs.push(config);
            return {
              isPending: false,
              ...createTaskStreamActionMock(
                {
                  openStream: apiMocks.openTaskWatchStream,
                  runStream: streamMocks.runStream,
                  submit: apiMocks.updatePackages,
                },
                config,
              ),
            };
          },
        },
      },
    },
  };
});

const { LinuxIOError } = await import("@/api");
const { TASK_TYPE_PACKAGE_UPDATE } =
  await import("@/constants/backgroundTaskTypes");
const {
  hasTerminalFeedbackOwner,
  markTerminalFeedbackEmitted,
  resetTerminalTaskFeedback,
} = await import("@/hooks/backgroundTasks/terminalTaskFeedback");
const { usePackageUpdater } = await import("@/hooks/usePackageUpdater");
const { act, renderHook } = await import("@/test/render");

function createStream(overrides: Partial<Stream> = {}): Stream {
  return {
    abort: vi.fn(),
    close: vi.fn(),
    id: 1,
    onClose: null,
    onData: null,
    onProgress: null,
    onResult: null,
    resize: vi.fn(),
    status: "open",
    type: "request",
    write: vi.fn(),
    ...overrides,
  };
}

function packageTaskProgress(
  detail: PkgUpdateProgress,
): TaskProgress<PkgUpdateProgress> {
  return {
    percentage: detail.percentage,
    phase: detail.status ?? detail.type,
    message: detail.message ?? detail.status,
    detail,
  };
}

describe("usePackageUpdater", () => {
  beforeEach(() => {
    apiMocks.cancelTask.mockResolvedValue(undefined);
    apiMocks.listTasks.mockResolvedValue([]);
    apiMocks.listTasks.mockClear();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
    streamMocks.streamActionConfigs.length = 0;
    resetTerminalTaskFeedback();
  });

  it("updates one package through the task stream and drives the progress bar", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "task-1",
      state: "running",
    });
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    streamMocks.runStream.mockImplementation(async (_stream, handlers) => {
      handlers.onProgress(
        packageTaskProgress({ type: "percentage", percentage: 40 }),
      );
    });
    const { result } = renderHook(() => usePackageUpdater());

    await act(async () => {
      await result.current.updateOne("nginx;1.24.0;amd64;ubuntu");
    });

    expect(apiMocks.updatePackages).toHaveBeenCalledWith({
      packageIds: ["nginx;1.24.0;amd64;ubuntu"],
    });

    expect(result.current.progress).toBe(100);
    expect(result.current.updatingPackage).toBeNull();
    expect(result.current.status).toBeNull();
    // The page keeps its manifest invalidation as the direct completion path.
    const config = streamMocks.streamActionConfigs.at(-1);
    expect(config).not.toHaveProperty("invalidates", []);
  });

  it("reports single-package update failures with the package name", async () => {
    apiMocks.updatePackages.mockRejectedValue(new Error("permission denied"));
    const { result } = renderHook(() => usePackageUpdater());

    await act(async () => {
      await result.current.updateOne("curl;8.0;amd64;ubuntu");
    });

    expect(result.current.error).toBe(
      "Failed to update curl: permission denied",
    );
    expect(result.current.updatingPackage).toBeNull();
  });

  it("reports a cancellation from another surface as a cancel, not a failure", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "task-canceled-elsewhere",
      state: "running",
    });
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    streamMocks.runStream.mockRejectedValue(
      new LinuxIOError("operation aborted", 499),
    );
    const { result } = renderHook(() => usePackageUpdater());

    await act(async () => {
      await result.current.updateOne("curl;8.0;amd64;ubuntu");
    });

    expect(result.current.error).toBe("Update cancelled");
    expect(result.current.updatingPackage).toBeNull();
    expect(result.current.status).toBeNull();
  });

  it("drives update-all state from stream progress and keeps global progress monotonic", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "task-1",
      state: "running",
    });
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    streamMocks.runStream.mockImplementation(async (_stream, handlers) => {
      handlers.onProgress(
        packageTaskProgress({ type: "percentage", percentage: 40 }),
      );
      handlers.onProgress(
        packageTaskProgress({
          type: "status",
          status: "Installing packages",
          percentage: 25,
        }),
      );
      handlers.onProgress(
        packageTaskProgress({
          type: "item_progress",
          package_id: "nginx;1.24.0;amd64;ubuntu",
          status: "Configuring",
          item_pct: 10,
        }),
      );
    });
    const { result } = renderHook(() => usePackageUpdater());

    await act(async () => {
      await result.current.updateAll([
        "nginx;1.24.0;amd64;ubuntu",
        "curl;8.0;amd64;ubuntu",
      ]);
    });

    expect(apiMocks.updatePackages).toHaveBeenCalledWith({
      packageIds: ["nginx;1.24.0;amd64;ubuntu", "curl;8.0;amd64;ubuntu"],
    });
    expect(apiMocks.openTaskWatchStream).toHaveBeenCalledWith("task-1");
    expect(result.current.progress).toBe(100);
    expect(result.current.eventLog).toEqual([
      "Initializing update transaction",
      "Installing packages",
      "Finished",
    ]);
    expect(result.current.updatingPackage).toBeNull();
    expect(result.current.status).toBeNull();
  });

  it("uses aggregate progress carried by package failure messages", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "task-progress-message",
      state: "running",
    });
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    let finishStream!: () => void;
    streamMocks.runStream.mockImplementation(
      (_stream, handlers) =>
        new Promise<void>((resolve) => {
          handlers.onProgress(
            packageTaskProgress({
              type: "message",
              message:
                "Failed to update curl. Continuing with remaining updates.",
              percentage: 50,
            }),
          );
          finishStream = resolve;
        }),
    );
    const { result } = renderHook(() => usePackageUpdater());

    let promise!: Promise<void>;
    await act(async () => {
      promise = result.current.updateAll(["curl", "nginx"]);
      await Promise.resolve();
    });

    expect(result.current.progress).toBe(50);

    await act(async () => {
      finishStream();
      await promise;
    });
    expect(result.current.updatingPackage).toBeNull();
  });

  it("requests backend cancellation and waits for its terminal frame", async () => {
    const stream = createStream();
    apiMocks.updatePackages.mockResolvedValue({
      id: "task-2",
      state: "running",
    });
    apiMocks.openTaskWatchStream.mockReturnValue(stream);
    let rejectStream!: (error: unknown) => void;
    streamMocks.runStream.mockImplementation(
      () =>
        new Promise((_resolve, reject) => {
          rejectStream = reject;
        }),
    );
    const { result } = renderHook(() => usePackageUpdater());

    let promise!: Promise<void>;
    await act(async () => {
      promise = result.current.updateAll(["nginx"]);
      await Promise.resolve();
    });
    await vi.waitFor(() => expect(streamMocks.runStream).toHaveBeenCalled());

    act(() => result.current.cancelUpdate());

    expect(stream.abort).not.toHaveBeenCalled();
    expect(apiMocks.cancelTask).toHaveBeenCalledWith({ taskId: "task-2" });
    expect(result.current.canCancel).toBe(false);
    expect(result.current.isUpdating).toBe(true);
    expect(result.current.error).toBeNull();
    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(true);

    await act(async () => {
      rejectStream(new LinuxIOError("operation aborted", 499));
      await promise;
    });

    expect(result.current.error).toBe("Update cancelled");
    expect(result.current.updatingPackage).toBeNull();
    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(false);
  });

  it("re-enables cancel when the backend rejects the cancellation request", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "task-cancel-rejected",
      state: "running",
    });
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    apiMocks.cancelTask.mockRejectedValueOnce(new Error("cancel failed"));
    let finishStream!: () => void;
    streamMocks.runStream.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finishStream = resolve;
        }),
    );
    const { result } = renderHook(() => usePackageUpdater());

    let promise!: Promise<void>;
    await act(async () => {
      promise = result.current.updateAll(["nginx"]);
      await Promise.resolve();
    });
    act(() => result.current.cancelUpdate());

    await vi.waitFor(() => expect(result.current.canCancel).toBe(true));
    expect(result.current.isUpdating).toBe(true);
    expect(result.current.error).toBeNull();

    await act(async () => {
      finishStream();
      await promise;
    });
  });

  it("leaves cancel inert once the transaction has finished", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "task-fast",
      state: "running",
    });
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    streamMocks.runStream.mockResolvedValue(undefined);
    const { result } = renderHook(() => usePackageUpdater());

    await act(async () => {
      await result.current.updateAll(["nginx;1.0;amd64;ubuntu"]);
    });

    act(() => result.current.cancelUpdate());

    expect(apiMocks.cancelTask).not.toHaveBeenCalled();
    expect(result.current.error).toBeNull();
    expect(result.current.updatingPackage).toBeNull();
  });

  it("drops the task handle once a failed run settles", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "task-failed",
      state: "running",
    });
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    streamMocks.runStream.mockRejectedValue(new Error("dpkg exploded"));
    const { result } = renderHook(() => usePackageUpdater());

    await act(async () => {
      await result.current.updateAll(["nginx;1.0;amd64;ubuntu"]);
    });
    expect(result.current.error).toBe("Failed to update nginx: dpkg exploded");

    // onSettled owns this cleanup: a stale task id would let cancel fire against
    // a dead task and overwrite the failure with "Update cancelled".
    act(() => result.current.cancelUpdate());

    expect(apiMocks.cancelTask).not.toHaveBeenCalled();
    expect(result.current.error).toBe("Failed to update nginx: dpkg exploded");
  });

  it("falls back to a generic message when the backend error is empty", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "task-empty-error",
      state: "running",
    });
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    streamMocks.runStream.mockRejectedValue(new LinuxIOError("", 500));
    const { result } = renderHook(() => usePackageUpdater());

    await act(async () => {
      await result.current.updateOne("nginx;1.0;amd64;ubuntu");
    });

    expect(result.current.error).toBe("Failed to update nginx: Update failed");
  });

  it("claims global feedback ownership only while a run is live", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "task-claimed",
      state: "running",
    });
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    let finishStream!: () => void;
    streamMocks.runStream.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finishStream = resolve;
        }),
    );
    const { result } = renderHook(() => usePackageUpdater());
    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(false);

    let promise!: Promise<void>;
    await act(async () => {
      promise = result.current.updateAll(["nginx;1.0;amd64;ubuntu"]);
      await Promise.resolve();
    });

    // While this page tracks the run, the global handler must stay silent —
    // the inline alert is the report.
    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(true);

    await act(async () => {
      finishStream();
      await promise;
    });

    // Settled runs hand ownership back immediately (no trailing window).
    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(false);
  });

  it("releases the feedback claim when unmounted mid-run", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "task-abandoned",
      state: "running",
    });
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    streamMocks.runStream.mockImplementation(
      () => new Promise(() => undefined),
    );
    const { result, unmount } = renderHook(() => usePackageUpdater());

    void result.current.updateAll(["nginx;1.0;amd64;ubuntu"]);
    await vi.waitFor(() => expect(streamMocks.runStream).toHaveBeenCalled());
    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(true);

    unmount();

    // The page can no longer paint feedback, so a failure arriving after
    // navigation must be free to toast globally.
    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(false);
  });

  it("marks a painted failure so the global fallback stays silent", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "task-painted",
      state: "running",
    });
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    streamMocks.runStream.mockRejectedValue(new Error("dpkg exploded"));
    const { result } = renderHook(() => usePackageUpdater());

    await act(async () => {
      await result.current.updateAll(["nginx;1.0;amd64;ubuntu"]);
    });

    expect(result.current.error).toBe("Failed to update nginx: dpkg exploded");
    // The claim is already released, but the task id was marked as surfaced, so
    // the global copy of the terminal event cannot toast a duplicate.
    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(false);
    expect(markTerminalFeedbackEmitted("task-painted")).toBe(false);
  });

  it("claims ownership when adopting a recovered update", async () => {
    apiMocks.listTasks.mockResolvedValue([
      {
        id: "recovered-claim",
        type: "packages.update",
        state: "running",
        metadata: { packageIds: ["nginx;1.0;amd64;ubuntu"] },
      },
    ]);
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    streamMocks.runStream.mockImplementation(
      () => new Promise(() => undefined),
    );
    renderHook(() => usePackageUpdater());

    await vi.waitFor(() =>
      expect(apiMocks.openTaskWatchStream).toHaveBeenCalledWith(
        "recovered-claim",
      ),
    );

    expect(hasTerminalFeedbackOwner(TASK_TYPE_PACKAGE_UPDATE)).toBe(true);
  });

  it("adopts a recovered update through completion", async () => {
    const stream = createStream();
    apiMocks.listTasks.mockResolvedValue([
      {
        id: "recovered-1",
        type: "packages.update",
        state: "running",
        metadata: { packageIds: ["nginx;1.0;amd64;ubuntu"] },
      },
    ]);
    apiMocks.openTaskWatchStream.mockReturnValue(stream);
    streamMocks.runStream.mockResolvedValue(undefined);
    const { result } = renderHook(() => usePackageUpdater());

    await vi.waitFor(() =>
      expect(apiMocks.openTaskWatchStream).toHaveBeenCalledWith("recovered-1"),
    );
    await vi.waitFor(() => expect(result.current.isUpdating).toBe(false));
    expect(result.current.updatingPackage).toBeNull();
    expect(result.current.status).toBeNull();
  });

  it("cancels a recovered update against the adopted task id", async () => {
    const stream = createStream();
    let rejectStream!: (error: unknown) => void;
    apiMocks.listTasks.mockResolvedValue([
      {
        id: "recovered-cancel",
        type: "packages.update",
        state: "running",
        metadata: { packageIds: ["nginx;1.0;amd64;ubuntu"] },
      },
    ]);
    apiMocks.openTaskWatchStream.mockReturnValue(stream);
    streamMocks.runStream.mockImplementation(
      () =>
        new Promise((_resolve, reject) => {
          rejectStream = reject;
        }),
    );
    const { result } = renderHook(() => usePackageUpdater());

    await vi.waitFor(() =>
      expect(apiMocks.openTaskWatchStream).toHaveBeenCalledWith(
        "recovered-cancel",
      ),
    );

    act(() => result.current.cancelUpdate());

    expect(apiMocks.cancelTask).toHaveBeenCalledWith({
      taskId: "recovered-cancel",
    });
    expect(stream.abort).not.toHaveBeenCalled();
    expect(result.current.isUpdating).toBe(true);
    expect(result.current.canCancel).toBe(false);

    await act(async () => {
      rejectStream(new LinuxIOError("operation aborted", 499));
    });
    expect(result.current.error).toBe("Update cancelled");
  });

  it("surfaces a recovered update failure", async () => {
    apiMocks.listTasks.mockResolvedValue([
      {
        id: "recovered-2",
        type: "packages.update",
        state: "running",
        metadata: { packageIds: ["curl;1.0;amd64;ubuntu"] },
      },
    ]);
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    streamMocks.runStream.mockRejectedValue(new Error("backend failed"));
    const { result } = renderHook(() => usePackageUpdater());

    await vi.waitFor(() =>
      expect(result.current.error).toBe(
        "Failed to update curl: backend failed",
      ),
    );
    expect(result.current.updatingPackage).toBeNull();
  });

  it("closes a stream that opens after the controller unmounts", async () => {
    const stream = createStream();
    let resolveSubmit!: (task: { id: string; state: string }) => void;
    apiMocks.updatePackages.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveSubmit = resolve;
        }),
    );
    apiMocks.openTaskWatchStream.mockReturnValue(stream);
    streamMocks.runStream.mockResolvedValue(undefined);
    const { result, unmount } = renderHook(() => usePackageUpdater());
    void result.current.updateAll(["nginx"]);
    await vi.waitFor(() => expect(apiMocks.updatePackages).toHaveBeenCalled());
    unmount();

    // The task was still being created at unmount, so onOpen lands detached and
    // has to close the stream itself.
    resolveSubmit({ id: "late-open", state: "running" });
    await vi.waitFor(() => expect(stream.close).toHaveBeenCalledTimes(1));
  });

  it("attaches a recovered update only once across controller rerenders", async () => {
    apiMocks.listTasks.mockResolvedValue([
      {
        id: "recovered-once",
        type: "packages.update",
        state: "running",
        metadata: { packageIds: ["nginx;1.0;amd64;ubuntu"] },
      },
    ]);
    apiMocks.openTaskWatchStream.mockReturnValue(createStream());
    streamMocks.runStream.mockImplementation(
      () => new Promise(() => undefined),
    );
    const { rerender } = renderHook(() => usePackageUpdater());

    await vi.waitFor(() =>
      expect(apiMocks.openTaskWatchStream).toHaveBeenCalledWith(
        "recovered-once",
      ),
    );
    rerender();
    await Promise.resolve();

    expect(apiMocks.listTasks).toHaveBeenCalledTimes(1);
    expect(apiMocks.openTaskWatchStream).toHaveBeenCalledTimes(1);
    expect(streamMocks.runStream).toHaveBeenCalledTimes(1);
  });

  it("closes a live stream on unmount without treating it as terminal", async () => {
    const stream = createStream();
    apiMocks.updatePackages.mockResolvedValue({
      id: "close-before-terminal",
      state: "running",
    });
    streamMocks.runStream.mockImplementation(
      () => new Promise(() => undefined),
    );
    apiMocks.openTaskWatchStream.mockReturnValue(stream);
    const { result, unmount } = renderHook(() => usePackageUpdater());

    void result.current.updateAll(["nginx;1.0;amd64;ubuntu"]);
    await vi.waitFor(() => expect(streamMocks.runStream).toHaveBeenCalled());
    unmount();

    expect(stream.close).toHaveBeenCalledTimes(1);
  });
});
