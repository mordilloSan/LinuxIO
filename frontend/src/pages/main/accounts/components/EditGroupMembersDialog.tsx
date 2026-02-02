import {
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Autocomplete,
  Chip,
  Box,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useState, useEffect, useEffectEvent } from "react";
import { toast } from "sonner";

import type {
  AccountGroup,
  ModifyGroupMembersRequest,
} from "@/api/linuxio-types";
import linuxio from "@/api/react-query";
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
  const queryClient = useQueryClient();
  const [selectedMembers, setSelectedMembers] = useState<string[]>(
    group.members,
  );

  const { data: users = [] } = linuxio.accounts.list_users.useQuery();

  const usersList = Array.isArray(users) ? users : [];

  const syncMembers = useEffectEvent(() => {
    setSelectedMembers(group.members);
  });

  useEffect(() => {
    syncMembers();
  }, [group]);

  const { mutate: modifyGroupMembers, isPending } =
    linuxio.accounts.modify_group_members.useMutation({
      onSuccess: () => {
        toast.success(`Group "${group.name}" members updated`);
        queryClient.invalidateQueries({
          queryKey: ["linuxio", "accounts", "list_groups"],
        });
        queryClient.invalidateQueries({
          queryKey: ["linuxio", "accounts", "list_users"],
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
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Edit Group Members: {group.name}</DialogTitle>
      <DialogContent>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2, mt: 1 }}>
          <Autocomplete
            multiple
            options={usersList.map((u) => u.username)}
            value={selectedMembers}
            onChange={(_, value) => setSelectedMembers(value)}
            renderInput={(params) => (
              <TextField {...params} label="Members" fullWidth />
            )}
            renderValue={(value, getItemProps) =>
              value.map((option, index) => {
                const itemProps = getItemProps({ index });
                const { key, ...chipProps } = itemProps;
                return (
                  <Chip key={key} label={option} size="small" {...chipProps} />
                );
              })
            }
          />
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={isPending}>
          Cancel
        </Button>
        <Button onClick={handleSubmit} variant="contained" disabled={isPending}>
          {isPending ? "Saving..." : "Save"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default EditGroupMembersDialog;
