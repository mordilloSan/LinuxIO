import { Icon } from "@iconify/react";
import { useLayoutEffect, useRef, useState } from "react";

import type { DockerContainerUpdateResult } from "@/api";
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

import "./compose-operation-dialog.css";

export interface DockerUpdateTarget {
  id: string;
  name: string;
}

export interface DockerUpdateEvent {
  message: string;
  phase: string;
}

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

export function DockerUpdateOperationDialog({
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
      slotProps={{ transition: { onExited: () => setShowDetails(false) } }}
      paperStyle={{
        maxHeight: "80vh",
      }}
    >
      <AppDialogTitle
        style={{
          alignItems: "center",
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
            <div>Updating {title}</div>
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
        flush
        style={{
          display: "flex",
          flexDirection: "column",
          maxHeight: 450,
          minHeight: 380,
          overflow: "hidden",
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
                className="compose-log__scroller"
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

      <AppDialogActions>
        <AppButton color="inherit" onClick={onClose}>
          {running ? "Run in background" : "Close"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
}
