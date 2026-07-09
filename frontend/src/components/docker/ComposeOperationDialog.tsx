import { Icon } from "@iconify/react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";

import {
  type ComposeMessage,
  type ComposeTask,
  mergeTask,
} from "./composeProgress";
import DockerComposeProgress from "./DockerComposeProgress";

import { linuxio, useStreamMux } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import {
  type AppDialogCloseEvent,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { JOB_TYPE_DOCKER_COMPOSE } from "@/constants/backgroundJobTypes";
import { useActiveJobRecovery } from "@/hooks/backgroundJobs/useActiveJobRecovery";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";

interface ComposeOperationDialogProps {
  action: "up" | "down" | "stop" | "restart";
  composePath?: string;
  onClose: () => void;
  open: boolean;
  projectName: string;
}

const ComposeOperationDialog = ({
  open,
  onClose,
  action,
  projectName,
  composePath,
}: ComposeOperationDialogProps) => {
  const theme = useAppTheme();
  const toast = useScopedToast({ href: "/docker", label: "Open Docker" });
  const [output, setOutput] = useState<string[]>([]);
  const [tasks, setTasks] = useState<Map<string, ComposeTask>>(new Map());
  const [showLog, setShowLog] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const outputBoxRef = useRef<HTMLDivElement>(null);
  // Started-guard: one run per dialog open (reset on dialog exit). The abort
  // controller is the run's detach handle — aborting closes the attach
  // stream (closeOnAbort: "close") while the job keeps running server-side.
  const startedRef = useRef(false);
  const abortRef = useRef<AbortController | null>(null);
  const { isOpen: muxIsOpen } = useStreamMux();

  const isRunning = !success && !error;

  const resetState = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    startedRef.current = false;
    setOutput([]);
    setTasks(new Map());
    setShowLog(false);
    setError(null);
    setSuccess(false);
  }, []);

  // Pin output to the bottom before paint to avoid a visible scroll jump.
  useLayoutEffect(() => {
    if (open && outputBoxRef.current) {
      outputBoxRef.current.scrollTop = outputBoxRef.current.scrollHeight;
    }
  }, [output, open]);

  // Detach from the stream when the dialog closes or unmounts; the compose
  // job itself keeps running.
  useEffect(() => {
    if (!open) {
      abortRef.current?.abort();
    }
  }, [open]);
  useEffect(() => () => abortRef.current?.abort(), []);

  const composeOperation = linuxio.docker.compose.useJobStreamAction<
    ComposeMessage,
    ComposeMessage
  >({
    closeMessage: "Compose operation stream closed unexpectedly",
    closeOnAbort: "close",
    error: (streamError) => {
      if (streamError.name === "AbortError") return;
      const message =
        streamError.message || "Failed to start compose operation";
      setError(message);
      toast.error(`Failed to ${action} stack: ${message}`);
    },
    invalidates: [
      linuxio.docker.list_compose_projects.queryKey(),
      linuxio.docker.list_containers.queryKey(),
    ],
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
          // In-dialog display only; the terminal error callback owns the
          // toast, so a failed run toasts once.
          setError(msg.message);
          break;
        case "complete":
          setSuccess(true);
          setOutput((prev) => [...prev, "✓ " + msg.message]);
          break;
      }
    },
    openErrorMessage: "Failed to attach compose operation",
    signal: () => abortRef.current?.signal,
    success: (msg) => {
      if (msg?.type === "complete") {
        setSuccess(true);
      }
    },
  });

  // One recovery scan per dialog open decides between the two start paths:
  // adopt a still-running operation for this project (dialog was closed and
  // reopened mid-run) or start a fresh one.
  const beginRun = (run: () => void) => {
    if (startedRef.current) return;
    startedRef.current = true;
    abortRef.current = new AbortController();
    run();
  };
  useActiveJobRecovery({
    type: JOB_TYPE_DOCKER_COMPOSE,
    scanKey: open && muxIsOpen ? `${action}:${projectName}` : null,
    match: (job) => {
      const request = job.request as
        | { action?: string; projectName?: string }
        | undefined;
      return request?.action === action && request?.projectName === projectName;
    },
    onRecover: (job) =>
      beginRun(() =>
        composeOperation.attach(job, { action, projectName, composePath }),
      ),
    onMiss: () =>
      beginRun(() =>
        composeOperation.mutate({ action, projectName, composePath }),
      ),
  });

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

    if (isRunning && startedRef.current) {
      abortRef.current?.abort();
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
