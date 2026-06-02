import React from "react";

import FileBrowserDialog from "../dialog/GeneralDialog";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

interface UnsavedChangesDialogProps {
  isSaving?: boolean;
  onDiscardAndExit: () => void;
  onKeepEditing: () => void;
  onSaveAndExit: () => void;
  open: boolean;
}

const UnsavedChangesDialog: React.FC<UnsavedChangesDialogProps> = ({
  open,
  onKeepEditing,
  onDiscardAndExit,
  onSaveAndExit,
  isSaving = false,
}) => {
  const theme = useAppTheme();

  return (
    <FileBrowserDialog
      fullWidth
      maxWidth="sm"
      onClose={onKeepEditing}
      open={open}
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
          fontWeight={600}
          style={{
            color: theme.palette.text.primary,
          }}
          variant="h5"
        >
          Unsaved Changes
        </AppTypography>

        {/* Message */}
        <AppTypography
          style={{
            marginTop: 8,
            color: theme.palette.text.secondary,
          }}
          variant="body1"
        >
          You have unsaved changes in the editor. What would you like to do?
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
            className="app-btn--dialog-action"
            disabled={isSaving}
            onClick={onKeepEditing}
            style={{ color: "var(--mui-palette-text-secondary)" }}
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
