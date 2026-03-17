import { Dialog } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { alpha } from "@/utils/color";

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
      return `The compose file for "${stackName}" has been saved.\nWould you like to restart the stack to apply the changes?`;
    }
    return `The compose file for "${stackName}" has been saved successfully.\nWould you like to start the stack now?`;
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
      maxWidth="xs"
      fullWidth
      slotProps={{
        paper: {
          sx: {
            backgroundColor: theme.header.background,
            borderRadius: 4,
            border: `1px solid ${alpha(theme.dialog.border, 0.2)}`,
            boxShadow: `0 0 10px ${alpha(theme.dialog.glow, 0.5)}, 0 0 20px ${alpha(theme.dialog.glow, 0.3)}, inset 0 0 20px ${alpha(theme.dialog.glow, 0.1)}`,
            backdropFilter: "blur(10px)",
          },
        },
        backdrop: {
          sx: {
            backdropFilter: "blur(4px)",
            backgroundColor: alpha(theme.dialog.backdrop, 0.7),
          },
        },
      }}
    >
      <div
        style={{
          padding: theme.spacing(4),
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(3),
          alignItems: "center",
          textAlign: "center",
        }}
      >
        {/* Title */}
        <AppTypography
          variant="h5"
          fontWeight={600}
          style={{
            color: theme.palette.text.primary,
          }}
        >
          Stack Saved Successfully
        </AppTypography>

        {/* Message */}
        <AppTypography
          variant="body1"
          style={{
            marginTop: theme.spacing(2),
            color: theme.palette.text.secondary,
            whiteSpace: "pre-line",
          }}
        >
          {getActionMessage()}
        </AppTypography>

        {/* Buttons */}
        <div
          style={{
            display: "flex",
            gap: theme.spacing(2),
            justifyContent: "center",
            width: "100%",
            marginTop: theme.spacing(2),
          }}
        >
          <AppButton
            onClick={onDoNothing}
            disabled={isExecuting}
            color="inherit"
            style={{
              paddingInline: 12,
              paddingBlock: 6,
              fontWeight: 600,
              letterSpacing: "0.5px",
              color: theme.palette.text.secondary,
            }}
          >
            Do Nothing
          </AppButton>

          <AppButton
            onClick={handleAction}
            disabled={isExecuting}
            style={{
              paddingInline: 12,
              paddingBlock: 6,
              fontWeight: 600,
              letterSpacing: "0.5px",
            }}
          >
            {getActionLabel()}
          </AppButton>
        </div>
      </div>
    </Dialog>
  );
};

export default ComposePostSaveDialog;
