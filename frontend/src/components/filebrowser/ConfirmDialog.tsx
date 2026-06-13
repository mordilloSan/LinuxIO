import React from "react";

import GeneralDialog from "../dialog/GeneralDialog";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

interface ConfirmDialogProps {
  cancelText?: string;
  confirmText?: string;
  message: string;
  onClose: () => void;
  onConfirm: () => void;
  open: boolean;
  title: string;
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
  const theme = useAppTheme();

  const handleConfirm: React.SubmitEventHandler<HTMLFormElement> = (event) => {
    event.preventDefault();
    onConfirm();
    onClose();
  };

  const handleKeyDown: React.KeyboardEventHandler<HTMLFormElement> = (
    event,
  ) => {
    if (event.key !== "Enter" || event.defaultPrevented || event.repeat) {
      return;
    }

    event.preventDefault();
    event.stopPropagation();
    onConfirm();
    onClose();
  };

  return (
    <GeneralDialog fullWidth maxWidth="xs" onClose={onClose} open={open}>
      <form
        onKeyDown={handleKeyDown}
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
        <AppTypography fontWeight={600} variant="h5">
          {title}
        </AppTypography>

        <AppTypography
          color="text.secondary"
          style={{ marginTop: theme.spacing(2) }}
          variant="body1"
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
            className="app-btn--dialog-action"
            onClick={onClose}
            style={{ color: "var(--app-palette-text-secondary)" }}
            type="button"
          >
            {cancelText}
          </AppButton>
          <AppButton autoFocus className="app-btn--dialog-action" type="submit">
            {confirmText}
          </AppButton>
        </div>
      </form>
    </GeneralDialog>
  );
};

export default ConfirmDialog;
