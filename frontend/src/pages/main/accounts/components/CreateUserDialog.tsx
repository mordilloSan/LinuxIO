import {
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  FormControlLabel,
  Checkbox,
  Autocomplete,
  Chip,
  Box,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useState, useEffect } from "react";
import { toast } from "sonner";

import { linuxio, type CreateUserRequest } from "@/api";
import { getMutationErrorMessage } from "@/utils/mutations";

interface CreateUserDialogProps {
  open: boolean;
  onClose: () => void;
}

const CreateUserDialog: React.FC<CreateUserDialogProps> = ({
  open,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [fullName, setFullName] = useState("");
  const [homeDir, setHomeDir] = useState("");
  const [shell, setShell] = useState("/bin/bash");
  const [selectedGroups, setSelectedGroups] = useState<string[]>([]);
  const [createHome, setCreateHome] = useState(true);

  const { data: shells = [] } = linuxio.accounts.list_shells.useQuery();
  const { data: groups = [] } = linuxio.accounts.list_groups.useQuery();

  const shellsList = Array.isArray(shells) ? shells : [];
  const groupsList = Array.isArray(groups) ? groups : [];

  const { mutate: createUser, isPending } =
    linuxio.accounts.create_user.useMutation({
      onSuccess: () => {
        toast.success(`User "${username}" created successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.accounts.list_users.queryKey(),
        });
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(getMutationErrorMessage(error, "Failed to create user"));
      },
    });

  useEffect(() => {
    if (username && !homeDir) {
      // Auto-suggest home directory
    }
  }, [username, homeDir]);

  const handleClose = () => {
    setUsername("");
    setPassword("");
    setConfirmPassword("");
    setFullName("");
    setHomeDir("");
    setShell("/bin/bash");
    setSelectedGroups([]);
    setCreateHome(true);
    onClose();
  };

  const handleSubmit = () => {
    if (!username.trim()) {
      toast.error("Username is required");
      return;
    }
    if (!password) {
      toast.error("Password is required");
      return;
    }
    if (password !== confirmPassword) {
      toast.error("Passwords do not match");
      return;
    }

    const request: CreateUserRequest = {
      username: username.trim(),
      password,
      fullName: fullName.trim() || undefined,
      homeDir: homeDir.trim() || undefined,
      shell: shell || undefined,
      groups: selectedGroups.length > 0 ? selectedGroups : undefined,
      createHome,
    };

    createUser([JSON.stringify(request)]);
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>Create User</DialogTitle>
      <DialogContent>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2, mt: 1 }}>
          <TextField
            label="Username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            fullWidth
            required
            autoFocus
          />
          <TextField
            label="Full Name"
            value={fullName}
            onChange={(e) => setFullName(e.target.value)}
            fullWidth
          />
          <TextField
            label="Password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            fullWidth
            required
          />
          <TextField
            label="Confirm Password"
            type="password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            fullWidth
            required
            error={confirmPassword !== "" && password !== confirmPassword}
            helperText={
              confirmPassword !== "" && password !== confirmPassword
                ? "Passwords do not match"
                : ""
            }
          />
          <TextField
            label="Home Directory"
            value={homeDir}
            onChange={(e) => setHomeDir(e.target.value)}
            fullWidth
            placeholder={username ? `/home/${username}` : "/home/username"}
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
          <FormControlLabel
            control={
              <Checkbox
                checked={createHome}
                onChange={(e) => setCreateHome(e.target.checked)}
              />
            }
            label="Create home directory"
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
          disabled={
            isPending || !username || !password || password !== confirmPassword
          }
        >
          {isPending ? "Creating..." : "Create"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default CreateUserDialog;
