import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

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

function createMux(stream: Stream | null = createStream()) {
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

  afterEach(() => {
    vi.useRealTimers();
  });

  it("opens a request stream with the generated route and payload", async () => {
    const stream = createStream();
    const mux = createMux(stream);
    muxMocks.getStreamMux.mockReturnValue(mux);
    streamHelperMocks.waitForStreamResult.mockResolvedValue({ ok: true });

    await expect(
      request("system", "get_info", { verbose: true }, { timeout: 5000 }),
    ).resolves.toEqual({ ok: true });

    // request() re-derives the remaining budget from the wall clock, so this is
    // 5000 minus however many whole ms have elapsed since the deadline was set.
    expect(muxMocks.waitForStreamMux).toHaveBeenCalledWith(
      expect.closeTo(5000, -2),
    );
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

  it("classifies a failed initial SYN as connection_unavailable", async () => {
    const mux = createMux(null);
    muxMocks.getStreamMux.mockReturnValue(mux);

    await expect(request("system", "get_info")).rejects.toMatchObject({
      code: "connection_unavailable",
      message: "Connection unavailable before request was sent",
    });

    expect(mux.openStream).toHaveBeenCalledTimes(1);
    expect(streamHelperMocks.waitForStreamResult).not.toHaveBeenCalled();
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

  it("does not initialize or wait for an already-aborted loader", async () => {
    const controller = new AbortController();
    controller.abort(new DOMException("superseded", "AbortError"));

    await expect(
      ensureLoaderRequestReady(5000, controller.signal),
    ).rejects.toMatchObject({ name: "AbortError" });

    expect(muxMocks.getStreamMux).not.toHaveBeenCalled();
    expect(muxMocks.initStreamMux).not.toHaveBeenCalled();
    expect(muxMocks.waitForStreamMux).not.toHaveBeenCalled();
  });

  it("propagates navigation abort while loader readiness is pending", async () => {
    const mux = createMux();
    const controller = new AbortController();
    muxMocks.getStreamMux.mockReturnValue(mux);
    muxMocks.waitForStreamMux.mockImplementation(
      (_timeoutMs: number, signal: AbortSignal) =>
        new Promise<boolean>((_resolve, reject) => {
          signal.addEventListener(
            "abort",
            () => reject(signal.reason ?? new Error("aborted")),
            { once: true },
          );
        }),
    );

    const readiness = ensureLoaderRequestReady(5000, controller.signal);
    await vi.waitFor(() =>
      expect(muxMocks.waitForStreamMux).toHaveBeenCalledWith(
        5000,
        controller.signal,
      ),
    );
    controller.abort(new DOMException("superseded", "AbortError"));

    await expect(readiness).rejects.toMatchObject({ name: "AbortError" });
  });

  it("keeps ordinary requests owned by the authenticated mux lifecycle", async () => {
    muxMocks.getStreamMux.mockReturnValue(null);

    await expect(request("system", "get_info")).rejects.toMatchObject({
      code: "connection_unavailable",
      message: "Connection unavailable before request was sent",
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

  it("throws connection_unavailable when loader readiness times out", async () => {
    muxMocks.getStreamMux.mockReturnValue(createMux());
    muxMocks.waitForStreamMux.mockResolvedValue(false);

    await expect(ensureLoaderRequestReady(5000)).rejects.toMatchObject({
      code: "connection_unavailable",
    });
  });

  it("throws connection_unavailable when request readiness times out", async () => {
    muxMocks.getStreamMux.mockReturnValue(createMux());
    muxMocks.waitForStreamMux.mockResolvedValue(false);

    await expect(request("system", "get_info")).rejects.toMatchObject({
      code: "connection_unavailable",
    });
  });

  it("rejects with a typed connection error when readiness fails", async () => {
    muxMocks.getStreamMux.mockReturnValue(createMux());
    muxMocks.waitForStreamMux.mockRejectedValue(new Error("socket failed"));

    await expect(ensureLoaderRequestReady(5000)).rejects.toMatchObject({
      code: "connection_unavailable",
      message: "Connection unavailable before request was sent",
    });
  });

  it("converts the internal deadline abort into a timeout error", async () => {
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
  });

  it("keeps one deadline across a connection-loss retry", async () => {
    vi.useFakeTimers();
    const stream = createStream();
    const mux = createMux(stream);
    muxMocks.getStreamMux.mockReturnValue(mux);
    streamHelperMocks.waitForStreamResult
      .mockImplementationOnce(
        () =>
          new Promise((_resolve, reject) => {
            setTimeout(
              () =>
                reject(
                  new LinuxIOError(
                    "Connection closed before receiving result",
                    "connection_closed",
                  ),
                ),
              60,
            );
          }),
      )
      .mockImplementationOnce(
        (_stream: Stream, options: { signal: AbortSignal }) =>
          new Promise((_resolve, reject) => {
            options.signal.addEventListener("abort", () => {
              const error = new Error("aborted");
              error.name = "AbortError";
              reject(error);
            });
          }),
      );

    const promise = request(
      "system",
      "slow",
      {},
      {
        retryPolicy: "connection_loss",
        timeout: 100,
      },
    );
    const expectation = expect(promise).rejects.toMatchObject({
      code: "timeout",
      message: "Request timeout",
    });
    await vi.advanceTimersByTimeAsync(60);
    await vi.advanceTimersByTimeAsync(40);

    await expectation;
    expect(mux.openStream).toHaveBeenCalledTimes(2);
  });

  it("propagates a caller abort without converting it to a timeout", async () => {
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
    const controller = new AbortController();

    const promise = request(
      "system",
      "slow",
      {},
      { signal: controller.signal, timeout: 5000 },
    );
    await vi.waitFor(() =>
      expect(streamHelperMocks.waitForStreamResult).toHaveBeenCalled(),
    );
    controller.abort();

    await expect(promise).rejects.toMatchObject({ name: "AbortError" });
    expect(muxMocks.waitForStreamMux).toHaveBeenCalledWith(
      expect.closeTo(5000, -2),
      controller.signal,
    );
  });

  it("does not open a stream for an already-aborted caller", async () => {
    const mux = createMux();
    muxMocks.getStreamMux.mockReturnValue(mux);
    const controller = new AbortController();
    controller.abort();

    await expect(
      request("system", "slow", {}, { signal: controller.signal }),
    ).rejects.toMatchObject({ name: "AbortError" });
    expect(mux.openStream).not.toHaveBeenCalled();
  });

  it("retries post-SYN connection loss only when the policy allows it", async () => {
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
      request("system", "get_info", {}, { retryPolicy: "connection_loss" }),
    ).resolves.toBe("retried");

    expect(mux.openStream).toHaveBeenCalledTimes(2);
  });

  it("retries a failed initial SYN only when the policy allows it", async () => {
    const stream = createStream();
    const mux = createMux(null);
    mux.openStream.mockReturnValueOnce(null).mockReturnValueOnce(stream);
    muxMocks.getStreamMux.mockReturnValue(mux);
    streamHelperMocks.waitForStreamResult.mockResolvedValue("retried");

    await expect(
      request("system", "get_info", {}, { retryPolicy: "connection_loss" }),
    ).resolves.toBe("retried");

    expect(mux.openStream).toHaveBeenCalledTimes(2);
    expect(streamHelperMocks.waitForStreamResult).toHaveBeenCalledTimes(1);
  });

  it("preserves numeric backend errors without retrying them", async () => {
    const backendError = new LinuxIOError("Conflict", 409);
    const mux = createMux();
    muxMocks.getStreamMux.mockReturnValue(mux);
    streamHelperMocks.waitForStreamResult.mockRejectedValue(backendError);

    await expect(
      request("system", "get_info", {}, { retryPolicy: "connection_loss" }),
    ).rejects.toBe(backendError);

    expect(mux.openStream).toHaveBeenCalledTimes(1);
  });

  it("reports post-SYN connection loss as outcome_unknown without retrying by default", async () => {
    const mux = createMux();
    muxMocks.getStreamMux.mockReturnValue(mux);
    streamHelperMocks.waitForStreamResult.mockRejectedValue(
      new LinuxIOError(
        "Connection closed before receiving result",
        "connection_closed",
      ),
    );

    await expect(request("system", "get_info")).rejects.toMatchObject({
      code: "outcome_unknown",
      message: "Connection closed before the server confirmed the outcome",
    });
    expect(mux.openStream).toHaveBeenCalledTimes(1);
  });
});
