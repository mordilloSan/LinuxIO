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

import type { AccountUser, ModifyUserRequest } from "@/api/linuxio-types";
import linuxio from "@/api/react-query";
import { getMutationErrorMessage } from "@/utils/mutations";

interface EditUserDialogProps {
  open: boolean;
  onClose: () => void;
  user: AccountUser;
}

const EditUserDialog: React.FC<EditUserDialogProps> = ({
  open,
  onClose,
  user,
}) => {
  const queryClient = useQueryClient();
  const [fullName, setFullName] = useState(user.gecos);
  const [homeDir, setHomeDir] = useState(user.homeDir);
  const [shell, setShell] = useState(user.shell);
  const [selectedGroups, setSelectedGroups] = useState<string[]>(
    user.groups || [],
  );

  const { data: shells = [] } = linuxio.accounts.list_shells.useQuery();
  const { data: groups = [] } = linuxio.accounts.list_groups.useQuery();

  const shellsList = Array.isArray(shells) ? shells : [];
  const groupsList = Array.isArray(groups) ? groups : [];

  const syncUserState = useEffectEvent(() => {
    setFullName(user.gecos);
    setHomeDir(user.homeDir);
    setShell(user.shell);
    setSelectedGroups(user.groups || []);
  });

  useEffect(() => {
    syncUserState();
  }, [user]);

  const { mutate: modifyUser, isPending } =
    linuxio.accounts.modify_user.useMutation({
      onSuccess: () => {
        toast.success(`User "${user.username}" updated successfully`);
        queryClient.invalidateQueries({
          queryKey: ["linuxio", "accounts", "list_users"],
        });
        onClose();
      },
      onError: (error: Error) => {
        toast.error(getMutationErrorMessage(error, "Failed to update user"));
      },
    });

  const handleSubmit = () => {
    const request: ModifyUserRequest = {
      username: user.username,
      fullName: fullName !== user.gecos ? fullName : undefined,
      homeDir: homeDir !== user.homeDir ? homeDir : undefined,
      shell: shell !== user.shell ? shell : undefined,
      groups:
        JSON.stringify(selectedGroups) !== JSON.stringify(user.groups || [])
          ? selectedGroups
          : undefined,
    };

    // Check if anything changed
    if (
      !request.fullName &&
      !request.homeDir &&
      !request.shell &&
      !request.groups
    ) {
      toast.info("No changes to save");
      onClose();
      return;
    }

    modifyUser([JSON.stringify(request)]);
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Edit User: {user.username}</DialogTitle>
      <DialogContent>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2, mt: 1 }}>
          <TextField
            label="Username"
            value={user.username}
            fullWidth
            disabled
          />
          <TextField
            label="Full Name"
            value={fullName}
            onChange={(e) => setFullName(e.target.value)}
            fullWidth
          />
          <TextField
            label="Home Directory"
            value={homeDir}
            onChange={(e) => setHomeDir(e.target.value)}
            fullWidth
          />
          <Autocomplete
            options={shellsList}
            value={shell}
            onChange={(_, value) => setShell(value || "/bin/bash")}
            renderInput={(params) => (
              <TextField {...params} label="Shell" fullWidth />
            )}
            freeSolo
          />
          <Autocomplete
            multiple
            options={groupsList.map((g) => g.name)}
            value={selectedGroups}
            onChange={(_, value) => setSelectedGroups(value)}
            renderInput={(params) => (
              <TextField {...params} label="Secondary Groups" fullWidth />
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

export default EditUserDialog;
