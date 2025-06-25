import { useTheme } from "@mui/material/styles";
import React, { useEffect, useRef } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import "xterm/css/xterm.css";
import { Box } from "@mui/material";

const TerminalXTerm: React.FC = () => {
  const termRef = useRef<HTMLDivElement>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const xterm = useRef<Terminal | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);
  const theme = useTheme();

  useEffect(() => {
    if (!termRef.current) return;

    xterm.current = new Terminal({
      fontFamily: "monospace",
      fontSize: 16,
      cursorBlink: true,
      scrollback: 1000,
      disableStdin: false,
    });
    fitAddon.current = new FitAddon();
    xterm.current.loadAddon(fitAddon.current);
    xterm.current.open(termRef.current);
    fitAddon.current.fit();

    // set the classname inside xterm child. This is used for styling the scrollbar
    setTimeout(() => {
      const viewport = termRef.current?.querySelector(".xterm-viewport");
      if (viewport) {
        viewport.classList.add("custom-scrollbar");
      }
    }, 0);

    // ---- Raw WebSocket for terminal channel ----
    const wsUrl = import.meta.env.DEV
      ? "ws://localhost:8080/ws"
      : window.location.protocol === "https:"
        ? `wss://${window.location.host}/ws`
        : `ws://${window.location.host}/ws`;

    const ws = new window.WebSocket(wsUrl);
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    ws.onopen = () => {
      ws.send(JSON.stringify({ type: "terminal_start" }));
    };

    ws.onmessage = (event) => {
      if (!xterm.current) return;
      let msg;
      try {
        msg = JSON.parse(event.data);
      } catch {
        msg = { type: "terminal_output", data: event.data };
      }
      if (msg.type === "terminal_output") {
        xterm.current.write(msg.data);
      }
    };

    xterm.current.onData((data) => {
      if (wsRef.current?.readyState === WebSocket.OPEN) {
        wsRef.current.send(
          JSON.stringify({
            type: "terminal_input",
            data: data,
          })
        );
      }
    });

    window.addEventListener("resize", () => {
      fitAddon.current?.fit();
    });

    return () => {
      ws.close();
      xterm.current?.dispose();
    };
  }, []);

  // Update terminal colors when the theme changes
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

  return (
    <Box
      ref={termRef}
      sx={{
        height: "100%",
        width: "100%",
      }}
    />
  );
};

export default TerminalXTerm;
