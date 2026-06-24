import { describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api";

const apiMocks = vi.hoisted(() => ({
  cancelJob: vi.fn(),
  installPackage: vi.fn(),
  openJobAttachStream: vi.fn(),
  updatePackages: vi.fn(),
}));

const streamResultMocks = vi.hoisted(() => ({
  run: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    openJobAttachStream: apiMocks.openJobAttachStream,
    linuxio: {
      ...actual.linuxio,
      jobs: {
        ...actual.linuxio.jobs,
        cancel: apiMocks.cancelJob,
      },
      packages: {
        ...actual.linuxio.packages,
        update: apiMocks.updatePackages,
      },
      updates: {
        ...actual.linuxio.updates,
        install_package: {
          useMutation: () => ({
            mutateAsync: apiMocks.installPackage,
          }),
        },
      },
    },
  };
});

vi.mock("@/hooks/useStreamResult", () => ({
  useStreamResult: () => ({
    run: streamResultMocks.run,
  }),
}));

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
  it("updates one package, shows the package name, and waits before clearing progress", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    apiMocks.installPackage.mockResolvedValue(undefined);
    const onComplete = vi.fn(async () => undefined);
    const { result } = renderHook(() => usePackageUpdater(onComplete));

    let promise!: Promise<void>;
    await act(async () => {
      promise = result.current.updateOne("nginx;1.24.0;amd64;ubuntu");
      await Promise.resolve();
    });

    expect(result.current.updatingPackage).toBe("nginx");
    expect(result.current.status).toBe("Installing");
    expect(result.current.eventLog).toEqual(["Installing: nginx"]);
    expect(apiMocks.installPackage).toHaveBeenCalledWith({
      packageId: "nginx;1.24.0;amd64;ubuntu",
    });

    await flushMinimumVisibleProgress(promise);

    expect(onComplete).toHaveBeenCalledTimes(1);
    expect(result.current.updatingPackage).toBeNull();
    expect(result.current.status).toBeNull();
  });

  it("reports single-package update failures with the package name", async () => {
    vi.useFakeTimers();
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);
    apiMocks.installPackage.mockRejectedValue(new Error("permission denied"));
    const { result } = renderHook(() => usePackageUpdater(vi.fn()));

    const promise = result.current.updateOne("curl;8.0;amd64;ubuntu");
    await flushMinimumVisibleProgress(promise);

    expect(result.current.error).toBe(
      "Failed to update curl: permission denied",
    );
    expect(result.current.updatingPackage).toBeNull();
    expect(consoleError).toHaveBeenCalledWith(
      "Failed to update curl;8.0;amd64;ubuntu",
      expect.any(Error),
    );
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
    const onComplete = vi.fn(async () => undefined);
    const { result } = renderHook(() => usePackageUpdater(onComplete));

    const promise = result.current.updateAll([
      "nginx;1.24.0;amd64;ubuntu",
      "curl;8.0;amd64;ubuntu",
    ]);
    await flushMinimumVisibleProgress(promise);

    expect(apiMocks.updatePackages).toHaveBeenCalledWith([
      "nginx;1.24.0;amd64;ubuntu",
      "curl;8.0;amd64;ubuntu",
    ]);
    expect(apiMocks.openJobAttachStream).toHaveBeenCalledWith("job-1");
    expect(result.current.progress).toBe(100);
    expect(result.current.eventLog).toEqual([
      "Initializing update transaction",
      "Installing packages",
      "Finished",
    ]);
    expect(result.current.updatingPackage).toBeNull();
    expect(result.current.status).toBeNull();
    expect(onComplete).toHaveBeenCalledTimes(1);
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
    const { result } = renderHook(() => usePackageUpdater(vi.fn()));

    void act(() => {
      void result.current.updateAll(["nginx"]);
    });
    await vi.waitFor(() => expect(streamResultMocks.run).toHaveBeenCalled());

    act(() => result.current.cancelUpdate());

    expect(stream.abort).toHaveBeenCalledTimes(1);
    expect(apiMocks.cancelJob).toHaveBeenCalledWith("job-2");
    expect(result.current.error).toBe("Update cancelled");
    expect(result.current.updatingPackage).toBeNull();
  });
});
