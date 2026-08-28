import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";

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
        <AppTypography color="text.primary" fontWeight={600} variant="h5">
          Unsaved Changes
        </AppTypography>

        {/* Message */}
        <AppTypography
          color="text.secondary"
          style={{ marginTop: 8 }}
          variant="body1"
        >
          You have unsaved changes in the editor. What would you like to do?
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
            className="app-btn--dialog-action"
            disabled={isSaving}
            onClick={onKeepEditing}
            style={{ color: "var(--app-palette-text-secondary)" }}
          >
            Keep Editing
          </AppButton>

          <AppButton
            className="app-btn--dialog-action"
            disabled={isSaving}
            onClick={onDiscardAndExit}
          >
            Discard and Exit
          </AppButton>

          <AppButton
            className="app-btn--dialog-action"
            disabled={isSaving}
            onClick={onSaveAndExit}
          >
            {isSaving ? "Saving..." : "Save and Exit"}
          </AppButton>
        </div>
      </div>
    </FileBrowserDialog>
  );
};

export default UnsavedChangesDialog;
