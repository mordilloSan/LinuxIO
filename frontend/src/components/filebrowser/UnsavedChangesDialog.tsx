import { Button, useTheme } from "@mui/material";
import React from "react";

import FileBrowserDialog from "../dialog/GeneralDialog";

import AppTypography from "@/components/ui/AppTypography";

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
  const theme = useTheme();

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
          <Button
            onClick={onKeepEditing}
            disabled={isSaving}
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
            Keep Editing
          </Button>

          <Button
            onClick={onDiscardAndExit}
            disabled={isSaving}
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
            Discard and Exit
          </Button>

          <Button
            onClick={onSaveAndExit}
            disabled={isSaving}
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
            {isSaving ? "Saving..." : "Save and Exit"}
          </Button>
        </div>
      </div>
    </FileBrowserDialog>
  );
};

export default UnsavedChangesDialog;
