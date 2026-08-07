import { beforeEach, describe, expect, it, vi } from "vitest";

const listJobs = vi.hoisted(() => vi.fn());

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      jobs: { ...actual.linuxio.jobs, list: listJobs },
    },
  };
});

const { useActiveJobRecovery } = await import("./useActiveJobRecovery");
const { renderHook, waitFor } = await import("@/test/render");

describe("useActiveJobRecovery", () => {
  beforeEach(() => listJobs.mockReset());

  it("reports its scan status and scans once for an unchanged scan key", async () => {
    listJobs.mockResolvedValue([]);
    const onRecover = vi.fn();
    const onMiss = vi.fn();
    const { result, rerender } = renderHook(() =>
      useActiveJobRecovery({
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

    expect(listJobs).toHaveBeenCalledTimes(1);
    expect(onRecover).not.toHaveBeenCalled();
    expect(onMiss).toHaveBeenCalledTimes(1);
  });

  it("reports recovered when a matching active job is found", async () => {
    const job = { id: "active", state: "running", type: "packages.update" };
    listJobs.mockResolvedValue([job]);
    const onRecover = vi.fn();
    const { result } = renderHook(() =>
      useActiveJobRecovery({
        type: "packages.update",
        scanKey: "updates-recover",
        match: () => true,
        onRecover,
      }),
    );

    await waitFor(() => expect(result.current.status).toBe("recovered"));
    expect(onRecover).toHaveBeenCalledWith(job);
  });

  it("rescans after null -> the same key and reports pending for the new scan", async () => {
    let resolveSecond!: (jobs: unknown[]) => void;
    listJobs.mockResolvedValueOnce([]).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveSecond = resolve;
        }),
    );
    const { result, rerender } = renderHook(
      ({ scanKey }) =>
        useActiveJobRecovery({
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
    expect(listJobs).toHaveBeenCalledTimes(2);
  });

  it("rescans when the job type changes", async () => {
    listJobs.mockResolvedValue([]);
    const { rerender } = renderHook(
      ({ type }) =>
        useActiveJobRecovery({
          type,
          scanKey: "updates",
          match: () => true,
          onRecover: vi.fn(),
        }),
      { initialProps: { type: "packages.update" } },
    );

    await waitFor(() => expect(listJobs).toHaveBeenCalledTimes(1));
    rerender({ type: "packages.refresh" });
    await waitFor(() => expect(listJobs).toHaveBeenCalledTimes(2));
  });
});
