import { beforeEach, describe, expect, it, vi } from "vitest";

import { act, render } from "@/test/render";

import Terminal from "./Terminal";

const mocks = vi.hoisted(() => {
  const fakeStream = () => ({
    close: vi.fn(),
    resize: vi.fn(),
    write: vi.fn(),
    onData: null as ((data: Uint8Array) => void) | null,
    status: "open",
  });
  return {
    fakeStream,
    existing: fakeStream(),
    location: { state: {} },
    navigate: vi.fn(),
    getStream: vi.fn(),
    openTerminalStream: vi.fn(),
    openTerminalLoginStream: vi.fn(),
    terminal: {
      cols: 80,
      rows: 24,
      focus() {},
      clear() {},
      reset() {},
      write(_data: string, done?: () => void) {
        done?.();
      },
      input: vi.fn(),
      modes: { bracketedPasteMode: false },
    },
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useLocation: () => mocks.location,
  useNavigate: () => mocks.navigate,
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
  mocks.navigate.mockReset();
  mocks.terminal.modes.bracketedPasteMode = false;
  mocks.terminal.input.mockReset();
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

describe("Terminal command from the Assistant", () => {
  it("types a one-line command into the shell without running it", () => {
    mocks.location = { state: { terminalCommand: "uptime\n" } };
    render(<Terminal />);

    expect(mocks.terminal.input).toHaveBeenCalledTimes(1);
    expect(mocks.terminal.input).toHaveBeenCalledWith("uptime");
  });

  it("drops the command from history state so a reload does not type it again", () => {
    mocks.location = { state: { terminalCommand: "uptime" } };
    render(<Terminal />);

    expect(mocks.navigate).toHaveBeenCalledWith(
      expect.objectContaining({ replace: true }),
    );
  });

  it("holds several lines until the shell takes a paste without running it", () => {
    mocks.location = { state: { terminalCommand: "one\ntwo" } };
    render(<Terminal />);
    expect(mocks.terminal.input).not.toHaveBeenCalled();

    // The shell prints its prompt and turns bracketed paste on.
    mocks.terminal.modes.bracketedPasteMode = true;
    act(() => mocks.existing.onData?.(new Uint8Array()));

    expect(mocks.terminal.input).toHaveBeenCalledTimes(1);
    expect(mocks.terminal.input).toHaveBeenCalledWith(
      "\u001b[200~one\rtwo\u001b[201~",
    );
  });

  it("types nothing into an agent login", () => {
    const loginStream = mocks.fakeStream();
    mocks.openTerminalLoginStream.mockReturnValue(loginStream);
    mocks.location = {
      state: {
        terminalCommand: "uptime",
        terminalLogin: { agent: "claude", args: [] },
      },
    };
    render(<Terminal />);

    expect(mocks.terminal.input).not.toHaveBeenCalled();
  });

  it("types nothing when there is no command", () => {
    render(<Terminal />);

    expect(mocks.terminal.input).not.toHaveBeenCalled();
    expect(mocks.navigate).not.toHaveBeenCalled();
  });
});
