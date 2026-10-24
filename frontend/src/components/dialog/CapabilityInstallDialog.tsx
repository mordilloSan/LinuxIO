import { Icon } from "@iconify/react";
import { useRef, useState } from "react";

import "./capability-install-dialog.css";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import InstallOutputPanel, {
  type InstallOutputLine,
} from "@/components/terminal/InstallOutputPanel";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";

interface CapabilityInstallDialogProps {
  capabilityLabel: string;
  error: string | null;
  message: string;
  onClose: () => void;
  open: boolean;
  output: InstallOutputLine[];
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
            <div>Installing {capabilityLabel}</div>
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

      <AppDialogContent className="capability-install-dialog__content" flush>
        <div
          aria-live="polite"
          className={`capability-install-dialog__progress ${
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

        <InstallOutputPanel
          className="capability-install-dialog__output"
          expanded={showOutput}
          id="capability-install-output"
          label="installation output"
          onToggle={() => setShowOutput((previous) => !previous)}
          output={output}
          outputHistoryIncomplete={outputHistoryIncomplete}
          scrollerRef={outputBoxRef}
        />
      </AppDialogContent>

      <AppDialogActions>
        <AppButton color="inherit" onClick={onClose}>
          {running ? "Run in background" : "Close"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default CapabilityInstallDialog;
