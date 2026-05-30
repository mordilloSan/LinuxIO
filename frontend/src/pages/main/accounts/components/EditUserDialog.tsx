import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";

import DeleteUserDialog from "./DeleteUserDialog";

import { type AccountUser, linuxio, type ModifyUserRequest } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAutocomplete from "@/components/ui/AppAutocomplete";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";
import useAuth from "@/hooks/useAuth";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

interface EditUserDialogProps {
  onClose: () => void;
  open: boolean;
  user: AccountUser;
}

const EditUserDialog: React.FC<EditUserDialogProps> = ({
  open,
  onClose,
  user,
}) => {
  const toast = useScopedToast({ href: "/accounts", label: "Open accounts" });
  const queryClient = useQueryClient();
  const { user: currentUser } = useAuth();
  const [fullName, setFullName] = useState(user.gecos);
  const [homeDir, setHomeDir] = useState(user.homeDir);
  const [shell, setShell] = useState(user.shell);
  const [selectedGroups, setSelectedGroups] = useState<string[]>(
    user.groups || [],
  );
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  const isProtected =
    user.username === "root" || user.username === currentUser?.name;

  const { data: shells = [] } = linuxio.accounts.list_shells.useQuery();
  const { data: groups = [] } = linuxio.accounts.list_groups.useQuery();

  const shellsList = Array.isArray(shells) ? shells : [];
  const groupsList = Array.isArray(groups) ? groups : [];

  const { mutate: modifyUser, isPending } =
    linuxio.accounts.modify_user.useMutation({
      onSuccess: () => {
        toast.success(`User "${user.username}" updated successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.accounts.list_users.queryKey(),
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

    modifyUser(request);
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <AppDialogTitle>Edit User: {user.username}</AppDialogTitle>
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
            disabled
            fullWidth
            label="Username"
            value={user.username}
          />
          <AppTextField
            fullWidth
            label="Full Name"
            onChange={(e) => setFullName(e.target.value)}
            value={fullName}
          />
          <AppTextField
            fullWidth
            label="Home Directory"
            onChange={(e) => setHomeDir(e.target.value)}
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
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton
          color="error"
          disabled={isPending || isProtected}
          onClick={() => setDeleteDialogOpen(true)}
          startIcon={<Icon height={18} icon="mdi:delete" width={18} />}
          style={{ marginRight: "auto" }}
        >
          Delete
        </AppButton>
        <AppButton disabled={isPending} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isPending}
          onClick={handleSubmit}
          variant="contained"
        >
          {isPending ? "Saving..." : "Save"}
        </AppButton>
      </AppDialogActions>

      <DeleteUserDialog
        onClose={() => setDeleteDialogOpen(false)}
        onSuccess={onClose}
        open={deleteDialogOpen}
        usernames={[user.username]}
      />
    </GeneralDialog>
  );
};

export default EditUserDialog;
