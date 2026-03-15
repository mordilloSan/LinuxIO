import { Icon } from "@iconify/react";
import { IconButton, TextField } from "@mui/material";
import React, { useState, useMemo } from "react";

import { openDockerLogsStream } from "@/api";
import LogDialog from "@/components/dialog/LogDialog";
import AppTooltip from "@/components/ui/AppTooltip";
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
          <Icon icon="mdi:magnify" width={20} height={20} />
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
          <AppTooltip title="Copy logs">
            <IconButton onClick={handleCopy} size="small">
              <Icon icon="mdi:content-copy" width={20} height={20} />
            </IconButton>
          </AppTooltip>
          <AppTooltip title="Download logs">
            <IconButton onClick={handleDownload} size="small">
              <Icon icon="mdi:download" width={20} height={20} />
            </IconButton>
          </AppTooltip>
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
