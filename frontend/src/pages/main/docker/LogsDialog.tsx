import { Icon } from "@iconify/react";
import React, { useMemo, useState } from "react";

import { openDockerLogsStream } from "@/api";
import LogDialog from "@/components/dialog/LogDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSearchField from "@/components/ui/AppSearchField";
import AppTooltip from "@/components/ui/AppTooltip";
import { useLogStream } from "@/hooks/useLogStream";

interface LogsDialogProps {
  containerId: string;
  containerName?: string;
  onClose: () => void;
  open: boolean;
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
      error={error}
      extraActions={
        <>
          <AppTooltip title="Copy logs">
            <AppIconButton onClick={handleCopy} size="small">
              <Icon height={20} icon="mdi:content-copy" width={20} />
            </AppIconButton>
          </AppTooltip>
          <AppTooltip title="Download logs">
            <AppIconButton onClick={handleDownload} size="small">
              <Icon height={20} icon="mdi:download" width={20} />
            </AppIconButton>
          </AppTooltip>
        </>
      }
      isLoading={isLoading}
      liveMode={liveMode}
      logs={filtered}
      logsBoxRef={logsBoxRef}
      onClose={onClose}
      onExited={() => {
        resetState();
        setSearch("");
      }}
      onLiveModeChange={setLiveMode}
      open={open}
      titleContent={
        <>
          <Icon height={20} icon="mdi:magnify" width={20} />
          <AppSearchField
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search logs…"
            style={{ marginLeft: 8, flex: 1 }}
            value={search}
            variant="standard"
          />
        </>
      }
    />
  );
};

export default LogsDialog;
