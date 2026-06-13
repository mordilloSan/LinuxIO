import { Icon } from "@iconify/react";
import React, { useCallback, useEffect, useRef, useState } from "react";

import {
  type ComposeMessage,
  type ComposeTask,
  mergeTask,
} from "./composeProgress";
import DockerComposeProgress from "./DockerComposeProgress";

import { linuxio, openJobAttachStream, type Stream, useStreamMux } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import {
  type AppDialogCloseEvent,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useStreamResult } from "@/hooks/useStreamResult";
import { useAppTheme } from "@/theme";

interface ComposeOperationDialogProps {
  action: "up" | "down" | "stop" | "restart";
  composePath?: string;
  onClose: () => void;
  open: boolean;
  projectName: string;
}

const ComposeOperationDialog: React.FC<ComposeOperationDialogProps> = ({
  open,
  onClose,
  action,
  projectName,
  composePath,
}) => {
  const theme = useAppTheme();
  const toast = useScopedToast({ href: "/docker", label: "Open Docker" });
  const [output, setOutput] = useState<string[]>([]);
  const [tasks, setTasks] = useState<Map<string, ComposeTask>>(new Map());
  const [showLog, setShowLog] = useState(false);
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
    setTasks(new Map());
    setShowLog(false);
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
        const job = await linuxio.docker.compose({
          action,
          projectName,
          composePath,
        });
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
              case "progress": {
                setTasks((prev) => mergeTask(prev, msg.progress));
                // Keep the raw log meaningful and bounded: record milestones
                // (status changes / completions), not every download tick.
                const { text, status } = msg.progress;
                if (
                  status === "Done" ||
                  (text !== "Downloading" && text !== "Extracting")
                ) {
                  setOutput((prev) => [...prev, msg.message]);
                }
                break;
              }
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
  }, [
    open,
    action,
    projectName,
    composePath,
    muxIsOpen,
    runStreamResult,
    toast,
  ]);

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

  const taskList = Array.from(tasks.values());
  const hasTasks = taskList.length > 0;

  return (
    <GeneralDialog
      fullWidth
      maxWidth="md"
      onClose={handleClose}
      open={open}
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
              color={theme.palette.success.main}
              height={24}
              icon="mdi:check-circle"
              width={24}
            />
          )}
          {error && (
            <Icon
              color={theme.palette.error.main}
              height={24}
              icon="mdi:alert-circle"
              width={24}
            />
          )}
          <AppTypography variant="h6">
            {getActionLabel()} Stack: {projectName}
          </AppTypography>
        </div>
        <AppIconButton onClick={() => handleClose()} size="small">
          <Icon height={20} icon="mdi:close" width={20} />
        </AppIconButton>
      </AppDialogTitle>

      <AppDialogContent style={{ padding: 0 }}>
        <div
          ref={outputBoxRef}
          style={{
            minHeight: "400px",
            maxHeight: "600px",
            overflowY: "auto",
          }}
        >
          {hasTasks ? (
            <DockerComposeProgress tasks={taskList} />
          ) : (
            isRunning &&
            output.length === 0 && (
              <AppTypography
                color="text.secondary"
                style={{ padding: theme.spacing(2) }}
              >
                Starting operation...
              </AppTypography>
            )
          )}

          {(hasTasks || output.length > 0) && (
            <>
              {hasTasks && (
                <div
                  onClick={() => setShowLog((prev) => !prev)}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: theme.spacing(0.5),
                    cursor: "pointer",
                    userSelect: "none",
                    padding: theme.spacing(1, 2),
                    borderTop: `1px solid ${theme.palette.divider}`,
                  }}
                >
                  <Icon
                    height={18}
                    icon={showLog ? "mdi:chevron-down" : "mdi:chevron-right"}
                    width={18}
                  />
                  <AppTypography
                    color="text.secondary"
                    style={{ fontSize: "0.8rem" }}
                  >
                    {showLog ? "Hide raw log" : "Show raw log"}
                  </AppTypography>
                </div>
              )}

              {(showLog || !hasTasks) && (
                <div
                  style={{
                    fontFamily: "monospace",
                    fontSize: "0.8125rem",
                    whiteSpace: "pre-wrap",
                    wordBreak: "break-word",
                    backgroundColor: theme.codeBlock.background,
                    color: theme.codeBlock.color,
                    padding: theme.spacing(2),
                  }}
                >
                  {output.map((line, index) => (
                    <div key={index}>{line}</div>
                  ))}
                </div>
              )}
            </>
          )}

          {error && (
            <AppTypography
              color="error"
              style={{ padding: theme.spacing(2), display: "block" }}
            >
              Error: {error}
            </AppTypography>
          )}
        </div>
      </AppDialogContent>
    </GeneralDialog>
  );
};

export default ComposeOperationDialog;
