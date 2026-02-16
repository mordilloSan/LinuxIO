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

import {
  linuxio,
  useStreamMux,
  openDockerReindexStream,
  awaitStreamResult,
  type Stream,
} from "@/api";

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
  const streamRef = useRef<Stream | null>(null);
  const hasCompletedRef = useRef(false);
  const closedByUserRef = useRef(false);

  const { isOpen: muxIsOpen } = useStreamMux();

  const { data: composeProjects = [] } =
    linuxio.docker.list_compose_projects.useQuery({
      enabled: open && success,
    });

  const stacksSummary = success
    ? {
        total: composeProjects.length,
        running: composeProjects.filter((p) => p.status === "running").length,
        stopped: composeProjects.filter((p) => p.status === "stopped").length,
      }
    : null;

  // Close stream helper
  const closeStream = useCallback(() => {
    if (streamRef.current) {
      closedByUserRef.current = true;
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
    hasCompletedRef.current = false;
    closedByUserRef.current = false;
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

    // Don't create duplicate streams or recreate after completion
    if (streamRef.current || hasCompletedRef.current) {
      return;
    }

    const stream = openDockerReindexStream();

    if (!stream) {
      queueMicrotask(() => {
        setError("Failed to start reindex operation");
        setIsRunning(false);
      });
      return;
    }

    streamRef.current = stream;
    closedByUserRef.current = false;

    void awaitStreamResult<ReindexResult, ReindexProgress>(stream, {
      onProgress: (progressData) => {
        setProgress(progressData);
      },
      closeMessage: "Reindex stream closed unexpectedly",
    })
      .then((reindexResult) => {
        hasCompletedRef.current = true;
        setResult(reindexResult);
        setSuccess(true);
        onComplete?.();
      })
      .catch((err: unknown) => {
        if (closedByUserRef.current) {
          return;
        }

        hasCompletedRef.current = true;
        const errorMessage =
          err instanceof Error ? err.message : "Reindex failed";
        setError(errorMessage);
      })
      .finally(() => {
        streamRef.current = null;
        setIsRunning(false);
      });
  }, [open, muxIsOpen, onComplete]);

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
              <Typography
                variant="caption"
                color="text.secondary"
                display="block"
              >
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
