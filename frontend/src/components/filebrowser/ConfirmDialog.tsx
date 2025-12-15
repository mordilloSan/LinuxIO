import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  DialogContentText,
  Button,
} from "@mui/material";
import React from "react";

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
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <form onSubmit={handleConfirm}>
        <DialogTitle>{title}</DialogTitle>
        <DialogContent>
          <DialogContentText>{message}</DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose} type="button">
            {cancelText}
          </Button>
          <Button
            type="submit"
            onClick={handleConfirm}
            variant="contained"
            color="error"
            autoFocus
          >
            {confirmText}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
};

export default ConfirmDialog;
