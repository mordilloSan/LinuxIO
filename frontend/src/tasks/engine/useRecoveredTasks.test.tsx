import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api";
import { createBackgroundTaskCache } from "@/tasks/taskCache";
import {
  TASK_TYPE_DOCKER_UPDATE,
  TASK_TYPE_FILE_CHMOD_BATCH,
  TASK_TYPE_FILE_DELETE_BATCH,
  TASK_TYPE_FILE_INDEXER,
  TASK_TYPE_FILE_TRASH_RESTORE,
  TASK_TYPE_PACKAGE_UPDATE,
  TASK_TYPE_STORAGE_BTRFS_BALANCE,
  TASK_TYPE_STORAGE_BTRFS_SCRUB,
  TASK_TYPE_SYSTEM_INSTALL_CAPABILITY,
} from "@/tasks/taskTypes";
import { makeCountedSet } from "@/tasks/taskUtils";
import { createTestQueryClient } from "@/test/render";

import type { BackgroundTaskRuntime } from "./useBackgroundTaskRuntime";

const apiMocks = vi.hoisted(() => ({
  openTaskEventsStream: vi.fn(),
  streamMux: { status: "open" },
}));
const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  info: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    openTaskEventsStream: apiMocks.openTaskEventsStream,
    // Still the real opener; spied on so a test can tell no watch opened.
    openTaskWatchStream: vi.fn(actual.openTaskWatchStream),
    useStreamMux: () => ({ status: apiMocks.streamMux.status }),
  };
});

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({ refreshCapabilities: vi.fn() }),
}));

vi.mock("sonner", () => ({
  toast: toastMocks,
}));

const { openTaskWatchStream } = await import("@/api");
const { useRecoveredTasks } = await import("./useRecoveredTasks");
const {
  claimTerminalFeedback,
  markTerminalFeedbackEmitted,
  resetTerminalTaskFeedback,
} = await import("@/tasks/terminalTaskFeedback");

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
    tasks: createBackgroundTaskCache(createTestQueryClient(), "anonymous"),
    activeBackgroundTaskIdsRef: { current: new Set<string>() },
    activeFileTransferTaskIdsRef: { current: new Set<string>() },
    activeIndexerIdsRef: { current: new Set<string>() },
    allocateDownloadLabelBase: vi.fn((base: string) => base),
    cancelBridgeTask: vi.fn(),
    locallyCanceledTaskIdsRef: { current: new Set<string>() },
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
      // Writes the runtime's own cache, as the provider wires it, so rows
      // can be read back from runtime.tasks.
      setBackgroundTasks: vi.fn(runtime.tasks.backgroundTasks.set),
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

  const { rerender } = renderHook(() => useRecoveredTasks(runtime, controls), {
    wrapper,
  });

  const emitTerminalEvent = (task: Record<string, unknown>) =>
    act(async () => {
      events.onProgress?.({ task, type: "task.result" } as never);
    });

  // Sends on whichever events stream the hook opened last.
  const emit = (type: string, task: Record<string, unknown>) =>
    act(async () => {
      const stream = apiMocks.openTaskEventsStream.mock.results.at(-1)
        ?.value as Stream;
      stream.onProgress?.({ task, type } as never);
    });

  // The latest events stream ends on its own, not through the effect cleanup.
  const closeEvents = () =>
    act(async () => {
      const stream = apiMocks.openTaskEventsStream.mock.results.at(-1)
        ?.value as Stream;
      stream.onClose?.();
    });

  // The mux drops and comes back, so the hook opens a new events stream.
  const reconnect = () => {
    apiMocks.streamMux.status = "closed";
    rerender();
    apiMocks.openTaskEventsStream.mockReturnValue(createStream());
    apiMocks.streamMux.status = "open";
    rerender();
  };

  return {
    closeEvents,
    controls,
    emit,
    emitTerminalEvent,
    events,
    queryClient,
    reconnect,
    runtime,
  };
}

function scheduledRun(
  type: string,
  state: string,
  overrides: Record<string, unknown> = {},
) {
  return {
    created_at: "2026-11-10T03:00:00Z",
    id: "12345678-90ab-cdef-1234-567890abcdef",
    state,
    type,
    updated_at: "2026-11-10T03:00:01Z",
    ...overrides,
  };
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

  it("restores an active durable Docker update to the background task UI", async () => {
    const { controls, emitTerminalEvent } = renderRecoveredTasks();

    await emitTerminalEvent({
      created_at: "2026-01-01T00:00:00Z",
      id: "docker-update-active",
      metadata: { identity: ["web"] },
      progress: { message: "Pulling the updated image", phase: "running" },
      state: "running",
      type: TASK_TYPE_DOCKER_UPDATE,
      updated_at: "2026-01-01T00:00:01Z",
    });

    expect(controls.genericTasks.setBackgroundTasks).toHaveBeenCalledOnce();
    const update = controls.genericTasks.setBackgroundTasks.mock.calls[0]?.[0];
    if (typeof update !== "function") {
      throw new Error("expected recovered task state updater");
    }
    expect(update([])).toEqual([
      expect.objectContaining({
        id: "docker-update-active",
        label: "Pulling the updated image",
        taskType: TASK_TYPE_DOCKER_UPDATE,
      }),
    ]);
  });

  it("lists a running btrfs balance in the tray with progress", async () => {
    const { controls, emitTerminalEvent } = renderRecoveredTasks();

    await emitTerminalEvent({
      created_at: "2026-01-01T00:00:00Z",
      id: "balance-active",
      metadata: { identity: ["uuid-1"] },
      progress: { percentage: 42, phase: "running" },
      state: "running",
      type: TASK_TYPE_STORAGE_BTRFS_BALANCE,
      updated_at: "2026-01-01T00:00:01Z",
    });

    const update = controls.genericTasks.setBackgroundTasks.mock.calls[0]?.[0];
    if (typeof update !== "function") {
      throw new Error("expected recovered task state updater");
    }
    expect(update([])).toEqual([
      expect.objectContaining({
        id: "balance-active",
        label: "Balancing btrfs filesystem (42%)",
        progress: 42,
        taskType: TASK_TYPE_STORAGE_BTRFS_BALANCE,
      }),
    ]);
  });

  it("lists a running btrfs scrub in the tray with progress", async () => {
    const { controls, emitTerminalEvent } = renderRecoveredTasks();

    await emitTerminalEvent({
      created_at: "2026-01-01T00:00:00Z",
      id: "scrub-active",
      metadata: { identity: ["uuid-1", "scrub"] },
      progress: { message: "Scrubbing: 42.0% complete", percentage: 42 },
      state: "running",
      type: TASK_TYPE_STORAGE_BTRFS_SCRUB,
      updated_at: "2026-01-01T00:00:01Z",
    });

    const update = controls.genericTasks.setBackgroundTasks.mock.calls[0]?.[0];
    if (typeof update !== "function") {
      throw new Error("expected recovered task state updater");
    }
    expect(update([])).toEqual([
      expect.objectContaining({
        id: "scrub-active",
        label: "Scrubbing btrfs filesystem (42%)",
        progress: 42,
        taskType: TASK_TYPE_STORAGE_BTRFS_SCRUB,
      }),
    ]);
  });

  it("names a running file job by what it does and the items it has done", async () => {
    const { emitTerminalEvent, runtime } = renderRecoveredTasks();

    for (const [id, type, detail] of [
      ["delete-active", TASK_TYPE_FILE_DELETE_BATCH, { processed: 3 }],
      ["restore-active", TASK_TYPE_FILE_TRASH_RESTORE, { processed: 1 }],
      ["chown-active", TASK_TYPE_FILE_CHMOD_BATCH, { phase: "chown" }],
    ] as const) {
      await emitTerminalEvent({
        created_at: "2026-01-01T00:00:00Z",
        id,
        metadata: { label: "12 items" },
        progress: { detail },
        state: "running",
        type,
        updated_at: "2026-01-01T00:00:01Z",
      });
    }

    expect(runtime.tasks.backgroundTasks.read()).toEqual([
      expect.objectContaining({
        id: "delete-active",
        label: "Deleting 3 items",
      }),
      expect.objectContaining({
        id: "restore-active",
        label: "Restoring 1 item from Trash",
      }),
      expect.objectContaining({
        id: "chown-active",
        label: "Changing ownership: 0 items",
      }),
    ]);
  });

  it("restores an active index operation after navigation", async () => {
    const { controls, emitTerminalEvent } = renderRecoveredTasks();

    await emitTerminalEvent({
      created_at: "2026-01-01T00:00:00Z",
      id: "indexer-active",
      metadata: { path: "/srv/data" },
      state: "running",
      type: TASK_TYPE_FILE_INDEXER,
      updated_at: "2026-01-01T00:00:01Z",
    });

    expect(controls.indexers.setIsIndexerDialogOpen).toHaveBeenCalledWith(true);
    expect(controls.indexers.setIndexers).toHaveBeenCalledOnce();
    const update = controls.indexers.setIndexers.mock.calls[0]?.[0];
    if (typeof update !== "function") {
      throw new Error("expected recovered indexer state updater");
    }
    expect(update([])).toEqual([
      expect.objectContaining({
        id: "indexer-active",
        path: "/srv/data",
        phase: "connecting",
      }),
    ]);
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

// A systemd timer runs these; the bridge answers tasks.watch and tasks.cancel
// of a running one with 409, so the events stream alone keeps the row.
describe("useRecoveredTasks scheduled runs", () => {
  afterEach(() => {
    apiMocks.streamMux.status = "open";
    resetTerminalTaskFeedback();
    vi.clearAllMocks();
  });

  const script = (overrides: Record<string, unknown> = {}) =>
    scheduledRun("scheduled.script", "running", {
      metadata: { identity: ["sched-1"], label: "Nightly backup" },
      ...overrides,
    });

  it("lists a running scheduled script without watching it", async () => {
    const { emit, runtime } = renderRecoveredTasks();

    await emit("task.snapshot", script());

    expect(runtime.tasks.backgroundTasks.read()).toEqual([
      expect.objectContaining({
        cancelRequested: false,
        id: "12345678-90ab-cdef-1234-567890abcdef",
        indeterminate: true,
        label: "Nightly backup",
        link: { search: { schedule: "sched-1" }, to: "/services/schedules" },
        progress: 0,
        taskType: "scheduled.script",
        type: "task",
      }),
    ]);
    expect(openTaskWatchStream).not.toHaveBeenCalled();
  });

  it("follows a scheduled run's progress from the events stream", async () => {
    const { emit, runtime } = renderRecoveredTasks();
    const resync = (overrides: Record<string, unknown> = {}) =>
      scheduledRun("scheduled.btrfs_resync", "running", {
        metadata: { identity: ["pool-uuid"], label: "Pool resync" },
        ...overrides,
      });

    await emit("task.snapshot", resync());
    await emit(
      "task.progress",
      resync({ progress: { message: "Scrubbing", percentage: 43 } }),
    );

    expect(runtime.tasks.backgroundTasks.read()).toEqual([
      expect.objectContaining({
        indeterminate: false,
        label: "Pool resync: Scrubbing",
        progress: 43,
      }),
    ]);
  });

  it.each([
    ["task.result", "completed"],
    ["task.error", "failed"],
    ["task.canceled", "canceled"],
    ["task.error", "interrupted"],
  ])("removes the row on %s (%s) and reports nothing", async (type, state) => {
    const { emit, queryClient, runtime } = renderRecoveredTasks();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await emit("task.snapshot", script());
    expect(runtime.tasks.backgroundTasks.read()).toHaveLength(1);
    await emit(
      type,
      script({
        error:
          state === "completed"
            ? undefined
            : { code: 500, message: "exit status 1" },
        finished_at: "2026-11-10T03:05:00Z",
        state,
      }),
    );

    expect(runtime.tasks.backgroundTasks.read()).toEqual([]);
    expect(toastMocks.error).not.toHaveBeenCalled();
    expect(toastMocks.info).not.toHaveBeenCalled();
    expect(toastMocks.success).not.toHaveBeenCalled();
    expect(toastMocks.warning).not.toHaveBeenCalled();
    expect(invalidate).not.toHaveBeenCalled();
  });

  it("ignores the end of a run it never saw running", async () => {
    const { emit, runtime } = renderRecoveredTasks();

    await expect(
      emit(
        "task.error",
        script({
          error: { code: 500, message: "exit status 1" },
          finished_at: "2026-11-10T03:05:00Z",
          state: "failed",
        }),
      ),
    ).resolves.toBeUndefined();

    expect(runtime.tasks.backgroundTasks.read()).toEqual([]);
    expect(toastMocks.error).not.toHaveBeenCalled();
    expect(toastMocks.info).not.toHaveBeenCalled();
    expect(toastMocks.success).not.toHaveBeenCalled();
    expect(toastMocks.warning).not.toHaveBeenCalled();
  });

  // Nothing reopens a stream that ended on its own until the mux status
  // changes, and no other source would ever end a scheduled row.
  it("drops scheduled rows when the events stream ends on its own", async () => {
    const { closeEvents, emit, runtime } = renderRecoveredTasks();
    const balance = {
      abortController: new AbortController(),
      id: "balance-1",
      label: "Balancing btrfs filesystem",
      progress: 10,
      taskId: "balance-1",
      taskType: TASK_TYPE_STORAGE_BTRFS_BALANCE,
      type: "task" as const,
    };
    runtime.tasks.backgroundTasks.set([balance]);

    await emit("task.snapshot", script());
    expect(runtime.tasks.backgroundTasks.read()).toHaveLength(2);

    await closeEvents();

    expect(runtime.tasks.backgroundTasks.read()).toEqual([balance]);
  });

  it("updates the row a second snapshot of the same run announces", async () => {
    const { emit, runtime } = renderRecoveredTasks();

    await emit("task.snapshot", script());
    await emit("task.snapshot", script({ cancel_requested: true }));

    expect(runtime.tasks.backgroundTasks.read()).toEqual([
      expect.objectContaining({
        cancelRequested: true,
        label: "Nightly backup",
      }),
    ]);
  });

  // A run that ends while the events stream is down sends its end to nobody,
  // so a new stream starts without scheduled rows and its opening snapshots
  // bring back the runs still going.
  it("drops scheduled rows a reopened events stream does not announce again", async () => {
    const { emit, reconnect, runtime } = renderRecoveredTasks();
    const balance = {
      abortController: new AbortController(),
      id: "balance-1",
      label: "Balancing btrfs filesystem",
      progress: 10,
      taskId: "balance-1",
      taskType: TASK_TYPE_STORAGE_BTRFS_BALANCE,
      type: "task" as const,
    };
    runtime.tasks.backgroundTasks.set([balance]);
    const snapshotRun = scheduledRun("scheduled.btrfs_snapshot", "running", {
      id: "snapshot-run",
      metadata: { label: "Snapshots" },
    });

    await emit("task.snapshot", script());
    await emit("task.snapshot", snapshotRun);
    expect(runtime.tasks.backgroundTasks.read()).toHaveLength(3);

    reconnect();
    await emit("task.snapshot", snapshotRun);

    expect(runtime.tasks.backgroundTasks.read()).toEqual([
      balance,
      expect.objectContaining({
        id: "snapshot-run",
        label: "Snapshots",
      }),
    ]);
  });
});
