import { Button } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import AppTypography from "@/components/ui/AppTypography";

import GeneralDialog from "../dialog/GeneralDialog";

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
  const theme = useTheme();

  const handleConfirm: React.SubmitEventHandler<HTMLFormElement> = (event) => {
    event.preventDefault();
    onConfirm();
    onClose();
  };

  return (
    <GeneralDialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <form
        onSubmit={handleConfirm}
        style={{
          padding: theme.spacing(4),
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(3),
          alignItems: "center",
          textAlign: "center",
        }}
      >
        <AppTypography variant="h5" fontWeight={600}>
          {title}
        </AppTypography>

        <AppTypography
          variant="body1"
          color="text.secondary"
          style={{ marginTop: theme.spacing(2) }}
        >
          {message}
        </AppTypography>

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
        </div>
      </form>
    </GeneralDialog>
  );
};

export default ConfirmDialog;
