import { useState } from "react";

import type { MountpointRemoveFstabRequest } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";

interface UnmountFilesystemDialogProps {
  isPending: boolean;
  mountpoint: string;
  onClose: () => void;
  onConfirm: (request: MountpointRemoveFstabRequest) => void;
}

/** Unmounts a filesystem, optionally dropping its /etc/fstab entry too. */
const UnmountFilesystemDialog = ({
  isPending,
  mountpoint,
  onClose,
  onConfirm,
}: UnmountFilesystemDialogProps) => {
  const [removeFstab, setRemoveFstab] = useState(false);

  return (
    <GeneralDialog
      fullWidth
      maxWidth="sm"
      onClose={isPending ? undefined : onClose}
      open
    >
      <AppDialogTitle>Unmount filesystem</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>{`Unmount ${mountpoint}?`}</AppDialogContentText>
        <AppFormControlLabel
          control={
            <AppCheckbox
              checked={removeFstab}
              onChange={(_, checked) => setRemoveFstab(checked)}
            />
          }
          label="Also remove from /etc/fstab"
        />
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isPending}
          onClick={() =>
            onConfirm({ mountpoint, removeFstab: removeFstab ? "true" : "" })
          }
          variant="contained"
        >
          {isPending ? "Unmounting..." : "Unmount"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default UnmountFilesystemDialog;
