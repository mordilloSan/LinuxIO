import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";

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
    <GeneralDialog fullWidth maxWidth="xs" onClose={onDoNothing} open={open}>
      <AppDialogTitle>Stack Saved Successfully</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText style={{ whiteSpace: "pre-line" }}>
          {getActionMessage()}
        </AppDialogContentText>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton color="inherit" disabled={isExecuting} onClick={onDoNothing}>
          Do Nothing
        </AppButton>

        <AppButton
          disabled={isExecuting}
          onClick={handleAction}
          variant="contained"
        >
          {getActionLabel()}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default ComposePostSaveDialog;
