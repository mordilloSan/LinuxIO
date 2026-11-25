import { useState } from "react";

import { linuxio } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";

interface RenamePoolDialogProps {
  label: string;
  onClose: () => void;
  uuid: string;
}

/** Sets a pool's name (its btrfs label). */
const RenamePoolDialog = ({ label, onClose, uuid }: RenamePoolDialogProps) => {
  const [name, setName] = useState(label);
  const { mutate: renamePool, isPending } =
    linuxio.storage.btrfs_set_label.useMutation({
      success: "Pool renamed",
      error: "Failed to rename pool",
    });
  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open>
      <AppDialogTitle>Rename pool</AppDialogTitle>
      <AppDialogContent>
        <AppTextField
          autoFocus
          fullWidth
          label="Name"
          onChange={(event) => setName(event.target.value)}
          value={name}
        />
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          disabled={isPending || name.trim() === ""}
          onClick={() =>
            renamePool({ uuid, label: name.trim() }, { onSuccess: onClose })
          }
          variant="contained"
        >
          Rename
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default RenamePoolDialog;
