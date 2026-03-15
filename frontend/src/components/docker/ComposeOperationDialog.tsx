import { Icon } from "@iconify/react";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  IconButton,
  Typography,
  LinearProgress,
  useTheme,
} from "@mui/material";
import React, { useState, useEffect, useRef, useCallback } from "react";
import { toast } from "sonner";

import { useStreamMux, decodeString, openDockerComposeStream } from "@/api";
import { useLiveStream } from "@/hooks/useLiveStream";

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
  const { streamRef, openStream, closeStream } = useLiveStream();

  const { isOpen: muxIsOpen } = useStreamMux();

  // Scroll to bottom when output changes
  useEffect(() => {
    if (open && outputBoxRef.current) {
      outputBoxRef.current.scrollTop = outputBoxRef.current.scrollHeight;
    }
  }, [output, open]);

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

    openStream({
      open: () => openDockerComposeStream(action, projectName, composePath),
      onOpenError: () => {
        queueMicrotask(() => {
          setError("Failed to start compose operation");
          setIsRunning(false);
          toast.error("Failed to start compose operation");
        });
      },
      onData: (data: Uint8Array) => {
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
      },
      onClose: () => {
        setIsRunning(false);
      },
    });
  }, [
    open,
    action,
    projectName,
    composePath,
    muxIsOpen,
    openStream,
    streamRef,
  ]);

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
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: theme.spacing(1),
          }}
        >
          {isRunning && <LinearProgress sx={{ width: 100 }} />}
          {success && <Icon icon="mdi:check-circle" width={24} height={24} color={theme.palette.success.main} />}
          {error && <Icon icon="mdi:alert-circle" width={24} height={24} color={theme.palette.error.main} />}
          <Typography variant="h6">
            {getActionLabel()} Stack: {projectName}
          </Typography>
        </div>
        <IconButton onClick={handleClose} size="small">
          <Icon icon="mdi:close" width={20} height={20} />
        </IconButton>
      </DialogTitle>

      <DialogContent sx={{ p: 0 }}>
        <div
          ref={outputBoxRef}
          style={{
            fontFamily: "monospace",
            fontSize: "0.875rem",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            backgroundColor: theme.codeBlock.background,
            color: theme.codeBlock.color,
            padding: theme.spacing(2),
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
        </div>
      </DialogContent>
    </Dialog>
  );
};

export default ComposeOperationDialog;
