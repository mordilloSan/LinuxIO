import { describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api";
import { useLiveStream } from "@/hooks/useLiveStream";
import { act, renderHook } from "@/test/render";

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
    type: "logs",
    write: vi.fn(),
    ...overrides,
  };
}

describe("useLiveStream", () => {
  it("opens, binds handlers, and ignores duplicate opens", () => {
    const stream = createStream();
    const open = vi.fn(() => stream);
    const onOpen = vi.fn();
    const onData = vi.fn();
    const { result } = renderHook(() => useLiveStream());

    let opened = false;
    act(() => {
      opened = result.current.openStream({ open, onOpen, onData });
    });
    act(() => {
      result.current.openStream({ open });
    });

    expect(opened).toBe(true);
    expect(open).toHaveBeenCalledTimes(1);
    expect(onOpen).toHaveBeenCalledWith(stream);
    stream.onData?.(new Uint8Array([1]));
    expect(onData).toHaveBeenCalledWith(new Uint8Array([1]));
  });

  it("reports open errors and returns false", () => {
    const onOpenError = vi.fn();
    const { result } = renderHook(() => useLiveStream());

    let opened = true;
    act(() => {
      opened = result.current.openStream({
        open: () => null,
        onOpenError,
      });
    });

    expect(opened).toBe(false);
    expect(onOpenError).toHaveBeenCalledTimes(1);
  });

  it("closes streams manually and on unmount", () => {
    const stream = createStream();
    const { result, unmount } = renderHook(() => useLiveStream());

    act(() => {
      result.current.openStream({ open: () => stream });
      result.current.closeStream();
    });
    expect(stream.close).toHaveBeenCalledTimes(1);
    expect(result.current.streamRef.current).toBeNull();

    const secondStream = createStream();
    act(() => {
      result.current.openStream({ open: () => secondStream });
    });
    unmount();
    expect(secondStream.close).toHaveBeenCalledTimes(1);
  });

  it("clears stream ref when the stream closes itself", () => {
    const stream = createStream();
    const onClose = vi.fn();
    const { result } = renderHook(() => useLiveStream());

    act(() => {
      result.current.openStream({ open: () => stream, onClose });
      stream.onClose?.();
    });

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(result.current.streamRef.current).toBeNull();
  });
});
