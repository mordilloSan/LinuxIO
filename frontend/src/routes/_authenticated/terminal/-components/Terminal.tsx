import { useLocation } from "@tanstack/react-router";
import { useMemo, useSyncExternalStore } from "react";
import "@xterm/xterm/css/xterm.css";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/700.css";

import {
  openTerminalLoginStream,
  openTerminalStream,
  useStreamMux,
} from "@/api";
import { type AssistantAgentId, isAssistantAgentId } from "@/api/acp/agents";
import TerminalContextMenu from "@/components/terminal/TerminalContextMenu";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { useConfigValue } from "@/hooks/useConfig";
import { useLiveStream } from "@/hooks/useLiveStream";
import { useTerminalContextMenu } from "@/hooks/useTerminalContextMenu";
import { useXtermStreamTerminal } from "@/hooks/useXtermStreamTerminal";
import { useAppTheme } from "@/theme";
import { cardBorderRadius, shadowSm } from "@/theme/constants";
import { alpha } from "@/utils/color";

interface TerminalLogin {
  agent: AssistantAgentId;
  args: string[];
}

// History state is only ever set by in-app navigation, but it is still
// untrusted input to a process spawn: validate before use.
function terminalLoginFromState(value: unknown): TerminalLogin | null {
  if (typeof value !== "object" || value === null) return null;
  const { agent, args } = value as { agent?: unknown; args?: unknown };
  if (!isAssistantAgentId(agent)) return null;
  if (!Array.isArray(args) || !args.every((arg) => typeof arg === "string")) {
    return null;
  }
  return { agent, args };
}

const MIN_FONT = 10;
const MAX_FONT = 28;

const TERMINAL_FONT = "JetBrains Mono";
const TERMINAL_FONT_STACK = `"${TERMINAL_FONT}", ui-monospace, SFMono-Regular, Menlo, Consolas, monospace`;

let terminalFontsReady = false;
let terminalFontsLoading = false;
const terminalFontListeners = new Set<() => void>();

function markTerminalFontsReady() {
  terminalFontsReady = true;
  terminalFontListeners.forEach((listener) => listener());
}

function loadTerminalFonts() {
  if (terminalFontsReady || terminalFontsLoading) return;

  if (
    typeof document === "undefined" ||
    typeof document.fonts?.load !== "function"
  ) {
    markTerminalFontsReady();
    return;
  }

  terminalFontsLoading = true;
  void Promise.all([
    document.fonts.load(`1em "${TERMINAL_FONT}"`),
    document.fonts.load(`bold 1em "${TERMINAL_FONT}"`),
  ]).then(markTerminalFontsReady, markTerminalFontsReady);
}

function subscribeToTerminalFonts(listener: () => void) {
  terminalFontListeners.add(listener);
  loadTerminalFonts();
  return () => terminalFontListeners.delete(listener);
}

function getTerminalFontsReady() {
  return (
    typeof document !== "undefined" &&
    (typeof document.fonts?.load !== "function" || terminalFontsReady)
  );
}

// One Dark / One Light ANSI palettes: the xterm defaults are the harsh
// pure-RGB primaries, which is most of what makes `ls` output look dated.
const ANSI_DARK = {
  black: "#3B4252",
  red: "#E06C75",
  green: "#98C379",
  yellow: "#E5C07B",
  blue: "#61AFEF",
  magenta: "#C678DD",
  cyan: "#56B6C2",
  white: "#DCDFE4",
  brightBlack: "#5C6370",
  brightRed: "#E06C75",
  brightGreen: "#98C379",
  brightYellow: "#E5C07B",
  brightBlue: "#61AFEF",
  brightMagenta: "#C678DD",
  brightCyan: "#56B6C2",
  brightWhite: "#FFFFFF",
};
const ANSI_LIGHT = {
  black: "#383A42",
  red: "#CA1243",
  green: "#50A14F",
  yellow: "#C18401",
  blue: "#4078F2",
  magenta: "#A626A4",
  cyan: "#0184BC",
  white: "#FAFAFA",
  brightBlack: "#A0A1A7",
  brightRed: "#CA1243",
  brightGreen: "#50A14F",
  brightYellow: "#C18401",
  brightBlue: "#4078F2",
  brightMagenta: "#A626A4",
  brightCyan: "#0184BC",
  brightWhite: "#FFFFFF",
};

const TerminalXTerm = () => {
  const theme = useAppTheme();

  const { isOpen, getStream } = useStreamMux();
  // Set when the Assistant sends the user here to log an agent in.
  const login = terminalLoginFromState(useLocation().state.terminalLogin);
  // The shell PTY persists server-side across page visits: never close it on
  // unmount, only detach the handlers (reattach happens via getStream). A login
  // PTY is separate and ends when the user leaves the page.
  const { streamRef, openStream, closeStream, detachStream } = useLiveStream({
    closeOnUnmount: Boolean(login),
  });
  const [fontSize, setConfigFontSize] = useConfigValue("terminalFontSize");

  // xterm measures its cell grid once on open; opening before the bundled
  // font is available would leave a mismatched grid until the next resize.
  const fontsReady = useSyncExternalStore(
    subscribeToTerminalFonts,
    getTerminalFontsReady,
    () => false,
  );

  const terminalTheme = useMemo(
    () => ({
      ...(theme.palette.mode === "light" ? ANSI_LIGHT : ANSI_DARK),
      cursor: theme.palette.primary.main,
      cursorAccent: theme.palette.background.default,
      selectionBackground: alpha(theme.palette.primary.main, 0.35),
    }),
    [theme],
  );
  const terminalOptions = useMemo(
    () => ({
      fontFamily: TERMINAL_FONT_STACK,
      fontSize,
      theme: terminalTheme,
    }),
    [fontSize, terminalTheme],
  );

  const {
    containerRef: termRef,
    terminalRef: xterm,
    writeData,
  } = useXtermStreamTerminal({
    background: theme.palette.background.default,
    enabled: fontsReady,
    foreground: theme.palette.text.primary,
    onReady: (terminal) => {
      if (!isOpen) return;

      // Reattach to the persistent PTY stream when one exists (page revisit),
      // otherwise open a fresh one. A login runs beside the shell, never in it.
      const opened = openStream({
        open: () =>
          login
            ? openTerminalLoginStream(terminal.cols, terminal.rows, login)
            : (getStream("terminal.open") ??
              openTerminalStream(terminal.cols, terminal.rows)),
        onData: writeData,
        onClose: login
          ? () =>
              terminal.write(
                "\r\n[login command finished — go back to the Assistant page]\r\n",
              )
          : undefined,
      });
      if (opened) {
        streamRef.current?.resize(terminal.cols, terminal.rows);
      }

      return () => {
        // The shell persists for reconnection; only a login PTY is closed.
        if (login) closeStream();
        else detachStream();
      };
    },
    sessionKey: `${isOpen ? "open" : "closed"}:${login ? `${login.agent}:${login.args.join(" ")}` : ""}`,
    streamRef,
    terminalOptions,
  });

  const {
    contextMenu,
    handleCloseContextMenu,
    handleContextMenu,
    handleCopy,
    handlePaste,
  } = useTerminalContextMenu({
    streamRef,
    terminalRef: xterm,
  });

  // Handler for reset - closes PTY and creates fresh terminal
  const handleReset = () => {
    const terminal = xterm.current;
    if (!terminal || !isOpen) return;

    // Close existing stream (terminates PTY on bridge)
    closeStream();

    // Clear xterm display
    terminal.clear();
    terminal.reset();

    // Open fresh stream (creates new PTY)
    openStream({
      open: () => openTerminalStream(terminal.cols, terminal.rows),
      onData: writeData,
    });

    terminal.focus();
  };

  const adjustFontSize = (delta: number) => {
    setConfigFontSize((prev) =>
      Math.min(MAX_FONT, Math.max(MIN_FONT, prev + delta)),
    );
  };

  return (
    <div
      style={{
        height: "100%",
        width: "100%",
        display: "flex",
        flexDirection: "column",
        minHeight: 0,
      }}
    >
      {/* Route actions: right-aligned icon row, same shape as the tab-strip
          actions on other routes. */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "flex-end",
          flexShrink: 0,
          marginBottom: 8,
        }}
      >
        <AppActionIconButton
          disabled={fontSize <= MIN_FONT}
          icon="mdi:format-font-size-decrease"
          iconSize={20}
          label="Decrease font size"
          onClick={() => adjustFontSize(-1)}
        />
        <AppTypography
          align="center"
          color="text.secondary"
          style={{ minWidth: 24, fontVariantNumeric: "tabular-nums" }}
          variant="body2"
        >
          {fontSize}
        </AppTypography>
        <AppActionIconButton
          disabled={fontSize >= MAX_FONT}
          icon="mdi:format-font-size-increase"
          iconSize={20}
          label="Increase font size"
          onClick={() => adjustFontSize(1)}
        />
        <AppActionIconButton
          icon="mdi:restart"
          iconSize={20}
          label="Reset terminal"
          onClick={handleReset}
        />
      </div>
      {/* TERMINAL */}
      <div
        style={{
          flex: 1,
          minHeight: 0,
          display: "flex",
          padding: 12,
          borderRadius: cardBorderRadius,
          border: `1px solid ${theme.palette.divider}`,
          background: theme.palette.background.default,
          boxShadow: shadowSm,
          overflow: "hidden",
        }}
      >
        <div
          onContextMenu={handleContextMenu}
          ref={termRef}
          style={{ flex: 1, minWidth: 0, overflow: "hidden" }}
        />
      </div>
      <TerminalContextMenu
        contextMenu={contextMenu}
        onClose={handleCloseContextMenu}
        onCopy={handleCopy}
        onPaste={handlePaste}
      />
    </div>
  );
};

export default TerminalXTerm;
