import { linuxio } from "@/api";
import type {
  BtrfsRollbackProgress,
  BtrfsRollbackResult,
  BtrfsSnapshot,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import usePowerAction from "@/hooks/usePowerAction";
import { formatDate } from "@/utils/formatters";

interface BtrfsRollbackDialogProps {
  isPending: boolean;
  onClose: () => void;
  onConfirm: () => void;
  progress: BtrfsRollbackProgress | null;
  result: BtrfsRollbackResult | null;
  snapshot: BtrfsSnapshot;
}

const BtrfsRollbackDialog = ({
  snapshot,
  isPending,
  progress,
  result,
  onConfirm,
  onClose,
}: BtrfsRollbackDialogProps) => {
  const { triggerReboot } = usePowerAction();
  // The server may die before responding, so errors are expected and only logged.
  const { mutate: reboot } = linuxio.control.reboot.useMutation({
    error: (error) => {
      console.warn("Reboot error (may be expected):", error);
    },
  });
  const handleReboot = () => {
    triggerReboot();
    reboot();
  };

  return (
    <GeneralDialog
      fullWidth
      maxWidth="sm"
      onClose={isPending ? undefined : onClose}
      open
    >
      <AppDialogTitle>Restore {snapshot.source}?</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Everything in {snapshot.source} goes back to how it was on{" "}
          {formatDate(snapshot.createdAt)}. Changes made since then disappear
          from {snapshot.source}, but a snapshot of the current state is kept so
          you can undo this. Nested subvolumes are not restored.
        </AppDialogContentText>
        {isPending && (
          <>
            <AppLinearProgress
              value={progress ? (progress.step * 100) / progress.total : 0}
              variant={progress ? "determinate" : "indeterminate"}
            />
            {progress && (
              <AppTypography variant="body2">{progress.message}</AppTypography>
            )}
          </>
        )}
        {result?.rebootRequired && (
          <AppAlert
            action={
              <AppButton color="inherit" onClick={handleReboot} size="small">
                Reboot now
              </AppButton>
            }
            severity="warning"
          >
            Reboot required to finish restoring {snapshot.source}.
          </AppAlert>
        )}
        {result?.leftoverPath && (
          <AppAlert severity="info">
            The previous contents were kept at {result.leftoverPath}.
          </AppAlert>
        )}
        {result?.skippedMounts && (
          <AppAlert severity="info">
            These mounts show a path inside {snapshot.source} and were left
            untouched: {result.skippedMounts.join(", ")}. Remount them or reboot
            to see the restored contents.
          </AppAlert>
        )}
        {result &&
          !result.remounted &&
          !result.rebootRequired &&
          !result.skippedMounts && (
            <AppAlert severity="info">
              Changes take effect after the next mount.
            </AppAlert>
          )}
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={onClose}>
          {result ? "Close" : "Cancel"}
        </AppButton>
        {!result && (
          <AppButton
            color="error"
            disabled={isPending}
            onClick={onConfirm}
            variant="contained"
          >
            {isPending ? "Restoring..." : "Restore"}
          </AppButton>
        )}
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default BtrfsRollbackDialog;
