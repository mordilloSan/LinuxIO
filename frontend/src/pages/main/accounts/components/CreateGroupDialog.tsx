import {
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Box,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";
import { toast } from "sonner";

import { linuxio, type CreateGroupRequest } from "@/api";
import { getMutationErrorMessage } from "@/utils/mutations";

interface CreateGroupDialogProps {
  open: boolean;
  onClose: () => void;
}

const CreateGroupDialog: React.FC<CreateGroupDialogProps> = ({
  open,
  onClose,
}) => {
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

    createGroup([JSON.stringify(request)]);
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>Create Group</DialogTitle>
      <DialogContent>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2, mt: 1 }}>
          <TextField
            label="Group Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            fullWidth
            required
            autoFocus
          />
          <TextField
            label="GID (optional)"
            value={gid}
            onChange={(e) => setGid(e.target.value.replace(/\D/g, ""))}
            fullWidth
            placeholder="Auto-assigned if empty"
            type="number"
          />
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={isPending}>
          Cancel
        </Button>
        <Button
          onClick={handleSubmit}
          variant="contained"
          disabled={isPending || !name.trim()}
        >
          {isPending ? "Creating..." : "Create"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default CreateGroupDialog;
