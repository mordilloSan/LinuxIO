import type { KeyboardEventHandler, SubmitEventHandler } from "react";

import type { TaskProgress } from "@/api";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppTypography from "@/components/ui/AppTypography";

import GeneralDialog from "../dialog/GeneralDialog";

interface ConfirmProgressDetail {
  processed?: number;
}

interface ConfirmDialogProps {
  cancelText?: string;
  confirmText?: string;
  message: string;
  onClose: () => void;
  onConfirm: () => void;
  open: boolean;
  isPending?: boolean;
  progress?: TaskProgress<ConfirmProgressDetail> | null;
  title: string;
}

const ConfirmDialog = ({
  open,
  title,
  message,
  confirmText = "Confirm",
  cancelText = "Cancel",
  onClose,
  onConfirm,
  isPending = false,
  progress,
}: ConfirmDialogProps) => {
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
    <GeneralDialog
      aria-busy={isPending || undefined}
      disableEscapeKeyDown={isPending}
      fullWidth
      maxWidth="xs"
      onClose={isPending ? undefined : onClose}
      open={open}
    >
      <form
        onKeyDown={handleKeyDown}
        onSubmit={handleConfirm}
        style={{
          padding: "var(--app-space-16)",
          display: "flex",
          flexDirection: "column",
          gap: "var(--app-space-12)",
          alignItems: "center",
          textAlign: "center",
        }}
      >
        <AppTypography fontWeight={600} variant="h5">
          {title}
        </AppTypography>

        <AppTypography
          color="text.secondary"
          style={{ marginTop: "var(--app-space-8)" }}
          variant="body1"
        >
          {message}
        </AppTypography>
        {isPending && (
          <AppTypography
            aria-live="polite"
            color="text.secondary"
            role="status"
            variant="body2"
          >
            {progressText}
            {progressSuffix}
          </AppTypography>
        )}

        <div
          style={{
            display: "flex",
            gap: "var(--app-space-8)",
            justifyContent: "center",
            width: "100%",
            marginTop: "var(--app-space-8)",
          }}
        >
          <AppButton
            className="app-btn--dialog-action"
            disabled={isPending}
            onClick={onClose}
            style={{ color: "var(--app-palette-text-secondary)" }}
            type="button"
          >
            {cancelText}
          </AppButton>
          <AppButton
            autoFocus
            className="app-btn--dialog-action"
            disabled={isPending}
            startIcon={
              isPending ? (
                <AppCircularProgress color="inherit" size={14} />
              ) : null
            }
            type="submit"
          >
            {isPending ? "Deleting…" : confirmText}
          </AppButton>
        </div>
      </form>
    </GeneralDialog>
  );
};

export default ConfirmDialog;
