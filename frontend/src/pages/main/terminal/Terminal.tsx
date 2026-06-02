import { Icon } from "@iconify/react";
import React, { useMemo, useRef, useState } from "react";
import "@xterm/xterm/css/xterm.css";

import {
  bindStreamHandlers,
  openTerminalStream,
  type Stream,
  useStreamMux,
} from "@/api";
import TerminalContextMenu from "@/components/terminal/TerminalContextMenu";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { shadowSm } from "@/constants";
import { useTerminalContextMenu } from "@/hooks/useTerminalContextMenu";
import { useXtermStreamTerminal } from "@/hooks/useXtermStreamTerminal";
import { useAppTheme } from "@/theme";

const MIN_FONT = 10;
const MAX_FONT = 28;
const DEFAULT_FONT = 16;

const TerminalXTerm: React.FC = () => {
  const streamRef = useRef<Stream | null>(null);
  const unbindRef = useRef<(() => void) | null>(null);
  const theme = useAppTheme();

  const { isOpen, getStream } = useStreamMux();
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

      let stream = getStream("terminal.open");
      console.log(
        "[Terminal] getStream('terminal.open'):",
        stream ? `found (id=${stream.id})` : "null",
      );

      if (stream) {
        console.log("[Terminal] Reattaching to existing stream");
        streamRef.current = stream;
      } else {
        stream = openTerminalStream(terminal.cols, terminal.rows);

        if (stream) {
          streamRef.current = stream;
        }
      }

      if (stream) {
        unbindRef.current = bindStreamHandlers(stream, {
          onData: writeData,
          onClose: () => {
            unbindRef.current = null;
            streamRef.current = null;
          },
        });

        stream.resize(terminal.cols, terminal.rows);
      }

      return () => {
        console.log("[Terminal] Unmounting, detaching handlers");
        // Do not close the stream; it persists for reconnection.
        if (unbindRef.current && streamRef.current) {
          console.log(
            `[Terminal] Detaching handlers for stream ${streamRef.current.id}`,
          );
          unbindRef.current();
          unbindRef.current = null;
        }
        streamRef.current = null;
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
    if (streamRef.current) {
      if (unbindRef.current) {
        unbindRef.current();
        unbindRef.current = null;
      }
      streamRef.current.close();
      streamRef.current = null;
    }

    // Clear xterm display
    terminal.clear();
    terminal.reset();

    // Open fresh stream (creates new PTY)
    const cols = terminal.cols;
    const rows = terminal.rows;
    const stream = openTerminalStream(cols, rows);

    if (stream) {
      streamRef.current = stream;

      unbindRef.current = bindStreamHandlers(stream, {
        onData: writeData,
        onClose: () => {
          unbindRef.current = null;
          streamRef.current = null;
        },
      });
    }

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
          onClick={() => setFontSize((f) => Math.max(MIN_FONT, f - 1))}
          size="small"
          style={{ color: "var(--mui-palette-text-secondary)" }}
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
          onClick={() => setFontSize((f) => Math.min(MAX_FONT, f + 1))}
          size="small"
          style={{ color: "var(--mui-palette-text-secondary)" }}
        >
          <Icon height={18} icon="mdi:plus" width={18} />
        </AppIconButton>

        <div style={{ flex: 1 }} />

        {/* Reset Button */}
        <AppIconButton
          onClick={handleReset}
          size="small"
          style={{ color: "var(--mui-palette-text-secondary)", marginLeft: 8 }}
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
