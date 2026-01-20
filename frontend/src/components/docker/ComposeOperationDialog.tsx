import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import CloseIcon from "@mui/icons-material/Close";
import ErrorIcon from "@mui/icons-material/Error";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  IconButton,
  Box,
  Typography,
  LinearProgress,
  useTheme,
} from "@mui/material";
import React, { useState, useEffect, useRef, useCallback } from "react";
import { toast } from "sonner";

import { useStreamMux, decodeString, encodeString } from "@/api/linuxio";
import type { Stream } from "@/api/linuxio";

interface ComposeOperationDialogProps {
  open: boolean;
  onClose: () => void;
  action: "up" | "down" | "stop" | "restart";
  projectName: string;
  composePath?: string;
}

interface ComposeMessage {
  type: "stdout" | "stderr" | "error" | "complete";
  message: string;
}

const ComposeOperationDialog: React.FC<ComposeOperationDialogProps> = ({
  open,
  onClose,
  action,
  projectName,
  composePath,
}) => {
  const theme = useTheme();
  const [output, setOutput] = useState<string[]>([]);
  const [isRunning, setIsRunning] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const outputBoxRef = useRef<HTMLDivElement>(null);
  const streamRef = useRef<Stream | null>(null);

  const { isOpen: muxIsOpen, openStream } = useStreamMux();

  // Scroll to bottom when output changes
  useEffect(() => {
    if (open && outputBoxRef.current) {
      outputBoxRef.current.scrollTop = outputBoxRef.current.scrollHeight;
    }
  }, [output, open]);

  // Close stream helper
  const closeStream = useCallback(() => {
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
  }, []);

  // Reset state helper - called from transition callbacks, not effects
  const resetState = useCallback(() => {
    closeStream();
    setOutput([]);
    setIsRunning(true);
    setError(null);
    setSuccess(false);
  }, [closeStream]);

  // Cleanup stream when dialog closes (only close stream, not state)
  useEffect(() => {
    if (!open) {
      closeStream();
    }
  }, [open, closeStream]);

  // Open stream when dialog opens
  useEffect(() => {
    if (!open || !muxIsOpen) {
      return;
    }

    // Don't create duplicate streams
    if (streamRef.current) {
      return;
    }

    // Build payload: docker-compose\0action\0projectName\0composePath
    let payloadStr = `docker-compose\0${action}\0${projectName}`;
    if (composePath) {
      payloadStr += `\0${composePath}`;
    }

    const payload = encodeString(payloadStr);
    const stream = openStream("docker-compose", payload);

    if (!stream) {
      queueMicrotask(() => {
        setError("Failed to start compose operation");
        setIsRunning(false);
        toast.error("Failed to start compose operation");
      });
      return;
    }

    streamRef.current = stream;

    // Handle incoming data
    stream.onData = (data: Uint8Array) => {
      const text = decodeString(data);
      try {
        const msg: ComposeMessage = JSON.parse(text);

        switch (msg.type) {
          case "stdout":
          case "stderr":
            setOutput((prev) => [...prev, msg.message]);
            break;
          case "error":
            setError(msg.message);
            setIsRunning(false);
            toast.error(`Failed to ${action} stack: ${msg.message}`);
            break;
          case "complete":
            setSuccess(true);
            setIsRunning(false);
            setOutput((prev) => [...prev, "✓ " + msg.message]);
            break;
        }
      } catch {
        // If not JSON, just append as-is
        setOutput((prev) => [...prev, text]);
      }
    };

    stream.onClose = () => {
      streamRef.current = null;
      setIsRunning(false);
    };
  }, [open, action, projectName, composePath, muxIsOpen, openStream]);

  const getActionLabel = () => {
    switch (action) {
      case "up":
        return "Starting";
      case "down":
        return "Removing";
      case "stop":
        return "Stopping";
      case "restart":
        return "Restarting";
      default:
        return "Processing";
    }
  };

  const handleClose = () => {
    if (isRunning) {
      // Close stream if still running
      closeStream();
    }
    onClose();
  };

  return (
    <Dialog
      open={open}
      onClose={handleClose}
      maxWidth="md"
      fullWidth
      slotProps={{
        paper: {
          sx: {
            backgroundColor: theme.palette.background.default,
            maxHeight: "80vh",
          },
        },
        transition: {
          onExited: resetState,
        },
      }}
    >
      <DialogTitle
        sx={{
          backgroundColor: theme.header.background,
          borderBottom: `1px solid ${theme.palette.divider}`,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          {isRunning && <LinearProgress sx={{ width: 100 }} />}
          {success && <CheckCircleIcon color="success" />}
          {error && <ErrorIcon color="error" />}
          <Typography variant="h6">
            {getActionLabel()} Stack: {projectName}
          </Typography>
        </Box>
        <IconButton onClick={handleClose} size="small">
          <CloseIcon />
        </IconButton>
      </DialogTitle>

      <DialogContent sx={{ p: 0 }}>
        <Box
          ref={outputBoxRef}
          sx={{
            fontFamily: "monospace",
            fontSize: "0.875rem",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            backgroundColor:
              theme.palette.mode === "dark" ? "#1e1e1e" : "#f5f5f5",
            color: theme.palette.mode === "dark" ? "#d4d4d4" : "#333",
            p: 2,
            minHeight: "400px",
            maxHeight: "600px",
            overflowY: "auto",
          }}
        >
          {output.length === 0 && isRunning && (
            <Typography color="text.secondary">
              Starting operation...
            </Typography>
          )}
          {output.map((line, index) => (
            <div key={index}>{line}</div>
          ))}
          {error && (
            <Typography color="error" sx={{ mt: 2 }}>
              Error: {error}
            </Typography>
          )}
        </Box>
      </DialogContent>
    </Dialog>
  );
};

export default ComposeOperationDialog;
