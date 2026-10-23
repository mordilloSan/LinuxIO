import type { KeyboardEventHandler, SubmitEventHandler } from "react";

import type { TaskProgress } from "@/api";

import AppButton from "./AppButton";
import AppCircularProgress from "./AppCircularProgress";
import {
  AppDialog,
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "./AppDialog";

interface ConfirmProgressDetail {
  processed?: number;
}

interface AppConfirmDialogProps {
  cancelText?: string;
  confirmText?: string;
  destructive?: boolean;
  message: string;
  onClose: () => void;
  onConfirm: () => void;
  open: boolean;
  isPending?: boolean;
  pendingText?: string;
  progress?: TaskProgress<ConfirmProgressDetail> | null;
  title: string;
}

const AppConfirmDialog = ({
  open,
  title,
  message,
  confirmText = "Confirm",
  cancelText = "Cancel",
  destructive = false,
  onClose,
  onConfirm,
  isPending = false,
  pendingText = "Deleting…",
  progress,
}: AppConfirmDialogProps) => {
  const handleConfirm: SubmitEventHandler<HTMLFormElement> = (event) => {
    event.preventDefault();
    if (!isPending) onConfirm();
  };

  const handleKeyDown: KeyboardEventHandler<HTMLFormElement> = (event) => {
    if (event.key !== "Enter" || event.defaultPrevented || event.repeat) {
      return;
    }

    event.preventDefault();
    event.stopPropagation();
    if (!isPending) onConfirm();
  };

  const progressText = progress?.message ?? progress?.phase ?? "Deleting items";
  const processed = progress?.detail?.processed;
  // Indeterminate progress carries no percentage; fall back to the running
  // item count so the dialog never sits on a frozen 0%.
  const progressSuffix =
    progress?.percentage !== undefined
      ? ` (${progress.percentage}%)`
      : typeof processed === "number" && processed > 0
        ? ` (${processed.toLocaleString()} items)`
        : "";

  return (
    <AppDialog
      aria-busy={isPending || undefined}
      disableEscapeKeyDown={isPending}
      fullWidth
      maxWidth="xs"
      onClose={isPending ? undefined : onClose}
      open={open}
    >
      <form
        className="app-dialog-form"
        onKeyDown={handleKeyDown}
        onSubmit={handleConfirm}
      >
        <AppDialogTitle>{title}</AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>{message}</AppDialogContentText>
          {isPending && (
            <div aria-live="polite" role="status">
              {progressText}
              {progressSuffix}
            </div>
          )}
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={isPending} onClick={onClose} type="button">
            {cancelText}
          </AppButton>
          <AppButton
            autoFocus
            color={destructive ? "error" : "primary"}
            disabled={isPending}
            startIcon={
              isPending ? (
                <AppCircularProgress color="inherit" size={14} />
              ) : null
            }
            type="submit"
            variant="contained"
          >
            {isPending ? pendingText : confirmText}
          </AppButton>
        </AppDialogActions>
      </form>
    </AppDialog>
  );
};

export default AppConfirmDialog;
