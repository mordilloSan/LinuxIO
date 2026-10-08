import { beforeEach, describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import Terminal from "./Terminal";

const mocks = vi.hoisted(() => {
  const fakeStream = () => ({
    close: vi.fn(),
    resize: vi.fn(),
    status: "open",
  });
  return {
    fakeStream,
    existing: fakeStream(),
    location: { state: {} },
    getStream: vi.fn(),
    openTerminalStream: vi.fn(),
    openTerminalLoginStream: vi.fn(),
    terminal: {
      cols: 80,
      rows: 24,
      focus() {},
      clear() {},
      reset() {},
      write() {},
    },
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useLocation: () => mocks.location,
}));
vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  useStreamMux: () => ({
    isOpen: true,
    status: "open",
    getStream: mocks.getStream,
  }),
  openTerminalStream: mocks.openTerminalStream,
  openTerminalLoginStream: mocks.openTerminalLoginStream,
}));
// xterm itself needs a real canvas; run onReady with a fake terminal instead.
vi.mock("@/hooks/useXtermStreamTerminal", async () => {
  const { useEffect } = await import("react");
  return {
    useXtermStreamTerminal: ({
      onReady,
    }: {
      onReady?: (terminal: unknown) => (() => void) | void;
    }) => {
      useEffect(() => onReady?.(mocks.terminal), [onReady]);
      return {
        containerRef: { current: null },
        terminalRef: { current: mocks.terminal },
        writeData: vi.fn(),
      };
    },
  };
});

beforeEach(() => {
  mocks.location = { state: {} };
  mocks.existing = mocks.fakeStream();
  mocks.getStream.mockReset().mockImplementation(() => mocks.existing);
  mocks.openTerminalStream
    .mockReset()
    .mockImplementation(() => mocks.fakeStream());
  mocks.openTerminalLoginStream
    .mockReset()
    .mockImplementation(() => mocks.fakeStream());
});

describe("Terminal login mode", () => {
  it("opens the login beside the existing shell without closing it", () => {
    mocks.location = {
      state: { terminalLogin: { agent: "claude", args: ["--cli"] } },
    };
    render(<Terminal />);

    expect(mocks.existing.close).not.toHaveBeenCalled();
    expect(mocks.openTerminalLoginStream).toHaveBeenCalledWith(80, 24, {
      agent: "claude",
      args: ["--cli"],
    });
    expect(mocks.openTerminalStream).not.toHaveBeenCalled();
  });

  it("ignores a malformed login in history state", () => {
    mocks.location = {
      state: { terminalLogin: { agent: "claude", args: ["ok", 1] } },
    };
    render(<Terminal />);

    expect(mocks.openTerminalLoginStream).not.toHaveBeenCalled();
    expect(mocks.existing.close).not.toHaveBeenCalled();
    expect(mocks.existing.resize).toHaveBeenCalledWith(80, 24);
  });

  it("reattaches to the existing shell when there is no login", () => {
    render(<Terminal />);

    expect(mocks.existing.close).not.toHaveBeenCalled();
    expect(mocks.openTerminalStream).not.toHaveBeenCalled();
    expect(mocks.openTerminalLoginStream).not.toHaveBeenCalled();
    expect(mocks.existing.resize).toHaveBeenCalledWith(80, 24);
  });
});
