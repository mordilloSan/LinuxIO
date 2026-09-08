import { describe, expect, it, vi } from "vitest";

import { LinuxIOError } from "@/api/linuxio-core";
import {
  bindStreamHandlers,
  streamWriteChunks,
  waitForStreamResult,
} from "@/api/stream-helpers";
import {
  BridgeOpcode,
  Flags,
  StreamMultiplexer,
  type Stream,
} from "@/api/StreamMultiplexer";
import {
  FakeWebSocket,
  readBridgeFrame,
  readMuxFrame,
} from "@/test/fakeWebSocket";

function createStream(overrides: Partial<Stream> = {}): Stream {
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
    type: "test",
    write: vi.fn(),
    ...overrides,
  };
}

describe("stream helpers", () => {
  it("binds and cleans up stream handlers", () => {
    const stream = createStream();
    const cleanup = bindStreamHandlers(stream, {
      onClose: vi.fn(),
      onData: vi.fn(),
      onProgress: vi.fn(),
      onResult: vi.fn(),
    });

    expect(stream.onClose).toBeTypeOf("function");
    expect(stream.onData).toBeTypeOf("function");
    expect(stream.onProgress).toBeTypeOf("function");
    expect(stream.onResult).toBeTypeOf("function");

    cleanup();
    expect(stream.onClose).toBeNull();
    expect(stream.onData).toBeNull();
    expect(stream.onProgress).toBeNull();
    expect(stream.onResult).toBeNull();
  });

  it("resolves ok result frames and detaches handlers", async () => {
    const stream = createStream();
    const promise = waitForStreamResult<string>(stream);

    stream.onResult?.({ status: "ok", data: "done" });

    await expect(promise).resolves.toBe("done");
    expect(stream.onResult).toBeNull();
    expect(stream.onClose).toBeNull();
  });

  it("rejects error result frames and close-before-result", async () => {
    const errorStream = createStream();
    const errorPromise = waitForStreamResult(errorStream);
    errorStream.onResult?.({ status: "error", error: "bad", code: 400 });
    await expect(errorPromise).rejects.toMatchObject({
      message: "bad",
      code: 400,
    });

    const closeStream = createStream();
    const closePromise = waitForStreamResult(closeStream, {
      closeMessage: "closed early",
    });
    closeStream.onClose?.();
    await expect(closePromise).rejects.toMatchObject({
      message: "closed early",
      code: "connection_closed",
    });
  });

  it("handles abort signals with abort, close, and none policies", async () => {
    const abortController = new AbortController();
    const abortStream = createStream();
    const abortPromise = waitForStreamResult(abortStream, {
      signal: abortController.signal,
    });
    abortController.abort();
    await expect(abortPromise).rejects.toMatchObject({ name: "AbortError" });
    expect(abortStream.abort).toHaveBeenCalledTimes(1);

    const closeController = new AbortController();
    const closeStream = createStream();
    const closePromise = waitForStreamResult(closeStream, {
      closeOnAbort: "close",
      signal: closeController.signal,
    });
    closeController.abort();
    await expect(closePromise).rejects.toMatchObject({ name: "AbortError" });
    expect(closeStream.close).toHaveBeenCalledTimes(1);

    const noneController = new AbortController();
    const noneStream = createStream();
    const nonePromise = waitForStreamResult(noneStream, {
      closeOnAbort: "none",
      signal: noneController.signal,
    });
    noneController.abort();
    await expect(nonePromise).rejects.toMatchObject({ name: "AbortError" });
    expect(noneStream.abort).not.toHaveBeenCalled();
    expect(noneStream.close).not.toHaveBeenCalled();
  });

  it("rejects unavailable streams", async () => {
    await expect(waitForStreamResult(null)).rejects.toBeInstanceOf(
      LinuxIOError,
    );
  });

  it("writes a byte view in order without retaining already-sent source bytes", async () => {
    vi.useFakeTimers();
    FakeWebSocket.install();
    const mux = new StreamMultiplexer("ws://linuxio.test/ws");
    const socket = FakeWebSocket.latest();
    socket.open();
    try {
      const stream = mux.openStream("tasks.data")!;
      const data = new Uint8Array([99, 1, 2, 3, 4, 5, 99]).subarray(1, -1);
      const promise = streamWriteChunks(stream, data, {
        chunkSize: 2,
        yieldMs: 5,
      });
      expect(socket.sent).toHaveLength(2); // SYN and first chunk, before yielding.
      data.fill(42, 0, 2);
      await vi.runAllTimersAsync();
      await promise;

      const chunks = socket.sent.slice(1, -1).map((frame) => {
        const outer = readMuxFrame(frame);
        expect(outer.flags).toBe(Flags.DATA);
        const inner = readBridgeFrame(outer.payload);
        expect(inner.opcode).toBe(BridgeOpcode.StreamData);
        return Array.from(inner.payload);
      });
      expect(chunks).toEqual([[1, 2], [3, 4], [5]]);
      const close = readMuxFrame(socket.sent.at(-1)!);
      expect(close.flags).toBe(Flags.FIN);
      expect(readBridgeFrame(close.payload).opcode).toBe(
        BridgeOpcode.StreamClose,
      );
    } finally {
      mux.close();
      vi.useRealTimers();
    }
  });
});
