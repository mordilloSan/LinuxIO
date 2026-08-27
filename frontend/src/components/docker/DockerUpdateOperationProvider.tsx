import { Icon } from "@iconify/react";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import {
  linuxio,
  type DockerContainerUpdateProgress,
  type DockerContainerUpdateRequest,
  type DockerContainerUpdateResult,
  type Stream,
  type TaskProgress,
  useStreamMux,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import {
  AppDialogActions,
  type AppDialogCloseEvent,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { TASK_TYPE_DOCKER_UPDATE } from "@/constants/backgroundTaskTypes";
import { useActiveTaskRecovery } from "@/hooks/backgroundTasks/useActiveTaskRecovery";
import { useScopedToast } from "@/hooks/useScopedToast";
import { createDockerContainerUpdateRequest } from "@/utils/dockerUpdates";

import "./compose-operation-dialog.css";

interface DockerUpdateTarget {
  id: string;
  name: string;
}

interface DockerUpdateEvent {
  message: string;
  phase: string;
}

interface DockerUpdateOperationContextValue {
  isUpdating: (containerId: string) => boolean;
  startUpdate: (containerId: string, containerName: string) => void;
  updating: boolean;
}

const DockerUpdateOperationContext =
  createContext<DockerUpdateOperationContextValue>({
    isUpdating: () => false,
    startUpdate: () => undefined,
    updating: false,
  });

export const useDockerUpdateOperation = () =>
  useContext(DockerUpdateOperationContext);

const phaseIcon = (phase: string) => {
  switch (phase) {
    case "rolling_back":
      return "mdi:backup-restore";
    case "checking":
      return "mdi:magnify";
    case "pulling":
      return "mdi:download";
    case "stopping":
      return "mdi:stop-circle-outline";
    case "creating":
      return "mdi:cube-outline";
    case "starting":
      return "mdi:play-circle-outline";
    case "verifying":
      return "mdi:shield-check-outline";
    case "cleanup":
      return "mdi:broom";
    case "current":
      return "mdi:check-circle-outline";
    default:
      return "mdi:cog-outline";
  }
};

const shortImageId = (value: string) =>
  value.replace(/^sha256:/, "").slice(0, 12);

function DockerUpdateOperationDialog({
  error,
  events,
  onClose,
  open,
  result,
  running,
  target,
}: {
  error: string | null;
  events: DockerUpdateEvent[];
  onClose: () => void;
  open: boolean;
  result: DockerContainerUpdateResult | null;
  running: boolean;
  target: DockerUpdateTarget | null;
}) {
  const [showDetails, setShowDetails] = useState(false);
  const outputBoxRef = useRef<HTMLDivElement>(null);
  const title = result?.containerName || target?.name || "Container";
  const outcomeColor = error
    ? "var(--app-palette-error-main)"
    : result
      ? "var(--app-palette-success-main)"
      : "var(--app-palette-primary-main)";

  const handleClose = (
    _event?: AppDialogCloseEvent,
    _reason?: "backdropClick" | "escapeKeyDown",
  ) => onClose();

  useLayoutEffect(() => {
    if (showDetails && outputBoxRef.current) {
      outputBoxRef.current.scrollTop = outputBoxRef.current.scrollHeight;
    }
    // `events` intentionally retriggers scrolling as operation output renders.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [events, showDetails]);

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
    >
      <AppDialogTitle
        style={{
          alignItems: "center",
          backgroundColor: "var(--app-header-background)",
          borderBottom: "1px solid var(--app-palette-divider)",
          display: "flex",
          justifyContent: "space-between",
        }}
      >
        <div style={{ alignItems: "center", display: "flex", gap: 10 }}>
          <Icon
            color={outcomeColor}
            height={24}
            icon={
              error
                ? "mdi:alert-circle"
                : result
                  ? "mdi:check-circle"
                  : "mdi:update"
            }
            width={24}
          />
          <div>
            <AppTypography style={{ fontWeight: 600 }} variant="subtitle1">
              Updating {title}
            </AppTypography>
            <AppTypography color="text.secondary" variant="caption">
              {running
                ? "The update continues if this dialog is closed"
                : result
                  ? result.updated
                    ? "Container update completed"
                    : "Container was already up to date"
                  : "Container update did not complete"}
            </AppTypography>
          </div>
        </div>
        <AppIconButton
          aria-label="Close container update dialog"
          onClick={onClose}
          size="small"
        >
          <Icon height={20} icon="mdi:close" width={20} />
        </AppIconButton>
      </AppDialogTitle>

      <AppDialogContent
        style={{
          display: "flex",
          flexDirection: "column",
          maxHeight: 450,
          minHeight: 380,
          overflow: "hidden",
          padding: 0,
        }}
      >
        <div
          aria-live="polite"
          className={`compose-progress-section ${showDetails ? "compose-progress-section--yielded" : ""}`.trim()}
        >
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: 8,
              padding: "var(--app-space-8)",
            }}
          >
            {events.length === 0 && running ? (
              <div style={{ alignItems: "center", display: "flex", gap: 10 }}>
                <AppCircularProgress size={18} />
                <AppTypography color="text.secondary" variant="body2">
                  Starting the update worker…
                </AppTypography>
              </div>
            ) : (
              events.map((event, index) => {
                const active = running && index === events.length - 1;
                return (
                  <div
                    key={`${event.phase}:${index}`}
                    style={{
                      alignItems: "center",
                      display: "grid",
                      gap: 10,
                      gridTemplateColumns: "24px minmax(0, 1fr)",
                      minHeight: 32,
                    }}
                  >
                    {active ? (
                      <AppCircularProgress size={18} />
                    ) : (
                      <Icon
                        color={
                          event.phase === "rolling_back"
                            ? "var(--app-palette-warning-main)"
                            : "var(--app-palette-text-secondary)"
                        }
                        height={20}
                        icon={phaseIcon(event.phase)}
                        width={20}
                      />
                    )}
                    <div>
                      <AppTypography
                        style={{ fontWeight: active ? 600 : 400 }}
                        variant="body2"
                      >
                        {event.message}
                      </AppTypography>
                      <AppTypography color="text.secondary" variant="caption">
                        {event.phase.replaceAll("_", " ")}
                      </AppTypography>
                    </div>
                  </div>
                );
              })
            )}

            {result && (
              <div
                style={{
                  borderTop: "1px solid var(--app-palette-divider)",
                  display: "grid",
                  gap: 6,
                  gridTemplateColumns: "max-content minmax(0, 1fr)",
                  paddingTop: "var(--app-space-8)",
                }}
              >
                <AppTypography color="text.secondary" variant="caption">
                  Image
                </AppTypography>
                <AppTypography variant="caption">{result.image}</AppTypography>
                {result.previousImageId && (
                  <>
                    <AppTypography color="text.secondary" variant="caption">
                      Previous
                    </AppTypography>
                    <AppTypography variant="caption">
                      {shortImageId(result.previousImageId)}
                    </AppTypography>
                  </>
                )}
                {result.newImageId && (
                  <>
                    <AppTypography color="text.secondary" variant="caption">
                      Current
                    </AppTypography>
                    <AppTypography variant="caption">
                      {shortImageId(result.newImageId)}
                    </AppTypography>
                  </>
                )}
              </div>
            )}

            {error && (
              <AppTypography color="error" variant="body2">
                {error}
              </AppTypography>
            )}
          </div>
        </div>

        {events.length > 0 && (
          <div
            className={`compose-log ${showDetails ? "compose-log--expanded" : ""}`.trim()}
          >
            <AppButton
              aria-controls="docker-update-details"
              aria-expanded={showDetails}
              className="compose-log__toggle"
              onClick={() => setShowDetails((previous) => !previous)}
              style={{
                appearance: "none",
                background: "none",
                border: 0,
                borderTop: "1px solid var(--app-palette-divider)",
                color: "inherit",
                cursor: "pointer",
                display: "flex",
                alignItems: "center",
                justifyContent: "flex-start",
                gap: "var(--app-space-2)",
                font: "inherit",
                padding: "var(--app-space-4) var(--app-space-8)",
                textAlign: "left",
                userSelect: "none",
                width: "100%",
              }}
              type="button"
            >
              <Icon
                className={`compose-log__chevron ${showDetails ? "compose-log__chevron--expanded" : ""}`.trim()}
                height={18}
                icon="mdi:chevron-right"
                width={18}
              />
              <AppTypography color="text.secondary" variant="body2">
                {showDetails ? "Hide details" : "Show details"}
              </AppTypography>
            </AppButton>
            <div className="compose-log__animator">
              <div
                aria-hidden={!showDetails}
                className="compose-log__scroller custom-scrollbar"
                id="docker-update-details"
                ref={outputBoxRef}
                style={{
                  backgroundColor: "var(--app-code-block-background)",
                  color: "var(--app-code-block-color)",
                }}
              >
                <div className="compose-log__lines">
                  {events.map((event, index) => (
                    <div key={`${event.phase}:detail:${index}`}>
                      [{event.phase.replaceAll("_", " ")}] {event.message}
                    </div>
                  ))}
                </div>
              </div>
            </div>
          </div>
        )}
      </AppDialogContent>

      <AppDialogActions
        style={{
          backgroundColor: "var(--app-header-background)",
          borderTop: "1px solid var(--app-palette-divider)",
        }}
      >
        <AppButton color="inherit" onClick={onClose}>
          {running ? "Run in background" : "Close"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
}

export function DockerUpdateOperationProvider({
  children,
}: {
  children: ReactNode;
}) {
  const toast = useScopedToast({ label: "Open Docker", to: "/docker" });
  const [target, setTarget] = useState<DockerUpdateTarget | null>(null);
  const [events, setEvents] = useState<DockerUpdateEvent[]>([]);
  const [result, setResult] = useState<DockerContainerUpdateResult | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const streamRef = useRef<Stream | null>(null);
  const { isOpen: muxIsOpen } = useStreamMux();

  const operation = linuxio.docker.update_container.useTaskStreamAction({
    closeMessage: "Container update stream closed unexpectedly",
    error: (streamError) => {
      if (streamError.name === "AbortError") return;
      streamRef.current = null;
      setError(streamError.message || "Container update failed");
    },
    onOpen: (stream) => {
      streamRef.current = stream;
    },
    onProgress: (progress: TaskProgress<DockerContainerUpdateProgress>) => {
      const detail = progress.detail;
      const phase = detail?.phase || progress.phase || "running";
      const message = detail?.message || progress.message;
      if (!message) return;
      setEvents((previous) => {
        const last = previous[previous.length - 1];
        if (last?.phase === phase && last.message === message) return previous;
        return [...previous, { message, phase }];
      });
    },
    openErrorMessage: "Failed to watch the container update",
    success: (updateResult) => {
      streamRef.current = null;
      setResult(updateResult);
      setError(null);
    },
  });
  const { isPending, mutate, watch } = operation;

  const begin = useCallback(
    (nextTarget: DockerUpdateTarget, request: DockerContainerUpdateRequest) => {
      setTarget(nextTarget);
      setEvents([]);
      setResult(null);
      setError(null);
      setOpen(true);
      mutate(request);
    },
    [mutate],
  );

  const startUpdate = useCallback(
    (containerId: string, containerName: string) => {
      if (isPending) {
        setOpen(true);
        return;
      }
      begin(
        { id: containerId, name: containerName },
        createDockerContainerUpdateRequest(containerId),
      );
    },
    [begin, isPending],
  );

  useActiveTaskRecovery({
    type: TASK_TYPE_DOCKER_UPDATE,
    scanKey: muxIsOpen ? "docker-update" : null,
    match: () => true,
    onRecover: (task) => {
      const containerId = task.metadata?.identity?.[0];
      if (!containerId || isPending) return;
      const recoveredTarget = {
        id: containerId,
        name: containerId.slice(0, 12),
      };
      setTarget(recoveredTarget);
      setEvents([]);
      setResult(null);
      setError(null);
      setOpen(true);
      watch(task, { containerId, runId: task.id });
    },
  });

  useEffect(
    () => () => {
      streamRef.current?.close();
    },
    [],
  );

  const handleClose = () => {
    setOpen(false);
    if (isPending) {
      toast.info("Container update is still running in the background");
    }
  };

  const value = useMemo<DockerUpdateOperationContextValue>(
    () => ({
      isUpdating: (containerId) => isPending && target?.id === containerId,
      startUpdate,
      updating: isPending,
    }),
    [isPending, startUpdate, target?.id],
  );

  return (
    <DockerUpdateOperationContext.Provider value={value}>
      {children}
      <DockerUpdateOperationDialog
        key={`${target?.id ?? "none"}:${open ? "open" : "closed"}`}
        error={error}
        events={events}
        onClose={handleClose}
        open={open}
        result={result}
        running={isPending}
        target={target}
      />
    </DockerUpdateOperationContext.Provider>
  );
}
