import React from "react";

import { openServiceLogsStream } from "@/api";
import LogDialog from "@/components/dialog/LogDialog";
import { useLogStream } from "@/hooks/useLogStream";

interface ServiceLogsDialogProps {
  open: boolean;
  onClose: () => void;
  serviceName: string;
}

const ServiceLogsDialog: React.FC<ServiceLogsDialogProps> = ({
  open,
  onClose,
  serviceName,
}) => {
  const { logs, isLoading, error, liveMode, setLiveMode, logsBoxRef, resetState } =
    useLogStream({
      open,
      createStream: (tail) => openServiceLogsStream(serviceName, tail),
    });

  return (
    <LogDialog
      open={open}
      onClose={onClose}
      title={`Logs: ${serviceName}`}
      logs={logs}
      isLoading={isLoading}
      error={error}
      liveMode={liveMode}
      onLiveModeChange={setLiveMode}
      logsBoxRef={logsBoxRef}
      onExited={resetState}
    />
  );
};

export default ServiceLogsDialog;
