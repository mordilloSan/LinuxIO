import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { FileProgress, Stream, TaskProgress } from "@/api";
import { makeCountedSet } from "@/utils/backgroundTasks";

import type { BackgroundTaskRuntime } from "./useBackgroundTaskRuntime";

const apiMocks = vi.hoisted(() => ({
  archive: vi.fn(),
  isConnected: vi.fn(),
  openTaskWatchStream: vi.fn(),
}));
const nativeDownloadMocks = vi.hoisted(() => ({
  triggerNativeArchiveDownload: vi.fn(),
  triggerNativeFileDownload: vi.fn(),
}));
const streamResultMocks = vi.hoisted(() => ({
  run: vi.fn(),
}));
const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  info: vi.fn(),
  success: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    isConnected: apiMocks.isConnected,
    openTaskWatchStream: apiMocks.openTaskWatchStream,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        archive: apiMocks.archive,
      },
    },
  };
});

vi.mock("@/hooks/useStreamResult", () => ({
  useStreamResult: () => ({ run: streamResultMocks.run }),
}));

vi.mock("@/utils/nativeDownload", () => ({
  triggerNativeArchiveDownload:
    nativeDownloadMocks.triggerNativeArchiveDownload,
  triggerNativeFileDownload: nativeDownloadMocks.triggerNativeFileDownload,
}));

vi.mock("sonner", () => ({ toast: toastMocks }));

const { useDownloadTasks } = await import("./useDownloadTasks");

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

function makeRuntime(): BackgroundTaskRuntime {
  const streamRefsRef = { current: new Map<string, Stream>() };
  const transferRatesRef = { current: new Map() };
  const runtime = {
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
    streamRefsRef,
    transferRatesRef,
  };
  return runtime;
}

interface CapturedStreamOptions {
  onOpen?: (stream: Stream) => void;
  onProgress?: (progress: TaskProgress<FileProgress>) => void;
  onSuccess?: () => void;
  open: () => Stream | null;
}

function setupDownload({
  progress,
  taskId = "task-1",
}: { progress?: Record<string, unknown>; taskId?: string } = {}) {
  const stream = createStream();
  const runtime = makeRuntime();
  const streamOptions: CapturedStreamOptions[] = [];
  apiMocks.isConnected.mockReturnValue(true);
  apiMocks.openTaskWatchStream.mockReturnValue(stream);
  apiMocks.archive.mockResolvedValue({ id: taskId, progress });
  streamResultMocks.run.mockImplementation((options: CapturedStreamOptions) => {
    streamOptions.push(options);
    const opened = options.open();
    if (opened) options.onOpen?.(opened);
    return Promise.resolve(undefined);
  });

  const hook = renderHook(() => useDownloadTasks(runtime));
  return { hook, runtime, stream, streamOptions };
}

describe("useDownloadTasks native browser handoff", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    let requestNumber = 0;
    vi.stubGlobal("crypto", {
      randomUUID: vi.fn(() => `request-${++requestNumber}`),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("starts a direct native download for a single file without creating a task", async () => {
    const { hook, runtime } = setupDownload();

    await act(async () => {
      await hook.result.current.startDownload(["/tmp/image.iso"]);
    });

    expect(nativeDownloadMocks.triggerNativeFileDownload).toHaveBeenCalledWith(
      "/tmp/image.iso",
    );
    expect(apiMocks.archive).not.toHaveBeenCalled();
    expect(apiMocks.isConnected).not.toHaveBeenCalled();
    expect(streamResultMocks.run).not.toHaveBeenCalled();
    expect(hook.result.current.downloads).toHaveLength(0);
    expect(runtime.activeFileTransferTaskIdsRef.current.size).toBe(0);
  });

  it("does not hand off an archive before waiting_for_client", async () => {
    const { hook, streamOptions } = setupDownload();

    await act(async () => {
      await hook.result.current.startDownload(["/tmp/photos/"]);
    });
    expect(
      nativeDownloadMocks.triggerNativeArchiveDownload,
    ).not.toHaveBeenCalled();

    act(() => {
      streamOptions[0]?.onProgress?.({
        phase: "preparing",
        detail: { bytes: 10, total: 100, pct: 10, phase: "preparing" },
      });
    });
    expect(
      nativeDownloadMocks.triggerNativeArchiveDownload,
    ).not.toHaveBeenCalled();
  });

  it("hands off an archive exactly once when waiting_for_client repeats", async () => {
    const { hook, runtime, stream, streamOptions } = setupDownload();

    await act(async () => {
      await hook.result.current.startDownload(["/tmp/photos/"]);
    });
    act(() => {
      streamOptions[0]?.onProgress?.({ phase: "waiting_for_client" });
      streamOptions[0]?.onProgress?.({ phase: "waiting_for_client" });
    });

    expect(
      nativeDownloadMocks.triggerNativeArchiveDownload,
    ).toHaveBeenCalledOnce();
    expect(
      nativeDownloadMocks.triggerNativeArchiveDownload,
    ).toHaveBeenCalledWith("task-1");
    expect(hook.result.current.downloads).toHaveLength(0);
    expect(runtime.activeFileTransferTaskIdsRef.current).toContain("task-1");
    expect(runtime.streamRefsRef.current.get("task-1")).toBe(stream);
    expect(stream.abort).not.toHaveBeenCalled();
    expect(runtime.cancelBridgeTask).not.toHaveBeenCalled();
    expect(toastMocks.success).not.toHaveBeenCalled();

    act(() => {
      streamOptions[0]?.onSuccess?.();
    });
    expect(runtime.activeFileTransferTaskIdsRef.current.has("task-1")).toBe(
      false,
    );
    expect(runtime.streamRefsRef.current.has("task-1")).toBe(false);
  });

  it("hands off an archive already waiting in the creation snapshot", async () => {
    const { hook } = setupDownload({
      progress: { phase: "waiting_for_client" },
    });

    await act(async () => {
      await hook.result.current.startDownload(["/tmp/photos/"]);
    });

    expect(
      nativeDownloadMocks.triggerNativeArchiveDownload,
    ).toHaveBeenCalledOnce();
    expect(
      nativeDownloadMocks.triggerNativeArchiveDownload,
    ).toHaveBeenCalledWith("task-1");
    expect(hook.result.current.downloads).toHaveLength(0);
  });

  it("removes archive state and stream state on terminal success", async () => {
    const { hook, runtime, stream, streamOptions } = setupDownload();

    await act(async () => {
      await hook.result.current.startDownload(["/tmp/photos/"]);
    });
    expect(hook.result.current.downloads).toHaveLength(1);
    expect(runtime.activeFileTransferTaskIdsRef.current).toContain("task-1");
    expect(runtime.streamRefsRef.current.get("task-1")).toBe(stream);

    act(() => {
      streamOptions[0]?.onSuccess?.();
    });

    expect(hook.result.current.downloads).toHaveLength(0);
    expect(runtime.activeFileTransferTaskIdsRef.current.has("task-1")).toBe(
      false,
    );
    expect(runtime.streamRefsRef.current.has("task-1")).toBe(false);
    expect(runtime.releaseDownloadLabelBase).toHaveBeenCalledWith("task-1");
  });

  it("aborts an archive watch and cancels the bridge task", async () => {
    const { hook, runtime, stream } = setupDownload();

    await act(async () => {
      await hook.result.current.startDownload(["/tmp/photos/"]);
    });
    act(() => {
      hook.result.current.cancelDownload("task-1");
    });

    expect(stream.abort).toHaveBeenCalledOnce();
    expect(runtime.cancelBridgeTask).toHaveBeenCalledWith("task-1");
    expect(hook.result.current.downloads).toHaveLength(0);
  });

  it("uses the task id for a directory archive handoff", async () => {
    const { hook, streamOptions } = setupDownload({ taskId: "archive-1" });

    await act(async () => {
      await hook.result.current.startDownload(["/tmp/photos/"]);
    });
    expect(apiMocks.archive).toHaveBeenCalledWith({
      format: "zip",
      paths: ["/tmp/photos/"],
    });

    act(() => {
      streamOptions[0]?.onProgress?.({ phase: "waiting_for_client" });
    });
    expect(
      nativeDownloadMocks.triggerNativeArchiveDownload,
    ).toHaveBeenCalledWith("archive-1");
  });
});
