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
import React, { useEffect, useRef, useState } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import "xterm/css/xterm.css";

import { useAppWebSocket } from "@/contexts/WebSocketContext";

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
  const [availableShells, setAvailableShells] = useState<string[]>(["sh"]);
  const { send, subscribe, ready } = useAppWebSocket();
  const theme = useTheme();

  const [shell, setShell] = useState("bash");

  // Reset shell on close
  useEffect(() => {
    if (!open) setShell("bash");
  }, [open]);

  // Fetch available shells on open
  useEffect(() => {
    if (open && ready && containerId) {
      send({
        type: "list_shells",
        target: "container",
        containerId,
      });
    }
  }, [open, ready, containerId, send]);

  // Listen for shell_list message
  useEffect(() => {
    const unsub = subscribe((msg) => {
      if (
        msg.type === "shell_list" &&
        msg.containerId === containerId &&
        Array.isArray(msg.data) &&
        open // Only set if open
      ) {
        setAvailableShells(msg.data);
        if (msg.data.length === 0) {
          setShell(""); // No shell to launch
        } else {
          setShell(msg.data[0]);
        }
      }
    });
    return unsub;
  }, [containerId, subscribe, open]);

  // Only setup xterm if shells are available and a shell is selected
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
      if (ready) {
        send({
          type: "terminal_input",
          target: "container",
          containerId,
          data,
        });
      }
    });

    // Send terminal_start when terminal is ready
    if (ready && shell) {
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

    return () => {
      unsub();
      xterm.current?.dispose();
      window.removeEventListener("resize", handleResize);
    };
  }, [
    open,
    shell,
    containerId,
    ready,
    send,
    subscribe,
    availableShells.length,
    theme.palette.background.default,
    theme.palette.text.primary,
  ]);

  // Re-apply theme if it changes (while dialog open)
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

  // Optional: Focus terminal on open
  useEffect(() => {
    if (open && availableShells.length > 0 && shell && termRef.current) {
      setTimeout(() => {
        termRef.current?.focus();
      }, 200);
    }
  }, [open, availableShells.length, shell]);

  //cleanup
  useEffect(() => {
    if (!open) {
      setShell(""); // clear shell on close
      setAvailableShells([]); // clear shell list
      xterm.current?.dispose();
      xterm.current = null;
      fitAddon.current = null;
    }
  }, [open]);

  // Shell picker handler
  const handleShellChange = (e: SelectChangeEvent<string>) => {
    setShell(e.target.value as string);
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
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
              disabled={!ready || availableShells.length === 0}
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
        {availableShells.length === 0 ? (
          <Box sx={{ p: 3, color: "error.main", textAlign: "center" }}>
            No shell available in this container.
            <br />
            (Try installing <b>bash</b> or <b>sh</b> in your container.)
          </Box>
        ) : (
          <Box
            ref={termRef}
            sx={{
              width: "100%",
              minHeight: 350,
              height: 420,
              background: theme.palette.background.default,
              "& .xterm-viewport": {
                // Optional: custom scrollbar styling
                scrollbarColor: "#777 #222",
              },
            }}
            tabIndex={0}
          />
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
};

export default TerminalDialog;
