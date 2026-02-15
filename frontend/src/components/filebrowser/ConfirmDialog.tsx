import { Box, Button, Typography } from "@mui/material";
import React from "react";

import FileBrowserDialog from "./FileBrowserDialog";

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  onClose: () => void;
  onConfirm: () => void;
}

const ConfirmDialog: React.FC<ConfirmDialogProps> = ({
  open,
  title,
  message,
  confirmText = "Confirm",
  cancelText = "Cancel",
  onClose,
  onConfirm,
}) => {
  const handleConfirm = (
    event?: React.FormEvent<HTMLFormElement> | React.MouseEvent,
  ) => {
    event?.preventDefault();
    onConfirm();
    onClose();
  };

  return (
    <FileBrowserDialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <Box
        component="form"
        onSubmit={handleConfirm}
        sx={{
          p: 4,
          display: "flex",
          flexDirection: "column",
          gap: 3,
          alignItems: "center",
          textAlign: "center",
        }}
      >
        <Typography variant="h5" fontWeight={600}>
          {title}
        </Typography>

        <Typography variant="body1" color="text.secondary" sx={{ mt: 2 }}>
          {message}
        </Typography>

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
            onClick={onClose}
            type="button"
            sx={{
              px: 3,
              py: 1.5,
              textTransform: "uppercase",
              fontSize: "0.875rem",
              fontWeight: 600,
              letterSpacing: "0.5px",
              color: "text.secondary",
              backgroundColor: "transparent",
              "&:hover": {
                backgroundColor: (theme) => `${theme.palette.primary.main}22`,
                boxShadow: (theme) =>
                  `0 0 12px ${theme.palette.primary.main}44`,
              },
            }}
          >
            {cancelText}
          </Button>
          <Button
            type="submit"
            onClick={handleConfirm}
            autoFocus
            sx={{
              px: 3,
              py: 1.5,
              textTransform: "uppercase",
              fontSize: "0.875rem",
              fontWeight: 600,
              letterSpacing: "0.5px",
              color: "primary.main",
              "&:hover": {
                backgroundColor: (theme) => `${theme.palette.primary.main}22`,
                boxShadow: (theme) =>
                  `0 0 12px ${theme.palette.primary.main}44`,
              },
            }}
          >
            {confirmText}
          </Button>
        </Box>
      </Box>
    </FileBrowserDialog>
  );
};

export default ConfirmDialog;
