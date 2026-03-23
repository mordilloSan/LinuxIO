import { Autocomplete } from "@mui/material";
import { useAppTheme } from "@/theme";
import { useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";
import { toast } from "sonner";

import {
  linuxio,
  type AccountGroup,
  type ModifyGroupMembersRequest,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";
import { getMutationErrorMessage } from "@/utils/mutations";

interface EditGroupMembersDialogProps {
  open: boolean;
  onClose: () => void;
  group: AccountGroup;
}

const EditGroupMembersDialog: React.FC<EditGroupMembersDialogProps> = ({
  open,
  onClose,
  group,
}) => {
  const theme = useAppTheme();
  const queryClient = useQueryClient();
  const [selectedMembers, setSelectedMembers] = useState<string[]>(
    group.members,
  );

  const { data: users = [] } = linuxio.accounts.list_users.useQuery();

  const usersList = Array.isArray(users) ? users : [];

  const { mutate: modifyGroupMembers, isPending } =
    linuxio.accounts.modify_group_members.useMutation({
      onSuccess: () => {
        toast.success(`Group "${group.name}" members updated`);
        queryClient.invalidateQueries({
          queryKey: linuxio.accounts.list_groups.queryKey(),
        });
        queryClient.invalidateQueries({
          queryKey: linuxio.accounts.list_users.queryKey(),
        });
        onClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to update group members"),
        );
      },
    });

  const handleSubmit = () => {
    // Check if anything changed
    const sortedCurrent = [...group.members].sort();
    const sortedNew = [...selectedMembers].sort();
    if (JSON.stringify(sortedCurrent) === JSON.stringify(sortedNew)) {
      toast.info("No changes to save");
      onClose();
      return;
    }

    const request: ModifyGroupMembersRequest = {
      groupName: group.name,
      members: selectedMembers,
    };

    modifyGroupMembers([JSON.stringify(request)]);
  };

  return (
    <GeneralDialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>Edit Group Members: {group.name}</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: theme.spacing(2),
            marginTop: theme.spacing(1),
          }}
        >
          <Autocomplete
            multiple
            options={usersList.map((u) => u.username)}
            value={selectedMembers}
            onChange={(_, value) => setSelectedMembers(value)}
            renderInput={(params) => (
              <AppTextField {...params} label="Members" fullWidth />
            )}
            renderValue={(value, getItemProps) =>
              value.map((option, index) => {
                const itemProps = getItemProps({ index });
                const { key, ...chipProps } = itemProps;
                return (
                  <Chip
                    key={key}
                    label={option}
                    size="small"
                    variant="soft"
                    {...chipProps}
                  />
                );
              })
            }
          />
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose} disabled={isPending}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleSubmit}
          variant="contained"
          disabled={isPending}
        >
          {isPending ? "Saving..." : "Save"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default EditGroupMembersDialog;
