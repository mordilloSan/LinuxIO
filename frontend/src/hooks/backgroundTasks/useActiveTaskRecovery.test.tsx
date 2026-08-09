import { beforeEach, describe, expect, it, vi } from "vitest";

const listTasks = vi.hoisted(() => vi.fn());

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    call: (route: string, request?: unknown) =>
      route === "tasks.list"
        ? listTasks(request)
        : actual.call(route as never, request as never),
    linuxio: {
      ...actual.linuxio,
      tasks: { ...actual.linuxio.tasks, list: listTasks },
    },
  };
});

const { useActiveTaskRecovery } = await import("./useActiveTaskRecovery");
const { renderHook, waitFor } = await import("@/test/render");

describe("useActiveTaskRecovery", () => {
  beforeEach(() => listTasks.mockReset());

  it("reports its scan status and scans once for an unchanged scan key", async () => {
    listTasks.mockResolvedValue([]);
    const onRecover = vi.fn();
    const onMiss = vi.fn();
    const { result, rerender } = renderHook(() =>
      useActiveTaskRecovery({
        type: "packages.update",
        scanKey: "updates",
        match: () => true,
        onRecover,
        onMiss,
      }),
    );

    expect(result.current).toMatchObject({
      isScanning: true,
      status: "pending",
    });
    await waitFor(() => expect(result.current.status).toBe("missed"));
    rerender();
    await Promise.resolve();

    expect(listTasks).toHaveBeenCalledTimes(1);
    expect(onRecover).not.toHaveBeenCalled();
    expect(onMiss).toHaveBeenCalledTimes(1);
  });

  it("reports recovered when a matching active task is found", async () => {
    const task = { id: "active", state: "running", type: "packages.update" };
    listTasks.mockResolvedValue([task]);
    const onRecover = vi.fn();
    const { result } = renderHook(() =>
      useActiveTaskRecovery({
        type: "packages.update",
        scanKey: "updates-recover",
        match: () => true,
        onRecover,
      }),
    );

    await waitFor(() => expect(result.current.status).toBe("recovered"));
    expect(onRecover).toHaveBeenCalledWith(task);
  });

  it("rescans after null -> the same key and reports pending for the new scan", async () => {
    let resolveSecond!: (tasks: unknown[]) => void;
    listTasks.mockResolvedValueOnce([]).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveSecond = resolve;
        }),
    );
    const { result, rerender } = renderHook(
      ({ scanKey }) =>
        useActiveTaskRecovery({
          type: "packages.update",
          scanKey,
          match: () => true,
          onRecover: vi.fn(),
        }),
      { initialProps: { scanKey: "updates" as string | null } },
    );

    await waitFor(() => expect(result.current.status).toBe("missed"));
    rerender({ scanKey: null });
    rerender({ scanKey: "updates" });

    expect(result.current).toMatchObject({
      isScanning: true,
      status: "pending",
    });
    resolveSecond([]);
    await waitFor(() => expect(result.current.status).toBe("missed"));
    expect(listTasks).toHaveBeenCalledTimes(2);
  });

  it("rescans when the task type changes", async () => {
    listTasks.mockResolvedValue([]);
    const { rerender } = renderHook(
      ({ type }) =>
        useActiveTaskRecovery({
          type,
          scanKey: "updates",
          match: () => true,
          onRecover: vi.fn(),
        }),
      { initialProps: { type: "packages.update" } },
    );

    await waitFor(() => expect(listTasks).toHaveBeenCalledTimes(1));
    rerender({ type: "packages.refresh" });
    await waitFor(() => expect(listTasks).toHaveBeenCalledTimes(2));
  });
});
