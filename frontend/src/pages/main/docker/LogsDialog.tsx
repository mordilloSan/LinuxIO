import DownloadIcon from "@mui/icons-material/Download";
import FileCopyIcon from "@mui/icons-material/FileCopy";
import SearchIcon from "@mui/icons-material/Search";
import { IconButton, TextField, Tooltip } from "@mui/material";
import React, { useState, useMemo } from "react";

import { openDockerLogsStream } from "@/api";
import LogDialog from "@/components/dialog/LogDialog";
import { useLogStream } from "@/hooks/useLogStream";

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

  const {
    logs,
    isLoading,
    error,
    liveMode,
    setLiveMode,
    logsBoxRef,
    resetState,
  } = useLogStream({
    open,
    createStream: (tail) => openDockerLogsStream(containerId, tail),
    initialTail: "100",
  });

  const filtered = useMemo(() => {
    if (!search || !logs) return logs;
    return logs
      .split("\n")
      .filter((line) => line.toLowerCase().includes(search.toLowerCase()))
      .join("\n");
  }, [logs, search]);

  const handleCopy = () => {
    if (filtered) navigator.clipboard.writeText(filtered);
  };

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
    <LogDialog
      open={open}
      onClose={onClose}
      titleContent={
        <>
          <SearchIcon fontSize="small" />
          <TextField
            variant="standard"
            placeholder="Search logs…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            size="small"
            sx={{ ml: 1, flex: 1 }}
          />
        </>
      }
      extraActions={
        <>
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
        </>
      }
      logs={filtered}
      isLoading={isLoading}
      error={error}
      liveMode={liveMode}
      onLiveModeChange={setLiveMode}
      logsBoxRef={logsBoxRef}
      onExited={() => {
        resetState();
        setSearch("");
      }}
    />
  );
};

export default LogsDialog;
