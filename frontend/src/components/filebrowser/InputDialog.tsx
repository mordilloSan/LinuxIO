import React, { useState, useEffect } from "react";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Button,
} from "@mui/material";

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
  const [value, setValue] = useState(defaultValue);

  useEffect(() => {
    if (open) {
      setValue(defaultValue);
    }
  }, [open, defaultValue]);

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
          onKeyPress={handleKeyPress}
        />
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button onClick={handleConfirm} disabled={!value.trim()} variant="contained">
          Create
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default InputDialog;
