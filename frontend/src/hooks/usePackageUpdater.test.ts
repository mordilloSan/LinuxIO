import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api";
import type { JobStreamActionMockConfig } from "@/test/jobStreamAction";

const apiMocks = vi.hoisted(() => ({
  cancelJob: vi.fn(),
  listJobs: vi.fn(),
  openJobAttachStream: vi.fn(),
  updatePackages: vi.fn(),
}));

const streamMocks = vi.hoisted(() => ({
  runStream: vi.fn(),
  streamActionConfigs: [] as (JobStreamActionMockConfig | undefined)[],
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  const { createJobStreamActionMock } = await import("@/test/jobStreamAction");
  return {
    ...actual,
    openJobAttachStream: apiMocks.openJobAttachStream,
    linuxio: {
      ...actual.linuxio,
      jobs: {
        ...actual.linuxio.jobs,
        list: apiMocks.listJobs,
        cancel: {
          useJobAction: () => ({
            isPending: false,
            mutate: (request: unknown) => apiMocks.cancelJob(request),
            mutateAsync: apiMocks.cancelJob,
          }),
        },
      },
      packages: {
        ...actual.linuxio.packages,
        update: {
          useJobStreamAction: (config?: JobStreamActionMockConfig) => {
            streamMocks.streamActionConfigs.push(config);
            return {
              isPending: false,
              ...createJobStreamActionMock(
                {
                  openStream: apiMocks.openJobAttachStream,
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

async function flushMinimumVisibleProgress(promise: Promise<unknown>) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1500);
    await promise;
  });
}

describe("usePackageUpdater", () => {
  beforeEach(() => {
    apiMocks.listJobs.mockResolvedValue([]);
    apiMocks.listJobs.mockClear();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
    streamMocks.streamActionConfigs.length = 0;
  });

  it("updates one package through the job stream and drives the progress bar", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    apiMocks.updatePackages.mockResolvedValue({
      id: "job-1",
      state: "running",
    });
    apiMocks.openJobAttachStream.mockReturnValue(createStream());
    streamMocks.runStream.mockImplementation(async (_stream, handlers) => {
      handlers.onProgress({ type: "percentage", percentage: 40 });
    });
    const { result } = renderHook(() => usePackageUpdater());

    let promise!: Promise<void>;
    await act(async () => {
      promise = result.current.updateOne("nginx;1.24.0;amd64;ubuntu");
      await Promise.resolve();
    });

    expect(result.current.updatingPackage).toBe("nginx");
    expect(apiMocks.updatePackages).toHaveBeenCalledWith({
      packageIds: ["nginx;1.24.0;amd64;ubuntu"],
    });

    await flushMinimumVisibleProgress(promise);

    expect(result.current.progress).toBe(100);
    expect(result.current.updatingPackage).toBeNull();
    expect(result.current.status).toBeNull();
    // The page keeps manifest invalidation and job ownership: opting out would
    // leave the updates list waiting on the global events stream alone.
    const config = streamMocks.streamActionConfigs.at(-1);
    expect(config).not.toHaveProperty("invalidates", []);
    expect(config).not.toHaveProperty("markHandled", false);
  });

  it("reports single-package update failures with the package name", async () => {
    vi.useFakeTimers();
    apiMocks.updatePackages.mockRejectedValue(new Error("permission denied"));
    const { result } = renderHook(() => usePackageUpdater());

    const promise = result.current.updateOne("curl;8.0;amd64;ubuntu");
    await flushMinimumVisibleProgress(promise);

    expect(result.current.error).toBe(
      "Failed to update curl: permission denied",
    );
    expect(result.current.updatingPackage).toBeNull();
  });

  it("reports a cancellation from another surface as a cancel, not a failure", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "job-canceled-elsewhere",
      state: "running",
    });
    apiMocks.openJobAttachStream.mockReturnValue(createStream());
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
    vi.useFakeTimers();
    vi.setSystemTime(0);
    apiMocks.updatePackages.mockResolvedValue({
      id: "job-1",
      state: "running",
    });
    apiMocks.openJobAttachStream.mockReturnValue(createStream());
    streamMocks.runStream.mockImplementation(async (_stream, handlers) => {
      handlers.onProgress({ type: "percentage", percentage: 40 });
      handlers.onProgress({
        type: "status",
        status: "Installing packages",
        percentage: 25,
      });
      handlers.onProgress({
        type: "item_progress",
        package_id: "nginx;1.24.0;amd64;ubuntu",
        status: "Configuring",
        item_pct: 10,
      });
    });
    const { result } = renderHook(() => usePackageUpdater());

    const promise = result.current.updateAll([
      "nginx;1.24.0;amd64;ubuntu",
      "curl;8.0;amd64;ubuntu",
    ]);
    await flushMinimumVisibleProgress(promise);

    expect(apiMocks.updatePackages).toHaveBeenCalledWith({
      packageIds: ["nginx;1.24.0;amd64;ubuntu", "curl;8.0;amd64;ubuntu"],
    });
    expect(apiMocks.openJobAttachStream).toHaveBeenCalledWith("job-1");
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
    vi.useFakeTimers();
    vi.setSystemTime(0);
    apiMocks.updatePackages.mockResolvedValue({
      id: "job-progress-message",
      state: "running",
    });
    apiMocks.openJobAttachStream.mockReturnValue(createStream());
    let finishStream!: () => void;
    streamMocks.runStream.mockImplementation(
      (_stream, handlers) =>
        new Promise<void>((resolve) => {
          handlers.onProgress({
            type: "message",
            message:
              "Failed to update curl. Continuing with remaining updates.",
            percentage: 50,
          });
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

    act(() => finishStream());
    await flushMinimumVisibleProgress(promise);
  });

  it("does not hold the panel past an update that already took the minimum", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    apiMocks.updatePackages.mockResolvedValue({
      id: "job-slow",
      state: "running",
    });
    apiMocks.openJobAttachStream.mockReturnValue(createStream());
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
      promise = result.current.updateAll(["nginx;1.0;amd64;ubuntu"]);
      await Promise.resolve();
    });
    let settled = false;
    void promise.then(() => {
      settled = true;
    });

    // The transaction itself ran longer than MIN_PROGRESS_VISIBLE_MS, so the
    // panel has already been visible long enough and must clear immediately.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    await act(async () => {
      finishStream();
      await vi.advanceTimersByTimeAsync(1);
    });
    const settledWithoutExtraHold = settled;
    // Drain any hold before asserting, so a regression fails here instead of
    // leaking a pending timer into the next test.
    await flushMinimumVisibleProgress(promise);

    expect(settledWithoutExtraHold).toBe(true);
    expect(result.current.updatingPackage).toBeNull();
    expect(result.current.status).toBeNull();
  });

  it("cancels active update streams and backend jobs", async () => {
    const stream = createStream();
    apiMocks.updatePackages.mockResolvedValue({
      id: "job-2",
      state: "running",
    });
    apiMocks.openJobAttachStream.mockReturnValue(stream);
    apiMocks.cancelJob.mockResolvedValue(undefined);
    streamMocks.runStream.mockImplementation(
      () => new Promise(() => undefined),
    );
    const { result } = renderHook(() => usePackageUpdater());

    void act(() => {
      void result.current.updateAll(["nginx"]);
    });
    await vi.waitFor(() => expect(streamMocks.runStream).toHaveBeenCalled());

    act(() => result.current.cancelUpdate());

    expect(stream.abort).toHaveBeenCalledTimes(1);
    expect(apiMocks.cancelJob).toHaveBeenCalledWith({ jobId: "job-2" });
    expect(result.current.error).toBe("Update cancelled");
    expect(result.current.updatingPackage).toBeNull();
  });

  it("leaves cancel inert once the transaction has finished", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    apiMocks.updatePackages.mockResolvedValue({
      id: "job-fast",
      state: "running",
    });
    apiMocks.openJobAttachStream.mockReturnValue(createStream());
    streamMocks.runStream.mockResolvedValue(undefined);
    const { result } = renderHook(() => usePackageUpdater());

    let promise!: Promise<void>;
    await act(async () => {
      promise = result.current.updateAll(["nginx;1.0;amd64;ubuntu"]);
      await Promise.resolve();
    });

    // Inside the minimum-visible hold the panel still shows "Finished", so the
    // cancel button is live — but there is nothing left to cancel.
    expect(result.current.status).toBe("Finished");
    act(() => result.current.cancelUpdate());
    const cancelCalls = apiMocks.cancelJob.mock.calls.length;
    const errorDuringHold = result.current.error;
    await flushMinimumVisibleProgress(promise);

    expect(cancelCalls).toBe(0);
    expect(errorDuringHold).toBeNull();
    expect(result.current.updatingPackage).toBeNull();
  });

  it("drops the job handle once a failed run settles", async () => {
    apiMocks.updatePackages.mockResolvedValue({
      id: "job-failed",
      state: "running",
    });
    apiMocks.openJobAttachStream.mockReturnValue(createStream());
    streamMocks.runStream.mockRejectedValue(new Error("dpkg exploded"));
    const { result } = renderHook(() => usePackageUpdater());

    await act(async () => {
      await result.current.updateAll(["nginx;1.0;amd64;ubuntu"]);
    });
    expect(result.current.error).toBe("Failed to update nginx: dpkg exploded");

    // onSettled owns this cleanup: a stale job id would let cancel fire against
    // a dead job and overwrite the failure with "Update cancelled".
    act(() => result.current.cancelUpdate());

    expect(apiMocks.cancelJob).not.toHaveBeenCalled();
    expect(result.current.error).toBe("Failed to update nginx: dpkg exploded");
  });

  it("adopts a recovered update through completion", async () => {
    vi.useFakeTimers();
    const stream = createStream();
    apiMocks.listJobs.mockResolvedValue([
      {
        id: "recovered-1",
        type: "packages.update",
        state: "running",
        request: { packageIds: ["nginx;1.0;amd64;ubuntu"] },
      },
    ]);
    apiMocks.openJobAttachStream.mockReturnValue(stream);
    streamMocks.runStream.mockResolvedValue(undefined);
    const { result } = renderHook(() => usePackageUpdater());

    await vi.waitFor(() =>
      expect(apiMocks.openJobAttachStream).toHaveBeenCalledWith("recovered-1"),
    );
    await act(async () => vi.advanceTimersByTimeAsync(1500));
    expect(result.current.updatingPackage).toBeNull();
    expect(result.current.status).toBeNull();
  });

  it("cancels a recovered update against the adopted job id", async () => {
    const stream = createStream();
    apiMocks.listJobs.mockResolvedValue([
      {
        id: "recovered-cancel",
        type: "packages.update",
        state: "running",
        request: { packageIds: ["nginx;1.0;amd64;ubuntu"] },
      },
    ]);
    apiMocks.openJobAttachStream.mockReturnValue(stream);
    streamMocks.runStream.mockImplementation(
      () => new Promise(() => undefined),
    );
    const { result } = renderHook(() => usePackageUpdater());

    await vi.waitFor(() =>
      expect(apiMocks.openJobAttachStream).toHaveBeenCalledWith(
        "recovered-cancel",
      ),
    );

    act(() => result.current.cancelUpdate());

    // attach fires onJobStart just like a fresh start, so the page-level cancel
    // reaches the backend instead of only detaching this stream.
    expect(apiMocks.cancelJob).toHaveBeenCalledWith({
      jobId: "recovered-cancel",
    });
    expect(stream.abort).toHaveBeenCalledTimes(1);
  });

  it("surfaces a recovered update failure", async () => {
    apiMocks.listJobs.mockResolvedValue([
      {
        id: "recovered-2",
        type: "packages.update",
        state: "running",
        request: { packageIds: ["curl;1.0;amd64;ubuntu"] },
      },
    ]);
    apiMocks.openJobAttachStream.mockReturnValue(createStream());
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
    let resolveSubmit!: (job: { id: string; state: string }) => void;
    apiMocks.updatePackages.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveSubmit = resolve;
        }),
    );
    apiMocks.openJobAttachStream.mockReturnValue(stream);
    streamMocks.runStream.mockResolvedValue(undefined);
    const { result, unmount } = renderHook(() => usePackageUpdater());
    void result.current.updateAll(["nginx"]);
    await vi.waitFor(() => expect(apiMocks.updatePackages).toHaveBeenCalled());
    unmount();

    // The job was still being created at unmount, so onOpen lands detached and
    // has to close the stream itself.
    resolveSubmit({ id: "late-open", state: "running" });
    await vi.waitFor(() => expect(stream.close).toHaveBeenCalledTimes(1));
  });

  it("attaches a recovered update only once across controller rerenders", async () => {
    apiMocks.listJobs.mockResolvedValue([
      {
        id: "recovered-once",
        type: "packages.update",
        state: "running",
        request: { packageIds: ["nginx;1.0;amd64;ubuntu"] },
      },
    ]);
    apiMocks.openJobAttachStream.mockReturnValue(createStream());
    streamMocks.runStream.mockImplementation(
      () => new Promise(() => undefined),
    );
    const { rerender } = renderHook(() => usePackageUpdater());

    await vi.waitFor(() =>
      expect(apiMocks.openJobAttachStream).toHaveBeenCalledWith(
        "recovered-once",
      ),
    );
    rerender();
    await Promise.resolve();

    expect(apiMocks.listJobs).toHaveBeenCalledTimes(1);
    expect(apiMocks.openJobAttachStream).toHaveBeenCalledTimes(1);
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
    apiMocks.openJobAttachStream.mockReturnValue(stream);
    const { result, unmount } = renderHook(() => usePackageUpdater());

    void result.current.updateAll(["nginx;1.0;amd64;ubuntu"]);
    await vi.waitFor(() => expect(streamMocks.runStream).toHaveBeenCalled());
    unmount();

    expect(stream.close).toHaveBeenCalledTimes(1);
  });
});
