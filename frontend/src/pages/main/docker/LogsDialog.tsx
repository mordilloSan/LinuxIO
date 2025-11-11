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
import React, { useState, useMemo, useEffect, useRef } from "react";

import ComponentLoader from "@/components/loaders/ComponentLoader";

interface LogsDialogProps {
  open: boolean;
  onClose: () => void;
  logs: string | null;
  loading?: boolean;
  error?: string | null;
  containerName?: string;
  onRefresh?: () => void;
  autoRefreshDefault?: boolean;
}

const LogsDialog: React.FC<LogsDialogProps> = ({
  open,
  onClose,
  logs,
  loading,
  error,
  containerName,
  onRefresh,
  autoRefreshDefault = false,
}) => {
  const [search, setSearch] = useState("");
  const [autoRefresh, setAutoRefresh] = useState(autoRefreshDefault);
  const logsBoxRef = useRef<HTMLDivElement>(null);

  // Scroll to bottom when logs change
  useEffect(() => {
    if (open && logsBoxRef.current) {
      logsBoxRef.current.scrollTop = logsBoxRef.current.scrollHeight;
    }
  }, [logs, open]);

  // Filter logs (no highlighting for simplicity)
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

  // Auto-refresh effect
  useEffect(() => {
    if (!onRefresh || !open) return;

    if (autoRefresh) {
      onRefresh(); // Immediate
      const interval = setInterval(onRefresh, 2000);
      return () => clearInterval(interval);
    }
  }, [autoRefresh, onRefresh, open]);

  // Reset form when dialog closes
  const prevOpen = useRef(open);
  useEffect(() => {
    if (prevOpen.current && !open) {
      // Dialog just closed - reset form
      setSearch("");
      setAutoRefresh(autoRefreshDefault);
    }
    prevOpen.current = open;
  }, [open, autoRefreshDefault]);

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
        {onRefresh && (
          <Tooltip title={autoRefresh ? "Auto-refresh ON" : "Auto-refresh OFF"}>
            <FormControlLabel
              control={
                <Switch
                  checked={autoRefresh}
                  onChange={() => setAutoRefresh((v) => !v)}
                  color="primary"
                  size="small"
                />
              }
              label="Auto-refresh"
              sx={{ ml: 1 }}
            />
          </Tooltip>
        )}
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
          {loading && logs == null && (
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
