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
    // What the terminal does with a command from another tab, and its stop.
    fromOtherTab: null as ((command: unknown) => void) | null,
    answersSearch: null as (() => boolean) | null,
    stopListening: vi.fn(),
    // The shell turns bracketed paste on (its prompt) or off (a program).
    pasteMode: (_on: boolean) => {},
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
      parser: {
        registerCsiHandler: vi.fn(),
      },
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
vi.mock("@/utils/terminalTab", () => ({
  listenForTypedCommands: (
    take: (command: unknown) => void,
    isUp: () => boolean,
  ) => {
    mocks.fromOtherTab = take;
    mocks.answersSearch = isUp;
    return mocks.stopListening;
  },
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
  mocks.fromOtherTab = null;
  mocks.answersSearch = null;
  // As xterm does: the mode changes, then the page's own handlers hear of it.
  const handlers: ((params: number[]) => boolean)[] = [];
  mocks.terminal.parser.registerCsiHandler
    .mockReset()
    .mockImplementation((_id: unknown, handler: (typeof handlers)[0]) => {
      handlers.push(handler);
      return { dispose: () => handlers.splice(handlers.indexOf(handler), 1) };
    });
  mocks.pasteMode = (on) => {
    mocks.terminal.modes.bracketedPasteMode = on;
    for (const handler of [...handlers]) handler([2004]);
  };
  mocks.stopListening.mockReset();
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

  it("waits for the prompt of a shell it has just started", () => {
    const fresh = mocks.fakeStream();
    mocks.getStream.mockImplementation(() => null);
    mocks.openTerminalStream.mockImplementation(() => fresh);
    mocks.location = { state: { terminalCommand: "uptime" } };
    render(<Terminal />);
    // Typed now, the terminal would echo it above the prompt and the shell
    // would show it a second time after it.
    expect(mocks.terminal.input).not.toHaveBeenCalled();

    mocks.terminal.modes.bracketedPasteMode = true;
    act(() => fresh.onData?.(new Uint8Array()));

    expect(mocks.terminal.input).toHaveBeenCalledTimes(1);
    expect(mocks.terminal.input).toHaveBeenCalledWith("uptime");
  });

  it("types one line into a new shell that never takes pastes", () => {
    vi.useFakeTimers();
    try {
      const fresh = mocks.fakeStream();
      mocks.getStream.mockImplementation(() => null);
      mocks.openTerminalStream.mockImplementation(() => fresh);
      mocks.location = { state: { terminalCommand: "uptime" } };
      render(<Terminal />);
      act(() => fresh.onData?.(new Uint8Array()));
      expect(mocks.terminal.input).not.toHaveBeenCalled();

      act(() => {
        vi.advanceTimersByTime(1500);
      });

      expect(mocks.terminal.input).toHaveBeenCalledTimes(1);
      expect(mocks.terminal.input).toHaveBeenCalledWith("uptime");
    } finally {
      vi.useRealTimers();
    }
  });

  it("waits for the prompt of a shell it started a moment ago and finds again", () => {
    const fresh = mocks.fakeStream();
    mocks.getStream.mockImplementation(() => null);
    mocks.openTerminalStream.mockImplementation(() => fresh);
    render(<Terminal />).unmount();

    // Set up again before the prompt is up: the stream exists now, and the
    // shell behind it is still not reading.
    mocks.getStream.mockImplementation(() => fresh);
    mocks.location = { state: { terminalCommand: "uptime" } };
    render(<Terminal />);
    expect(mocks.terminal.input).not.toHaveBeenCalled();

    mocks.terminal.modes.bracketedPasteMode = true;
    act(() => fresh.onData?.(new Uint8Array()));

    expect(mocks.terminal.input).toHaveBeenCalledTimes(1);
    expect(mocks.terminal.input).toHaveBeenCalledWith("uptime");
  });

  it("types at once into a shell it started longer ago", () => {
    vi.useFakeTimers();
    try {
      const fresh = mocks.fakeStream();
      mocks.getStream.mockImplementation(() => null);
      mocks.openTerminalStream.mockImplementation(() => fresh);
      render(<Terminal />).unmount();
      vi.advanceTimersByTime(1500);

      mocks.getStream.mockImplementation(() => fresh);
      mocks.location = { state: { terminalCommand: "uptime" } };
      render(<Terminal />);

      expect(mocks.terminal.input).toHaveBeenCalledTimes(1);
      expect(mocks.terminal.input).toHaveBeenCalledWith("uptime");
    } finally {
      vi.useRealTimers();
    }
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

describe("Terminal command from the Assistant in another tab", () => {
  it("types a command sent while the shell is at its prompt, without running it", () => {
    render(<Terminal />);
    act(() => mocks.pasteMode(true));
    expect(mocks.terminal.input).not.toHaveBeenCalled();

    act(() => mocks.fromOtherTab?.("uptime\n"));
    expect(mocks.terminal.input).toHaveBeenCalledTimes(1);
    expect(mocks.terminal.input).toHaveBeenCalledWith("uptime");

    act(() => mocks.fromOtherTab?.("df -h"));
    expect(mocks.terminal.input).toHaveBeenLastCalledWith("df -h");
  });

  it("reduces what it is sent to a typed command", () => {
    render(<Terminal />);
    act(() => mocks.pasteMode(true));
    act(() => mocks.fromOtherTab?.("echo hi\u0003\u001b[201~"));
    expect(mocks.terminal.input).toHaveBeenCalledWith("echo hi[201~");

    mocks.terminal.input.mockReset();
    act(() => mocks.fromOtherTab?.({ command: "uptime" }));
    expect(mocks.terminal.input).not.toHaveBeenCalled();
  });

  it("holds a command while a program has the terminal, until the prompt is back", () => {
    vi.useFakeTimers();
    try {
      render(<Terminal />);
      // The shell ran a program: bracketed paste goes off for its duration.
      act(() => mocks.pasteMode(true));
      act(() => mocks.pasteMode(false));

      act(() => mocks.fromOtherTab?.("systemctl restart nginx"));
      act(() => void vi.advanceTimersByTime(5000));
      expect(mocks.terminal.input).not.toHaveBeenCalled();

      act(() => mocks.pasteMode(true));
      act(() => mocks.existing.onData?.(new Uint8Array([36])));
      expect(mocks.terminal.input).toHaveBeenCalledWith(
        "systemctl restart nginx",
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it("forgets what a reattached shell did last once the shell says otherwise", () => {
    // Left at a prompt that takes pastes, then found again.
    const first = render(<Terminal />);
    act(() => mocks.pasteMode(true));
    first.unmount();
    mocks.terminal.modes.bracketedPasteMode = false;
    render(<Terminal />);
    // The shell starts a program that takes no paste.
    act(() => mocks.pasteMode(false));

    act(() => mocks.fromOtherTab?.("one\ntwo"));
    expect(mocks.terminal.input).not.toHaveBeenCalled();
  });

  it("types one line into a shell that never shows a paste mode", () => {
    vi.useFakeTimers();
    try {
      mocks.getStream.mockImplementation(() => null);
      render(<Terminal />);
      act(() => mocks.fromOtherTab?.("uptime"));
      expect(mocks.terminal.input).not.toHaveBeenCalled();
      act(() => void vi.advanceTimersByTime(1500));
      expect(mocks.terminal.input).toHaveBeenCalledWith("uptime");
    } finally {
      vi.useRealTimers();
    }
  });

  it("answers the Assistant's search only while its shell is up", () => {
    render(<Terminal />);
    expect(mocks.answersSearch?.()).toBe(true);
  });

  it("does not listen in an agent login, and stops when the page is left", () => {
    mocks.location = {
      state: { terminalLogin: { agent: "claude", args: ["--cli"] } },
    };
    render(<Terminal />).unmount();
    expect(mocks.fromOtherTab).toBeNull();

    mocks.location = { state: {} };
    render(<Terminal />).unmount();
    expect(mocks.fromOtherTab).not.toBeNull();
    expect(mocks.stopListening).toHaveBeenCalledTimes(1);
  });
});
