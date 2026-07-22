import { Icon } from "@iconify/react";
import { useState } from "react";

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

import DeleteUserDialog from "./DeleteUserDialog";

const ACCOUNTS_TOAST_META = { href: "/accounts", label: "Open accounts" };

interface EditUserDialogProps {
  onClose: () => void;
  open: boolean;
  user: AccountUser;
}

const EditUserDialog = ({ open, onClose, user }: EditUserDialogProps) => {
  const toast = useScopedToast(ACCOUNTS_TOAST_META);
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

  const { data: shells = [], isLoading: shellsLoading } =
    linuxio.accounts.list_shells.useQuery({ enabled: open });
  const { data: groups = [], isLoading: groupsLoading } =
    linuxio.accounts.list_groups.useQuery({ enabled: open });

  const shellsList = Array.isArray(shells) ? shells : [];
  const groupsList = Array.isArray(groups) ? groups : [];

  const { mutate: modifyUser, isPending } =
    linuxio.accounts.modify_user.useJobAction({
      success: () => {
        toast.success(`User "${user.username}" updated successfully`);
        onClose();
      },
      error: "Failed to update user",
      toast: ACCOUNTS_TOAST_META,
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
            loading={shellsLoading}
            onChange={(value) => setShell(value || "/bin/bash")}
            onInputChange={setShell}
            options={shellsList}
            value={shell}
          />
          <AppAutocomplete
            fullWidth
            label="Secondary Groups"
            loading={groupsLoading}
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
