import { useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";

import { type CreateUserRequest, linuxio } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAutocomplete from "@/components/ui/AppAutocomplete";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppTextField from "@/components/ui/AppTextField";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

interface CreateUserDialogProps {
  onClose: () => void;
  open: boolean;
}

const CreateUserDialog: React.FC<CreateUserDialogProps> = ({
  open,
  onClose,
}) => {
  const toast = useScopedToast({ href: "/accounts", label: "Open accounts" });
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

    createUser(request);
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
      <AppDialogTitle>Create User</AppDialogTitle>
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
            label="Username"
            onChange={(e) => setUsername(e.target.value)}
            required
            value={username}
          />
          <AppTextField
            fullWidth
            label="Full Name"
            onChange={(e) => setFullName(e.target.value)}
            value={fullName}
          />
          <AppTextField
            fullWidth
            label="Password"
            onChange={(e) => setPassword(e.target.value)}
            required
            type="password"
            value={password}
          />
          <AppTextField
            error={confirmPassword !== "" && password !== confirmPassword}
            fullWidth
            helperText={
              confirmPassword !== "" && password !== confirmPassword
                ? "Passwords do not match"
                : ""
            }
            label="Confirm Password"
            onChange={(e) => setConfirmPassword(e.target.value)}
            required
            type="password"
            value={confirmPassword}
          />
          <AppTextField
            fullWidth
            label="Home Directory"
            onChange={(e) => setHomeDir(e.target.value)}
            placeholder={username ? `/home/${username}` : "/home/username"}
            value={homeDir}
          />
          <AppAutocomplete
            freeSolo
            fullWidth
            label="Shell"
            onChange={(value) => setShell(value || "/bin/bash")}
            onInputChange={setShell}
            options={shellsList}
            value={shell}
          />
          <AppAutocomplete
            fullWidth
            label="Secondary Groups"
            multiple
            onChange={(values) => {
              const added = values[0];
              if (added && !selectedGroups.includes(added)) {
                setSelectedGroups([...selectedGroups, added]);
              }
            }}
            options={groupsList
              .map((g) => g.name)
              .filter((g) => !selectedGroups.includes(g))}
            value={[]}
          />
          {selectedGroups.length > 0 && (
            <div
              style={{
                display: "flex",
                flexWrap: "wrap",
                gap: 6,
              }}
            >
              {selectedGroups.map((group) => (
                <Chip
                  key={group}
                  label={group}
                  onDelete={() =>
                    setSelectedGroups(selectedGroups.filter((g) => g !== group))
                  }
                  size="small"
                  variant="soft"
                />
              ))}
            </div>
          )}
          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={createHome}
                onChange={(e) => setCreateHome(e.target.checked)}
              />
            }
            label="Create home directory"
          />
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={
            isPending || !username || !password || password !== confirmPassword
          }
          onClick={handleSubmit}
          variant="contained"
        >
          {isPending ? "Creating..." : "Create"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default CreateUserDialog;
