import AppButton from "./AppButton";
import {
  AppDialog,
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "./AppDialog";

interface AppUnsavedChangesDialogProps {
  isSaving?: boolean;
  onDiscardAndExit: () => void;
  onKeepEditing: () => void;
  onSaveAndExit: () => void;
  open: boolean;
}

const AppUnsavedChangesDialog = ({
  open,
  onKeepEditing,
  onDiscardAndExit,
  onSaveAndExit,
  isSaving = false,
}: AppUnsavedChangesDialogProps) => {
  return (
    <AppDialog fullWidth maxWidth="sm" onClose={onKeepEditing} open={open}>
      <AppDialogTitle>Unsaved Changes</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          You have unsaved changes in the editor. What would you like to do?
        </AppDialogContentText>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isSaving} onClick={onKeepEditing}>
          Keep Editing
        </AppButton>

        <AppButton
          color="error"
          disabled={isSaving}
          onClick={onDiscardAndExit}
          variant="outlined"
        >
          Discard and Exit
        </AppButton>

        <AppButton
          disabled={isSaving}
          onClick={onSaveAndExit}
          variant="contained"
        >
          {isSaving ? "Saving..." : "Save and Exit"}
        </AppButton>
      </AppDialogActions>
    </AppDialog>
  );
};

export default AppUnsavedChangesDialog;
