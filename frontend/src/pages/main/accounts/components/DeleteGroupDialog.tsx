import {
  Button,
  Chip,
  DialogTitle,
  DialogContent,
  DialogActions,
  DialogContentText,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { useQueryClient } from "@tanstack/react-query";
import React from "react";
import { toast } from "sonner";

import { linuxio } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import { getMutationErrorMessage } from "@/utils/mutations";

interface DeleteGroupDialogProps {
  open: boolean;
  onClose: () => void;
  groupNames: string[];
  onSuccess: () => void;
}

const DeleteGroupDialog: React.FC<DeleteGroupDialogProps> = ({
  open,
  onClose,
  groupNames,
  onSuccess,
}) => {
  const theme = useTheme();
  const queryClient = useQueryClient();

  const { mutateAsync: deleteGroup, isPending: isDeleting } =
    linuxio.accounts.delete_group.useMutation({
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to delete group(s)"),
        );
      },
    });

  const handleDelete = async () => {
    for (const name of groupNames) {
      await deleteGroup([name]);
    }
    const successMessage =
      groupNames.length === 1
        ? `Group "${groupNames[0]}" deleted successfully`
        : `${groupNames.length} groups deleted successfully`;
    toast.success(successMessage);
    queryClient.invalidateQueries({
      queryKey: linuxio.accounts.list_groups.queryKey(),
    });
    onSuccess();
    onClose();
  };

  return (
    <GeneralDialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Delete Group{groupNames.length > 1 ? "s" : ""}</DialogTitle>
      <DialogContent>
        <DialogContentText>
          Are you sure you want to delete the following group
          {groupNames.length > 1 ? "s" : ""}?
        </DialogContentText>
        <div
          style={{
            marginTop: theme.spacing(2),
            marginBottom: theme.spacing(1),
          }}
        >
          {groupNames.map((name) => (
            <Chip key={name} label={name} size="small" sx={{ mr: 1, mb: 1 }} />
          ))}
        </div>
        <DialogContentText sx={{ mt: 2, color: "warning.main" }}>
          This action cannot be undone. Groups that are primary groups for users
          cannot be deleted.
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
    </GeneralDialog>
  );
};

export default DeleteGroupDialog;
