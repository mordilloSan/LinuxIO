import { useCallback, useState, type KeyboardEvent } from "react";

import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";

import FileBrowserDialog from "../dialog/GeneralDialog";

interface InputDialogProps {
  confirmText?: string;
  defaultValue?: string;
  label: string;
  onClose: () => void;
  onConfirm: (value: string) => Promise<void> | void;
  isPending?: boolean;
  open: boolean;
  title: string;
}

const InputDialog = ({
  open,
  title,
  label,
  defaultValue = "",
  onClose,
  onConfirm,
  confirmText = "Create",
  isPending = false,
}: InputDialogProps) => {
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
  if (dialogState !== normalizedState) {
    setDialogState(normalizedState);
  }
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

  const handleConfirm = async () => {
    if (value.trim() && !isPending) {
      try {
        await onConfirm(value.trim());
        onClose();
      } catch {
        // Retain the dialog and draft for correction/retry.
      }
    }
  };

  const handleKeyPress = (e: KeyboardEvent) => {
    if (e.key === "Enter" && value.trim() && !isPending) {
      e.preventDefault();
      void handleConfirm();
    }
  };

  return (
    <FileBrowserDialog
      aria-busy={isPending || undefined}
      disableEscapeKeyDown={isPending}
      fullWidth
      maxWidth="xs"
      onClose={isPending ? undefined : onClose}
      open={open}
    >
      <AppDialogTitle>{title}</AppDialogTitle>
      <AppDialogContent>
        <AppTextField
          autoFocus
          disabled={isPending}
          fullWidth
          label={label}
          onChange={(e) => !isPending && setValue(e.target.value)}
          onKeyDown={handleKeyPress}
          type="text"
          value={value}
          variant="outlined"
        />
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={!value.trim() || isPending}
          onClick={() => void handleConfirm()}
          variant="contained"
        >
          {isPending ? "Creating…" : confirmText}
        </AppButton>
      </AppDialogActions>
    </FileBrowserDialog>
  );
};

export default InputDialog;
