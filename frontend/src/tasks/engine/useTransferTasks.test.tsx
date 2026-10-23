import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api";
import { createBackgroundTaskCache } from "@/tasks/taskCache";
import { makeCountedSet } from "@/tasks/taskUtils";
import { createTestQueryClient } from "@/test/render";

import type { BackgroundTaskRuntime } from "./useBackgroundTaskRuntime";

const apiMocks = vi.hoisted(() => ({
  copy_batch: vi.fn(),
  isConnected: vi.fn(),
  move_batch: vi.fn(),
  openTaskWatchStream: vi.fn(),
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
        copy_batch: apiMocks.copy_batch,
        move_batch: apiMocks.move_batch,
      },
    },
  };
});

vi.mock("@/hooks/useStreamResult", () => ({
  useStreamResult: () => ({ run: streamResultMocks.run }),
}));

vi.mock("sonner", () => ({ toast: toastMocks }));

const { useTransferTasks } = await import("./useTransferTasks");

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
  return {
    tasks: createBackgroundTaskCache(createTestQueryClient(), "anonymous"),
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

interface CapturedStreamOptions {
  onOpen?: (stream: Stream) => void;
  onSuccess?: (result: unknown) => void;
  open: () => Stream | null;
}

function setupTransfer() {
  const streamOptions: CapturedStreamOptions[] = [];
  apiMocks.isConnected.mockReturnValue(true);
  apiMocks.openTaskWatchStream.mockReturnValue(createStream());
  apiMocks.copy_batch.mockResolvedValue({ id: "task-copy" });
  apiMocks.move_batch.mockResolvedValue({ id: "task-move" });
  streamResultMocks.run.mockImplementation((options: CapturedStreamOptions) => {
    streamOptions.push(options);
    const opened = options.open();
    if (opened) options.onOpen?.(opened);
    return Promise.resolve(undefined);
  });
  const hook = renderHook(() => useTransferTasks(makeRuntime()));
  return { hook, streamOptions };
}

describe("useTransferTasks batch results", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("reports per-item copy failures instead of success", async () => {
    const { hook, streamOptions } = setupTransfer();
    const onComplete = vi.fn();

    await act(async () => {
      await hook.result.current.startCopy({
        sources: ["/src/a.txt", "/src/b.txt"],
        destination: "/dst",
        onComplete,
      });
    });
    act(() => {
      streamOptions[0]?.onSuccess?.({
        total: 2,
        succeeded: 1,
        failed: [{ path: "/src/b.txt", error: "permission denied" }],
      });
    });

    expect(toastMocks.error).toHaveBeenCalledWith(
      "Copied 2 items with 1 failure",
      { description: "b.txt: permission denied" },
    );
    expect(toastMocks.success).not.toHaveBeenCalled();
    expect(onComplete).toHaveBeenCalledOnce();
  });

  it("sends keepBoth with the copy and move requests", async () => {
    const { hook } = setupTransfer();

    await act(async () => {
      await hook.result.current.startCopy({
        sources: ["/src/a.txt"],
        destination: "/dst",
        keepBoth: ["/src/a.txt"],
      });
      await hook.result.current.startMove({
        sources: ["/src/b.txt"],
        destination: "/dst",
        keepBoth: ["/src/b.txt"],
      });
    });

    expect(apiMocks.copy_batch).toHaveBeenCalledWith({
      sources: ["/src/a.txt"],
      destination: "/dst",
      overwrite: undefined,
      keepBoth: ["/src/a.txt"],
    });
    expect(apiMocks.move_batch).toHaveBeenCalledWith({
      sources: ["/src/b.txt"],
      destination: "/dst",
      overwrite: undefined,
      keepBoth: ["/src/b.txt"],
    });
  });

  it("keeps the success toast when every item moved", async () => {
    const { hook, streamOptions } = setupTransfer();
    const onComplete = vi.fn();

    await act(async () => {
      await hook.result.current.startMove({
        sources: ["/src/photos/"],
        destination: "/dst",
        onComplete,
      });
    });
    act(() => {
      streamOptions[0]?.onSuccess?.({ total: 1, succeeded: 1, failed: [] });
    });

    expect(toastMocks.success).toHaveBeenCalledWith("Moved photos");
    expect(toastMocks.error).not.toHaveBeenCalled();
    expect(onComplete).toHaveBeenCalledOnce();
  });
});
