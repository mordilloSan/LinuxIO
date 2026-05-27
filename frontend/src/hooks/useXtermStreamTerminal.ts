import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import {
  type RefObject,
  useCallback,
  useEffect,
  useEffectEvent,
  useRef,
} from "react";

import { decodeString, encodeString, type Stream } from "@/api";

type TerminalOptions = NonNullable<ConstructorParameters<typeof Terminal>[0]>;
type ReadyCleanup = (() => void) | void;
type ReadyMode = "animationFrame" | "timeout";

export interface UseXtermStreamTerminalOptions {
  background: string;
  enabled: boolean;
  focusDelayMs?: number;
  foreground: string;
  onKeyDown?: (event: KeyboardEvent, terminal: Terminal) => boolean | void;
  onReady?: (terminal: Terminal) => ReadyCleanup;
  readyMode?: ReadyMode;
  sessionKey?: number | string;
  streamRef: RefObject<Stream | null>;
  terminalOptions?: TerminalOptions;
}

export interface UseXtermStreamTerminalResult {
  containerRef: RefObject<HTMLDivElement | null>;
  fitAddonRef: RefObject<FitAddon | null>;
  fitAndResize: () => void;
  terminalRef: RefObject<Terminal | null>;
  writeData: (data: Uint8Array) => void;
}

function copySelection(terminal: Terminal) {
  const selection = terminal.getSelection();
  if (selection) {
    void navigator.clipboard.writeText(selection);
  }
}

function isCopyShortcut(event: KeyboardEvent) {
  return (
    event.ctrlKey &&
    event.shiftKey &&
    event.key === "C" &&
    !event.altKey &&
    !event.metaKey
  );
}

export function useXtermStreamTerminal({
  background,
  enabled,
  focusDelayMs = 0,
  foreground,
  onKeyDown,
  onReady,
  readyMode = "animationFrame",
  sessionKey,
  streamRef,
  terminalOptions,
}: UseXtermStreamTerminalOptions): UseXtermStreamTerminalResult {
  const containerRef = useRef<HTMLDivElement>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const terminalRef = useRef<Terminal | null>(null);

  const fitAndResize = useCallback(() => {
    fitAddonRef.current?.fit();
    if (terminalRef.current && streamRef.current) {
      streamRef.current.resize(
        terminalRef.current.cols,
        terminalRef.current.rows,
      );
    }
  }, [streamRef]);

  const writeData = useCallback((data: Uint8Array) => {
    const terminal = terminalRef.current;
    if (!terminal) return;

    const text = decodeString(data);
    terminal.write(text, () => {
      terminalRef.current?.scrollToBottom();
    });
  }, []);

  const getTerminalOptions = useEffectEvent((): TerminalOptions => {
    const baseOptions = terminalOptions ?? {};
    return {
      cursorBlink: true,
      disableStdin: false,
      scrollback: 2000,
      ...baseOptions,
      theme: {
        ...baseOptions.theme,
        background,
        foreground,
      },
    };
  });

  const handleInput = useEffectEvent((data: string) => {
    streamRef.current?.write(encodeString(data));
  });

  const handleKeyDown = useEffectEvent(
    (event: KeyboardEvent, terminal: Terminal) => {
      const hostResult = onKeyDown?.(event, terminal);
      if (hostResult === false) return false;

      if (isCopyShortcut(event)) {
        event.preventDefault();
        event.stopPropagation();
        copySelection(terminal);
        return false;
      }

      return true;
    },
  );

  const handleReady = useEffectEvent((terminal: Terminal) =>
    onReady?.(terminal),
  );

  useEffect(() => {
    const terminal = terminalRef.current;
    const fontSize = terminalOptions?.fontSize;
    if (!terminal || fontSize == null) return;

    terminal.options.fontSize = fontSize;
    terminal.refresh(0, terminal.rows - 1);
    fitAndResize();
  }, [fitAndResize, terminalOptions?.fontSize]);

  useEffect(() => {
    const terminal = terminalRef.current;
    if (terminal) {
      terminal.options.theme = {
        ...terminal.options.theme,
        background,
        foreground,
      };
      terminal.refresh(0, terminal.rows - 1);
    }

    if (containerRef.current) {
      containerRef.current.style.background = background;
    }
  }, [background, foreground]);

  useEffect(() => {
    if (!enabled || !containerRef.current) return;

    terminalRef.current?.dispose();

    const terminal = new Terminal(getTerminalOptions());
    const fitAddon = new FitAddon();
    let disposed = false;
    let focusTimeoutId: number | null = null;
    let readyCleanup: (() => void) | undefined;
    let readyRafId: number | null = null;
    let readyTimeoutId: number | null = null;

    terminalRef.current = terminal;
    fitAddonRef.current = fitAddon;
    terminal.loadAddon(fitAddon);
    terminal.open(containerRef.current);

    const inputDisposable = terminal.onData((data) => {
      handleInput(data);
    });

    terminal.attachCustomKeyEventHandler((event) => {
      if (event.type !== "keydown") return true;
      return handleKeyDown(event, terminal);
    });

    const handleResize = () => {
      fitAndResize();
    };
    window.addEventListener("resize", handleResize);

    const focusTerminal = () => {
      if (!disposed) {
        terminalRef.current?.focus();
      }
    };

    const runReady = () => {
      if (disposed) return;

      containerRef.current
        ?.querySelector(".xterm-viewport")
        ?.classList.add("custom-scrollbar");

      fitAndResize();
      const cleanup = handleReady(terminal);
      if (typeof cleanup === "function") {
        readyCleanup = cleanup;
      }
      fitAndResize();

      if (focusDelayMs > 0) {
        focusTimeoutId = window.setTimeout(focusTerminal, focusDelayMs);
      } else {
        focusTerminal();
      }
    };

    if (readyMode === "animationFrame") {
      readyRafId = requestAnimationFrame(runReady);
    } else {
      readyTimeoutId = window.setTimeout(runReady, 0);
    }

    return () => {
      disposed = true;
      if (readyRafId !== null) {
        cancelAnimationFrame(readyRafId);
      }
      if (readyTimeoutId !== null) {
        window.clearTimeout(readyTimeoutId);
      }
      if (focusTimeoutId !== null) {
        window.clearTimeout(focusTimeoutId);
      }

      inputDisposable.dispose();
      readyCleanup?.();
      window.removeEventListener("resize", handleResize);
      terminal.dispose();

      if (terminalRef.current === terminal) {
        terminalRef.current = null;
      }
      if (fitAddonRef.current === fitAddon) {
        fitAddonRef.current = null;
      }
    };
  }, [enabled, fitAndResize, focusDelayMs, readyMode, sessionKey]);

  return {
    containerRef,
    fitAddonRef,
    fitAndResize,
    terminalRef,
    writeData,
  };
}
