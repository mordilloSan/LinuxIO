import { FileCopy, Download, Close, Search } from "@mui/icons-material";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  IconButton,
  TextField,
  Box,
  Button,
  Tooltip,
  Typography,
  CircularProgress,
  Switch,
  FormControlLabel,
} from "@mui/material";
import React, { useState, useMemo, useEffect } from "react";

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
    if (!autoRefresh || !onRefresh || !open) return;
    const interval = setInterval(() => {
      onRefresh();
    }, 2000); // 2s refresh interval, tweak as needed
    return () => clearInterval(interval);
  }, [autoRefresh, onRefresh, open]);

  // Reset search/autorefresh when closed
  useEffect(() => {
    if (!open) {
      setSearch("");
      setAutoRefresh(autoRefreshDefault);
    }
  }, [open, autoRefreshDefault]);

  // Immediately refresh when enabling auto-refresh
  useEffect(() => {
    if (autoRefresh && onRefresh && open) {
      onRefresh();
    }
    // Only trigger when autoRefresh goes true, or dialog opens with autoRefresh
  }, [autoRefresh]);

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle sx={{ display: "flex", alignItems: "center", gap: 1 }}>
        <Search fontSize="small" />
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
            <FileCopy fontSize="small" />
          </IconButton>
        </Tooltip>
        <Tooltip title="Download logs">
          <IconButton onClick={handleDownload} size="small">
            <Download fontSize="small" />
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
          <Close fontSize="small" />
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
        }}>
        <Box
          sx={{
            position: "relative",
            whiteSpace: "pre-wrap",
            minHeight: 240,
            maxHeight: 540,
            overflowY: "auto",
          }}>
          {error ? (
            <Typography color="error" sx={{ p: 2 }}>
              {error}
            </Typography>
          ) : (
            filtered || "No logs available."
          )}
          {/* Only overlay spinner if logs are null (first load or after error) */}
          {loading && logs == null && (
            <Box
              sx={{
                position: "absolute",
                top: 0,
                left: 0,
                right: 0,
                bottom: 0,
                background: "rgba(25,25,29,0.85)",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                zIndex: 10,
                borderRadius: 2,
              }}>
              <CircularProgress />
            </Box>
          )}
        </Box>
      </DialogContent>

      <DialogActions>
        <Button onClick={onClose} color="primary" size="small">
          Close
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default LogsDialog;
