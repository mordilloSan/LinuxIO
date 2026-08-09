import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api";
import {
  TASK_TYPE_PACKAGE_UPDATE,
  TASK_TYPE_SYSTEM_INSTALL_CAPABILITY,
} from "@/constants/backgroundTaskTypes";
import { createTestQueryClient } from "@/test/render";
import { makeCountedSet } from "@/utils/backgroundTasks";

import type { BackgroundTaskRuntime } from "./useBackgroundTaskRuntime";

const apiMocks = vi.hoisted(() => ({
  openTaskEventsStream: vi.fn(),
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
    openTaskEventsStream: apiMocks.openTaskEventsStream,
    useStreamMux: () => ({ status: "open" }),
  };
});

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({ refreshCapabilities: vi.fn() }),
}));

vi.mock("sonner", () => ({
  toast: toastMocks,
}));

const { useRecoveredTasks } = await import("./useRecoveredTasks");
const {
  claimTerminalFeedback,
  markTerminalFeedbackEmitted,
  resetTerminalTaskFeedback,
} = await import("./terminalTaskFeedback");

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

// Typed for real (no `as unknown as` escape hatch) so runtime shape changes —
// like pendingLocalTaskKeysRef being a CountedSet, not a Map — fail to compile
// here instead of drifting silently.
function makeRuntime(): BackgroundTaskRuntime {
  return {
    activeBackgroundTaskIdsRef: { current: new Set<string>() },
    activeFileTransferTaskIdsRef: { current: new Set<string>() },
    activeIndexerIdsRef: { current: new Set<string>() },
    allocateDownloadLabelBase: vi.fn((base: string) => base),
    cancelBridgeTask: vi.fn(),
    pendingLocalTaskKeysRef: { current: makeCountedSet() },
    primeTransferRate: vi.fn(),
    recordTransferRate: vi.fn(() => undefined),
    recoveringTaskIdsRef: { current: new Set<string>() },
    releaseDownloadLabelBase: vi.fn(),
    streamRefsRef: { current: new Map<string, Stream>() },
    transferRatesRef: { current: new Map() },
  };
}

function renderRecoveredTasks() {
  const events = createStream();
  apiMocks.openTaskEventsStream.mockReturnValue(events);
  const queryClient = createTestQueryClient();
  const runtime = makeRuntime();
  const controls = {
    genericTasks: {
      removeBackgroundTask: vi.fn(),
      setBackgroundTasks: vi.fn(),
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

  renderHook(() => useRecoveredTasks(runtime, controls), { wrapper });

  const emitTerminalEvent = (task: Record<string, unknown>) =>
    act(async () => {
      events.onProgress?.({ task, type: "task.result" } as never);
    });

  return { controls, emitTerminalEvent, events, queryClient, runtime };
}

function packageUpdateTask(
  id: string,
  state: string,
  overrides: Record<string, unknown> = {},
) {
  return {
    created_at: "2026-01-01T00:00:00Z",
    finished_at: "2026-01-01T00:01:00Z",
    id,
    request: { packageIds: ["nginx;1.0;amd64;ubuntu"] },
    state,
    type: TASK_TYPE_PACKAGE_UPDATE,
    updated_at: "2026-01-01T00:01:00Z",
    ...overrides,
  };
}

describe("useRecoveredTasks package updates", () => {
  afterEach(() => {
    resetTerminalTaskFeedback();
    vi.clearAllMocks();
  });

  it("authoritatively invalidates updates after a detached package stream closes", async () => {
    const { emitTerminalEvent, queryClient } = renderRecoveredTasks();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    // This models a page-owned package stream closing during navigation. The
    // terminal event arrives independently on the persistent global stream.
    await emitTerminalEvent(packageUpdateTask("package-detached", "completed"));

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["linuxio", "updates", "get_updates_basic"],
    });
  });

  it("toasts a terminal package failure once when no page stream owns it", async () => {
    const { emitTerminalEvent } = renderRecoveredTasks();
    const failure = packageUpdateTask("package-failed", "failed", {
      error: { message: "dpkg failed" },
    });

    await emitTerminalEvent(failure);
    await emitTerminalEvent(failure);

    expect(toastMocks.error).toHaveBeenCalledOnce();
    expect(toastMocks.error).toHaveBeenCalledWith("dpkg failed");
  });

  it("suppresses the failure toast while a mounted page claims package updates", async () => {
    const { emitTerminalEvent } = renderRecoveredTasks();

    // The Updates page claims ownership for the duration of a run; its inline
    // alert is the report, so the same failure must not also toast.
    const release = claimTerminalFeedback(TASK_TYPE_PACKAGE_UPDATE);
    await emitTerminalEvent(
      packageUpdateTask("package-owned", "failed", {
        error: { message: "dpkg failed" },
      }),
    );
    expect(toastMocks.error).not.toHaveBeenCalled();

    // Released claims take effect immediately (no unmark delay): a failure
    // arriving after navigation must surface here, or it surfaces nowhere.
    release();
    await emitTerminalEvent(
      packageUpdateTask("package-after-nav", "failed", {
        error: { message: "dpkg failed" },
      }),
    );
    expect(toastMocks.error).toHaveBeenCalledOnce();
  });

  it("stays silent for a failure the owning page already painted", async () => {
    const { emitTerminalEvent } = renderRecoveredTasks();

    // finishError marks the task the moment it paints the inline alert, so the
    // global copy of the event stays silent even after the claim is released.
    markTerminalFeedbackEmitted("package-painted");
    await emitTerminalEvent(
      packageUpdateTask("package-painted", "failed", {
        error: { message: "dpkg failed" },
      }),
    );

    expect(toastMocks.error).not.toHaveBeenCalled();
  });

  it("does not report a canceled package update", async () => {
    const { emitTerminalEvent } = renderRecoveredTasks();

    await emitTerminalEvent(
      packageUpdateTask("package-canceled", "canceled", {
        error: { message: "operation aborted", code: 499 },
      }),
    );

    expect(toastMocks.error).not.toHaveBeenCalled();
  });

  it("still reports a capability install canceled elsewhere as an error", async () => {
    const { emitTerminalEvent } = renderRecoveredTasks();

    await emitTerminalEvent({
      created_at: "2026-01-01T00:00:00Z",
      error: { message: "operation aborted", code: 499 },
      finished_at: "2026-01-01T00:01:00Z",
      id: "cap-canceled",
      request: { capability: "docker" },
      state: "canceled",
      type: TASK_TYPE_SYSTEM_INSTALL_CAPABILITY,
      updated_at: "2026-01-01T00:01:00Z",
    });

    expect(toastMocks.error).toHaveBeenCalledWith(
      "operation aborted",
      undefined,
    );
  });
});
