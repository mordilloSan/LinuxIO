import CancelIcon from "@mui/icons-material/Cancel";
import CloseIcon from "@mui/icons-material/Close";
import {
  Alert,
  IconButton,
  LinearProgress,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

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
                <CloseIcon fontSize="inherit" />
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
            <Typography variant="body2" color="text.secondary">
              {getStatusText()}
            </Typography>
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: theme.spacing(1),
              }}
            >
              <Typography variant="body2" color="text.secondary">
                {Math.round(progress)}%
              </Typography>
              {onCancel && (
                <Tooltip title="Cancel update">
                  <IconButton size="small" onClick={onCancel} sx={{ ml: 0.5 }}>
                    <CancelIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
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
                <Typography
                  key={`${index}-${line}`}
                  variant="caption"
                  color="text.secondary"
                  sx={{ display: "block" }}
                >
                  {line}
                </Typography>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export default UpdateActions;
