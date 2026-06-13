import type { Terminal } from "@xterm/xterm";

import { describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api";
import { decodeString } from "@/api";
import { useTerminalContextMenu } from "@/hooks/useTerminalContextMenu";
import { act, renderHook } from "@/test/render";

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
    type: "terminal.open",
    write: vi.fn(),
  };
}

function contextMenuEvent(x = 10, y = 20) {
  return {
    clientX: x,
    clientY: y,
    preventDefault: vi.fn(),
    stopPropagation: vi.fn(),
  } as unknown as React.MouseEvent;
}

describe("useTerminalContextMenu", () => {
  it("opens and closes the context menu", () => {
    vi.useFakeTimers();
    const stream = createStream();
    const terminal = { getSelection: vi.fn() } as unknown as Terminal;
    const { result } = renderHook(() =>
      useTerminalContextMenu({
        streamRef: { current: stream },
        terminalRef: { current: terminal },
      }),
    );
    const event = contextMenuEvent(30, 40);

    act(() => {
      result.current.handleContextMenu(event);
      vi.runOnlyPendingTimers();
    });

    expect(event.preventDefault).toHaveBeenCalledTimes(1);
    expect(event.stopPropagation).toHaveBeenCalledTimes(1);
    expect(result.current.contextMenu).toEqual({ mouseX: 30, mouseY: 40 });

    act(() => result.current.handleCloseContextMenu());
    expect(result.current.contextMenu).toBeNull();
  });

  it("copies terminal selection and closes the menu", () => {
    const writeText = vi.fn();
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText, readText: vi.fn() },
    });
    const terminal = {
      getSelection: vi.fn(() => "selected"),
    } as unknown as Terminal;
    const { result } = renderHook(() =>
      useTerminalContextMenu({
        streamRef: { current: createStream() },
        terminalRef: { current: terminal },
      }),
    );

    act(() => result.current.handleCopy());

    expect(writeText).toHaveBeenCalledWith("selected");
  });

  it("pastes clipboard text into the stream", async () => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        readText: vi.fn().mockResolvedValue("paste me"),
        writeText: vi.fn(),
      },
    });
    const stream = createStream();
    const { result } = renderHook(() =>
      useTerminalContextMenu({
        streamRef: { current: stream },
        terminalRef: { current: null },
      }),
    );

    await act(async () => {
      await result.current.handlePaste();
    });

    expect(stream.write).toHaveBeenCalledTimes(1);
    expect(decodeString(vi.mocked(stream.write).mock.calls[0][0])).toBe(
      "paste me",
    );
  });

  it("ignores denied clipboard reads", async () => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        readText: vi.fn().mockRejectedValue(new Error("denied")),
        writeText: vi.fn(),
      },
    });
    const stream = createStream();
    const { result } = renderHook(() =>
      useTerminalContextMenu({
        streamRef: { current: stream },
        terminalRef: { current: null },
      }),
    );

    await act(async () => {
      await result.current.handlePaste();
    });

    expect(stream.write).not.toHaveBeenCalled();
  });
});
