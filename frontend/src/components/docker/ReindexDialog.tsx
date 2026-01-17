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

import { useStreamMux, decodeString, encodeString } from "@/api/linuxio";
import linuxio from "@/api/react-query";
import type { Stream } from "@/api/linuxio";

interface ReindexDialogProps {
  open: boolean;
  onClose: () => void;
  onComplete?: () => void;
}

interface ReindexProgress {
  files_indexed: number;
  dirs_indexed: number;
  current_path?: string;
  phase?: string;
}

interface ReindexResult {
  path: string;
  files_indexed: number;
  dirs_indexed: number;
  duration_ms: number;
}

const ReindexDialog: React.FC<ReindexDialogProps> = ({
  open,
  onClose,
  onComplete,
}) => {
  const theme = useTheme();
  const [progress, setProgress] = useState<ReindexProgress>({
    files_indexed: 0,
    dirs_indexed: 0,
    phase: "connecting",
  });
  const [isRunning, setIsRunning] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [result, setResult] = useState<ReindexResult | null>(null);
  const [stacksSummary, setStacksSummary] = useState<{
    total: number;
    running: number;
    stopped: number;
  } | null>(null);
  const streamRef = useRef<Stream | null>(null);

  const { isOpen: muxIsOpen, openStream } = useStreamMux();

  // Fetch stacks after successful reindex
  const fetchStacksSummary = useCallback(async () => {
    try {
      const projects = await linuxio.call<
        Array<{ status: string; name: string }>
      >("docker", "list_compose_projects", []);

      const summary = {
        total: projects.length,
        running: projects.filter((p) => p.status === "running").length,
        stopped: projects.filter((p) => p.status === "stopped").length,
      };
      setStacksSummary(summary);
    } catch (err) {
      console.error("Failed to fetch stacks summary:", err);
    }
  }, []);

  // Close stream helper
  const closeStream = useCallback(() => {
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
  }, []);

  // Reset state helper
  const resetState = useCallback(() => {
    closeStream();
    setProgress({ files_indexed: 0, dirs_indexed: 0, phase: "connecting" });
    setIsRunning(true);
    setError(null);
    setSuccess(false);
    setResult(null);
    setStacksSummary(null);
  }, [closeStream]);

  // Cleanup stream when dialog closes
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

    // Build payload: docker-reindex (no args needed)
    const payload = encodeString("docker-reindex");
    const stream = openStream("docker-reindex", payload);

    if (!stream) {
      queueMicrotask(() => {
        setError("Failed to start reindex operation");
        setIsRunning(false);
      });
      return;
    }

    streamRef.current = stream;

    // Handle progress updates
    stream.onProgress = (progressData: ReindexProgress) => {
      setProgress(progressData);
    };

    // Handle result
    stream.onResult = (resultFrame: {
      status: string;
      data?: ReindexResult;
      error?: string;
    }) => {
      if (resultFrame.status === "ok" && resultFrame.data) {
        setResult(resultFrame.data);
        setSuccess(true);
        setIsRunning(false);
        // Fetch stacks summary
        fetchStacksSummary();
        if (onComplete) {
          onComplete();
        }
      } else {
        setError(resultFrame.error || "Reindex failed");
        setIsRunning(false);
      }
    };

    stream.onClose = () => {
      streamRef.current = null;
      if (!success && !error) {
        setIsRunning(false);
      }
    };
  }, [open, muxIsOpen, openStream, onComplete, success, error, fetchStacksSummary]);

  const handleClose = () => {
    if (isRunning) {
      closeStream();
    }
    onClose();
  };

  const getPhaseLabel = () => {
    switch (progress.phase) {
      case "connecting":
        return "Connecting to indexer...";
      case "indexing":
        return "Indexing Docker folder...";
      default:
        return "Processing...";
    }
  };

  return (
    <Dialog
      open={open}
      onClose={handleClose}
      maxWidth="sm"
      fullWidth
      slotProps={{
        paper: {
          sx: {
            backgroundColor: theme.palette.background.default,
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
          <Typography variant="h6">Reindexing Docker Folder</Typography>
        </Box>
        <IconButton onClick={handleClose} size="small">
          <CloseIcon />
        </IconButton>
      </DialogTitle>

      <DialogContent sx={{ p: 3 }}>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
          {/* Phase indicator */}
          <Typography variant="body2" color="text.secondary">
            {getPhaseLabel()}
          </Typography>

          {/* Progress stats */}
          {progress.phase === "indexing" && (
            <Box
              sx={{
                display: "flex",
                gap: 3,
                p: 2,
                backgroundColor:
                  theme.palette.mode === "dark" ? "#1e1e1e" : "#f5f5f5",
                borderRadius: 1,
              }}
            >
              <Box>
                <Typography variant="h4" color="primary">
                  {progress.files_indexed.toLocaleString()}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  Files indexed
                </Typography>
              </Box>
              <Box>
                <Typography variant="h4" color="primary">
                  {progress.dirs_indexed.toLocaleString()}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  Directories indexed
                </Typography>
              </Box>
            </Box>
          )}

          {/* Success result */}
          {success && result && (
            <Box sx={{ mt: 2 }}>
              <Typography variant="body2" color="success.main" gutterBottom>
                ✓ Reindex completed successfully!
              </Typography>
              <Typography variant="caption" color="text.secondary" display="block">
                Indexed {result.files_indexed.toLocaleString()} files and{" "}
                {result.dirs_indexed.toLocaleString()} directories in{" "}
                {(result.duration_ms / 1000).toFixed(2)}s
              </Typography>

              {/* Stacks summary */}
              {stacksSummary && (
                <Box
                  sx={{
                    mt: 2,
                    p: 2,
                    backgroundColor:
                      theme.palette.mode === "dark" ? "#1e1e1e" : "#f5f5f5",
                    borderRadius: 1,
                  }}
                >
                  <Typography
                    variant="subtitle2"
                    color="text.primary"
                    gutterBottom
                  >
                    Docker Compose Stacks Found:
                  </Typography>
                  <Box sx={{ display: "flex", gap: 3, mt: 1 }}>
                    <Box>
                      <Typography variant="h5" color="primary">
                        {stacksSummary.total}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        Total stacks
                      </Typography>
                    </Box>
                    <Box>
                      <Typography variant="h5" color="success.main">
                        {stacksSummary.running}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        Running
                      </Typography>
                    </Box>
                    <Box>
                      <Typography variant="h5" color="text.secondary">
                        {stacksSummary.stopped}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        Stopped
                      </Typography>
                    </Box>
                  </Box>
                </Box>
              )}
            </Box>
          )}

          {/* Error message */}
          {error && (
            <Typography color="error" variant="body2">
              Error: {error}
            </Typography>
          )}
        </Box>
      </DialogContent>
    </Dialog>
  );
};

export default ReindexDialog;
