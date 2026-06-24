import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  BridgeOpcode,
  closeStreamMux,
  decodeString,
  encodeString,
  Flags,
  getStreamMux,
  initStreamMux,
  StreamMultiplexer,
  waitForStreamMux,
} from "@/api/StreamMultiplexer";
import {
  FakeWebSocket,
  makeBridgeFrame,
  makeInboundMuxFrame,
  readBridgeFrame,
  readMuxFrame,
} from "@/test/fakeWebSocket";

const silenceConsole = () => {
  vi.spyOn(console, "log").mockImplementation(() => {});
  vi.spyOn(console, "warn").mockImplementation(() => {});
};

describe("StreamMultiplexer", () => {
  beforeEach(() => {
    FakeWebSocket.install();
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0);
    silenceConsole();
  });

  afterEach(() => {
    closeStreamMux();
  });

  function openMux() {
    const mux = new StreamMultiplexer("ws://linuxio.test/ws");
    const socket = FakeWebSocket.latest();
    socket.open();
    return { mux, socket };
  }

  it("encodes stream open frames with odd stream ids", () => {
    const { mux, socket } = openMux();
    const stream = mux.openStream("jobs.attach", encodeString("payload"));

    expect(stream.id).toBe(1);
    expect(stream.status).toBe("open");

    const muxFrame = readMuxFrame(socket.sent[0]);
    expect(muxFrame.streamID).toBe(1);
    expect(muxFrame.flags).toBe(Flags.SYN);

    const bridgeFrame = readBridgeFrame(muxFrame.payload);
    expect(bridgeFrame).toMatchObject({
      opcode: BridgeOpcode.StreamOpen,
      streamID: 1,
      payloadLength: "payload".length,
    });
    expect(decodeString(bridgeFrame.payload)).toBe("payload");
  });

  it("reuses only persistent terminal streams", () => {
    const { mux } = openMux();

    const terminalA = mux.openStream("terminal.open");
    const terminalB = mux.openStream("terminal.open");
    const requestA = mux.openStream("jobs.attach");
    const requestB = mux.openStream("jobs.attach");

    expect(terminalB).toBe(terminalA);
    expect(requestB).not.toBe(requestA);
    expect([terminalA.id, requestA.id, requestB.id]).toEqual([1, 3, 5]);
  });

  it("routes split inbound bridge data, progress, result, and close frames", () => {
    const { mux, socket } = openMux();
    const stream = mux.openStream("jobs.attach");
    const onData = vi.fn();
    const onProgress = vi.fn();
    const onResult = vi.fn();
    const onClose = vi.fn();
    stream.onData = onData;
    stream.onProgress = onProgress;
    stream.onResult = onResult;
    stream.onClose = onClose;

    const dataFrame = makeBridgeFrame(
      BridgeOpcode.StreamData,
      stream.id,
      encodeString("hello"),
    );
    socket.receive(
      makeInboundMuxFrame(stream.id, Flags.DATA, dataFrame.slice(0, 4)),
    );
    socket.receive(
      makeInboundMuxFrame(stream.id, Flags.DATA, dataFrame.slice(4)),
    );

    socket.receive(
      makeInboundMuxFrame(
        stream.id,
        Flags.DATA,
        makeBridgeFrame(
          BridgeOpcode.StreamProgress,
          stream.id,
          encodeString(JSON.stringify({ pct: 50, bytes: 5, total: 10 })),
        ),
      ),
    );
    socket.receive(
      makeInboundMuxFrame(
        stream.id,
        Flags.DATA,
        makeBridgeFrame(
          BridgeOpcode.StreamResult,
          stream.id,
          encodeString(JSON.stringify({ status: "ok", data: "done" })),
        ),
      ),
    );
    socket.receive(
      makeInboundMuxFrame(
        stream.id,
        Flags.DATA,
        makeBridgeFrame(BridgeOpcode.StreamClose, stream.id),
      ),
    );

    expect(decodeString(onData.mock.calls[0][0])).toBe("hello");
    expect(onProgress).toHaveBeenCalledWith({ pct: 50, bytes: 5, total: 10 });
    expect(onResult).toHaveBeenCalledWith({ status: "ok", data: "done" });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(stream.status).toBe("closed");
  });

  it("buffers detached data and does not duplicate buffered bytes on reattach", () => {
    const { mux, socket } = openMux();
    const stream = mux.openStream("terminal.open");

    socket.receive(
      makeInboundMuxFrame(
        stream.id,
        Flags.DATA,
        makeBridgeFrame(BridgeOpcode.StreamData, stream.id, encodeString("a")),
      ),
    );
    socket.receive(
      makeInboundMuxFrame(
        stream.id,
        Flags.DATA,
        makeBridgeFrame(BridgeOpcode.StreamData, stream.id, encodeString("b")),
      ),
    );

    const firstAttach = vi.fn();
    stream.onData = firstAttach;
    expect(firstAttach.mock.calls.map(([data]) => decodeString(data))).toEqual([
      "a",
      "b",
    ]);

    stream.onData = null;
    socket.receive(
      makeInboundMuxFrame(
        stream.id,
        Flags.DATA,
        makeBridgeFrame(BridgeOpcode.StreamData, stream.id, encodeString("c")),
      ),
    );

    const secondAttach = vi.fn();
    stream.onData = secondAttach;
    expect(secondAttach.mock.calls.map(([data]) => decodeString(data))).toEqual(
      ["ab", "c"],
    );
  });

  it("sends write, resize, close, and abort bridge opcodes", () => {
    const { mux, socket } = openMux();
    const stream = mux.openStream("terminal.open");
    socket.sent = [];

    stream.write(encodeString("input"));
    stream.resize(200_000, -5);
    stream.close();
    stream.abort();

    const opcodes = socket.sent
      .slice(0, -1)
      .map((frame) => readBridgeFrame(readMuxFrame(frame).payload));
    expect(opcodes.map((frame) => frame.opcode)).toEqual([
      BridgeOpcode.StreamData,
      BridgeOpcode.StreamResize,
      BridgeOpcode.StreamClose,
      BridgeOpcode.StreamAbort,
    ]);
    expect(readMuxFrame(socket.sent.at(-1)!).flags).toBe(Flags.RST);

    const resizePayload = opcodes[1].payload;
    const resizeView = new DataView(
      resizePayload.buffer,
      resizePayload.byteOffset,
      resizePayload.byteLength,
    );
    expect(resizeView.getUint16(0, false)).toBe(0xffff);
    expect(resizeView.getUint16(2, false)).toBe(0);
  });

  it("notifies status and updating listeners and unsubscribes cleanly", () => {
    const mux = new StreamMultiplexer("ws://linuxio.test/ws");
    const statusListener = vi.fn();
    const updatingListener = vi.fn();
    const unsubscribeStatus = mux.addStatusListener(statusListener);
    const unsubscribeUpdating = mux.addUpdatingListener(updatingListener);

    FakeWebSocket.latest().open();
    mux.setUpdating(true);
    unsubscribeStatus();
    unsubscribeUpdating();
    mux.setUpdating(false);
    FakeWebSocket.latest().closeWith({ code: 1006 });

    expect(statusListener).toHaveBeenCalledWith("open");
    expect(statusListener).not.toHaveBeenCalledWith("closed");
    expect(updatingListener).toHaveBeenCalledTimes(1);
    expect(updatingListener).toHaveBeenCalledWith(true);
  });

  it("reconnects after non-auth closes without duplicate timers", () => {
    const { socket } = openMux();

    socket.closeWith({ code: 1006 });
    expect(FakeWebSocket.instances).toHaveLength(1);

    vi.advanceTimersByTime(999);
    expect(FakeWebSocket.instances).toHaveLength(1);

    vi.advanceTimersByTime(1);
    expect(FakeWebSocket.instances).toHaveLength(2);
  });

  it("moves to error on auth close code and repeated rapid closes", () => {
    const direct = new StreamMultiplexer("ws://linuxio.test/ws");
    const directStatus = vi.fn();
    direct.addStatusListener(directStatus);
    FakeWebSocket.latest().open();
    FakeWebSocket.latest().closeWith({ code: 1008 });
    expect(direct.status).toBe("error");
    expect(directStatus).toHaveBeenCalledWith("error");
    direct.close();

    const rapid = new StreamMultiplexer("ws://linuxio.test/ws");
    const rapidStatus = vi.fn();
    rapid.addStatusListener(rapidStatus);

    for (let i = 0; i < 3; i += 1) {
      const socket = FakeWebSocket.latest();
      socket.open();
      socket.closeWith({ code: 1006 });
      if (i < 2) {
        vi.advanceTimersByTime(1000);
      }
    }

    expect(rapid.status).toBe("error");
    expect(rapidStatus).toHaveBeenCalledWith("error");
  });

  it("manages the singleton lifecycle and waitForStreamMux results", async () => {
    const mux = initStreamMux();
    expect(getStreamMux()).toBe(mux);
    expect(FakeWebSocket.latest().url).toBe("wss://linuxio.test/ws");

    const ready = waitForStreamMux(100);
    FakeWebSocket.latest().open();
    await expect(ready).resolves.toBe(true);

    closeStreamMux();
    expect(getStreamMux()).toBeNull();

    initStreamMux();
    const timedOut = waitForStreamMux(25);
    vi.advanceTimersByTime(25);
    await expect(timedOut).resolves.toBe(false);
  });
});
