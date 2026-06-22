import { Icon } from "@iconify/react";
import React, { useMemo, useState } from "react";

import { openDockerLogsStream } from "@/api";
import LogDialog from "@/components/dialog/LogDialog";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppSearchField from "@/components/ui/AppSearchField";
import AppSelect from "@/components/ui/AppSelect";
import { useLogStream } from "@/hooks/useLogStream";

const LOG_LINE_OPTIONS = [
  { value: "50", label: "50" },
  { value: "100", label: "100" },
  { value: "200", label: "200" },
  { value: "500", label: "500" },
  { value: "1000", label: "1000" },
  { value: "2000", label: "2000" },
  { value: "5000", label: "5000" },
];

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
  const [tailLines, setTailLines] = useState("100");

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
    initialTail: tailLines,
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

  const handleTailLinesChange = (value: string) => {
    if (value === tailLines) return;
    resetState();
    setTailLines(value);
  };

  return (
    <LogDialog
      error={error}
      extraActions={
        <>
          <AppActionIconButton
            icon="mdi:content-copy"
            iconSize={20}
            label="Copy logs"
            onClick={handleCopy}
          />
          <AppActionIconButton
            icon="mdi:download"
            iconSize={20}
            label="Download logs"
            onClick={handleDownload}
          />
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
        setTailLines("100");
      }}
      onLiveModeChange={setLiveMode}
      open={open}
      titleContent={
        <>
          <AppSelect
            label="Lines"
            onChange={(event) => handleTailLinesChange(event.target.value)}
            size="small"
            style={{ width: 112, flexShrink: 0 }}
            value={tailLines}
          >
            {LOG_LINE_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </AppSelect>
          <Icon
            height={20}
            icon="mdi:magnify"
            style={{ marginLeft: 8, flexShrink: 0 }}
            width={20}
          />
          <AppSearchField
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search logs…"
            style={{ marginLeft: 4, flex: 1, minWidth: 120 }}
            value={search}
            variant="standard"
          />
        </>
      }
    />
  );
};

export default LogsDialog;
