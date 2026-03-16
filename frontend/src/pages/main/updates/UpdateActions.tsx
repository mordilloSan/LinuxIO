import { Icon } from "@iconify/react";
import { Alert, IconButton, LinearProgress } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";

interface UpdateActionsProps {
  isUpdating: boolean;
  currentPackage: string | null;
  progress: number;
  status?: string | null;
  eventLog?: string[];
  error?: string | null;
  onClearError?: () => void;
  onCancel?: () => void;
}

const UpdateActions: React.FC<UpdateActionsProps> = ({
  isUpdating,
  currentPackage,
  progress,
  status,
  eventLog,
  error,
  onClearError,
  onCancel,
}) => {
  const theme = useTheme();
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
    <div style={{ marginBottom: theme.spacing(3) }}>
      {/* Error Alert */}
      {error && (
        <Alert
          severity="error"
          sx={{ mb: 2 }}
          action={
            onClearError && (
              <IconButton
                aria-label="close"
                color="inherit"
                size="small"
                onClick={onClearError}
              >
                <Icon icon="mdi:close" width={18} height={18} />
              </IconButton>
            )
          }
        >
          {error}
        </Alert>
      )}

      {/* Progress Indicator */}
      {isUpdating && (
        <div style={{ marginTop: theme.spacing(2) }}>
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
              marginBottom: theme.spacing(1),
            }}
          >
            <AppTypography variant="body2" color="text.secondary">
              {getStatusText()}
            </AppTypography>
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: theme.spacing(1),
              }}
            >
              <AppTypography variant="body2" color="text.secondary">
                {Math.round(progress)}%
              </AppTypography>
              {onCancel && (
                <AppTooltip title="Cancel update">
                  <IconButton size="small" onClick={onCancel} sx={{ ml: 0.5 }}>
                    <Icon icon="mdi:cancel" width={20} height={20} />
                  </IconButton>
                </AppTooltip>
              )}
            </div>
          </div>
          <LinearProgress
            variant="determinate"
            value={progress}
            sx={{ height: 8, borderRadius: 1 }}
          />
          {eventLog && eventLog.length > 0 && (
            <div style={{ marginTop: theme.spacing(1) }}>
              {eventLog.map((line, index) => (
                <AppTypography
                  key={`${index}-${line}`}
                  variant="caption"
                  color="text.secondary"
                  style={{ display: "block" }}
                >
                  {line}
                </AppTypography>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export default UpdateActions;
