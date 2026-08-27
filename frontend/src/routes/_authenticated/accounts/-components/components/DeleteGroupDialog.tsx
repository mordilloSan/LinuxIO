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

const ACCOUNTS_TOAST_META = {
  label: "Open accounts",
  to: "/accounts",
} as const;

interface DeleteGroupDialogProps {
  groupNames: string[];
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}

const DeleteGroupDialog = ({
  open,
  onClose,
  groupNames,
  onSuccess,
}: DeleteGroupDialogProps) => {
  const toast = useScopedToast(ACCOUNTS_TOAST_META);

  // Configless: this is a batch flow — the caller owns aggregation and toasts.
  const { mutateAsync: deleteGroup, isPending: isDeleting } = useCallMutation(
    linuxio.accounts.delete_group,
  );

  const handleDelete = async () => {
    const failures: string[] = [];
    for (const name of groupNames) {
      try {
        await deleteGroup({ groupName: name });
      } catch {
        failures.push(name);
      }
    }
    if (failures.length > 0) {
      toast.error(
        `Failed to delete ${failures.length} of ${groupNames.length} group${groupNames.length === 1 ? "" : "s"}`,
      );
    } else {
      const successMessage =
        groupNames.length === 1
          ? `Group "${groupNames[0]}" deleted successfully`
          : `${groupNames.length} groups deleted successfully`;
      toast.success(successMessage);
    }
    onSuccess();
    onClose();
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
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
            marginTop: "var(--app-space-8)",
            marginBottom: "var(--app-space-4)",
          }}
        >
          {groupNames.map((name) => (
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
          This action cannot be undone. Groups that are primary groups for users
          cannot be deleted.
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

export default DeleteGroupDialog;
