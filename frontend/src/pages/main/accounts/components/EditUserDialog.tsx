import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";

import DeleteUserDialog from "./DeleteUserDialog";

import { linuxio, type AccountUser, type ModifyUserRequest } from "@/api";
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
  open: boolean;
  onClose: () => void;
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

    modifyUser([request]);
  };

  return (
    <GeneralDialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
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
            label="Username"
            value={user.username}
            fullWidth
            disabled
          />
          <AppTextField
            label="Full Name"
            value={fullName}
            onChange={(e) => setFullName(e.target.value)}
            fullWidth
          />
          <AppTextField
            label="Home Directory"
            value={homeDir}
            onChange={(e) => setHomeDir(e.target.value)}
            fullWidth
          />
          <AppAutocomplete
            options={shellsList}
            value={shell}
            onChange={(value) => setShell(value || "/bin/bash")}
            onInputChange={setShell}
            label="Shell"
            fullWidth
            freeSolo
          />
          <AppAutocomplete
            multiple
            options={groupsList
              .map((g) => g.name)
              .filter((g) => !selectedGroups.includes(g))}
            value={[]}
            onChange={(values) => {
              const added = values[0];
              if (added && !selectedGroups.includes(added)) {
                setSelectedGroups([...selectedGroups, added]);
              }
            }}
            label="Secondary Groups"
            fullWidth
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
                  size="small"
                  variant="soft"
                  onDelete={() =>
                    setSelectedGroups(selectedGroups.filter((g) => g !== group))
                  }
                />
              ))}
            </div>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton
          onClick={() => setDeleteDialogOpen(true)}
          color="error"
          disabled={isPending || isProtected}
          startIcon={<Icon icon="mdi:delete" width={18} height={18} />}
          style={{ marginRight: "auto" }}
        >
          Delete
        </AppButton>
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

      <DeleteUserDialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        usernames={[user.username]}
        onSuccess={onClose}
      />
    </GeneralDialog>
  );
};

export default EditUserDialog;
