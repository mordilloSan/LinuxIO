import { useState } from "react";

import { getMutationErrorMessage, linuxio, type SharedFolder } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";

interface StopSharingDialogProps {
  folder: SharedFolder;
  onClose: () => void;
  onExited?: () => void;
  open: boolean;
}

/**
 * Takes a shared folder off the network. The folder, what is in it and its
 * access stay, so it can be shared again.
 */
const StopSharingDialog = ({
  folder,
  onClose,
  onExited,
  open,
}: StopSharingDialogProps) => {
  const [problem, setProblem] = useState<string | null>(null);
  const { mutate: update, isPending } =
    linuxio.shares.update_folder.useMutation({
      success: "Sharing stopped",
      error: (error) =>
        setProblem(getMutationErrorMessage(error, "Failed to stop sharing")),
      options: { onSuccess: onClose },
    });
  return (
    <GeneralDialog
      fullWidth
      maxWidth="sm"
      onClose={onClose}
      open={open}
      slotProps={{ transition: { onExited } }}
    >
      <AppDialogTitle>Stop sharing</AppDialogTitle>
      <AppDialogContent>
        <div className="app-dialog-fields">
          <AppDialogContentText>
            {`${folder.name} is taken off the network. The folder, everything in it, its snapshots and its access table stay, and it can be shared again.`}
          </AppDialogContentText>
          {problem && <AppAlert severity="error">{problem}</AppAlert>}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          disabled={isPending}
          onClick={() => {
            setProblem(null);
            update({
              uuid: folder.uuid,
              path: folder.path,
              name: folder.name,
              access: folder.access,
              everyone: folder.everyone,
              smb: false,
              nfs: [],
            });
          }}
          variant="contained"
        >
          Stop sharing
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default StopSharingDialog;
