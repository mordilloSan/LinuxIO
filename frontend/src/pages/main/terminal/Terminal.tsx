import { Box, IconButton, Typography } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import React, { useEffect, useRef, useState } from "react";
import { Minus, Plus, RotateCcw } from "lucide-react";

import "@xterm/xterm/css/xterm.css";
import useAppWebSocket from "@/hooks/useAppWebSocket";

const MIN_FONT = 10;
const MAX_FONT = 28;
const DEFAULT_FONT = 16;

const TerminalXTerm: React.FC = () => {
  const termRef = useRef<HTMLDivElement>(null);
  const xterm = useRef<Terminal | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);
  const theme = useTheme();
  const { send, subscribe, ready } = useAppWebSocket();
  const startedRef = useRef(false);

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

    // Always dispose old instance!
    xterm.current?.dispose();

    xterm.current = new Terminal({
      fontSize,
      fontFamily: 'DejaVu Sans Mono, Liberation Mono, Menlo, Consolas, monospace',
      fontWeight: 'bold',
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

    // Set custom scrollbar on xterm viewport
    setTimeout(() => {
      const viewport = termRef.current?.querySelector(".xterm-viewport");
      if (viewport) viewport.classList.add("custom-scrollbar");
      fitAddon.current?.fit();
      if (xterm.current && ready && !startedRef.current) {
        send({
          type: "terminal_resize",
          payload: { cols: xterm.current.cols, rows: xterm.current.rows },
        });
        setTimeout(() => {
          send({ type: "terminal_start" });
          startedRef.current = true;
        }, 40); // Small delay to let backend resize pty before launching shell
      }
    }, 30);

    // Listen for websocket messages
    const unsub = subscribe((msg) => {
      if (msg.type === "terminal_output" && xterm.current) {
        xterm.current.write(msg.data, () => {
          xterm.current?.scrollToBottom();
        });
      }
    });

    // Terminal input -> send to socket
    xterm.current.onData((data) => {
      if (ready) {
        send({ type: "terminal_input", data });
      }
    });

    // Responsive fit on window resize
    const doFit = () => {
      fitAddon.current?.fit();
      if (xterm.current && ready) {
        send({
          type: "terminal_resize",
          payload: { cols: xterm.current.cols, rows: xterm.current.rows },
        });
      }
    };
    window.addEventListener("resize", doFit);

    return () => {
      unsub();
      xterm.current?.dispose();
      window.removeEventListener("resize", doFit);
      startedRef.current = false;
    };
    // eslint-disable-next-line
  }, [
    ready,
    send,
    subscribe,
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

  // Handler for reset (Ctrl+L)
  const handleReset = () => {
    if (xterm.current) {
      xterm.current.clear();
      xterm.current.write("\x0c");
      xterm.current.scrollToBottom();
    }
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
        sx={{
          display: "flex",
          alignItems: "center",
          px: 3,
          py: 1,
          minHeight: 64,
          background: "rgba(50,60,70,0.74)",
          mr: 2,
          mb: 2,
          boxShadow: "0 1px 8px rgba(0,0,0,0.10)",
        }}
      >
        {/* Font Size Controls */}
        <Typography variant="body2" sx={{ color: "#82909e", fontWeight: 500, mr: 2 }}>
          Font
        </Typography>
        <IconButton
          size="small"
          sx={{ color: "#82909e" }}
          onClick={() => setFontSize((f) => Math.max(MIN_FONT, f - 1))}
        >
          <Minus size={18} />
        </IconButton>
        <Typography variant="body2" sx={{ minWidth: 28, textAlign: "center", color: "#82909e" }}>
          {fontSize}
        </Typography>
        <IconButton
          size="small"
          sx={{ color: "#82909e" }}
          onClick={() => setFontSize((f) => Math.min(MAX_FONT, f + 1))}
        >
          <Plus size={18} />
        </IconButton>

        {/* Spacer */}
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
