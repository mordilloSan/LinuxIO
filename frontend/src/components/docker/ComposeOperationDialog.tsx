import { Icon } from "@iconify/react";
import React, { useState, useEffect, useRef, useCallback } from "react";
import { toast } from "sonner";

import { linuxio, useStreamMux, openJobAttachStream, type Stream } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import {
  AppDialogContent,
  AppDialogTitle,
  type AppDialogCloseEvent,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { useStreamResult } from "@/hooks/useStreamResult";
import { useAppTheme } from "@/theme";

const JOB_TYPE_DOCKER_COMPOSE = "docker.compose";

interface ComposeOperationDialogProps {
  open: boolean;
  onClose: () => void;
  action: "up" | "down" | "stop" | "restart";
  projectName: string;
  composePath?: string;
}

interface ComposeMessage {
  type: "stdout" | "stderr" | "error" | "complete";
  message: string;
}

const ComposeOperationDialog: React.FC<ComposeOperationDialogProps> = ({
  open,
  onClose,
  action,
  projectName,
  composePath,
}) => {
  const theme = useAppTheme();
  const [output, setOutput] = useState<string[]>([]);
  const [isRunning, setIsRunning] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const outputBoxRef = useRef<HTMLDivElement>(null);
  const streamRef = useRef<Stream | null>(null);
  const abortControllerRef = useRef<AbortController | null>(null);
  const jobIdRef = useRef<string | null>(null);
  const closedByUserRef = useRef(false);
  const { isOpen: muxIsOpen } = useStreamMux();
  const { run: runStreamResult } = useStreamResult();

  const closeJobStream = useCallback(() => {
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
    abortControllerRef.current?.abort();
    abortControllerRef.current = null;
  }, []);

  const resetState = useCallback(() => {
    closeJobStream();
    setOutput([]);
    setIsRunning(true);
    setError(null);
    setSuccess(false);
    jobIdRef.current = null;
    closedByUserRef.current = false;
  }, [closeJobStream]);

  useEffect(() => {
    if (open && outputBoxRef.current) {
      outputBoxRef.current.scrollTop = outputBoxRef.current.scrollHeight;
    }
  }, [output, open]);

  useEffect(() => {
    if (!open) {
      closeJobStream();
    }
  }, [open, closeJobStream]);

  useEffect(() => {
    if (!open || !muxIsOpen) return;
    if (streamRef.current || jobIdRef.current) return;

    const abortController = new AbortController();
    abortControllerRef.current = abortController;
    closedByUserRef.current = false;
    let cancelled = false;

    void (async () => {
      try {
        const jobArgs = composePath
          ? [action, projectName, composePath]
          : [action, projectName];
        const job = await linuxio.jobs.start.call(
          JOB_TYPE_DOCKER_COMPOSE,
          ...jobArgs,
        );
        if (cancelled) {
          return;
        }
        jobIdRef.current = job.id;

        await runStreamResult<ComposeMessage, ComposeMessage>({
          open: () => openJobAttachStream(job.id),
          signal: abortController.signal,
          closeOnAbort: "none",
          openErrorMessage: "Failed to attach compose operation",
          closeMessage: "Compose operation stream closed unexpectedly",
          onOpen: (stream) => {
            streamRef.current = stream;
          },
          onProgress: (msg) => {
            switch (msg.type) {
              case "stdout":
              case "stderr":
                setOutput((prev) => [...prev, msg.message]);
                break;
              case "error":
                setError(msg.message);
                setIsRunning(false);
                toast.error(`Failed to ${action} stack: ${msg.message}`);
                break;
              case "complete":
                setSuccess(true);
                setIsRunning(false);
                setOutput((prev) => [...prev, "✓ " + msg.message]);
                break;
            }
          },
          onSuccess: (msg) => {
            if (msg?.type === "complete") {
              setSuccess(true);
            }
          },
        });
      } catch (streamError) {
        if (closedByUserRef.current) return;
        const message =
          streamError instanceof Error
            ? streamError.message
            : "Failed to start compose operation";
        setError(message);
        toast.error(`Failed to ${action} stack: ${message}`);
      } finally {
        if (!closedByUserRef.current) {
          setIsRunning(false);
        }
        streamRef.current = null;
        abortControllerRef.current = null;
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [open, action, projectName, composePath, muxIsOpen, runStreamResult]);

  const getActionLabel = () => {
    switch (action) {
      case "up":
        return "Starting";
      case "down":
        return "Removing";
      case "stop":
        return "Stopping";
      case "restart":
        return "Restarting";
      default:
        return "Processing";
    }
  };

  const handleClose = (
    _event?: AppDialogCloseEvent,
    reason?: "backdropClick" | "escapeKeyDown",
  ) => {
    if (
      isRunning &&
      (reason === "backdropClick" || reason === "escapeKeyDown")
    ) {
      return;
    }

    if (isRunning) {
      closedByUserRef.current = true;
      closeJobStream();
      toast.info("Compose operation is still running in the background");
    }
    onClose();
  };

  return (
    <GeneralDialog
      open={open}
      onClose={handleClose}
      maxWidth="md"
      fullWidth
      paperStyle={{
        backgroundColor: theme.palette.background.default,
        maxHeight: "80vh",
      }}
      slotProps={{
        transition: {
          onExited: resetState,
        },
      }}
    >
      <AppDialogTitle
        style={{
          backgroundColor: theme.header.background,
          borderBottom: `1px solid ${theme.palette.divider}`,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: theme.spacing(1),
          }}
        >
          {isRunning && <AppLinearProgress style={{ width: 100 }} />}
          {success && (
            <Icon
              icon="mdi:check-circle"
              width={24}
              height={24}
              color={theme.palette.success.main}
            />
          )}
          {error && (
            <Icon
              icon="mdi:alert-circle"
              width={24}
              height={24}
              color={theme.palette.error.main}
            />
          )}
          <AppTypography variant="h6">
            {getActionLabel()} Stack: {projectName}
          </AppTypography>
        </div>
        <AppIconButton onClick={() => handleClose()} size="small">
          <Icon icon="mdi:close" width={20} height={20} />
        </AppIconButton>
      </AppDialogTitle>

      <AppDialogContent style={{ padding: 0 }}>
        <div
          ref={outputBoxRef}
          style={{
            fontFamily: "monospace",
            fontSize: "0.875rem",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            backgroundColor: theme.codeBlock.background,
            color: theme.codeBlock.color,
            padding: theme.spacing(2),
            minHeight: "400px",
            maxHeight: "600px",
            overflowY: "auto",
          }}
        >
          {output.length === 0 && isRunning && (
            <AppTypography color="text.secondary">
              Starting operation...
            </AppTypography>
          )}
          {output.map((line, index) => (
            <div key={index}>{line}</div>
          ))}
          {error && (
            <AppTypography color="error" style={{ marginTop: 8 }}>
              Error: {error}
            </AppTypography>
          )}
        </div>
      </AppDialogContent>
    </GeneralDialog>
  );
};

export default ComposeOperationDialog;
