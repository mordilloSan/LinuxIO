import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api";

const streamRef: { current: Stream | null } = { current: null };

const apiMocks = vi.hoisted(() => ({
  decodeString: vi.fn((data: Uint8Array) => new TextDecoder().decode(data)),
  muxIsOpen: true,
}));

const liveStreamMocks = vi.hoisted(() => ({
  closeStream: vi.fn(() => {
    streamRef.current = null;
  }),
  openStream: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    decodeString: apiMocks.decodeString,
    useStreamMux: () => ({ isOpen: apiMocks.muxIsOpen }),
  };
});

vi.mock("@/hooks/useLiveStream", () => ({
  useLiveStream: () => ({
    streamRef,
    openStream: liveStreamMocks.openStream,
    closeStream: liveStreamMocks.closeStream,
  }),
}));

const { useLogStream } = await import("@/hooks/useLogStream");
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

function setupOpenStream() {
  let handlers: Parameters<typeof liveStreamMocks.openStream>[0] | undefined;
  liveStreamMocks.openStream.mockImplementation((options) => {
    handlers = options;
    const opened = options.open();
    streamRef.current = opened;
    if (!opened) {
      options.onOpenError?.();
    }
  });
  return {
    get handlers() {
      if (!handlers) throw new Error("stream has not opened");
      return handlers;
    },
  };
}

describe("useLogStream", () => {
  beforeEach(() => {
    apiMocks.muxIsOpen = true;
    streamRef.current = null;
    liveStreamMocks.closeStream.mockImplementation(() => {
      streamRef.current = null;
    });
    liveStreamMocks.openStream.mockReset();
  });

  it("does not open a stream when closed or when the mux is unavailable", () => {
    const createStreamFn = vi.fn(() => createStream());

    renderHook(() =>
      useLogStream({ createStream: createStreamFn, open: false }),
    );

    expect(liveStreamMocks.openStream).not.toHaveBeenCalled();

    apiMocks.muxIsOpen = false;
    renderHook(() =>
      useLogStream({ createStream: createStreamFn, open: true }),
    );

    expect(liveStreamMocks.openStream).not.toHaveBeenCalled();
  });

  it("opens with the initial tail and appends decoded data", async () => {
    const harness = setupOpenStream();
    const stream = createStream();
    const createStreamFn = vi.fn(() => stream);
    const { result } = renderHook(() =>
      useLogStream({
        createStream: createStreamFn,
        initialTail: "500",
        open: true,
      }),
    );

    expect(createStreamFn).toHaveBeenCalledWith("500");
    expect(result.current.isLoading).toBe(true);

    await act(async () => {
      harness.handlers.onData?.(new TextEncoder().encode("line 1\n"));
      harness.handlers.onData?.(new TextEncoder().encode("line 2\n"));
    });

    expect(result.current.logs).toBe("line 1\nline 2\n");
    expect(result.current.isLoading).toBe(false);
  });

  it("clears loading after initial stream silence", () => {
    vi.useFakeTimers();
    setupOpenStream();
    const { result } = renderHook(() =>
      useLogStream({ createStream: () => createStream(), open: true }),
    );

    expect(result.current.isLoading).toBe(true);

    act(() => {
      vi.advanceTimersByTime(1500);
    });

    expect(result.current.isLoading).toBe(false);
  });

  it("surfaces stream result errors", async () => {
    const harness = setupOpenStream();
    const { result } = renderHook(() =>
      useLogStream({ createStream: () => createStream(), open: true }),
    );

    await act(async () => {
      harness.handlers.onResult?.({
        status: "error",
        error: "journald unavailable",
      });
    });

    expect(result.current.error).toBe("journald unavailable");
    expect(result.current.isLoading).toBe(false);
  });

  it("closes live streams when paused and reopens with the live tail", async () => {
    setupOpenStream();
    const createStreamFn = vi.fn(() => createStream());
    const { result } = renderHook(() =>
      useLogStream({
        createStream: createStreamFn,
        liveTail: "0",
        open: true,
      }),
    );

    expect(liveStreamMocks.openStream).toHaveBeenCalledTimes(1);

    await act(async () => {
      result.current.setLiveMode(false);
    });
    expect(liveStreamMocks.closeStream).toHaveBeenCalledTimes(1);

    await act(async () => {
      result.current.setLiveMode(true);
    });

    expect(liveStreamMocks.openStream).toHaveBeenCalledTimes(2);
    expect(createStreamFn).toHaveBeenLastCalledWith("0");
  });

  it("resetState closes the stream and restores initial state", async () => {
    const harness = setupOpenStream();
    const { result } = renderHook(() =>
      useLogStream({ createStream: () => createStream(), open: true }),
    );

    await act(async () => {
      harness.handlers.onData?.(new TextEncoder().encode("old logs"));
      harness.handlers.onResult?.({ status: "error", error: "failed" });
    });
    expect(result.current.logs).toBe("old logs");
    expect(result.current.error).toBe("failed");

    act(() => result.current.resetState());

    expect(liveStreamMocks.closeStream).toHaveBeenCalled();
    expect(result.current.logs).toBe("");
    expect(result.current.error).toBeNull();
    expect(result.current.liveMode).toBe(true);
    expect(result.current.isLoading).toBe(true);
  });
});
