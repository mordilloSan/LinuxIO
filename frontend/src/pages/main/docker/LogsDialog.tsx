import CloseIcon from "@mui/icons-material/Close";
import DownloadIcon from "@mui/icons-material/Download";
import FileCopyIcon from "@mui/icons-material/FileCopy";
import SearchIcon from "@mui/icons-material/Search";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  IconButton,
  TextField,
  Box,
  Tooltip,
  Typography,
  Switch,
  FormControlLabel,
} from "@mui/material";
import React, {
  useState,
  useMemo,
  useEffect,
  useRef,
  useCallback,
} from "react";

import {
  useStreamMux,
  openDockerLogsStream,
  decodeString,
} from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useLiveStream } from "@/hooks/useLiveStream";

interface LogsDialogProps {
  open: boolean;
  onClose: () => void;
  containerName?: string;
  containerId: string;
}

const LogsDialog: React.FC<LogsDialogProps> = ({
  open,
  onClose,
  containerName,
  containerId,
}) => {
  const [search, setSearch] = useState("");
  const [liveMode, setLiveMode] = useState(true);
  const [logs, setLogs] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const logsBoxRef = useRef<HTMLDivElement>(null);
  const hasReceivedData = useRef(false);
  const lastContainerId = useRef<string | null>(null);
  const { streamRef, openStream, closeStream } = useLiveStream();

  const { isOpen: muxIsOpen } = useStreamMux();

  // Scroll to bottom when logs change
  useEffect(() => {
    if (open && logsBoxRef.current) {
      logsBoxRef.current.scrollTop = logsBoxRef.current.scrollHeight;
    }
  }, [logs, open]);

  // Reset state helper - called from transition callbacks, not effects
  const resetState = useCallback(() => {
    closeStream();
    setLogs("");
    setError(null);
    setLiveMode(true);
    setIsLoading(true);
    hasReceivedData.current = false;
    lastContainerId.current = null;
  }, [closeStream]);

  // Open stream when dialog opens
  useEffect(() => {
    if (!open || !containerId || !muxIsOpen) {
      return;
    }

    // Don't create duplicate streams
    if (streamRef.current) {
      return;
    }

    // Track that we're connecting to this container
    lastContainerId.current = containerId;
    hasReceivedData.current = false;

    openStream({
      open: () => openDockerLogsStream(containerId, "100"),
      onOpenError: () => {
        queueMicrotask(() => {
          setError("Failed to connect to log stream");
          setIsLoading(false);
        });
      },
      onData: (data: Uint8Array) => {
        const text = decodeString(data);
        if (!hasReceivedData.current) {
          hasReceivedData.current = true;
          setIsLoading(false);
        }
        setLogs((prev) => prev + text);
      },
      onClose: () => {
        if (!hasReceivedData.current) {
          setIsLoading(false);
        }
      },
    });
  }, [open, containerId, muxIsOpen, openStream, streamRef]);

  // Handle liveMode toggle - close stream when disabled
  useEffect(() => {
    if (!liveMode && streamRef.current) {
      closeStream();
      if (!hasReceivedData.current) {
        setIsLoading(false);
      }
    } else if (
      liveMode &&
      !streamRef.current &&
      open &&
      containerId &&
      muxIsOpen
    ) {
      // Re-open stream when live mode is re-enabled
      openStream({
        open: () => openDockerLogsStream(containerId, "0"), // Don't re-fetch old logs
        onData: (data: Uint8Array) => {
          const text = decodeString(data);
          setLogs((prev) => prev + text);
        },
      });
    }
  }, [liveMode, open, containerId, muxIsOpen, closeStream, openStream, streamRef]);

  // Cleanup stream when dialog closes (only close stream, not state)
  useEffect(() => {
    if (!open) {
      closeStream();
    }
  }, [open, closeStream]);

  // Filter logs
  const filtered = useMemo(() => {
    if (!search || !logs) return logs;
    return logs
      .split("\n")
      .filter((line) => line.toLowerCase().includes(search.toLowerCase()))
      .join("\n");
  }, [logs, search]);

  // Copy logs
  const handleCopy = () => {
    if (filtered) navigator.clipboard.writeText(filtered);
  };

  // Download logs
  const handleDownload = () => {
    if (!filtered) return;
    const blob = new Blob([filtered], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${containerName || "container"}-logs.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="md"
      fullWidth
      slotProps={{
        transition: {
          onEntered: () => {
            if (logsBoxRef.current) {
              logsBoxRef.current.scrollTop = logsBoxRef.current.scrollHeight;
            }
          },
          onExited: () => {
            // Reset all state when dialog fully closes
            resetState();
            setSearch("");
          },
        },
      }}
    >
      <DialogTitle sx={{ display: "flex", alignItems: "center", gap: 1 }}>
        <SearchIcon fontSize="small" />
        <TextField
          variant="standard"
          placeholder="Search logs…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          size="small"
          sx={{ ml: 1, flex: 1 }}
        />
        <Tooltip title="Copy logs">
          <IconButton onClick={handleCopy} size="small">
            <FileCopyIcon fontSize="small" />
          </IconButton>
        </Tooltip>
        <Tooltip title="Download logs">
          <IconButton onClick={handleDownload} size="small">
            <DownloadIcon fontSize="small" />
          </IconButton>
        </Tooltip>
        <Tooltip title={liveMode ? "Live streaming ON" : "Live streaming OFF"}>
          <FormControlLabel
            control={
              <Switch
                checked={liveMode}
                onChange={(_, checked) => setLiveMode(checked)}
                color="primary"
                size="small"
              />
            }
            label="Live"
            sx={{ ml: 1 }}
          />
        </Tooltip>
        <IconButton onClick={onClose} size="small">
          <CloseIcon fontSize="small" />
        </IconButton>
      </DialogTitle>
      <DialogContent
        dividers
        sx={{
          minHeight: 300,
          maxHeight: 600,
          fontFamily: "Fira Mono, monospace",
          fontSize: "0.95rem",
          background: "#19191d",
          color: "#ececec",
          p: 2,
        }}
      >
        <Box
          sx={{
            position: "relative",
            whiteSpace: "pre-wrap",
            minHeight: 240,
            maxHeight: 540,
            overflowY: "auto",
          }}
          ref={logsBoxRef}
        >
          {error ? (
            <Typography color="error" sx={{ p: 2 }}>
              {error}
            </Typography>
          ) : (
            filtered || "No logs available."
          )}
          {isLoading && (
            <Box
              sx={{
                position: "absolute",
                top: 0,
                left: 0,
                right: 0,
                bottom: 0,
                background: "rgba(25,25,29,0.85)",
                zIndex: 10,
                borderRadius: 2,
              }}
            >
              <ComponentLoader />
            </Box>
          )}
        </Box>
      </DialogContent>
    </Dialog>
  );
};

export default LogsDialog;
