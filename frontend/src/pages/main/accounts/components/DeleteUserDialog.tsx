import {
  Box,
  Button,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  DialogContentText,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React from "react";
import { toast } from "sonner";

import linuxio from "@/api/react-query";
import { getMutationErrorMessage } from "@/utils/mutations";

interface DeleteUserDialogProps {
  open: boolean;
  onClose: () => void;
  usernames: string[];
  onSuccess: () => void;
}

const DeleteUserDialog: React.FC<DeleteUserDialogProps> = ({
  open,
  onClose,
  usernames,
  onSuccess,
}) => {
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
      queryKey: ["linuxio", "accounts", "list_users"],
    });
    onSuccess();
    onClose();
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Delete User{usernames.length > 1 ? "s" : ""}</DialogTitle>
      <DialogContent>
        <DialogContentText>
          Are you sure you want to delete the following user
          {usernames.length > 1 ? "s" : ""}?
        </DialogContentText>
        <Box sx={{ mt: 2, mb: 1 }}>
          {usernames.map((name) => (
            <Chip key={name} label={name} size="small" sx={{ mr: 1, mb: 1 }} />
          ))}
        </Box>
        <DialogContentText sx={{ mt: 2, color: "warning.main" }}>
          This action cannot be undone. The user&apos;s home directory will also
          be deleted.
        </DialogContentText>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={isDeleting}>
          Cancel
        </Button>
        <Button
          onClick={handleDelete}
          variant="contained"
          color="error"
          disabled={isDeleting}
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default DeleteUserDialog;
