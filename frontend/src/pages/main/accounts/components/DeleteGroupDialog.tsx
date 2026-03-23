import { useAppTheme } from "@/theme";
import { useQueryClient } from "@tanstack/react-query";
import React from "react";
import { toast } from "sonner";

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
  const theme = useAppTheme();
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
      <AppDialogTitle>
        Delete Group{groupNames.length > 1 ? "s" : ""}
      </AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to delete the following group
          {groupNames.length > 1 ? "s" : ""}?
        </AppDialogContentText>
        <div
          style={{
            marginTop: theme.spacing(2),
            marginBottom: theme.spacing(1),
          }}
        >
          {groupNames.map((name) => (
            <Chip
              key={name}
              label={name}
              size="small"
              variant="soft"
              style={{ marginRight: 4, marginBottom: 4 }}
            />
          ))}
        </div>
        <AppDialogContentText
          style={{ marginTop: 8, color: "var(--mui-palette-warning-main)" }}
        >
          This action cannot be undone. Groups that are primary groups for users
          cannot be deleted.
        </AppDialogContentText>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose} disabled={isDeleting}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleDelete}
          variant="contained"
          color="error"
          disabled={isDeleting}
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default DeleteGroupDialog;
