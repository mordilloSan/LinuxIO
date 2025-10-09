import { Close as CloseIcon } from "@mui/icons-material";
import {
  Box,
  LinearProgress,
  Typography,
  Alert,
  IconButton,
} from "@mui/material";
import React from "react";

interface UpdateActionsProps {
  onUpdateAll: () => void;
  isUpdating: boolean;
  currentPackage: string | null;
  progress: number;
  error?: string | null;
  onClearError?: () => void;
  updateCount: number;
}

const UpdateActions: React.FC<UpdateActionsProps> = ({
  isUpdating,
  currentPackage,
  progress,
  error,
  onClearError,
}) => {
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
              mb: 1,
            }}
          >
            <Typography variant="body2" color="text.secondary">
              {currentPackage
                ? `Updating: ${currentPackage.split(";")[0]}`
                : "Preparing..."}
            </Typography>
            <Typography variant="body2" color="text.secondary">
              {Math.round(progress)}%
            </Typography>
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
