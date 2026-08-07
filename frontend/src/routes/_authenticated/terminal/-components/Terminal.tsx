import { Icon } from "@iconify/react";
import { useMemo, useState } from "react";
import "@xterm/xterm/css/xterm.css";

import { openTerminalStream, useStreamMux } from "@/api";
import TerminalContextMenu from "@/components/terminal/TerminalContextMenu";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { useLiveStream } from "@/hooks/useLiveStream";
import { useTerminalContextMenu } from "@/hooks/useTerminalContextMenu";
import { useXtermStreamTerminal } from "@/hooks/useXtermStreamTerminal";
import { useAppTheme } from "@/theme";
import { shadowSm } from "@/theme/constants";

const MIN_FONT = 10;
const MAX_FONT = 28;
const DEFAULT_FONT = 16;

const TerminalXTerm = () => {
  const theme = useAppTheme();

  const { isOpen, getStream } = useStreamMux();
  // The PTY stream persists server-side across page visits: never close it
  // on unmount, only detach the handlers (reattach happens via getStream).
  const { streamRef, openStream, closeStream, detachStream } = useLiveStream({
    closeOnUnmount: false,
  });
  const [fontSize, setFontSize] = useState(DEFAULT_FONT);
  const terminalOptions = useMemo(
    () => ({
      fontFamily:
        "DejaVu Sans Mono, Liberation Mono, Menlo, Consolas, monospace",
      fontSize,
      fontWeight: "bold" as const,
    }),
    [fontSize],
  );

  const {
    containerRef: termRef,
    terminalRef: xterm,
    writeData,
  } = useXtermStreamTerminal({
    background: theme.palette.background.default,
    enabled: true,
    foreground: theme.palette.text.primary,
    onReady: (terminal) => {
      if (!isOpen) return;

      // Reattach to the persistent PTY stream when one exists (page revisit),
      // otherwise open a fresh one.
      const opened = openStream({
        open: () =>
          getStream("terminal.open") ??
          openTerminalStream(terminal.cols, terminal.rows),
        onData: writeData,
      });
      if (opened) {
        streamRef.current?.resize(terminal.cols, terminal.rows);
      }

      return () => {
        // Do not close the stream; it persists for reconnection.
        detachStream();
      };
    },
    sessionKey: isOpen ? "open" : "closed",
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

  return (
    <div
      style={{
        height: "100%",
        width: "100%",
        background: theme.palette.background.default,
        display: "flex",
        flexDirection: "column",
      }}
    >
      {/* HEADER BAR */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          padding: `${theme.spacing(1)} ${theme.spacing(3)}`,
          minHeight: 64,
          backgroundColor:
            theme.palette.mode === "light"
              ? theme.darken(theme.sidebar.background, 0.13)
              : theme.lighten(theme.sidebar.background, 0.06),
          boxShadow: shadowSm,
        }}
      >
        {/* Font Size Controls */}
        <AppTypography
          color="text.secondary"
          fontWeight={500}
          style={{ marginRight: 8 }}
          variant="body2"
        >
          Font
        </AppTypography>
        <AppIconButton
          aria-label="Decrease terminal font size"
          onClick={() => setFontSize((f) => Math.max(MIN_FONT, f - 1))}
          size="small"
          style={{ color: "var(--app-palette-text-secondary)" }}
        >
          <Icon height={18} icon="mdi:minus" width={18} />
        </AppIconButton>
        <AppTypography
          align="center"
          color="text.secondary"
          style={{ minWidth: 28 }}
          variant="body2"
        >
          {fontSize}
        </AppTypography>
        <AppIconButton
          aria-label="Increase terminal font size"
          onClick={() => setFontSize((f) => Math.min(MAX_FONT, f + 1))}
          size="small"
          style={{ color: "var(--app-palette-text-secondary)" }}
        >
          <Icon height={18} icon="mdi:plus" width={18} />
        </AppIconButton>

        <div style={{ flex: 1 }} />

        {/* Reset Button */}
        <AppIconButton
          aria-label="Reset terminal"
          onClick={handleReset}
          size="small"
          style={{ color: "var(--app-palette-text-secondary)", marginLeft: 8 }}
          title="Reset Terminal"
        >
          <Icon height={18} icon="mdi:restart" width={18} />
        </AppIconButton>
      </div>
      {/* TERMINAL */}
      <div
        className="my-terminal-root"
        onContextMenu={handleContextMenu}
        ref={termRef}
        style={{
          flex: 1,
          overflow: "hidden",
          borderRadius: "0 0 16px 16px",
          background: theme.palette.background.default,
        }}
      />
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
