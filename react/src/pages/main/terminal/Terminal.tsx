import { useAppWebSocket } from "@/contexts/WebSocketContext";
import { Box } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React, { useEffect, useRef } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import "xterm/css/xterm.css";

const TerminalXTerm: React.FC = () => {
  const termRef = useRef<HTMLDivElement>(null);
  const xterm = useRef<Terminal | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);
  const theme = useTheme();
  const { send, subscribe, ready } = useAppWebSocket();

  useEffect(() => {
    if (!termRef.current) return;

    // Always clean up old terminal!
    xterm.current?.dispose();

    xterm.current = new Terminal({
      fontFamily: "monospace",
      fontSize: 16,
      cursorBlink: true,
      scrollback: 1000,
      disableStdin: false,
      theme: {
        background: theme.palette.background.default,
        foreground: theme.palette.text.primary,
      },
    });
    fitAddon.current = new FitAddon();
    xterm.current.loadAddon(fitAddon.current);
    xterm.current.open(termRef.current);
    fitAddon.current.fit();

    setTimeout(() => {
      const viewport = termRef.current?.querySelector(".xterm-viewport");
      if (viewport) viewport.classList.add("custom-scrollbar");
    }, 0);

    // Listen for websocket messages
    const unsub = subscribe((msg) => {
      if (msg.type === "terminal_output" && xterm.current) {
        xterm.current.write(msg.data);
      }
    });

    // Terminal input -> send to socket
    xterm.current.onData((data) => {
      if (ready) {
        send({ type: "terminal_input", data });
      }
    });

    // On mount, send terminal_start
    if (ready) {
      send({ type: "terminal_start" });
    }

    window.addEventListener("resize", () => {
      fitAddon.current?.fit();
    });

    return () => {
      unsub();
      xterm.current?.dispose();
    };
  }, [ready, send, subscribe]);

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
