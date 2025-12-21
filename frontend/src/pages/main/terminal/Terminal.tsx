import { Box, IconButton, Typography } from "@mui/material";
import { darken, lighten, useTheme } from "@mui/material/styles";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import Minus from "lucide-react/dist/esm/icons/minus";
import Plus from "lucide-react/dist/esm/icons/plus";
import RotateCcw from "lucide-react/dist/esm/icons/rotate-ccw";
import React, { useEffect, useRef, useState } from "react";

import "@xterm/xterm/css/xterm.css";
import useStreamMux from "@/hooks/useStreamMux";
import {
  Stream,
  encodeString,
  decodeString,
} from "@/utils/StreamMultiplexer";

const MIN_FONT = 10;
const MAX_FONT = 28;
const DEFAULT_FONT = 16;

// Build terminal open payload: "terminal\0cols\0rows"
function buildTerminalPayload(cols: number, rows: number): Uint8Array {
  return encodeString(`terminal\0${cols}\0${rows}`);
}

const TerminalXTerm: React.FC = () => {
  const termRef = useRef<HTMLDivElement>(null);
  const xterm = useRef<Terminal | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);
  const streamRef = useRef<Stream | null>(null);
  const theme = useTheme();

  const { isOpen, openStream, getStream } = useStreamMux();
  const [fontSize, setFontSize] = useState(DEFAULT_FONT);

  // Update xterm font size when fontSize changes
  useEffect(() => {
    if (xterm.current) {
      xterm.current.options.fontSize = fontSize;
      xterm.current.refresh(0, xterm.current.rows - 1);
      fitAddon.current?.fit();
    }
  }, [fontSize]);

  // Init and manage xterm
  useEffect(() => {
    if (!termRef.current) return;

    // Dispose old xterm instance (but keep stream alive)
    xterm.current?.dispose();

    xterm.current = new Terminal({
      fontSize,
      fontFamily:
        "DejaVu Sans Mono, Liberation Mono, Menlo, Consolas, monospace",
      fontWeight: "bold",
      cursorBlink: true,
      scrollback: 2000,
      disableStdin: false,
      theme: {
        background: theme.palette.background.default,
        foreground: theme.palette.text.primary,
      },
    });

    fitAddon.current = new FitAddon();
    xterm.current.loadAddon(fitAddon.current);
    xterm.current.open(termRef.current);

    // Set custom scrollbar and connect to stream after DOM is ready
    requestAnimationFrame(() => {
      const viewport = termRef.current?.querySelector(".xterm-viewport");
      if (viewport) viewport.classList.add("custom-scrollbar");
      fitAddon.current?.fit();

      if (!xterm.current || !isOpen) return;

      // Check for existing terminal stream first
      let stream = getStream("terminal");
      console.log(
        "[Terminal] getStream('terminal'):",
        stream ? `found (id=${stream.id})` : "null",
      );

      if (stream) {
        // Reattach to existing stream
        console.log("[Terminal] Reattaching to existing stream");
        streamRef.current = stream;
        // Note: xterm scrollback is lost on dispose, user needs to press Enter for prompt
      } else {
        // Create new stream
        const cols = xterm.current.cols;
        const rows = xterm.current.rows;
        const payload = buildTerminalPayload(cols, rows);
        stream = openStream("terminal", payload);

        if (stream) {
          streamRef.current = stream;
        }
      }

      if (stream) {
        // Wire up data handler (reattach on each mount)
        stream.onData = (data: Uint8Array) => {
          if (xterm.current) {
            const text = decodeString(data);
            xterm.current.write(text, () => {
              xterm.current?.scrollToBottom();
            });
          }
        };

        stream.onClose = () => {
          streamRef.current = null;
        };
      }

      // Auto-focus terminal
      xterm.current?.focus();
    });

    // Terminal input -> send to stream as raw bytes
    const onDataDispose = xterm.current.onData((data) => {
      if (streamRef.current) {
        streamRef.current.write(encodeString(data));
      }
    });

    // Responsive fit on window resize
    const doFit = () => {
      fitAddon.current?.fit();
    };
    window.addEventListener("resize", doFit);

    return () => {
      console.log("[Terminal] Unmounting, detaching handlers");
      onDataDispose.dispose();
      xterm.current?.dispose();
      window.removeEventListener("resize", doFit);
      // Don't close stream - it persists for reconnection
      // Detach handler so data gets buffered while unmounted
      if (streamRef.current) {
        console.log(
          `[Terminal] Setting onData=null for stream ${streamRef.current.id}`,
        );
        streamRef.current.onData = null;
        streamRef.current.onClose = null;
      }
      streamRef.current = null;
    };
  }, [
    isOpen,
    openStream,
    getStream,
    theme.palette.background.default,
    theme.palette.text.primary,
    fontSize,
  ]);

  // Live update theme
  useEffect(() => {
    if (xterm.current) {
      xterm.current.options.theme = {
        background: theme.palette.background.default,
        foreground: theme.palette.text.primary,
      };
      xterm.current.refresh(0, xterm.current.rows - 1);
    }
    if (termRef.current) {
      termRef.current.style.background = theme.palette.background.default;
    }
  }, [theme.palette.background.default, theme.palette.text.primary]);

  // Handler for reset - closes PTY and creates fresh terminal
  const handleReset = () => {
    if (!xterm.current || !isOpen) return;

    // Close existing stream (terminates PTY on bridge)
    if (streamRef.current) {
      streamRef.current.onData = null;
      streamRef.current.onClose = null;
      streamRef.current.close();
      streamRef.current = null;
    }

    // Clear xterm display
    xterm.current.clear();
    xterm.current.reset();

    // Open fresh stream (creates new PTY)
    const cols = xterm.current.cols;
    const rows = xterm.current.rows;
    const payload = buildTerminalPayload(cols, rows);
    const stream = openStream("terminal", payload);

    if (stream) {
      streamRef.current = stream;

      stream.onData = (data: Uint8Array) => {
        if (xterm.current) {
          const text = decodeString(data);
          xterm.current.write(text, () => {
            xterm.current?.scrollToBottom();
          });
        }
      };

      stream.onClose = () => {
        streamRef.current = null;
      };
    }

    xterm.current.focus();
  };

  return (
    <Box
      sx={{
        height: "100%",
        width: "100%",
        background: theme.palette.background.default,
        display: "flex",
        flexDirection: "column",
      }}
    >
      {/* HEADER BAR */}
      <Box
        sx={(theme) => ({
          display: "flex",
          alignItems: "center",
          px: 3,
          py: 1,
          minHeight: 64,
          backgroundColor:
            theme.palette.mode === "light"
              ? darken(theme.sidebar.background, 0.13)
              : lighten(theme.sidebar.background, 0.06),
          boxShadow: theme.shadows[2],
        })}
      >
        {/* Font Size Controls */}
        <Typography
          variant="body2"
          sx={{ color: "#82909e", fontWeight: 500, mr: 2 }}
        >
          Font
        </Typography>
        <IconButton
          size="small"
          sx={{ color: "#82909e" }}
          onClick={() => setFontSize((f) => Math.max(MIN_FONT, f - 1))}
        >
          <Minus size={18} />
        </IconButton>
        <Typography
          variant="body2"
          sx={{ minWidth: 28, textAlign: "center", color: "#82909e" }}
        >
          {fontSize}
        </Typography>
        <IconButton
          size="small"
          sx={{ color: "#82909e" }}
          onClick={() => setFontSize((f) => Math.min(MAX_FONT, f + 1))}
        >
          <Plus size={18} />
        </IconButton>

        <Box sx={{ flex: 1 }} />

        {/* Reset Button */}
        <IconButton
          size="small"
          sx={{ color: "#82909e", ml: 1 }}
          onClick={handleReset}
          title="Reset Terminal"
        >
          <RotateCcw size={18} />
        </IconButton>
      </Box>

      {/* TERMINAL */}
      <Box
        ref={termRef}
        className="my-terminal-root"
        sx={{
          flex: 1,
          overflow: "hidden",
          borderRadius: "0 0 16px 16px",
          background: theme.palette.background.default,
        }}
      />
    </Box>
  );
};

export default TerminalXTerm;
