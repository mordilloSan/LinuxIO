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
} from "@mui/material";
import React, { useState, useMemo } from "react";

interface LogsDialogProps {
  open: boolean;
  onClose: () => void;
  logs: string | null;
  loading?: boolean;
  error?: string | null;
  containerName?: string;
}

const LogsDialog: React.FC<LogsDialogProps> = ({
  open,
  onClose,
  logs,
  loading,
  error,
  containerName,
}) => {
  const [search, setSearch] = useState("");
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
        }}
      >
        {loading ? (
          <Box sx={{ display: "flex", justifyContent: "center", p: 4 }}>
            <CircularProgress />
          </Box>
        ) : error ? (
          <Typography color="error" sx={{ p: 2 }}>
            {error}
          </Typography>
        ) : (
          <Box
            sx={{
              whiteSpace: "pre-wrap",
              minHeight: 240,
              maxHeight: 540,
              overflowY: "auto",
            }}
          >
            {filtered || "No logs available."}
          </Box>
        )}
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
