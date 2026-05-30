import { useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";

import {
  type AccountGroup,
  linuxio,
  type ModifyGroupMembersRequest,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAutocomplete from "@/components/ui/AppAutocomplete";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

interface EditGroupMembersDialogProps {
  group: AccountGroup;
  onClose: () => void;
  open: boolean;
}

const EditGroupMembersDialog: React.FC<EditGroupMembersDialogProps> = ({
  open,
  onClose,
  group,
}) => {
  const toast = useScopedToast({ href: "/accounts", label: "Open accounts" });
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

    modifyGroupMembers(request);
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <AppDialogTitle
        style={{
          fontSize: "1rem",
          fontWeight: 600,
          padding: "12px 20px",
          lineHeight: 1.4,
        }}
      >
        Edit Group Members: {group.name}
      </AppDialogTitle>
      <AppDialogContent style={{ padding: "12px 20px", fontSize: "0.85rem" }}>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 8,
            marginTop: 4,
          }}
        >
          <AppAutocomplete
            fullWidth
            label="Members"
            multiple
            onChange={(values) => {
              const added = values[0];
              if (added && !selectedMembers.includes(added)) {
                setSelectedMembers([...selectedMembers, added]);
              }
            }}
            options={usersList
              .map((u) => u.username)
              .filter((u) => !selectedMembers.includes(u))}
            size="small"
            value={[]}
          />
          {selectedMembers.length > 0 && (
            <div
              style={{
                display: "flex",
                flexWrap: "wrap",
                gap: 6,
              }}
            >
              {selectedMembers.map((member) => (
                <Chip
                  key={member}
                  label={member}
                  onDelete={() =>
                    setSelectedMembers(
                      selectedMembers.filter((m) => m !== member),
                    )
                  }
                  size="small"
                  style={{ fontSize: "0.7rem", height: 22 }}
                  variant="soft"
                />
              ))}
            </div>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={onClose} size="small">
          Cancel
        </AppButton>
        <AppButton
          disabled={isPending}
          onClick={handleSubmit}
          size="small"
          variant="contained"
        >
          {isPending ? "Saving..." : "Save"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default EditGroupMembersDialog;
