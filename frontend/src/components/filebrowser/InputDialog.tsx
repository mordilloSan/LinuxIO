import {
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
} from "@mui/material";
import React, { useState, useCallback } from "react";

import FileBrowserDialog from "../dialog/GeneralDialog";

import AppButton from "@/components/ui/AppButton";

interface InputDialogProps {
  open: boolean;
  title: string;
  label: string;
  defaultValue?: string;
  onClose: () => void;
  onConfirm: (value: string) => void;
  confirmText?: string;
}

const InputDialog: React.FC<InputDialogProps> = ({
  open,
  title,
  label,
  defaultValue = "",
  onClose,
  onConfirm,
  confirmText = "Create",
}) => {
  const [dialogState, setDialogState] = useState({
    open,
    defaultValue,
    value: defaultValue,
  });
  const normalizedState =
    dialogState.open === open && dialogState.defaultValue === defaultValue
      ? dialogState
      : open
        ? { open, defaultValue, value: defaultValue }
        : { open, defaultValue, value: dialogState.value };
  const { value } = normalizedState;
  const setValue = useCallback(
    (nextValue: string) => {
      setDialogState((prev) => {
        const current =
          prev.open === open && prev.defaultValue === defaultValue
            ? prev
            : open
              ? { open, defaultValue, value: defaultValue }
              : { open, defaultValue, value: prev.value };
        return { ...current, value: nextValue };
      });
    },
    [open, defaultValue],
  );

  const handleConfirm = () => {
    if (value.trim()) {
      onConfirm(value.trim());
      onClose();
    }
  };

  const handleKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && value.trim()) {
      handleConfirm();
    }
  };

  return (
    <FileBrowserDialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>{title}</DialogTitle>
      <DialogContent>
        <TextField
          autoFocus
          margin="dense"
          label={label}
          type="text"
          fullWidth
          variant="outlined"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={handleKeyPress}
        />
      </DialogContent>
      <DialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          onClick={handleConfirm}
          disabled={!value.trim()}
          variant="contained"
        >
          {confirmText}
        </AppButton>
      </DialogActions>
    </FileBrowserDialog>
  );
};

export default InputDialog;
