import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Box,
  MenuItem,
  Select,
  FormControl,
  InputLabel,
  SelectChangeEvent,
  Menu,
  Typography,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import React, { useEffect, useRef, useState, useCallback } from "react";

import "@xterm/xterm/css/xterm.css";
import { useStreamMux } from "@/api/linuxio";
import linuxio from "@/api/react-query";
import { encodeString, decodeString } from "@/api/StreamMultiplexer";
import type { Stream } from "@/api/StreamMultiplexer";
import ComponentLoader from "@/components/loaders/ComponentLoader";

interface Props {
  open: boolean;
  onClose: () => void;
  containerId: string;
  containerName?: string;
}

// Build container terminal payload: "container\0containerID\0shell\0cols\0rows"
function buildContainerPayload(
  containerId: string,
  shell: string,
  cols: number,
  rows: number,
): Uint8Array {
  return encodeString(`container\0${containerId}\0${shell}\0${cols}\0${rows}`);
}

const TerminalDialog: React.FC<Props> = ({
  open,
  onClose,
  containerId,
  containerName,
}) => {
  const termRef = useRef<HTMLDivElement>(null);
  const xterm = useRef<Terminal | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);
  const streamRef = useRef<Stream | null>(null);

  const [terminalKey, setTerminalKey] = useState(0);
  const [selectedShell, setSelectedShell] = useState<string | null>(null);
  const [contextMenu, setContextMenu] = useState<{
    mouseX: number;
    mouseY: number;
  } | null>(null);

  const { isOpen, openStream } = useStreamMux();
  const theme = useTheme();

  // Fetch available shells when dialog opens
  const {
    data: shells,
    isLoading: loadingShells,
    isFetched: hasFetchedShells,
  } = linuxio.terminal.list_shells.useQuery(containerId, {
    enabled: open && !!containerId,
  });

  const availableShells = React.useMemo(() => {
    if (!shells) return [];
    return shells.filter((s) => s && typeof s === "string" && s.trim() !== "");
  }, [shells]);

  const activeShell = React.useMemo(() => {
    if (selectedShell && availableShells.includes(selectedShell)) {
      return selectedShell;
    }
    return availableShells[0] ?? "";
  }, [selectedShell, availableShells]);

  const handleDialogEntered = useCallback(() => {
    setSelectedShell(null);
  }, []);

  const handleDialogExited = useCallback(() => {
    // Close stream on dialog exit
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
    setSelectedShell(null);
    xterm.current?.dispose();
    xterm.current = null;
    fitAddon.current = null;
  }, []);

  // Setup xterm and stream when shell is selected
  useEffect(() => {
    if (
      !open ||
      !termRef.current ||
      availableShells.length === 0 ||
      !activeShell ||
      !isOpen
    )
      return;

    // Dispose previous instance
    xterm.current?.dispose();
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }

    xterm.current = new Terminal({
      fontFamily: "monospace",
      fontSize: 15,
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
    fitAddon.current.fit();

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

    setTimeout(() => {
      // xterm.js 6.0 still uses .xterm-viewport for scrolling
      if (termRef.current) {
        const viewport = termRef.current.querySelector(".xterm-viewport");
        if (viewport) {
          viewport.classList.add("custom-scrollbar");
        }
      }
    }, 0);

    // Open container terminal stream
    const cols = xterm.current.cols;
    const rows = xterm.current.rows;
    const payload = buildContainerPayload(containerId, activeShell, cols, rows);
    const stream = openStream("container", payload);

    if (stream) {
      streamRef.current = stream;

      // Wire up data handler
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

    // Terminal input -> send to stream
    const onDataDispose = xterm.current.onData((data) => {
      if (streamRef.current) {
        streamRef.current.write(encodeString(data));
      }
    });

    // Fit on window resize
    const handleResize = () => {
      fitAddon.current?.fit();
      if (xterm.current && streamRef.current) {
        streamRef.current.resize(xterm.current.cols, xterm.current.rows);
      }
    };
    window.addEventListener("resize", handleResize);

    // Focus on open
    setTimeout(() => {
      xterm.current?.focus();
    }, 200);

    return () => {
      onDataDispose.dispose();
      xterm.current?.dispose();
      window.removeEventListener("resize", handleResize);
      // Close stream when effect cleans up
      if (streamRef.current) {
        streamRef.current.close();
        streamRef.current = null;
      }
    };
  }, [
    open,
    activeShell,
    containerId,
    isOpen,
    openStream,
    availableShells.length,
    theme.palette.background.default,
    theme.palette.text.primary,
    terminalKey,
  ]);

  // Shell picker handler
  const handleShellChange = (e: SelectChangeEvent) => {
    const newShell = e.target.value;
    // Close existing stream
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
    setSelectedShell(newShell);
    setTerminalKey((k) => k + 1); // Force remount of xterm
  };

  // Dialog close handler
  const handleDialogClose = () => {
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
    onClose();
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

  return (
    <Dialog
      open={open}
      onClose={handleDialogClose}
      maxWidth="md"
      fullWidth
      slotProps={{
        transition: {
          onEntered: handleDialogEntered,
          onExited: handleDialogExited,
        },
      }}
    >
      <DialogTitle>
        {containerName ? `Shell for ${containerName}` : "Container Shell"}
        <Box sx={{ float: "right" }}>
          <FormControl size="small" variant="standard">
            <InputLabel id="shell-label">Shell</InputLabel>
            <Select
              labelId="shell-label"
              value={activeShell}
              onChange={handleShellChange}
              sx={{ minWidth: 80 }}
              disabled={!isOpen || availableShells.length === 0}
            >
              {availableShells.map((s) => (
                <MenuItem value={s} key={s}>
                  {s}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        </Box>
      </DialogTitle>
      <DialogContent
        dividers
        sx={{
          minHeight: 350,
          maxHeight: 600,
          fontFamily: "Fira Mono, monospace",
          p: 0,
          background: theme.palette.background.default,
        }}
      >
        {loadingShells ? (
          <Box sx={{ p: 3, textAlign: "center" }}>
            <ComponentLoader />
          </Box>
        ) : hasFetchedShells && availableShells.length === 0 ? (
          <Box sx={{ p: 3, color: "error.main", textAlign: "center" }}>
            No shell available in this container.
            <br />
            (Try installing <b>bash</b> or <b>sh</b> in your container.)
          </Box>
        ) : availableShells.length > 0 ? (
          <Box
            key={terminalKey}
            ref={termRef}
            onContextMenu={handleContextMenu}
            sx={{
              width: "100%",
              minHeight: 350,
              height: 420,
              background: theme.palette.background.default,
            }}
            tabIndex={0}
          />
        ) : null}
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
              <Typography
                variant="body2"
                sx={{ color: "text.secondary", ml: 2 }}
              >
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
              <Typography
                variant="body2"
                sx={{ color: "text.secondary", ml: 2 }}
              >
                Shift+V
              </Typography>
            </Box>
          </MenuItem>
        </Menu>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleDialogClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
};

export default TerminalDialog;
