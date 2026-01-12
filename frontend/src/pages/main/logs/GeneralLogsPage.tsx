import DownloadIcon from "@mui/icons-material/Download";
import FileCopyIcon from "@mui/icons-material/FileCopy";
import SearchIcon from "@mui/icons-material/Search";
import {
  Alert,
  Box,
  FormControlLabel,
  IconButton,
  InputAdornment,
  Switch,
  TextField,
  Tooltip,
  Typography,
} from "@mui/material";
import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { useStreamMux, generalLogsPayload, decodeString } from "@/api/linuxio";
import type { Stream } from "@/api/linuxio";
import ComponentLoader from "@/components/loaders/ComponentLoader";

const DEFAULT_TAIL = "200";

const GeneralLogsPage: React.FC = () => {
  const [liveMode, setLiveMode] = useState(true);
  const [logs, setLogs] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const logsBoxRef = useRef<HTMLDivElement>(null);
  const streamRef = useRef<Stream | null>(null);
  const hasReceivedData = useRef(false);
  const hasOpenedOnce = useRef(false);

  const { isOpen: muxIsOpen, openStream } = useStreamMux();

  // Scroll to bottom when logs change
  useEffect(() => {
    if (liveMode && logsBoxRef.current) {
      logsBoxRef.current.scrollTop = logsBoxRef.current.scrollHeight;
    }
  }, [logs, liveMode]);

  // Close stream helper
  const closeStream = useCallback(() => {
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
  }, []);

  const openLogsStream = useCallback(
    (lines: string) => {
      if (!muxIsOpen) return false;

      hasReceivedData.current = false;

      const payload = generalLogsPayload(lines);
      const stream = openStream("general-logs", payload);

      if (!stream) {
        queueMicrotask(() => {
          setError("Failed to connect to log stream");
          setIsLoading(false);
        });
        return false;
      }

      streamRef.current = stream;

      stream.onData = (data: Uint8Array) => {
        const text = decodeString(data);
        if (!hasReceivedData.current) {
          hasReceivedData.current = true;
          setIsLoading(false);
          setError(null);
        }
        setLogs((prev) => [...prev, text.trimEnd()]);
      };

      stream.onClose = () => {
        streamRef.current = null;
        if (!hasReceivedData.current) {
          setIsLoading(false);
        }
      };
      return true;
    },
    [muxIsOpen, openStream],
  );

  // Open stream on mount
  useEffect(() => {
    if (!muxIsOpen || !liveMode || streamRef.current) {
      return;
    }
    const tail = hasOpenedOnce.current ? "0" : DEFAULT_TAIL;
    if (openLogsStream(tail)) {
      hasOpenedOnce.current = true;
    }
  }, [muxIsOpen, liveMode, openLogsStream]);

  const handleLiveModeChange = (
    _: React.ChangeEvent<HTMLInputElement>,
    checked: boolean,
  ) => {
    setLiveMode(checked);
    if (!checked) {
      closeStream();
      return;
    }
    setError(null);
  };

  // Cleanup stream when page unmounts
  useEffect(() => {
    return () => {
      closeStream();
    };
  }, [closeStream]);

  const filteredLogs = useMemo(() => {
    const trimmed = search.trim();
    if (!trimmed) return logs;
    const needle = trimmed.toLowerCase();
    return logs.filter((line) => line.toLowerCase().includes(needle));
  }, [logs, search]);

  const handleCopy = () => {
    if (filteredLogs.length === 0) return;
    navigator.clipboard.writeText(filteredLogs.join("\n"));
  };

  const handleDownload = () => {
    if (filteredLogs.length === 0) return;
    const blob = new Blob([filteredLogs.join("\n")], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "general-logs.txt";
    link.click();
    URL.revokeObjectURL(url);
  };

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 2,
          flexWrap: "wrap",
        }}
      >
        <Typography variant="h5">General Logs</Typography>
        <TextField
          size="small"
          placeholder="Search logs..."
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          sx={{ minWidth: 220, flex: "1 1 260px" }}
          slotProps={{
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <SearchIcon fontSize="small" />
                </InputAdornment>
              ),
            },
          }}
        />
        <Tooltip title="Copy logs">
          <span>
            <IconButton
              onClick={handleCopy}
              size="small"
              disabled={filteredLogs.length === 0}
            >
              <FileCopyIcon fontSize="small" />
            </IconButton>
          </span>
        </Tooltip>
        <Tooltip title="Download logs">
          <span>
            <IconButton
              onClick={handleDownload}
              size="small"
              disabled={filteredLogs.length === 0}
            >
              <DownloadIcon fontSize="small" />
            </IconButton>
          </span>
        </Tooltip>
        <Tooltip title={liveMode ? "Live streaming ON" : "Live streaming OFF"}>
          <FormControlLabel
            control={
              <Switch
                checked={liveMode}
                onChange={handleLiveModeChange}
                color="primary"
                size="small"
              />
            }
            label="Live"
            sx={{ ml: 1 }}
          />
        </Tooltip>
      </Box>

      {isLoading && <ComponentLoader />}

      {error && <Alert severity="error">{error}</Alert>}

      {!isLoading && !error && (
        <Box
          ref={logsBoxRef}
          className="custom-scrollbar"
          sx={{
            flex: 1,
            minHeight: 420,
            bgcolor: "#1e1e1e",
            color: "#d4d4d4",
            p: 2,
            borderRadius: 1,
            overflow: "auto",
            fontFamily: "Fira Mono, monospace",
            fontSize: "0.9rem",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
          }}
        >
          {filteredLogs.length > 0 ? (
            filteredLogs.map((line, idx) => <div key={idx}>{line}</div>)
          ) : (
            <Typography color="text.secondary">
              {logs.length === 0 ? "No logs available." : "No matching logs."}
            </Typography>
          )}
        </Box>
      )}
    </Box>
  );
};

export default GeneralLogsPage;
