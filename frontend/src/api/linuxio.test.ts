import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api/StreamMultiplexer";

const mocks = vi.hoisted(() => ({
  getStreamMux: vi.fn(),
  request: vi.fn(),
}));

vi.mock("@/api/linuxio-core", () => ({
  request: mocks.request,
}));

vi.mock("@/api/StreamMultiplexer", async () => {
  const actual = await vi.importActual<
    typeof import("@/api/StreamMultiplexer")
  >("@/api/StreamMultiplexer");
  return { ...actual, getStreamMux: mocks.getStreamMux };
});

const { openAppUpdateStream } = await import("@/api/linuxio");

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
    type: "tasks.watch",
    write: vi.fn(),
  };
}

describe("session-bound app-update stream", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("detaches on close without implicitly canceling the Task", async () => {
    const watch = createStream();
    mocks.getStreamMux.mockReturnValue({
      openStream: vi.fn().mockReturnValue(watch),
      status: "open",
    });
    mocks.request.mockResolvedValue({ id: "operation-1", state: "running" });

    const stream = openAppUpdateStream(
      "00000000-0000-4000-8000-000000000042",
      "v2.3.4",
    );
    expect(stream).not.toBeNull();
    await vi.waitFor(() => expect(mocks.request).toHaveBeenCalledTimes(1));

    stream?.close();

    expect(watch.close).toHaveBeenCalledTimes(1);
    expect(mocks.request).not.toHaveBeenCalledWith(
      "tasks",
      "cancel",
      expect.anything(),
    );
  });

  it("cancels explicitly when aborted", async () => {
    const watch = createStream();
    mocks.getStreamMux.mockReturnValue({
      openStream: vi.fn().mockReturnValue(watch),
      status: "open",
    });
    mocks.request.mockResolvedValue({ id: "operation-1", state: "running" });

    const stream = openAppUpdateStream("00000000-0000-4000-8000-000000000042");
    await vi.waitFor(() => expect(mocks.request).toHaveBeenCalledTimes(1));
    stream?.abort();

    await vi.waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith("tasks", "cancel", {
        taskId: "operation-1",
      }),
    );
    expect(watch.abort).toHaveBeenCalledTimes(1);
  });
});
