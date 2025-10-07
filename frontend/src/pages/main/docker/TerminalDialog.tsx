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
import React, { useEffect, useRef, useState } from "react";

import "@xterm/xterm/css/xterm.css";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import useWebSocket from "@/hooks/useWebSocket";

interface Props {
  open: boolean;
  onClose: () => void;
  containerId: string;
  containerName?: string;
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

  const [terminalKey, setTerminalKey] = useState(0);
  const [shell, setShell] = useState("");
  const [availableShells, setAvailableShells] = useState<string[]>([]);
  const [loadingShells, setLoadingShells] = useState(false);
  const [hasLoadedShells, setHasLoadedShells] = useState(false);

  // CHANGED: ready -> status
  const { send, subscribe, status } = useWebSocket();
  const isOpen = status === "open";

  const theme = useTheme();

  // --- 1. On open: fetch available shells, set initial shell, and cleanup on close ---
  useEffect(() => {
    if (open && isOpen && containerId) {
      setLoadingShells(true);
      setHasLoadedShells(false);
      send({
        type: "list_shells",
        target: "container",
        containerId,
      });
    }
    if (!open) {
      setShell("");
      setAvailableShells([]);
      setLoadingShells(false);
      setHasLoadedShells(false);
      xterm.current?.dispose();
      xterm.current = null;
      fitAddon.current = null;
    }
  }, [open, isOpen, containerId, send]);

  // --- 2. Listen for shell_list and set availableShells and initial shell ---
  useEffect(() => {
    const unsub = subscribe((msg) => {
      if (
        msg.type === "shell_list" &&
        msg.containerId === containerId &&
        Array.isArray(msg.data) &&
        open
      ) {
        // Clean out empty/falsy entries!
        const validShells = msg.data.filter(
          (s: string) => s && typeof s === "string" && s.trim() !== "",
        );
        setAvailableShells(validShells);
        setShell(validShells.length > 0 ? validShells[0] : "");
        setLoadingShells(false);
        setHasLoadedShells(true);
      }
    });
    return unsub;
  }, [containerId, subscribe, open]);

  // --- 3. Setup xterm and terminal session whenever open, shell, etc. changes ---
  useEffect(() => {
    if (!open || !termRef.current || availableShells.length === 0 || !shell)
      return;

    // Dispose previous instance
    xterm.current?.dispose();

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
      const viewport = termRef.current?.querySelector(".xterm-viewport");
      if (viewport) viewport.classList.add("custom-scrollbar");
    }, 0);

    // Subscribe to terminal output for this container
    const unsub = subscribe((msg) => {
      if (
        msg.type === "terminal_output" &&
        msg.containerId === containerId &&
        xterm.current
      ) {
        xterm.current.write(msg.data);
      }
    });

    // Terminal input handler
    xterm.current.onData((data) => {
      if (isOpen) {
        send({
          type: "terminal_input",
          target: "container",
          containerId,
          data,
        });
      }
    });

    // Send terminal_start when terminal is ready
    if (isOpen && shell) {
      send({
        type: "terminal_start",
        target: "container",
        containerId,
        data: shell,
      });
    }

    // Fit on window resize
    const handleResize = () => fitAddon.current?.fit();
    window.addEventListener("resize", handleResize);

    // Focus on open
    setTimeout(() => {
      termRef.current?.focus();
    }, 200);

    return () => {
      unsub();
      xterm.current?.dispose();
      window.removeEventListener("resize", handleResize);
    };
  }, [
    open,
    shell,
    containerId,
    isOpen,
    send,
    subscribe,
    availableShells.length,
    theme.palette.background.default,
    theme.palette.text.primary,
    terminalKey,
  ]);

  // --- Shell picker handler ---
  const handleShellChange = (e: SelectChangeEvent<string>) => {
    const newShell = e.target.value as string;
    if (isOpen && containerId && shell) {
      send({
        type: "terminal_close",
        target: "container",
        containerId,
      });
    }
    setShell(newShell);
    setTerminalKey((k) => k + 1); // Force remount of xterm
  };

  // --- Dialog close handler ---
  const handleDialogClose = () => {
    if (isOpen && containerId) {
      send({
        type: "terminal_close",
        target: "container",
        containerId,
      });
    }
    onClose();
  };

  return (
    <Dialog open={open} onClose={handleDialogClose} maxWidth="md" fullWidth>
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
