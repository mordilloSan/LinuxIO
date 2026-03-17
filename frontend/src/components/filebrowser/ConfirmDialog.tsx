import { useTheme } from "@mui/material/styles";
import React from "react";

import GeneralDialog from "../dialog/GeneralDialog";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";

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
          <AppButton
            onClick={onClose}
            type="button"
            className="app-btn--dialog-action"
            style={{ color: "var(--mui-palette-text-secondary)" }}
          >
            {cancelText}
          </AppButton>
          <AppButton type="submit" autoFocus className="app-btn--dialog-action">
            {confirmText}
          </AppButton>
        </div>
      </form>
    </GeneralDialog>
  );
};

export default ConfirmDialog;
