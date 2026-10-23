import { linuxio, useCallMutation } from "@/api";
import BatchDeleteDialog from "@/components/dialog/BatchDeleteDialog";

const ACCOUNTS_TOAST_META = {
  label: "Open accounts",
  to: "/accounts",
} as const;

interface DeleteGroupDialogProps {
  groupNames: string[];
  onClose: () => void;
  onExited?: () => void;
  onSuccess: () => void;
  open: boolean;
}

const DeleteGroupDialog = ({
  open,
  onClose,
  onExited,
  groupNames,
  onSuccess,
}: DeleteGroupDialogProps) => {
  // Configless: this is a batch flow — the dialog owns aggregation and toasts.
  const { mutateAsync: deleteGroup } = useCallMutation(
    linuxio.accounts.delete_group,
  );

  return (
    <BatchDeleteDialog
      items={groupNames.map((name) => ({ key: name, label: name }))}
      noun="group"
      onClose={onClose}
      onDeleteOne={({ key }) => deleteGroup({ groupName: key })}
      onExited={onExited}
      onSuccess={onSuccess}
      open={open}
      toastMeta={ACCOUNTS_TOAST_META}
      warning="Groups that are primary groups for users cannot be deleted."
    />
  );
};

export default DeleteGroupDialog;
