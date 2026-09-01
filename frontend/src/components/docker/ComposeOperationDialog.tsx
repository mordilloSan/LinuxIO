import { Icon } from "@iconify/react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";

import "./compose-operation-dialog.css";

import { linuxio, useStreamMux } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import {
  type AppDialogCloseEvent,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { TASK_TYPE_DOCKER_COMPOSE } from "@/constants/backgroundTaskTypes";
import { useActiveTaskRecovery } from "@/hooks/backgroundTasks/useActiveTaskRecovery";
import { useScopedToast } from "@/hooks/useScopedToast";

import { type ComposeTask, mergeTask } from "./composeProgress";
import DockerComposeProgress from "./DockerComposeProgress";

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
  const toast = useScopedToast({ label: "Open Docker", to: "/docker" });
  const [output, setOutput] = useState<string[]>([]);
  const [tasks, setTasks] = useState<Map<string, ComposeTask>>(new Map());
  const [showLog, setShowLog] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const outputBoxRef = useRef<HTMLDivElement>(null);
  // Started-guard: one run per dialog open (reset on dialog exit). The abort
  // controller is the run's detach handle — aborting closes the watch
  // stream (closeOnAbort: "close") while the task keeps running server-side.
  // It is dropped as soon as the task reports a terminal state: from then on
  // the stream is only waiting to deliver its result frame, and that frame is
  // what resolves the mutation and applies the route's invalidations. Cutting
  // it short rejects the mutation with an AbortError instead, so nothing would
  // refresh the Docker caches.
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
  // Re-pin on showLog so expanding the panel lands on the newest line.
  useLayoutEffect(() => {
    if (open && outputBoxRef.current) {
      outputBoxRef.current.scrollTop = outputBoxRef.current.scrollHeight;
    }
    // Output and disclosure changes intentionally retrigger the DOM scroll.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [output, open, showLog]);

  // Detach from the stream when the dialog closes or unmounts; the compose
  // task itself keeps running.
  useEffect(() => {
    if (!open) {
      abortRef.current?.abort();
    }
  }, [open]);
  useEffect(() => () => abortRef.current?.abort(), []);

  const composeOperation = linuxio.docker.compose.useTaskStreamAction({
    closeMessage: "Compose operation stream closed unexpectedly",
    closeOnAbort: "close",
    error: (streamError) => {
      if (streamError.name === "AbortError") return;
      const message =
        streamError.message || "Failed to start compose operation";
      // The toast still fires for a run the user walked away from; only the
      // in-dialog state is dropped once this run is no longer the current one.
      if (startedRef.current) {
        setError(message);
      }
      toast.error(`Failed to ${action} stack: ${message}`);
    },
    onProgress: (taskProgress) => {
      // A finished run stays attached until its result frame lands, so frames
      // can still arrive after the dialog was closed and reset. Writing them
      // back would leave the next open showing the previous run's state.
      if (!startedRef.current) return;
      const msg = taskProgress.detail;
      if (!msg) return;
      switch (msg.type) {
        case "progress": {
          const progress = msg.progress;
          if (!progress) break;
          setTasks((prev) => mergeTask(prev, progress));
          // Keep the raw log meaningful and bounded: record milestones
          // (status changes / completions), not every download tick.
          const { text, status } = progress;
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
      }
    },
    openErrorMessage: "Failed to watch compose operation",
    signal: () => abortRef.current?.signal,
    success: (msg) => {
      if (msg?.type === "complete" && startedRef.current) {
        abortRef.current = null;
        setSuccess(true);
        setOutput((prev) => [...prev, "✓ " + msg.message]);
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
  useActiveTaskRecovery({
    type: TASK_TYPE_DOCKER_COMPOSE,
    scanKey: open && muxIsOpen ? `${action}:${projectName}` : null,
    match: (task) => {
      const metadata = task.metadata;
      return (
        metadata?.action === action && metadata?.projectName === projectName
      );
    },
    onRecover: (task) =>
      beginRun(() =>
        composeOperation.watch(task, { action, projectName, composePath }),
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
  // Without a task tree the log is the only content, so it is always shown.
  const logVisible = showLog || !hasTasks;

  return (
    <GeneralDialog
      fullWidth
      maxWidth="md"
      onClose={handleClose}
      open={open}
      paperStyle={{
        backgroundColor: "var(--app-palette-background-default)",
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
          backgroundColor: "var(--app-header-background)",
          borderBottom: "1px solid var(--app-palette-divider)",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: "var(--app-space-4)",
          }}
        >
          <AppTypography variant="h6">
            {getActionLabel()} Stack: {projectName}
          </AppTypography>
          {/* Outcome marker: spinner while the operation runs, then the state
              it settled in. */}
          {isRunning ? (
            <AppCircularProgress size={20} />
          ) : error ? (
            <Icon
              color="var(--app-palette-error-main)"
              height={24}
              icon="mdi:alert-circle"
              width={24}
            />
          ) : (
            <Icon
              color="var(--app-palette-success-main)"
              height={24}
              icon="mdi:check-circle"
              width={24}
            />
          )}
        </div>
        <AppIconButton
          aria-label="Close compose operation dialog"
          onClick={() => handleClose()}
          size="small"
        >
          <Icon height={20} icon="mdi:close" width={20} />
        </AppIconButton>
      </AppDialogTitle>

      {/* Stable frame; what changes with the content is how the two sections
          divide it. */}
      <AppDialogContent
        style={{
          padding: 0,
          display: "flex",
          flexDirection: "column",
          minHeight: "380px",
          maxHeight: "450px",
          overflow: "hidden",
        }}
      >
        {/* Section 1 — progress. Holds the free space while the log is closed
            and yields it as the log opens; scrolls on its own once the task
            tree outgrows what it is left with. */}
        <div
          className={`compose-progress-section ${
            logVisible ? "compose-progress-section--yielded" : ""
          }`.trim()}
        >
          {hasTasks ? (
            <DockerComposeProgress tasks={taskList} />
          ) : (
            isRunning &&
            output.length === 0 && (
              <AppTypography
                color="text.secondary"
                style={{ padding: "var(--app-space-8)" }}
              >
                Starting operation...
              </AppTypography>
            )
          )}

          {error && (
            <AppTypography
              color="error"
              style={{ padding: "var(--app-space-8)", display: "block" }}
            >
              Error: {error}
            </AppTypography>
          )}
        </div>

        {/* Section 2 — raw log. Grows into the space the task tree gives up,
            and scrolls once it has all of it. */}
        {(hasTasks || output.length > 0) && (
          <div
            className={[
              "compose-log",
              logVisible && "compose-log--expanded",
              // No task tree means no toggle bar to offset the panel from.
              !hasTasks && "compose-log--headless",
            ]
              .filter(Boolean)
              .join(" ")}
          >
            {hasTasks && (
              <AppButton
                aria-controls="compose-raw-log"
                aria-expanded={showLog}
                className="compose-log__toggle"
                onClick={() => setShowLog((prev) => !prev)}
                style={{
                  appearance: "none",
                  background: "none",
                  border: 0,
                  color: "inherit",
                  cursor: "pointer",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "flex-start",
                  gap: "var(--app-space-2)",
                  font: "inherit",
                  userSelect: "none",
                  padding: "var(--app-space-4) var(--app-space-8)",
                  borderTop: "1px solid var(--app-palette-divider)",
                  textAlign: "left",
                  width: "100%",
                }}
                type="button"
              >
                {/* One icon rotated rather than two swapped, so the marker
                    animates in step with the panel. */}
                <Icon
                  className={`compose-log__chevron ${
                    showLog ? "compose-log__chevron--expanded" : ""
                  }`.trim()}
                  height={18}
                  icon="mdi:chevron-right"
                  width={18}
                />
                <AppTypography color="text.secondary" variant="body2">
                  {showLog ? "Hide raw log" : "Show raw log"}
                </AppTypography>
              </AppButton>
            )}

            <div className="compose-log__animator">
              <div
                aria-hidden={!logVisible}
                className="compose-log__scroller"
                id="compose-raw-log"
                ref={outputBoxRef}
                style={{
                  backgroundColor: "var(--app-code-block-background)",
                  color: "var(--app-code-block-color)",
                }}
              >
                <div className="compose-log__lines">
                  {output.map((line, index) => (
                    <div key={index}>{line}</div>
                  ))}
                </div>
              </div>
            </div>
          </div>
        )}
      </AppDialogContent>
    </GeneralDialog>
  );
};

export default ComposeOperationDialog;
