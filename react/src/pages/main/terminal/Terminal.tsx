import React, { useEffect, useRef, useState } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";

import { useWebSocket } from "@/contexts/WebSocketContext";
import "xterm/css/xterm.css";

const TerminalXTerm: React.FC = () => {
  const termRef = useRef<HTMLDivElement>(null);
  const xterm = useRef<Terminal | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);

  const { send, lastMessage } = useWebSocket();

  // Track if we've resized and started the shell
  const [didResize, setDidResize] = useState(false);

  // --- Setup xterm.js only once ---
  useEffect(() => {
    if (!termRef.current) return;

    if (!xterm.current) {
      xterm.current = new Terminal({
        fontFamily: "monospace",
        fontSize: 16,
        theme: {
          background: "#263143",
          foreground: "#e9f0fa",
        },
        cursorBlink: true,
        scrollback: 1000,
        disableStdin: false,
      });
      fitAddon.current = new FitAddon();
      xterm.current.loadAddon(fitAddon.current);
      xterm.current.open(termRef.current);
      fitAddon.current.fit();

      // On user input, send to backend
      xterm.current.onData((data) => {
        send({ type: "terminal_input", data });
      });
    }

    // Always resize after mount
    setTimeout(() => {
      if (xterm.current) {
        send({
          type: "terminal_resize",
          payload: {
            cols: xterm.current.cols,
            rows: xterm.current.rows,
          },
        });
        setDidResize(true);
      }
    }, 100); // Wait just a bit for xterm to layout

    // Resize on window resize
    const handleResize = () => {
      fitAddon.current?.fit();
      if (xterm.current) {
        send({
          type: "terminal_resize",
          payload: {
            cols: xterm.current.cols,
            rows: xterm.current.rows,
          },
        });
      }
    };
    window.addEventListener("resize", handleResize);

    return () => {
      window.removeEventListener("resize", handleResize);
      xterm.current?.dispose();
      xterm.current = null;
    };
  }, [send]);

  // --- Start shell ONLY after we've sent a resize ---
  useEffect(() => {
    if (didResize) {
      send({ type: "terminal_start" });
    }
  }, [didResize, send]);

  // --- Display backend output ---
  useEffect(() => {
    if (
      lastMessage &&
      lastMessage.type === "terminal_output" &&
      xterm.current
    ) {
      xterm.current.write(lastMessage.data);
    }
  }, [lastMessage]);

  return (
    <div
      ref={termRef}
      style={{
        height: 800,
        width: "100%",
        background: "#263143",
        borderRadius: 8,
      }}
    />
  );
};

export default TerminalXTerm;
