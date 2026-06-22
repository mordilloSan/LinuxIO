import { describe, expect, it, vi } from "vitest";

import { createStreamMessageChannel } from "./stream-channel";

import type { Stream } from "./StreamMultiplexer";

function fakeStream(): Stream & {
  writes: Uint8Array[];
  emitClose: () => void;
  emitData: (data: Uint8Array) => void;
} {
  const stream: Stream & {
    writes: Uint8Array[];
    emitClose: () => void;
    emitData: (data: Uint8Array) => void;
  } = {
    abort: vi.fn(),
    close: vi.fn(),
    emitClose: () => stream.onClose?.(),
    emitData: (data) => stream.onData?.(data),
    id: 1,
    onClose: null,
    onData: null,
    onProgress: null,
    onResult: null,
    resize: vi.fn(),
    status: "open",
    type: "virt.console_open",
    write: (data) => stream.writes.push(data),
    writes: [],
  };
  return stream;
}

describe("StreamMessageChannel", () => {
  it("forwards sent bytes to the stream", () => {
    const stream = fakeStream();
    const channel = createStreamMessageChannel(stream);

    channel.send(new Uint8Array([1, 2, 3]).subarray(1));
    channel.send("ok");

    expect(Array.from(stream.writes[0])).toEqual([2, 3]);
    expect(new TextDecoder().decode(stream.writes[1])).toBe("ok");
  });

  it("emits stream bytes as array buffer messages", () => {
    const stream = fakeStream();
    const channel = createStreamMessageChannel(stream);
    const onMessage = vi.fn();
    const listener = vi.fn();
    channel.onmessage = onMessage;
    channel.addEventListener("message", listener);

    stream.emitData(new Uint8Array([4, 5, 6]));

    expect(onMessage).toHaveBeenCalledTimes(1);
    expect(listener).toHaveBeenCalledTimes(1);
    expect(Array.from(new Uint8Array(onMessage.mock.calls[0][0].data))).toEqual(
      [4, 5, 6],
    );

    channel.removeEventListener("message", listener);
    stream.emitData(new Uint8Array([7]));
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it("replays data buffered before a consumer attaches", async () => {
    const stream = fakeStream();
    const channel = createStreamMessageChannel(stream);

    stream.emitData(new Uint8Array([82, 70, 66]));

    const onMessage = vi.fn();
    channel.onmessage = onMessage;

    expect(onMessage).not.toHaveBeenCalled();

    await Promise.resolve();

    expect(onMessage).toHaveBeenCalledTimes(1);
    expect(Array.from(new Uint8Array(onMessage.mock.calls[0][0].data))).toEqual(
      [82, 70, 66],
    );
  });

  it("preserves order between buffered and live data", async () => {
    const stream = fakeStream();
    const channel = createStreamMessageChannel(stream);

    stream.emitData(new Uint8Array([1]));

    const received: number[] = [];
    channel.onmessage = (event) => {
      received.push(...new Uint8Array(event.data));
    };
    stream.emitData(new Uint8Array([2]));

    await Promise.resolve();

    expect(received).toEqual([1, 2]);
  });

  it("closes through the underlying stream", () => {
    const stream = fakeStream();
    const channel = createStreamMessageChannel(stream);
    const onClose = vi.fn();
    channel.onclose = onClose;

    channel.close();
    stream.emitClose();

    expect(stream.close).toHaveBeenCalled();
    expect(channel.readyState).toBe("closed");
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("forwards stream result frames to the result handler", () => {
    const stream = fakeStream();
    const onResult = vi.fn();
    createStreamMessageChannel(stream, { onResult });

    stream.onResult?.({ status: "error", error: "no socket" });

    expect(onResult).toHaveBeenCalledWith({
      status: "error",
      error: "no socket",
    });
  });

  it("reports errors instead of writing after close", () => {
    const stream = fakeStream();
    const channel = createStreamMessageChannel(stream);
    const onError = vi.fn();
    channel.onerror = onError;

    stream.emitClose();
    channel.send(new Uint8Array([1]));

    expect(stream.writes).toEqual([]);
    expect(onError).toHaveBeenCalledTimes(1);
  });
});
