import { Box, Button, Typography, useTheme } from "@mui/material";
import React from "react";

import FileBrowserDialog from "../dialog/GeneralDialog";

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
          Unsaved Changes
        </Typography>

        {/* Message */}
        <Typography
          variant="body1"
          sx={{
            mt: 2,
            color: theme.palette.text.secondary,
          }}
        >
          You have unsaved changes in the editor. What would you like to do?
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
        </Box>
      </Box>
    </FileBrowserDialog>
  );
};

export default UnsavedChangesDialog;
