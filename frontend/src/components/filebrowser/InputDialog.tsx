import React, { useCallback, useState } from "react";

import FileBrowserDialog from "../dialog/GeneralDialog";

import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";

interface InputDialogProps {
  confirmText?: string;
  defaultValue?: string;
  label: string;
  onClose: () => void;
  onConfirm: (value: string) => void;
  open: boolean;
  title: string;
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
    <FileBrowserDialog fullWidth maxWidth="xs" onClose={onClose} open={open}>
      <AppDialogTitle>{title}</AppDialogTitle>
      <AppDialogContent>
        <AppTextField
          autoFocus
          fullWidth
          label={label}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={handleKeyPress}
          type="text"
          value={value}
          variant="outlined"
        />
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          disabled={!value.trim()}
          onClick={handleConfirm}
          variant="contained"
        >
          {confirmText}
        </AppButton>
      </AppDialogActions>
    </FileBrowserDialog>
  );
};

export default InputDialog;
