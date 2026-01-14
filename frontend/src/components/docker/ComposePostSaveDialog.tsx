import { Box, Button, Dialog, Typography, useTheme } from "@mui/material";
import React from "react";

interface ComposePostSaveDialogProps {
  open: boolean;
  stackName: string;
  stackState: "new" | "running" | "stopped";
  onStart: () => void;
  onRestart: () => void;
  onDoNothing: () => void;
  isExecuting?: boolean;
}

const ComposePostSaveDialog: React.FC<ComposePostSaveDialogProps> = ({
  open,
  stackName,
  stackState,
  onStart,
  onRestart,
  onDoNothing,
  isExecuting = false,
}) => {
  const theme = useTheme();

  const getActionLabel = () => {
    if (stackState === "running") {
      return isExecuting ? "Restarting..." : "Restart Stack";
    }
    return isExecuting ? "Starting..." : "Start Stack";
  };

  const getActionMessage = () => {
    if (stackState === "running") {
      return `The compose file for "${stackName}" has been saved. Would you like to restart the stack to apply the changes?`;
    }
    return `The compose file for "${stackName}" has been saved successfully. Would you like to start the stack now?`;
  };

  const handleAction = () => {
    if (stackState === "running") {
      onRestart();
    } else {
      onStart();
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onDoNothing}
      maxWidth="sm"
      fullWidth
      slotProps={{
        paper: {
          sx: {
            backgroundColor: theme.header.background,
            borderRadius: 4,
            border: `1px solid rgba(255, 255, 255, 0.2)`,
            boxShadow: `0 0 10px rgba(255, 255, 255, 0.5), 0 0 20px rgba(255, 255, 255, 0.3), inset 0 0 20px rgba(255, 255, 255, 0.1)`,
            backdropFilter: "blur(10px)",
          },
        },
        backdrop: {
          sx: {
            backdropFilter: "blur(4px)",
            backgroundColor: "rgba(0, 0, 0, 0.7)",
          },
        },
      }}
    >
      <Box
        sx={{
          p: 4,
          display: "flex",
          flexDirection: "column",
          gap: 3,
          alignItems: "center",
          textAlign: "center",
        }}
      >
        {/* Title */}
        <Typography
          variant="h5"
          fontWeight={600}
          sx={{
            color: theme.palette.text.primary,
          }}
        >
          Stack Saved Successfully
        </Typography>

        {/* Message */}
        <Typography
          variant="body1"
          sx={{
            mt: 2,
            color: theme.palette.text.secondary,
          }}
        >
          {getActionMessage()}
        </Typography>

        {/* Buttons */}
        <Box
          sx={{
            display: "flex",
            gap: 2,
            justifyContent: "center",
            width: "100%",
            mt: 2,
          }}
        >
          <Button
            onClick={onDoNothing}
            disabled={isExecuting}
            sx={{
              px: 3,
              py: 1.5,
              textTransform: "uppercase",
              fontSize: "0.875rem",
              fontWeight: 600,
              letterSpacing: "0.5px",
              color: theme.palette.text.secondary,
              backgroundColor: "transparent",
              "&:hover": {
                backgroundColor: `${theme.palette.primary.main}22`,
                boxShadow: `0 0 12px ${theme.palette.primary.main}44`,
              },
              "&:disabled": {
                opacity: 0.5,
                cursor: "not-allowed",
              },
            }}
          >
            Do Nothing
          </Button>

          <Button
            onClick={handleAction}
            disabled={isExecuting}
            sx={{
              px: 3,
              py: 1.5,
              textTransform: "uppercase",
              fontSize: "0.875rem",
              fontWeight: 600,
              letterSpacing: "0.5px",
              color: theme.palette.primary.main,
              "&:hover": {
                backgroundColor: `${theme.palette.primary.main}22`,
                boxShadow: `0 0 12px ${theme.palette.primary.main}44`,
              },
              "&:disabled": {
                opacity: 0.5,
                cursor: "not-allowed",
              },
            }}
          >
            {getActionLabel()}
          </Button>
        </Box>
      </Box>
    </Dialog>
  );
};

export default ComposePostSaveDialog;
