import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";

import FileBrowserDialog from "../dialog/GeneralDialog";

interface UnsavedChangesDialogProps {
  isSaving?: boolean;
  onDiscardAndExit: () => void;
  onKeepEditing: () => void;
  onSaveAndExit: () => void;
  open: boolean;
}

const UnsavedChangesDialog = ({
  open,
  onKeepEditing,
  onDiscardAndExit,
  onSaveAndExit,
  isSaving = false,
}: UnsavedChangesDialogProps) => {
  return (
    <FileBrowserDialog
      fullWidth
      maxWidth="sm"
      onClose={onKeepEditing}
      open={open}
    >
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
    </FileBrowserDialog>
  );
};

export default UnsavedChangesDialog;
