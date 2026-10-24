import { Icon } from "@iconify/react";
import { useRef, useState } from "react";

import "./update-actions.css";

import InstallOutputPanel, {
  type InstallOutputLine,
} from "@/components/terminal/InstallOutputPanel";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";

interface UpdateActionsProps {
  canCancel?: boolean;
  currentPackage: string | null;
  error?: string | null;
  eventLog?: string[];
  isUpdating: boolean;
  onCancel?: () => void;
  onClearError?: () => void;
  output?: InstallOutputLine[];
  outputHistoryIncomplete?: boolean;
  progress: number;
  status?: string | null;
}

const UpdateActions = ({
  canCancel = false,
  isUpdating,
  currentPackage,
  progress,
  status,
  eventLog,
  output = [],
  outputHistoryIncomplete = false,
  error,
  onClearError,
  onCancel,
}: UpdateActionsProps) => {
  const [showOutput, setShowOutput] = useState(false);
  const outputRef = useRef<HTMLDivElement>(null);

  // Every section below is conditional, so an idle section rendered an empty
  // box whose bottom margin still pushed the package list down — the tab strip
  // owns that gap now (tab-container.css). Bail out instead of painting it.
  if (!error && !isUpdating) {
    return null;
  }

  // Build the status text: "Status: packageName" or just "Status" or "Preparing..."
  const getStatusText = () => {
    if (!currentPackage) {
      return status || "Preparing...";
    }
    const packageName = currentPackage.split(";")[0];
    if (status) {
      const normalized = status.toLowerCase();
      if (
        normalized.includes(packageName.toLowerCase()) ||
        normalized.includes("unpack") ||
        normalized.includes("setting up") ||
        normalized.includes("processing triggers")
      ) {
        return status;
      }
      return `${status}: ${packageName}`;
    }
    return `Updating: ${packageName}`;
  };

  return (
    <div style={{ marginBottom: "var(--app-space-12)" }}>
      {/* Error Alert */}
      {error && (
        <AppAlert
          action={
            onClearError && (
              <AppIconButton
                aria-label="Clear update error"
                color="inherit"
                onClick={onClearError}
                size="small"
              >
                <Icon height={18} icon="mdi:close" width={18} />
              </AppIconButton>
            )
          }
          severity="error"
          style={{ marginBottom: 16 }}
        >
          {error}
        </AppAlert>
      )}

      {/* Progress Indicator */}
      {isUpdating && (
        <div style={{ marginTop: "var(--app-space-8)" }}>
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
              marginBottom: "var(--app-space-4)",
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              {getStatusText()}
            </AppTypography>
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: "var(--app-space-4)",
              }}
            >
              <AppTypography color="text.secondary" variant="body2">
                {Math.round(progress)}%
              </AppTypography>
              {canCancel && onCancel && (
                <AppActionIconButton
                  icon="mdi:cancel"
                  iconSize={20}
                  label="Cancel update"
                  onClick={onCancel}
                />
              )}
            </div>
          </div>
          <AppLinearProgress
            style={{ height: 8, borderRadius: 1 }}
            value={progress}
            variant="determinate"
          />
          {eventLog && eventLog.length > 0 && (
            <div style={{ marginTop: "var(--app-space-4)" }}>
              {eventLog.map((line, index) => (
                <AppTypography
                  color="text.secondary"
                  key={`${index}-${line}`}
                  style={{ display: "block" }}
                  variant="caption"
                >
                  {line}
                </AppTypography>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Kept after a failure: the dpkg output is what explains it. */}
      {output.length > 0 && (
        <InstallOutputPanel
          className="update-actions__output"
          expanded={showOutput}
          id="package-update-output"
          label="update output"
          onToggle={() => setShowOutput((previous) => !previous)}
          output={output}
          outputHistoryIncomplete={outputHistoryIncomplete}
          scrollerRef={outputRef}
        />
      )}
    </div>
  );
};

export default UpdateActions;
