import { useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";

import { type CreateGroupRequest, linuxio } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

interface CreateGroupDialogProps {
  onClose: () => void;
  open: boolean;
}

const CreateGroupDialog: React.FC<CreateGroupDialogProps> = ({
  open,
  onClose,
}) => {
  const toast = useScopedToast({ href: "/accounts", label: "Open accounts" });
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [gid, setGid] = useState("");

  const { mutate: createGroup, isPending } =
    linuxio.accounts.create_group.useMutation({
      onSuccess: () => {
        toast.success(`Group "${name}" created successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.accounts.list_groups.queryKey(),
        });
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(getMutationErrorMessage(error, "Failed to create group"));
      },
    });

  const handleClose = () => {
    setName("");
    setGid("");
    onClose();
  };

  const handleSubmit = () => {
    if (!name.trim()) {
      toast.error("Group name is required");
      return;
    }

    const request: CreateGroupRequest = {
      name: name.trim(),
      gid: gid ? parseInt(gid, 10) : undefined,
    };

    createGroup(request);
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
      <AppDialogTitle>Create Group</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 8,
            marginTop: 4,
          }}
        >
          <AppTextField
            autoFocus
            fullWidth
            label="Group Name"
            onChange={(e) => setName(e.target.value)}
            required
            value={name}
          />
          <AppTextField
            fullWidth
            label="GID (optional)"
            onChange={(e) => setGid(e.target.value.replace(/\D/g, ""))}
            placeholder="Auto-assigned if empty"
            type="number"
            value={gid}
          />
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isPending || !name.trim()}
          onClick={handleSubmit}
          variant="contained"
        >
          {isPending ? "Creating..." : "Create"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default CreateGroupDialog;
