import React from "react";

import FileBrowserDialog from "../dialog/GeneralDialog";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

interface UnsavedChangesDialogProps {
  open: boolean;
  onKeepEditing: () => void;
  onDiscardAndExit: () => void;
  onSaveAndExit: () => void;
  isSaving?: boolean;
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
      open={open}
      onClose={onKeepEditing}
      maxWidth="sm"
      fullWidth
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
          Unsaved Changes
        </AppTypography>

        {/* Message */}
        <AppTypography
          variant="body1"
          style={{
            marginTop: 8,
            color: theme.palette.text.secondary,
          }}
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
            onClick={onKeepEditing}
            disabled={isSaving}
            className="app-btn--dialog-action"
            style={{ color: "var(--mui-palette-text-secondary)" }}
          >
            Keep Editing
          </AppButton>

          <AppButton
            onClick={onDiscardAndExit}
            disabled={isSaving}
            className="app-btn--dialog-action"
          >
            Discard and Exit
          </AppButton>

          <AppButton
            onClick={onSaveAndExit}
            disabled={isSaving}
            className="app-btn--dialog-action"
          >
            {isSaving ? "Saving..." : "Save and Exit"}
          </AppButton>
        </div>
      </div>
    </FileBrowserDialog>
  );
};

export default UnsavedChangesDialog;
