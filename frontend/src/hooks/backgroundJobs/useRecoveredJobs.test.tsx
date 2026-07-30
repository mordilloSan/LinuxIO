import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api";
import { createTestQueryClient } from "@/test/render";

import type { BackgroundJobRuntime } from "./useBackgroundJobRuntime";

const apiMocks = vi.hoisted(() => ({
  openJobEventsStream: vi.fn(),
}));
const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    openJobEventsStream: apiMocks.openJobEventsStream,
    useStreamMux: () => ({ status: "open" }),
  };
});

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({ refreshCapabilities: vi.fn() }),
}));

vi.mock("sonner", () => ({
  toast: toastMocks,
}));

const { useRecoveredJobs } = await import("./useRecoveredJobs");

function createStream(): Stream {
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
  };
}

describe("useRecoveredJobs package updates", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("authoritatively invalidates updates after a detached package stream closes", async () => {
    const events = createStream();
    apiMocks.openJobEventsStream.mockReturnValue(events);
    const queryClient = createTestQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const runtime = {
      activeBackgroundJobIdsRef: { current: new Set<string>() },
      activeFileTransferJobIdsRef: { current: new Set<string>() },
      activeIndexerIdsRef: { current: new Set<string>() },
      allocateDownloadLabelBase: vi.fn(),
      cancelBridgeJob: vi.fn(),
      pendingLocalJobKeysRef: { current: new Map<string, number>() },
      primeTransferRate: vi.fn(),
      recordTransferRate: vi.fn(),
      recoveringJobIdsRef: { current: new Set<string>() },
      releaseDownloadLabelBase: vi.fn(),
      streamRefsRef: { current: new Map<string, Stream>() },
      transferRatesRef: { current: new Map() },
    } as unknown as BackgroundJobRuntime;
    const controls = {
      genericJobs: {
        removeBackgroundJob: vi.fn(),
        setBackgroundJobs: vi.fn(),
      },
      indexers: {
        removeIndexer: vi.fn(),
        setIndexers: vi.fn(),
        setIsIndexerDialogOpen: vi.fn(),
        setLastIndexerError: vi.fn(),
        setLastIndexerResult: vi.fn(),
      },
      recoverTransfer: vi.fn(() => false),
    };
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );

    renderHook(() => useRecoveredJobs(runtime, controls), { wrapper });

    // This models a page-owned package stream closing during navigation. The
    // terminal event arrives independently on the persistent global stream.
    await act(async () => {
      events.onProgress?.({
        job: {
          created_at: "2026-01-01T00:00:00Z",
          finished_at: "2026-01-01T00:01:00Z",
          id: "package-detached",
          request: { packageIds: ["nginx;1.0;amd64;ubuntu"] },
          state: "completed",
          type: "packages.update",
          updated_at: "2026-01-01T00:01:00Z",
        },
        type: "job.result",
      } as never);
    });

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["linuxio", "updates", "get_updates_basic"],
    });
  });

  it("toasts a terminal package failure once when no page stream owns it", async () => {
    const events = createStream();
    apiMocks.openJobEventsStream.mockReturnValue(events);
    const queryClient = createTestQueryClient();
    const runtime = {
      activeBackgroundJobIdsRef: { current: new Set<string>() },
      activeFileTransferJobIdsRef: { current: new Set<string>() },
      activeIndexerIdsRef: { current: new Set<string>() },
      allocateDownloadLabelBase: vi.fn(),
      cancelBridgeJob: vi.fn(),
      pendingLocalJobKeysRef: { current: new Map<string, number>() },
      primeTransferRate: vi.fn(),
      recordTransferRate: vi.fn(),
      recoveringJobIdsRef: { current: new Set<string>() },
      releaseDownloadLabelBase: vi.fn(),
      streamRefsRef: { current: new Map<string, Stream>() },
      transferRatesRef: { current: new Map() },
    } as unknown as BackgroundJobRuntime;
    const controls = {
      genericJobs: {
        removeBackgroundJob: vi.fn(),
        setBackgroundJobs: vi.fn(),
      },
      indexers: {
        removeIndexer: vi.fn(),
        setIndexers: vi.fn(),
        setIsIndexerDialogOpen: vi.fn(),
        setLastIndexerError: vi.fn(),
        setLastIndexerResult: vi.fn(),
      },
      recoverTransfer: vi.fn(() => false),
    };
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );

    renderHook(() => useRecoveredJobs(runtime, controls), { wrapper });

    const failureEvent = {
      job: {
        created_at: "2026-01-01T00:00:00Z",
        error: { message: "dpkg failed" },
        finished_at: "2026-01-01T00:01:00Z",
        id: "package-failed",
        request: { packageIds: ["nginx;1.0;amd64;ubuntu"] },
        state: "failed",
        type: "packages.update",
        updated_at: "2026-01-01T00:01:00Z",
      },
      type: "job.error",
    };

    await act(async () => {
      events.onProgress?.(failureEvent as never);
      events.onProgress?.(failureEvent as never);
    });

    expect(toastMocks.error).toHaveBeenCalledOnce();
    expect(toastMocks.error).toHaveBeenCalledWith("dpkg failed");
  });
});
