import { describe, expect, it, vi } from "vitest";

const apiMocks = vi.hoisted(() => ({
  cancelTask: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    call: (route: string, request?: unknown) =>
      route === "tasks.cancel"
        ? apiMocks.cancelTask(request)
        : actual.call(route as never, request as never),
    linuxio: {
      ...actual.linuxio,
      tasks: {
        ...actual.linuxio.tasks,
        cancel: apiMocks.cancelTask,
      },
    },
  };
});

const { useBackgroundTaskRuntime } =
  await import("@/hooks/backgroundTasks/useBackgroundTaskRuntime");
const { act, renderHook } = await import("@/test/render");

describe("useBackgroundTaskRuntime", () => {
  it("keeps stable refs and pending local task counters", () => {
    const { result, rerender } = renderHook(() => useBackgroundTaskRuntime());
    const first = result.current;

    act(() => {
      first.activeBackgroundTaskIdsRef.current.add("task-1");
      first.pendingLocalTaskKeysRef.current.add("copy:/a:/b");
      first.pendingLocalTaskKeysRef.current.add("copy:/a:/b");
    });

    rerender();

    expect(result.current).toBe(first);
    expect(
      result.current.activeBackgroundTaskIdsRef.current.has("task-1"),
    ).toBe(true);
    expect(
      result.current.pendingLocalTaskKeysRef.current.has("copy:/a:/b"),
    ).toBe(true);

    result.current.pendingLocalTaskKeysRef.current.delete("copy:/a:/b");
    expect(
      result.current.pendingLocalTaskKeysRef.current.has("copy:/a:/b"),
    ).toBe(true);
  });

  it("records transfer rates from byte deltas and throttles after the first emission", () => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000);
    const { result } = renderHook(() => useBackgroundTaskRuntime());

    expect(result.current.recordTransferRate("upload-1", 100)).toBeUndefined();

    vi.setSystemTime(1_500);
    expect(result.current.recordTransferRate("upload-1", 600)).toBe(1000);

    vi.setSystemTime(1_700);
    expect(result.current.recordTransferRate("upload-1", 700)).toBeUndefined();

    vi.setSystemTime(2_500);
    expect(result.current.recordTransferRate("upload-1", 1700)).toBe(1100);
  });

  it("clears transfer samples for invalid ids or regressing byte counters", () => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000);
    const { result } = renderHook(() => useBackgroundTaskRuntime());

    result.current.primeTransferRate("download-1", 500);
    vi.setSystemTime(2_000);

    expect(
      result.current.recordTransferRate("download-1", 250),
    ).toBeUndefined();
    expect(
      result.current.transferRatesRef.current.get("download-1")?.bytes,
    ).toBe(250);

    expect(result.current.recordTransferRate("download-1", -1)).toBeUndefined();
    expect(result.current.transferRatesRef.current.has("download-1")).toBe(
      false,
    );
  });

  it("allocates duplicate download labels and releases counters", () => {
    const { result } = renderHook(() => useBackgroundTaskRuntime());

    expect(result.current.allocateDownloadLabelBase("archive.zip", "a")).toBe(
      "archive.zip",
    );
    expect(result.current.allocateDownloadLabelBase("archive.zip", "b")).toBe(
      "archive.zip (2)",
    );

    result.current.releaseDownloadLabelBase("b");

    expect(result.current.allocateDownloadLabelBase("archive.zip", "c")).toBe(
      "archive.zip (2)",
    );
  });

  it("cancels bridge tasks and swallows backend cancellation errors", async () => {
    apiMocks.cancelTask.mockRejectedValue(new Error("already gone"));
    const { result } = renderHook(() => useBackgroundTaskRuntime());

    expect(() => result.current.cancelBridgeTask("task-1")).not.toThrow();

    await vi.waitFor(() => {
      expect(apiMocks.cancelTask).toHaveBeenCalledWith({ taskId: "task-1" });
    });
  });
});
