import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";

interface ComposePostSaveDialogProps {
  isExecuting?: boolean;
  onDoNothing: () => void;
  onRestart: () => void;
  onStart: () => void;
  open: boolean;
  stackName: string;
  stackState: "new" | "running" | "stopped";
}

const ComposePostSaveDialog = ({
  open,
  stackName,
  stackState,
  onStart,
  onRestart,
  onDoNothing,
  isExecuting = false,
}: ComposePostSaveDialogProps) => {
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
    <GeneralDialog
      fullWidth
      maxWidth="xs"
      onClose={onDoNothing}
      open={open}
      paperStyle={{
        backgroundColor: "var(--app-header-background)",
      }}
    >
      <div
        style={{
          padding: "var(--app-space-16)",
          display: "flex",
          flexDirection: "column",
          gap: "var(--app-space-12)",
          alignItems: "center",
          textAlign: "center",
        }}
      >
        {/* Title */}
        <AppTypography
          fontWeight={600}
          style={{
            color: "var(--app-palette-text-primary)",
          }}
          variant="h5"
        >
          Stack Saved Successfully
        </AppTypography>

        {/* Message */}
        <AppTypography
          style={{
            marginTop: "var(--app-space-8)",
            color: "var(--app-palette-text-secondary)",
            whiteSpace: "pre-line",
          }}
          variant="body1"
        >
          {getActionMessage()}
        </AppTypography>

        {/* Buttons */}
        <div
          style={{
            display: "flex",
            gap: "var(--app-space-8)",
            justifyContent: "center",
            width: "100%",
            marginTop: "var(--app-space-8)",
          }}
        >
          <AppButton
            color="inherit"
            disabled={isExecuting}
            onClick={onDoNothing}
            style={{
              paddingInline: 12,
              paddingBlock: 6,
              fontWeight: 600,
              letterSpacing: "0.5px",
              color: "var(--app-palette-text-secondary)",
            }}
          >
            Do Nothing
          </AppButton>

          <AppButton
            disabled={isExecuting}
            onClick={handleAction}
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
    </GeneralDialog>
  );
};

export default ComposePostSaveDialog;
