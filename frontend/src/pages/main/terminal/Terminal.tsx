import { Menu, MenuItem } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import Minus from "lucide-react/dist/esm/icons/minus";
import Plus from "lucide-react/dist/esm/icons/plus";
import RotateCcw from "lucide-react/dist/esm/icons/rotate-ccw";
import React, { useEffect, useRef, useState } from "react";

import "@xterm/xterm/css/xterm.css";
import {
  useStreamMux,
  bindStreamHandlers,
  encodeString,
  decodeString,
  openTerminalStream,
  type Stream,
} from "@/api";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";

const MIN_FONT = 10;
const MAX_FONT = 28;
const DEFAULT_FONT = 16;

const TerminalXTerm: React.FC = () => {
  const termRef = useRef<HTMLDivElement>(null);
  const xterm = useRef<Terminal | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);
  const streamRef = useRef<Stream | null>(null);
  const unbindRef = useRef<(() => void) | null>(null);
  const theme = useTheme();

  const { isOpen, getStream } = useStreamMux();
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
        stream = openTerminalStream(cols, rows);

        if (stream) {
          streamRef.current = stream;
        }
      }

      if (stream) {
        // Wire up data handler (reattach on each mount)
        unbindRef.current = bindStreamHandlers(stream, {
          onData: (data: Uint8Array) => {
            if (xterm.current) {
              const text = decodeString(data);
              xterm.current.write(text, () => {
                xterm.current?.scrollToBottom();
              });
            }
          },
          onClose: () => {
            unbindRef.current = null;
            streamRef.current = null;
          },
        });

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
      if (unbindRef.current && streamRef.current) {
        console.log(
          `[Terminal] Detaching handlers for stream ${streamRef.current.id}`,
        );
        unbindRef.current();
        unbindRef.current = null;
      }
      streamRef.current = null;
    };
  }, [
    isOpen,
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
      if (unbindRef.current) {
        unbindRef.current();
        unbindRef.current = null;
      }
      streamRef.current.close();
      streamRef.current = null;
    }

    // Clear xterm display
    xterm.current.clear();
    xterm.current.reset();

    // Open fresh stream (creates new PTY)
    const cols = xterm.current.cols;
    const rows = xterm.current.rows;
    const stream = openTerminalStream(cols, rows);

    if (stream) {
      streamRef.current = stream;

      unbindRef.current = bindStreamHandlers(stream, {
        onData: (data: Uint8Array) => {
          if (xterm.current) {
            const text = decodeString(data);
            xterm.current.write(text, () => {
              xterm.current?.scrollToBottom();
            });
          }
        },
        onClose: () => {
          unbindRef.current = null;
          streamRef.current = null;
        },
      });
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
          boxShadow: theme.shadows[2],
        }}
      >
        {/* Font Size Controls */}
        <AppTypography
          variant="body2"
          color="text.secondary"
          fontWeight={500}
          style={{ marginRight: 8 }}
        >
          Font
        </AppTypography>
        <AppIconButton
          size="small"
          style={{ color: "var(--mui-palette-text-secondary)" }}
          onClick={() => setFontSize((f) => Math.max(MIN_FONT, f - 1))}
        >
          <Minus size={18} />
        </AppIconButton>
        <AppTypography
          variant="body2"
          color="text.secondary"
          align="center"
          style={{ minWidth: 28 }}
        >
          {fontSize}
        </AppTypography>
        <AppIconButton
          size="small"
          style={{ color: "var(--mui-palette-text-secondary)" }}
          onClick={() => setFontSize((f) => Math.min(MAX_FONT, f + 1))}
        >
          <Plus size={18} />
        </AppIconButton>

        <div style={{ flex: 1 }} />

        {/* Reset Button */}
        <AppIconButton
          size="small"
          style={{ color: "var(--mui-palette-text-secondary)", marginLeft: 8 }}
          onClick={handleReset}
          title="Reset Terminal"
        >
          <RotateCcw size={18} />
        </AppIconButton>
      </div>
      {/* TERMINAL */}
      <div
        ref={termRef}
        className="my-terminal-root"
        onContextMenu={handleContextMenu}
        style={{
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
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              width: "100%",
            }}
          >
            <span>Copy</span>
            <AppTypography
              variant="body2"
              color="text.secondary"
              style={{ marginLeft: 8 }}
            >
              Shift+C
            </AppTypography>
          </div>
        </MenuItem>
        <MenuItem onClick={handlePaste} sx={{ py: 1 }}>
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              width: "100%",
              gap: theme.spacing(4),
            }}
          >
            <span>Paste</span>
            <AppTypography
              variant="body2"
              color="text.secondary"
              style={{ marginLeft: 8 }}
            >
              Shift+V
            </AppTypography>
          </div>
        </MenuItem>
      </Menu>
    </div>
  );
};

export default TerminalXTerm;
