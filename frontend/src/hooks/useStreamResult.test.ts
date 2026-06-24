import { describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api";

const apiMocks = vi.hoisted(() => ({
  streamWriteChunks: vi.fn(),
  waitForStreamResult: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    streamWriteChunks: apiMocks.streamWriteChunks,
    waitForStreamResult: apiMocks.waitForStreamResult,
  };
});

const { useStreamResult } = await import("@/hooks/useStreamResult");
const { act, renderHook } = await import("@/test/render");

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

describe("useStreamResult", () => {
  it("opens a stream, awaits the result, and calls lifecycle callbacks", async () => {
    const stream = createStream();
    const onOpen = vi.fn();
    const onSuccess = vi.fn();
    const onFinally = vi.fn();
    apiMocks.waitForStreamResult.mockResolvedValue("done");
    const { result } = renderHook(() => useStreamResult());

    await expect(
      result.current.run({
        open: () => stream,
        onOpen,
        onSuccess,
        onFinally,
      }),
    ).resolves.toBe("done");

    expect(onOpen).toHaveBeenCalledWith(stream);
    expect(onSuccess).toHaveBeenCalledWith("done");
    expect(onFinally).toHaveBeenCalledTimes(1);
  });

  it("throws by default when opening fails", async () => {
    const { result } = renderHook(() => useStreamResult());

    await expect(
      result.current.run({ open: () => null }),
    ).rejects.toMatchObject({ code: "stream_unavailable" });
  });

  it("returns undefined on handled errors when throwOnError is false", async () => {
    const onError = vi.fn();
    apiMocks.waitForStreamResult.mockRejectedValue(new Error("bad"));
    const { result } = renderHook(() => useStreamResult());

    await expect(
      result.current.run({
        open: () => createStream(),
        onError,
        throwOnError: false,
      }),
    ).resolves.toBeUndefined();

    expect(onError).toHaveBeenCalledWith(expect.any(Error));
  });

  it("writes chunked data and aborts open streams when writing fails", async () => {
    const stream = createStream();
    apiMocks.waitForStreamResult.mockResolvedValue("ignored");
    apiMocks.streamWriteChunks.mockRejectedValue(new Error("write failed"));
    const { result } = renderHook(() => useStreamResult());

    await expect(
      result.current.runChunked({
        data: new Uint8Array([1, 2, 3]),
        open: () => stream,
      }),
    ).rejects.toThrow("write failed");

    expect(stream.abort).toHaveBeenCalledTimes(1);
  });

  it("passes chunking options through to streamWriteChunks", async () => {
    const stream = createStream();
    const data = new Uint8Array([1, 2, 3]);
    apiMocks.streamWriteChunks.mockResolvedValue(undefined);
    apiMocks.waitForStreamResult.mockResolvedValue("uploaded");
    const { result } = renderHook(() => useStreamResult());

    await act(async () => {
      await result.current.runChunked({
        chunkSize: 2,
        closeAtEnd: false,
        data,
        open: () => stream,
        yieldMs: 1,
      });
    });

    expect(apiMocks.streamWriteChunks).toHaveBeenCalledWith(stream, data, {
      chunkSize: 2,
      yieldMs: 1,
      closeAtEnd: false,
      signal: undefined,
    });
  });
});
