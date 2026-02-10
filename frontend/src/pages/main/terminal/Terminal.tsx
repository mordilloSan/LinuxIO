import { Box, IconButton, Typography, Menu, MenuItem } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import Minus from "lucide-react/dist/esm/icons/minus";
import Plus from "lucide-react/dist/esm/icons/plus";
import RotateCcw from "lucide-react/dist/esm/icons/rotate-ccw";
import React, { useEffect, useRef, useState } from "react";

import "@xterm/xterm/css/xterm.css";
import { useStreamMux, encodeString, decodeString, type Stream } from "@/api";

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
  const [contextMenu, setContextMenu] = useState<{
    mouseX: number;
    mouseY: number;
  } | null>(null);

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

    // Handle copy/paste with Shift+C/V
    xterm.current.attachCustomKeyEventHandler((event) => {
      // Shift+C - Copy
      if (
        event.shiftKey &&
        event.key === "C" &&
        !event.ctrlKey &&
        !event.altKey &&
        !event.metaKey
      ) {
        const selection = xterm.current?.getSelection();
        if (selection) {
          navigator.clipboard.writeText(selection);
        }
        return false; // Prevent default behavior
      }

      // Shift+V - Paste
      if (
        event.shiftKey &&
        event.key === "V" &&
        !event.ctrlKey &&
        !event.altKey &&
        !event.metaKey
      ) {
        navigator.clipboard.readText().then((text) => {
          if (streamRef.current) {
            streamRef.current.write(encodeString(text));
          }
        });
        return false; // Prevent default behavior
      }

      return true; // Allow default behavior for other keys
    });

    // Set custom scrollbar and connect to stream after DOM is ready
    requestAnimationFrame(() => {
      // xterm.js 6.0 still uses .xterm-viewport for scrolling
      if (termRef.current) {
        const viewport = termRef.current.querySelector(".xterm-viewport");
        if (viewport) {
          viewport.classList.add("custom-scrollbar");
        }
      }
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

        stream.resize(xterm.current.cols, xterm.current.rows);
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
      if (xterm.current && streamRef.current) {
        streamRef.current.resize(xterm.current.cols, xterm.current.rows);
      }
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

  // Context menu handlers
  const handleContextMenu = (event: React.MouseEvent) => {
    event.preventDefault();
    event.stopPropagation();

    // Always close first, then open at new position if it was closed
    const wasOpen = contextMenu !== null;
    setContextMenu(null);

    if (!wasOpen) {
      // Small timeout to ensure state updates
      setTimeout(() => {
        setContextMenu({ mouseX: event.clientX, mouseY: event.clientY });
      }, 0);
    }
  };

  const handleCloseContextMenu = () => {
    setContextMenu(null);
  };

  // Close context menu when tab loses focus
  useEffect(() => {
    const handleBlur = () => {
      setContextMenu(null);
    };
    window.addEventListener("blur", handleBlur);
    return () => {
      window.removeEventListener("blur", handleBlur);
    };
  }, []);

  const handleCopy = () => {
    const selection = xterm.current?.getSelection();
    if (selection) {
      navigator.clipboard.writeText(selection);
    }
    handleCloseContextMenu();
  };

  const handlePaste = () => {
    navigator.clipboard.readText().then((text) => {
      if (streamRef.current) {
        streamRef.current.write(encodeString(text));
      }
    });
    handleCloseContextMenu();
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
              ? theme.darken(theme.sidebar.background, 0.13)
              : theme.lighten(theme.sidebar.background, 0.06),
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
        onContextMenu={handleContextMenu}
        sx={{
          flex: 1,
          overflow: "hidden",
          borderRadius: "0 0 16px 16px",
          background: theme.palette.background.default,
        }}
      />
      {/* CONTEXT MENU */}
      <Menu
        open={contextMenu !== null}
        onClose={handleCloseContextMenu}
        anchorReference="anchorPosition"
        anchorPosition={
          contextMenu !== null
            ? { top: contextMenu.mouseY, left: contextMenu.mouseX }
            : undefined
        }
        autoFocus={false}
        slotProps={{
          paper: {
            sx: {
              borderRadius: 2,
            },
          },
          backdrop: {
            onClick: handleCloseContextMenu,
            onContextMenu: (e: React.MouseEvent) => {
              e.preventDefault();
              handleCloseContextMenu();
            },
          },
        }}
      >
        <MenuItem onClick={handleCopy} sx={{ py: 1 }}>
          <Box
            sx={{
              display: "flex",
              justifyContent: "space-between",
              width: "100%",
            }}
          >
            <span>Copy</span>
            <Typography variant="body2" sx={{ color: "text.secondary", ml: 2 }}>
              Shift+C
            </Typography>
          </Box>
        </MenuItem>
        <MenuItem onClick={handlePaste} sx={{ py: 1 }}>
          <Box
            sx={{
              display: "flex",
              justifyContent: "space-between",
              width: "100%",
              gap: 4,
            }}
          >
            <span>Paste</span>
            <Typography variant="body2" sx={{ color: "text.secondary", ml: 2 }}>
              Shift+V
            </Typography>
          </Box>
        </MenuItem>
      </Menu>
    </Box>
  );
};

export default TerminalXTerm;
