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
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import React, { useEffect, useRef, useState, useCallback } from "react";

import "@xterm/xterm/css/xterm.css";
import { linuxio, Stream, encodeString, decodeString } from "@/api/linuxio";
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
  const [shell, setShell] = useState("");
  const [availableShells, setAvailableShells] = useState<string[]>([]);
  const [loadingShells, setLoadingShells] = useState(false);
  const [hasLoadedShells, setHasLoadedShells] = useState(false);

  const { isOpen, openStream } = linuxio.useStreamMux();
  const theme = useTheme();

  // Fetch available shells when dialog opens
  const fetchShells = useCallback(async () => {
    if (!containerId) return;

    setLoadingShells(true);
    try {
      const shells = await linuxio.request<string[]>(
        "terminal",
        "list_shells",
        [containerId],
      );
      const validShells = shells.filter(
        (s: string) => s && typeof s === "string" && s.trim() !== "",
      );
      setAvailableShells(validShells);
      setShell(validShells.length > 0 ? validShells[0] : "");
      setHasLoadedShells(true);
    } catch (error) {
      console.error("Failed to fetch container shells:", error);
      setAvailableShells([]);
      setHasLoadedShells(true);
    } finally {
      setLoadingShells(false);
    }
  }, [containerId]);

  const handleDialogEntered = useCallback(() => {
    setShell("");
    setAvailableShells([]);
    setHasLoadedShells(false);
    fetchShells();
  }, [fetchShells]);

  const handleDialogExited = useCallback(() => {
    // Close stream on dialog exit
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
    setShell("");
    setAvailableShells([]);
    setHasLoadedShells(false);
    setLoadingShells(false);
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
      !shell ||
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
    const payload = buildContainerPayload(containerId, shell, cols, rows);
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
    }

    // Terminal input -> send to stream
    const onDataDispose = xterm.current.onData((data) => {
      if (streamRef.current) {
        streamRef.current.write(encodeString(data));
      }
    });

    // Fit on window resize
    const handleResize = () => fitAddon.current?.fit();
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
    shell,
    containerId,
    isOpen,
    openStream,
    availableShells.length,
    theme.palette.background.default,
    theme.palette.text.primary,
    terminalKey,
  ]);

  // Shell picker handler
  const handleShellChange = (e: SelectChangeEvent<string>) => {
    const newShell = e.target.value as string;
    // Close existing stream
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
    setShell(newShell);
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
              value={shell}
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
        ) : hasLoadedShells && availableShells.length === 0 ? (
          <Box sx={{ p: 3, color: "error.main", textAlign: "center" }}>
            No shell available in this container.
            <br />
            (Try installing <b>bash</b> or <b>sh</b> in your container.)
          </Box>
        ) : availableShells.length > 0 ? (
          <Box
            key={terminalKey}
            ref={termRef}
            sx={{
              width: "100%",
              minHeight: 350,
              height: 420,
              background: theme.palette.background.default,
            }}
            tabIndex={0}
          />
        ) : null}
      </DialogContent>
      <DialogActions>
        <Button onClick={handleDialogClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
};

export default TerminalDialog;
