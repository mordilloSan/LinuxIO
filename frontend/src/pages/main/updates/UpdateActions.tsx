import CancelIcon from "@mui/icons-material/Cancel";
import CloseIcon from "@mui/icons-material/Close";
import {
  Box,
  LinearProgress,
  Typography,
  Alert,
  IconButton,
  Tooltip,
} from "@mui/material";
import React from "react";

interface UpdateActionsProps {
  isUpdating: boolean;
  currentPackage: string | null;
  progress: number;
  status?: string | null;
  error?: string | null;
  onClearError?: () => void;
  onCancel?: () => void;
}

const UpdateActions: React.FC<UpdateActionsProps> = ({
  isUpdating,
  currentPackage,
  progress,
  status,
  error,
  onClearError,
  onCancel,
}) => {
  // Build the status text: "Status: packageName" or just "Status" or "Preparing..."
  const getStatusText = () => {
    if (!currentPackage) {
      return status || "Preparing...";
    }
    const packageName = currentPackage.split(";")[0];
    if (status) {
      return `${status}: ${packageName}`;
    }
    return `Updating: ${packageName}`;
  };

  return (
    <Box sx={{ mb: 3 }}>
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
        <Box sx={{ mt: 2 }}>
          <Box
            sx={{
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
              mb: 1,
            }}
          >
            <Typography variant="body2" color="text.secondary">
              {getStatusText()}
            </Typography>
            <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
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
            </Box>
          </Box>
          <LinearProgress
            variant="determinate"
            value={progress}
            sx={{ height: 8, borderRadius: 1 }}
          />
        </Box>
      )}
    </Box>
  );
};

export default UpdateActions;
