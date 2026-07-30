import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api";

const apiMocks = vi.hoisted(() => ({
  cancelJob: vi.fn(),
  listJobs: vi.fn(),
  openJobAttachStream: vi.fn(),
  updatePackages: vi.fn(),
}));

const streamResultMocks = vi.hoisted(() => ({
  run: vi.fn(),
  streamActionConfigs: [] as unknown[],
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  interface StreamActionConfig {
    error?: (error: unknown, variables: unknown) => void;
    onJobStart?: (job: unknown, variables: unknown) => void;
    onOpen?: (stream: unknown, job: unknown, variables: unknown) => void;
    onProgress?: (progress: unknown, job: unknown, variables: unknown) => void;
    success?: (result: unknown, variables: unknown) => void;
  }
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
          // Mirrors useJobStreamAction: submit -> onJobStart -> attach ->
          // stream result, with the stream lifecycle driven by
          // streamResultMocks.run so tests control progress frames.
          useJobStreamAction: (config?: StreamActionConfig) => {
            streamResultMocks.streamActionConfigs.push(config);
            const run = async (request: unknown) => {
              let job: { id: string };
              try {
                job = await apiMocks.updatePackages(request);
              } catch (error) {
                config?.error?.(error, request);
                throw error;
              }
              config?.onJobStart?.(job, request);
              return streamResultMocks
                .run({
                  open: () => apiMocks.openJobAttachStream(job.id),
                  onOpen: (stream: unknown) =>
                    config?.onOpen?.(stream, job, request),
                  onProgress: (progress: unknown) =>
                    config?.onProgress?.(progress, job, request),
                })
                .then(
                  (result: unknown) => {
                    config?.success?.(result, request);
                    return result;
                  },
                  (error: unknown) => {
                    config?.error?.(error, request);
                    throw error;
                  },
                );
            };
            return {
              attach: (job: { id: string }, request: unknown) => {
                void streamResultMocks
                  .run({
                    open: () => apiMocks.openJobAttachStream(job.id),
                    onOpen: (stream: unknown) =>
                      config?.onOpen?.(stream, job, request),
                    onProgress: (progress: unknown) =>
                      config?.onProgress?.(progress, job, request),
                  })
                  .then(() => config?.success?.(undefined, request))
                  .catch((error: unknown) => config?.error?.(error, request));
              },
              isPending: false,
              mutate: (request: unknown) => {
                void run(request).catch(() => undefined);
              },
              mutateAsync: run,
            };
          },
        },
      },
    },
  };
});

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
    streamResultMocks.streamActionConfigs.length = 0;
  });

  it("updates one package through the job stream and drives the progress bar", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    apiMocks.updatePackages.mockResolvedValue({ id: "job-1" });
    apiMocks.openJobAttachStream.mockReturnValue(createStream());
    streamResultMocks.run.mockImplementation(async (options) => {
      const stream = options.open();
      options.onOpen?.(stream);
      options.onProgress?.({ type: "percentage", percentage: 40 });
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
    expect(streamResultMocks.streamActionConfigs.at(-1)).toMatchObject({
      invalidates: [],
      markHandled: false,
    });
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

  it("drives update-all state from stream progress and keeps global progress monotonic", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    apiMocks.updatePackages.mockResolvedValue({ id: "job-1" });
    apiMocks.openJobAttachStream.mockReturnValue(createStream());
    streamResultMocks.run.mockImplementation(async (options) => {
      const stream = options.open();
      options.onOpen?.(stream);
      options.onProgress?.({ type: "percentage", percentage: 40 });
      options.onProgress?.({
        type: "status",
        status: "Installing packages",
        percentage: 25,
      });
      options.onProgress?.({
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

  it("cancels active update streams and backend jobs", async () => {
    const stream = createStream();
    apiMocks.updatePackages.mockResolvedValue({ id: "job-2" });
    apiMocks.openJobAttachStream.mockReturnValue(stream);
    apiMocks.cancelJob.mockResolvedValue(undefined);
    streamResultMocks.run.mockImplementation((options) => {
      const opened = options.open();
      options.onOpen?.(opened);
      return new Promise(() => undefined);
    });
    const { result } = renderHook(() => usePackageUpdater());

    void act(() => {
      void result.current.updateAll(["nginx"]);
    });
    await vi.waitFor(() => expect(streamResultMocks.run).toHaveBeenCalled());

    act(() => result.current.cancelUpdate());

    expect(stream.abort).toHaveBeenCalledTimes(1);
    expect(apiMocks.cancelJob).toHaveBeenCalledWith({ jobId: "job-2" });
    expect(result.current.error).toBe("Update cancelled");
    expect(result.current.updatingPackage).toBeNull();
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
    streamResultMocks.run.mockImplementation(async (options) => {
      options.onOpen?.(options.open());
    });
    const { result } = renderHook(() => usePackageUpdater());

    await vi.waitFor(() =>
      expect(apiMocks.openJobAttachStream).toHaveBeenCalledWith("recovered-1"),
    );
    await act(async () => vi.advanceTimersByTimeAsync(1500));
    expect(result.current.updatingPackage).toBeNull();
    expect(result.current.status).toBeNull();
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
    streamResultMocks.run.mockRejectedValue(new Error("backend failed"));
    const { result } = renderHook(() => usePackageUpdater());

    await vi.waitFor(() =>
      expect(result.current.error).toBe(
        "Failed to update curl: backend failed",
      ),
    );
    expect(result.current.updatingPackage).toBeNull();
  });

  it("closes a stream that opens after the controller unmounts", async () => {
    let open!: (stream: unknown) => void;
    apiMocks.updatePackages.mockResolvedValue({ id: "late-open" });
    streamResultMocks.run.mockImplementation(
      (options) =>
        new Promise((resolve) => {
          open = options.onOpen!;
          resolve(undefined);
        }),
    );
    const stream = createStream();
    const { result, unmount } = renderHook(() => usePackageUpdater());
    void result.current.updateAll(["nginx"]);
    await vi.waitFor(() => expect(open).toBeTypeOf("function"));
    unmount();
    act(() => open(stream));
    expect(stream.close).toHaveBeenCalledTimes(1);
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
    streamResultMocks.run.mockImplementation((options) => {
      options.onOpen?.(options.open());
      return new Promise(() => undefined);
    });
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
    expect(streamResultMocks.run).toHaveBeenCalledTimes(1);
  });

  it("closes a live stream on unmount without treating it as terminal", async () => {
    const stream = createStream();
    apiMocks.updatePackages.mockResolvedValue({ id: "close-before-terminal" });
    streamResultMocks.run.mockImplementation((options) => {
      options.onOpen?.(options.open());
      return new Promise(() => undefined);
    });
    apiMocks.openJobAttachStream.mockReturnValue(stream);
    const { result, unmount } = renderHook(() => usePackageUpdater());

    void result.current.updateAll(["nginx;1.0;amd64;ubuntu"]);
    await vi.waitFor(() => expect(streamResultMocks.run).toHaveBeenCalled());
    unmount();

    expect(stream.close).toHaveBeenCalledTimes(1);
  });
});
