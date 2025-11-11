import { Box, Button, Dialog, Typography, useTheme } from "@mui/material";
import React from "react";

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
    <Dialog
      open={open}
      onClose={onKeepEditing}
      maxWidth="sm"
      fullWidth
      PaperProps={{
        sx: {
          backgroundColor: theme.palette.background.paper,
          borderRadius: 4,
          border: `1px solid rgba(255, 255, 255, 0.2)`,
          boxShadow: `0 0 10px rgba(255, 255, 255, 0.5), 0 0 20px rgba(255, 255, 255, 0.3), inset 0 0 20px rgba(255, 255, 255, 0.1)`,
          backdropFilter: "blur(10px)",
        },
      }}
      BackdropProps={{
        sx: {
          backdropFilter: "blur(4px)",
          backgroundColor: "rgba(0, 0, 0, 0.7)",
        },
      }}
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
            mt:2,
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
    </Dialog>
  );
};

export default UnsavedChangesDialog;
