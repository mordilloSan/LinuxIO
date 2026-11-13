import { linuxio } from "@/api";
import BatchDeleteDialog from "@/components/dialog/BatchDeleteDialog";

const ACCOUNTS_TOAST_META = {
  label: "Open accounts",
  to: "/accounts",
} as const;

interface DeleteUserDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
  usernames: string[];
}

const DeleteUserDialog = ({
  open,
  onClose,
  usernames,
  onSuccess,
}: DeleteUserDialogProps) => {
  // Configless: this is a batch flow — the dialog owns aggregation and toasts.
  const { mutateAsync: deleteUser } =
    linuxio.accounts.delete_user.useMutation();

  return (
    <BatchDeleteDialog
      items={usernames.map((username) => ({ key: username, label: username }))}
      noun="user"
      onClose={onClose}
      onDeleteOne={({ key }) => deleteUser({ username: key })}
      onSuccess={onSuccess}
      open={open}
      toastMeta={ACCOUNTS_TOAST_META}
      warning="The user's home directory will also be deleted."
    />
  );
};

export default DeleteUserDialog;
