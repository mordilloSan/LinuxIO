import {
  onlineManager,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { linuxio } from "@/api";
import { LinuxIOError } from "@/api/errors";
import type {
  TaskHostStatus,
  TaskSnapshot,
} from "@/api/generated/linuxio-types";
import * as core from "@/api/linuxio-core";
import { createQueryClient } from "@/api/query-client";

import { useJobHostStatus } from "./useJobHostStatus";

const mocks = vi.hoisted(() => ({ isOpen: true, toastError: vi.fn() }));

vi.mock("sonner", () => ({
  toast: {
    error: mocks.toastError,
    success: vi.fn(),
    warning: vi.fn(),
  },
}));

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  useStreamMux: () => ({
    status: mocks.isOpen ? "open" : "connecting",
    isOpen: mocks.isOpen,
    getStream: () => null,
  }),
}));

const job: TaskSnapshot = {
  created_at: "2026-11-10T10:00:00Z",
  id: "job-1",
  state: "running",
  type: "storage.btrfs_scrub_start",
  updated_at: "2026-11-10T10:00:00Z",
};

const pending = (overrides: Partial<TaskHostStatus> = {}): TaskHostStatus => ({
  update_pending: true,
  running: 1,
  jobs: [job],
  ...overrides,
});

function renderHostStatus(queryClient: QueryClient) {
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
  return renderHook(() => useJobHostStatus(), { wrapper });
}

const testQueryClient = () =>
  new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });

describe("useJobHostStatus", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // Importing the app's query client ties "online" to the live transport,
    // which a unit test never opens.
    onlineManager.setEventListener(() => () => {});
    onlineManager.setOnline(true);
    mocks.isOpen = true;
    mocks.toastError.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("asks once while nothing waits and reports no pending update", async () => {
    const request = vi
      .spyOn(core, "request")
      .mockResolvedValue({ update_pending: false, running: 0 });

    const { result } = renderHostStatus(testQueryClient());
    await act(() => vi.advanceTimersByTimeAsync(60_000));

    expect(request).toHaveBeenCalledTimes(1);
    expect(request.mock.calls[0].slice(0, 2)).toEqual(["tasks", "host"]);
    expect(result.current).toEqual({
      updatePending: false,
      running: 0,
      jobs: [],
    });
  });

  it("does not ask before the stream is open", async () => {
    mocks.isOpen = false;
    const request = vi.spyOn(core, "request").mockResolvedValue(pending());

    renderHostStatus(testQueryClient());
    await act(() => vi.advanceTimersByTimeAsync(60_000));

    expect(request).not.toHaveBeenCalled();
  });

  it("asks again every 15 seconds while an update waits, and stops when it does not", async () => {
    const request = vi
      .spyOn(core, "request")
      .mockResolvedValueOnce(pending({ running: 2, jobs: [job, job] }))
      .mockResolvedValueOnce(pending())
      .mockResolvedValue({ update_pending: false, running: 0 });

    const { result } = renderHostStatus(testQueryClient());
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(request).toHaveBeenCalledTimes(1);
    expect(result.current).toMatchObject({ updatePending: true, running: 2 });

    await act(() => vi.advanceTimersByTimeAsync(14_999));
    expect(request).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(request).toHaveBeenCalledTimes(2);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(result.current).toMatchObject({
      updatePending: true,
      running: 1,
      jobs: [job],
    });

    await act(() => vi.advanceTimersByTimeAsync(15_000));
    expect(request).toHaveBeenCalledTimes(3);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(result.current).toEqual({
      updatePending: false,
      running: 0,
      jobs: [],
    });

    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(request).toHaveBeenCalledTimes(3);
  });

  it("keeps the last answer when one poll fails, and keeps polling", async () => {
    const request = vi
      .spyOn(core, "request")
      .mockResolvedValueOnce(pending())
      .mockRejectedValueOnce(new LinuxIOError("bridge busy", 503))
      .mockResolvedValue({ update_pending: false, running: 0 });

    const { result } = renderHostStatus(testQueryClient());
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(result.current).toMatchObject({ updatePending: true, jobs: [job] });

    await act(() => vi.advanceTimersByTimeAsync(15_000));
    expect(request).toHaveBeenCalledTimes(2);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(result.current).toMatchObject({
      updatePending: true,
      running: 1,
      jobs: [job],
    });

    await act(() => vi.advanceTimersByTimeAsync(15_000));
    expect(request).toHaveBeenCalledTimes(3);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(result.current).toEqual({
      updatePending: false,
      running: 0,
      jobs: [],
    });
  });

  it("asks at once when a mutation fails because an update is pending", async () => {
    const request = vi
      .spyOn(core, "request")
      .mockResolvedValue({ update_pending: false, running: 0 });
    vi.spyOn(core, "mutationRequest").mockRejectedValue(
      new LinuxIOError("update_pending: LinuxIO was updated", 409),
    );
    const queryClient = testQueryClient();

    const { result } = renderHostStatus(queryClient);
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(request).toHaveBeenCalledTimes(1);

    request.mockResolvedValue(pending());
    const mutation = renderHook(
      () => linuxio.docker.start_container.useMutation(),
      {
        wrapper: ({ children }: { children: ReactNode }) =>
          createElement(QueryClientProvider, { client: queryClient }, children),
      },
    );
    await act(async () => {
      mutation.result.current.mutate({ containerId: "abc" });
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(request).toHaveBeenCalledTimes(2);
    expect(result.current.updatePending).toBe(true);
  });

  it("counts an error as nothing waiting, without a retry or a toast", async () => {
    const request = vi
      .spyOn(core, "request")
      .mockRejectedValue(new LinuxIOError("route not found", 404));

    const { result } = renderHostStatus(createQueryClient());
    await act(() => vi.advanceTimersByTimeAsync(60_000));

    expect(request).toHaveBeenCalledTimes(1);
    expect(mocks.toastError).not.toHaveBeenCalled();
    expect(result.current).toEqual({
      updatePending: false,
      running: 0,
      jobs: [],
    });
  });
});
