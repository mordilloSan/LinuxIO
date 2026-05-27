import { useQueryClient } from "@tanstack/react-query";
import React from "react";

import { linuxio } from "@/api";
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
import { getMutationErrorMessage } from "@/utils/mutations";

interface DeleteUserDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
  usernames: string[];
}

const DeleteUserDialog: React.FC<DeleteUserDialogProps> = ({
  open,
  onClose,
  usernames,
  onSuccess,
}) => {
  const theme = useAppTheme();
  const toast = useScopedToast({ href: "/accounts", label: "Open accounts" });
  const queryClient = useQueryClient();

  const { mutateAsync: deleteUser, isPending: isDeleting } =
    linuxio.accounts.delete_user.useMutation({
      onError: (error: Error) => {
        toast.error(getMutationErrorMessage(error, "Failed to delete user(s)"));
      },
    });

  const handleDelete = async () => {
    for (const username of usernames) {
      await deleteUser([username]);
    }
    const successMessage =
      usernames.length === 1
        ? `User "${usernames[0]}" deleted successfully`
        : `${usernames.length} users deleted successfully`;
    toast.success(successMessage);
    queryClient.invalidateQueries({
      queryKey: linuxio.accounts.list_users.queryKey(),
    });
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
          style={{ marginTop: 8, color: "var(--mui-palette-warning-main)" }}
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
