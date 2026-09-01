import { Icon } from "@iconify/react";
import { useLayoutEffect, useRef, useState } from "react";

import "./capability-install-dialog.css";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";

export type CapabilityInstallOutputStream = "status" | "stderr" | "stdout";

export interface CapabilityInstallOutputLine {
  id: number;
  stream: CapabilityInstallOutputStream;
  text: string;
}

interface CapabilityInstallDialogProps {
  capabilityLabel: string;
  error: string | null;
  message: string;
  onClose: () => void;
  open: boolean;
  output: CapabilityInstallOutputLine[];
  outputHistoryIncomplete?: boolean;
  percentage: number | null;
  running: boolean;
  stage: string;
  success: boolean;
  warning: string | null;
}

const formatStage = (stage: string) =>
  stage ? stage.replaceAll("_", " ") : "Preparing";

const CapabilityInstallDialog = ({
  capabilityLabel,
  error,
  message,
  onClose,
  open,
  output,
  outputHistoryIncomplete = false,
  percentage,
  running,
  stage,
  success,
  warning,
}: CapabilityInstallDialogProps) => {
  const [showOutput, setShowOutput] = useState(false);
  const outputBoxRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    if (showOutput && outputBoxRef.current) {
      outputBoxRef.current.scrollTop = outputBoxRef.current.scrollHeight;
    }
    // New output intentionally retriggers scrolling even though only the DOM
    // height, not the output value itself, is read inside the Effect.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [output, showOutput]);

  const progressVariant = percentage === null ? "indeterminate" : "determinate";
  const outcomeColor = error
    ? "var(--app-palette-error-main)"
    : warning
      ? "var(--app-palette-warning-main)"
      : success
        ? "var(--app-palette-success-main)"
        : "var(--app-palette-primary-main)";

  return (
    <GeneralDialog
      aria-busy={running}
      fullWidth
      maxWidth="md"
      onClose={onClose}
      open={open}
      paperStyle={{
        backgroundColor: "var(--app-palette-background-default)",
        maxHeight: "80vh",
      }}
      slotProps={{
        transition: {
          onEntered: () => {
            if (showOutput && outputBoxRef.current) {
              outputBoxRef.current.scrollTop =
                outputBoxRef.current.scrollHeight;
            }
          },
        },
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
        <div className="capability-install-dialog__title">
          <Icon
            color={outcomeColor}
            height={24}
            icon={
              error || warning
                ? "mdi:alert-circle"
                : success
                  ? "mdi:check-circle"
                  : "mdi:download"
            }
            width={24}
          />
          <div>
            <AppTypography style={{ fontWeight: 600 }} variant="subtitle1">
              Installing {capabilityLabel}
            </AppTypography>
            <AppTypography color="text.secondary" variant="caption">
              {running
                ? "Installation continues if this dialog is closed"
                : warning
                  ? "Capability installation completed with a warning"
                  : success
                    ? "Capability installation completed"
                    : "Capability installation did not complete"}
            </AppTypography>
          </div>
        </div>
        <AppIconButton
          aria-label="Close capability installation dialog"
          onClick={onClose}
          size="small"
        >
          <Icon height={20} icon="mdi:close" width={20} />
        </AppIconButton>
      </AppDialogTitle>

      <AppDialogContent className="capability-install-dialog__content">
        <div
          aria-live="polite"
          className={`capability-install-dialog__progress custom-scrollbar ${
            showOutput ? "capability-install-dialog__progress--yielded" : ""
          }`.trim()}
        >
          <div className="capability-install-dialog__progress-body">
            {running ? (
              <AppLinearProgress
                aria-label="Capability installation progress"
                value={percentage ?? 0}
                variant={progressVariant}
              />
            ) : null}
            <div className="capability-install-dialog__phase">
              <AppTypography style={{ fontWeight: 600 }} variant="body2">
                {success
                  ? "Completed"
                  : warning
                    ? "Completed with warning"
                    : error
                      ? "Failed"
                      : formatStage(stage)}
              </AppTypography>
              {percentage !== null && running ? (
                <AppTypography color="text.secondary" variant="caption">
                  {Math.round(percentage)}%
                </AppTypography>
              ) : null}
            </div>
            <AppTypography
              color={
                error
                  ? "error"
                  : warning
                    ? "warning"
                    : success
                      ? "success"
                      : "text.secondary"
              }
              variant="body2"
            >
              {error ??
                warning ??
                (success ? `✓ ${capabilityLabel} installed` : message)}
            </AppTypography>
            {outputHistoryIncomplete ? (
              <AppTypography color="warning" variant="caption">
                This view contains retained output only. Earlier records may be
                unavailable.
              </AppTypography>
            ) : null}
          </div>
        </div>

        <div
          className={`capability-install-dialog__output ${
            showOutput ? "capability-install-dialog__output--expanded" : ""
          }`.trim()}
        >
          <AppButton
            aria-controls="capability-install-output"
            aria-expanded={showOutput}
            className="capability-install-dialog__output-toggle"
            onClick={() => setShowOutput((previous) => !previous)}
            type="button"
          >
            <Icon
              className={`capability-install-dialog__chevron ${
                showOutput ? "capability-install-dialog__chevron--expanded" : ""
              }`.trim()}
              height={18}
              icon="mdi:chevron-right"
              width={18}
            />
            <AppTypography color="text.secondary" variant="caption">
              {showOutput
                ? "Hide installation output"
                : "Show installation output"}
            </AppTypography>
          </AppButton>
          <div className="capability-install-dialog__output-animator">
            <div
              aria-hidden={!showOutput}
              className="capability-install-dialog__output-scroller custom-scrollbar"
              id="capability-install-output"
              ref={outputBoxRef}
              style={{
                backgroundColor: "var(--app-code-block-background)",
                color: "var(--app-code-block-color)",
              }}
            >
              <div className="capability-install-dialog__output-lines">
                {outputHistoryIncomplete ? (
                  <div className="capability-install-dialog__output-note">
                    [status] Earlier output may not have been retained.
                  </div>
                ) : null}
                {output.map((line) => (
                  <div
                    className={`capability-install-dialog__output-line capability-install-dialog__output-line--${line.stream}`}
                    key={line.id}
                  >
                    {line.stream === "status"
                      ? `[status] ${line.text}`
                      : line.text}
                  </div>
                ))}
                {output.length === 0 && !outputHistoryIncomplete ? (
                  <div className="capability-install-dialog__output-note">
                    Waiting for installer output…
                  </div>
                ) : null}
              </div>
            </div>
          </div>
        </div>
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
};

export default CapabilityInstallDialog;
