import { Icon } from "@iconify/react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import React, { useEffect, useRef, useState } from "react";
import "@xterm/xterm/css/xterm.css";

import {
  bindStreamHandlers,
  decodeString,
  encodeString,
  openTerminalStream,
  type Stream,
  useStreamMux,
} from "@/api";
import AppIconButton from "@/components/ui/AppIconButton";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTypography from "@/components/ui/AppTypography";
import { shadowSm } from "@/constants";
import { useAppTheme } from "@/theme";

const MIN_FONT = 10;
const MAX_FONT = 28;
const DEFAULT_FONT = 16;

const TerminalXTerm: React.FC = () => {
  const termRef = useRef<HTMLDivElement>(null);
  const xterm = useRef<Terminal | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);
  const streamRef = useRef<Stream | null>(null);
  const unbindRef = useRef<(() => void) | null>(null);
  const theme = useAppTheme();

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

    // Handle Ctrl+Shift+C to copy the xterm selection.
    // Paste is handled natively: pressing Ctrl+Shift+V (or Ctrl+V) makes the
    // browser paste into xterm's hidden textarea, which fires onData below.
    xterm.current.attachCustomKeyEventHandler((event) => {
      if (event.type !== "keydown") return true;

      if (
        event.ctrlKey &&
        event.shiftKey &&
        event.key === "C" &&
        !event.altKey &&
        !event.metaKey
      ) {
        // preventDefault stops Chrome from opening the DevTools inspector.
        event.preventDefault();
        event.stopPropagation();
        const selection = xterm.current?.getSelection();
        if (selection) {
          navigator.clipboard.writeText(selection);
        }
        return false;
      }

      return true;
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
      let stream = getStream("terminal.open");
      console.log(
        "[Terminal] getStream('terminal.open'):",
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
      {/* CONTEXT MENU */}
      <AppMenu
        anchorPosition={
          contextMenu !== null
            ? { top: contextMenu.mouseY, left: contextMenu.mouseX }
            : undefined
        }
        autoFocus={false}
        minWidth={168}
        onClose={handleCloseContextMenu}
        open={contextMenu !== null}
      >
        <AppMenuItem
          endAdornment={
            <AppTypography
              color="text.secondary"
              style={{ marginLeft: 8 }}
              variant="body2"
            >
              Ctrl+Shift+C
            </AppTypography>
          }
          onClick={handleCopy}
        >
          Copy
        </AppMenuItem>
        <AppMenuItem
          endAdornment={
            <AppTypography
              color="text.secondary"
              style={{ marginLeft: 8 }}
              variant="body2"
            >
              Ctrl+Shift+V
            </AppTypography>
          }
          onClick={handlePaste}
        >
          Paste
        </AppMenuItem>
      </AppMenu>
    </div>
  );
};

export default TerminalXTerm;
