import { linuxio } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";

export interface DestroyTarget {
  uuid: string;
  name: string;
  disks: string[];
  /** A pool found on disk but not mounted has nothing to unmount. */
  mounted: boolean;
}

interface DestroyPoolDialogProps {
  onClose: () => void;
  onDestroyed?: () => void;
  /** The pool to destroy; null keeps the dialog closed. */
  target: DestroyTarget | null;
}

/**
 * Destroys a pool, mounted or not: the pool page and the Disks overview's
 * card of a pool that is not mounted both open it.
 */
const DestroyPoolDialog = ({
  onClose,
  onDestroyed,
  target,
}: DestroyPoolDialogProps) => {
  // Closing and leaving run with the mutation, not the call: the pool page
  // that opened the dialog is gone once the refreshed list no longer has it.
  const { mutate: destroyPool, isPending } =
    linuxio.storage.btrfs_pool_destroy.useMutation({
      success: "Pool destroyed",
      error: "Failed to destroy pool",
      options: {
        onSuccess: () => {
          onClose();
          onDestroyed?.();
        },
      },
    });
  return (
    <GeneralDialog
      fullWidth
      maxWidth="sm"
      onClose={onClose}
      open={target !== null}
    >
      <AppDialogTitle>Destroy pool</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          {target &&
            `Destroy ${target.name}? It is ${target.mounted ? "unmounted, " : ""}no longer mounted at startup, and erased from ${target.disks.join(", ")}. Everything on it is lost.`}
        </AppDialogContentText>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          color="error"
          disabled={isPending}
          onClick={() => {
            if (!target) return;
            destroyPool({ uuid: target.uuid });
          }}
          variant="contained"
        >
          Destroy
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default DestroyPoolDialog;
