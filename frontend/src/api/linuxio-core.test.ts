import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api/StreamMultiplexer";

const muxMocks = vi.hoisted(() => ({
  encodeString: vi.fn((value: string) => new TextEncoder().encode(value)),
  getStreamMux: vi.fn(),
  initStreamMux: vi.fn(),
  waitForStreamMux: vi.fn(),
}));

const streamHelperMocks = vi.hoisted(() => ({
  waitForStreamResult: vi.fn(),
}));

vi.mock("@/api/StreamMultiplexer", async () => {
  const actual = await vi.importActual<
    typeof import("@/api/StreamMultiplexer")
  >("@/api/StreamMultiplexer");
  return {
    ...actual,
    encodeString: muxMocks.encodeString,
    getStreamMux: muxMocks.getStreamMux,
    initStreamMux: muxMocks.initStreamMux,
    waitForStreamMux: muxMocks.waitForStreamMux,
  };
});

vi.mock("@/api/stream-helpers", () => ({
  waitForStreamResult: streamHelperMocks.waitForStreamResult,
}));

const { LinuxIOError, ensureLoaderRequestReady, request } =
  await import("@/api/linuxio-core");

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
    type: "request",
    write: vi.fn(),
    ...overrides,
  };
}

function createMux(stream = createStream()) {
  const openStream = vi.fn((type: string, initialPayload?: Uint8Array) => {
    void type;
    void initialPayload;
    return stream;
  });
  return {
    addStatusListener: vi.fn(() => () => undefined),
    close: vi.fn(),
    getStream: vi.fn(),
    offUpdating: vi.fn(),
    onUpdating: vi.fn(() => () => undefined),
    openStream,
    removeStatusListener: vi.fn(),
    setUpdating: vi.fn(),
    status: "open",
    url: "ws://localhost/ws",
  };
}

describe("linuxio-core request", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    muxMocks.waitForStreamMux.mockResolvedValue(true);
  });

  it("opens a request stream with the generated route and payload", async () => {
    const stream = createStream();
    const mux = createMux(stream);
    muxMocks.getStreamMux.mockReturnValue(mux);
    streamHelperMocks.waitForStreamResult.mockResolvedValue({ ok: true });

    await expect(
      request("system", "get_info", { verbose: true }, { timeout: 5000 }),
    ).resolves.toEqual({ ok: true });

    expect(muxMocks.waitForStreamMux).toHaveBeenCalledWith(5000);
    expect(mux.openStream).toHaveBeenCalledTimes(1);
    expect(mux.openStream.mock.calls[0][0]).toBe("system.get_info");
    const streamPayload = mux.openStream.mock.calls[0][1];
    expect(streamPayload).toBeDefined();
    expect(Array.from(streamPayload as Uint8Array)).toEqual(
      Array.from(muxMocks.encodeString.mock.results[0].value),
    );
    const encodedJson = muxMocks.encodeString.mock.calls[0][0];
    expect(JSON.parse(encodedJson)).toEqual({
      route: "system.get_info",
      request: { verbose: true },
    });
    expect(streamHelperMocks.waitForStreamResult).toHaveBeenCalledWith(
      stream,
      expect.objectContaining({
        closeMessage: "Connection closed before receiving result",
        signal: expect.any(AbortSignal),
      }),
    );
  });

  it("initializes an absent mux and waits for it to open for loader readiness", async () => {
    const mux = createMux();
    muxMocks.getStreamMux.mockReturnValueOnce(null).mockReturnValue(mux);
    muxMocks.initStreamMux.mockReturnValue(mux);

    await expect(ensureLoaderRequestReady(5000)).resolves.toBe(mux);

    expect(muxMocks.initStreamMux).toHaveBeenCalledTimes(1);
    expect(muxMocks.waitForStreamMux).toHaveBeenCalledWith(5000);
  });

  it("reuses an open mux for loader readiness without reinitializing it", async () => {
    const mux = createMux();
    muxMocks.getStreamMux.mockReturnValue(mux);

    await expect(ensureLoaderRequestReady(5000)).resolves.toBe(mux);

    expect(muxMocks.initStreamMux).not.toHaveBeenCalled();
    expect(muxMocks.waitForStreamMux).toHaveBeenCalledWith(5000);
  });

  it("keeps ordinary requests owned by the authenticated mux lifecycle", async () => {
    muxMocks.getStreamMux.mockReturnValue(null);

    await expect(request("system", "get_info")).rejects.toMatchObject({
      code: "not_initialized",
      message: "StreamMux not initialized",
    });
    expect(muxMocks.initStreamMux).not.toHaveBeenCalled();
  });

  it("reinitializes closed muxes before waiting for loader readiness", async () => {
    const stream = createStream();
    const closedMux = createMux(stream);
    closedMux.status = "closed";
    const openMux = createMux(stream);
    muxMocks.getStreamMux
      .mockReturnValueOnce(closedMux)
      .mockReturnValue(openMux);
    await expect(ensureLoaderRequestReady(5000)).resolves.toBe(openMux);

    expect(muxMocks.initStreamMux).toHaveBeenCalledTimes(1);
  });

  it("reinitializes a closed mux before an ordinary request opens its stream", async () => {
    const stream = createStream();
    const closedMux = createMux(stream);
    closedMux.status = "closed";
    const openMux = createMux(stream);
    muxMocks.getStreamMux
      .mockReturnValueOnce(closedMux)
      .mockReturnValue(openMux);
    streamHelperMocks.waitForStreamResult.mockResolvedValue("ready");

    await expect(request("system", "get_info")).resolves.toBe("ready");

    expect(muxMocks.initStreamMux).toHaveBeenCalledTimes(1);
    expect(openMux.openStream).toHaveBeenCalledTimes(1);
    expect(openMux.openStream.mock.calls[0][0]).toBe("system.get_info");
  });

  it("throws connection_closed when the mux does not become ready", async () => {
    muxMocks.getStreamMux.mockReturnValue(createMux());
    muxMocks.waitForStreamMux.mockResolvedValue(false);

    await expect(ensureLoaderRequestReady(5000)).rejects.toMatchObject({
      code: "connection_closed",
    });
  });

  it("throws connection_closed when an ordinary request mux does not become ready", async () => {
    muxMocks.getStreamMux.mockReturnValue(createMux());
    muxMocks.waitForStreamMux.mockResolvedValue(false);

    await expect(request("system", "get_info")).rejects.toMatchObject({
      code: "connection_closed",
    });
  });

  it("rejects with a typed connection error when readiness fails", async () => {
    muxMocks.getStreamMux.mockReturnValue(createMux());
    muxMocks.waitForStreamMux.mockRejectedValue(new Error("socket failed"));

    await expect(ensureLoaderRequestReady(5000)).rejects.toMatchObject({
      code: "connection_closed",
      message: "Connection closed before receiving result",
    });
  });

  it("converts request aborts into timeout errors and closes the stream", async () => {
    vi.useFakeTimers();
    const stream = createStream();
    muxMocks.getStreamMux.mockReturnValue(createMux(stream));
    streamHelperMocks.waitForStreamResult.mockImplementation(
      (_stream: Stream, options: { signal: AbortSignal }) =>
        new Promise((_resolve, reject) => {
          options.signal.addEventListener("abort", () => {
            const error = new Error("aborted");
            error.name = "AbortError";
            reject(error);
          });
        }),
    );

    const promise = request("system", "slow", {}, { timeout: 50 });
    const expectation = expect(promise).rejects.toMatchObject({
      code: "timeout",
      message: "Request timeout",
    });
    await vi.advanceTimersByTimeAsync(50);

    await expectation;
    expect(stream.close).toHaveBeenCalledTimes(1);
  });

  it("retries connection-closed errors only when the retry policy allows it", async () => {
    const stream = createStream();
    const mux = createMux(stream);
    muxMocks.getStreamMux.mockReturnValue(mux);
    streamHelperMocks.waitForStreamResult
      .mockRejectedValueOnce(
        new LinuxIOError(
          "Connection closed before receiving result",
          "connection_closed",
        ),
      )
      .mockResolvedValueOnce("retried");

    await expect(
      request("system", "get_info", {}, { retryPolicy: "connection_closed" }),
    ).resolves.toBe("retried");

    expect(mux.openStream).toHaveBeenCalledTimes(2);
  });

  it("does not retry connection-closed errors by default", async () => {
    const mux = createMux();
    muxMocks.getStreamMux.mockReturnValue(mux);
    streamHelperMocks.waitForStreamResult.mockRejectedValue(
      new LinuxIOError(
        "Connection closed before receiving result",
        "connection_closed",
      ),
    );

    await expect(request("system", "get_info")).rejects.toMatchObject({
      code: "connection_closed",
    });
    expect(mux.openStream).toHaveBeenCalledTimes(1);
  });
});
