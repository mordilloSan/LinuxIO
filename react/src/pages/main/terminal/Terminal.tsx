import { useTheme } from "@mui/material/styles";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import React, { useEffect, useRef } from "react";

import "@xterm/xterm/css/xterm.css";
import useAppWebSocket from "@/hooks/useAppWebSocket";

const TerminalXTerm: React.FC = () => {
  const termRef = useRef<HTMLDivElement>(null);
  const xterm = useRef<Terminal | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);
  const theme = useTheme();
  const { send, subscribe, ready } = useAppWebSocket();
  const startedRef = useRef(false);

  useEffect(() => {
    if (!termRef.current) return;

    // Always dispose old instance!
    xterm.current?.dispose();

    xterm.current = new Terminal({
      fontSize: 16,
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
  }, [
    ready,
    send,
    subscribe,
    theme.palette.background.default,
    theme.palette.text.primary,
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

  return (
    <div
      ref={termRef}
      className="my-terminal-root"
      style={{
        height: "100%",
        width: "100%",
        overflow: "hidden",
        background: theme.palette.background.default,
      }}
    />
  );
};

export default TerminalXTerm;
