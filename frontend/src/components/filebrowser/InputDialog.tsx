import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Button,
} from "@mui/material";
import React, { useState, useCallback } from "react";

interface InputDialogProps {
  open: boolean;
  title: string;
  label: string;
  defaultValue?: string;
  onClose: () => void;
  onConfirm: (value: string) => void;
}

const InputDialog: React.FC<InputDialogProps> = ({
  open,
  title,
  label,
  defaultValue = "",
  onClose,
  onConfirm,
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
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
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
        <Button onClick={onClose}>Cancel</Button>
        <Button
          onClick={handleConfirm}
          disabled={!value.trim()}
          variant="contained"
        >
          Create
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default InputDialog;
