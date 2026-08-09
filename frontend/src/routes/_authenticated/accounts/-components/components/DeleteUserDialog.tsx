import { linuxio, useCallMutation } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";

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
  const theme = useAppTheme();
  const toast = useScopedToast(ACCOUNTS_TOAST_META);

  // Configless: this is a batch flow — the caller owns aggregation and toasts.
  const { mutateAsync: deleteUser, isPending: isDeleting } = useCallMutation(
    linuxio.accounts.delete_user,
  );

  const handleDelete = async () => {
    const failures: string[] = [];
    for (const username of usernames) {
      try {
        await deleteUser({ username });
      } catch {
        failures.push(username);
      }
    }
    if (failures.length > 0) {
      toast.error(
        `Failed to delete ${failures.length} of ${usernames.length} user${usernames.length === 1 ? "" : "s"}`,
      );
    } else {
      const successMessage =
        usernames.length === 1
          ? `User "${usernames[0]}" deleted successfully`
          : `${usernames.length} users deleted successfully`;
      toast.success(successMessage);
    }
    onSuccess();
    onClose();
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <AppDialogTitle>
        Delete User{usernames.length > 1 ? "s" : ""}
      </AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to delete the following user
          {usernames.length > 1 ? "s" : ""}?
        </AppDialogContentText>
        <div
          style={{
            marginTop: theme.spacing(2),
            marginBottom: theme.spacing(1),
          }}
        >
          {usernames.map((name) => (
            <Chip
              key={name}
              label={name}
              size="small"
              style={{ marginRight: 4, marginBottom: 4 }}
              variant="soft"
            />
          ))}
        </div>
        <AppDialogContentText
          style={{ marginTop: 8, color: "var(--app-palette-warning-main)" }}
        >
          This action cannot be undone. The user&apos;s home directory will also
          be deleted.
        </AppDialogContentText>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isDeleting} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isDeleting}
          onClick={handleDelete}
          variant="contained"
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default DeleteUserDialog;
