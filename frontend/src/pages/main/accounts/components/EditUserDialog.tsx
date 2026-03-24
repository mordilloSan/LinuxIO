import { useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";
import { toast } from "sonner";

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
import { useAppTheme } from "@/theme";
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
  const theme = useAppTheme();
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

    modifyUser([JSON.stringify(request)]);
  };

  return (
    <GeneralDialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>Edit User: {user.username}</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: theme.spacing(2),
            marginTop: theme.spacing(1),
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
            options={groupsList.map((g) => g.name)}
            value={selectedGroups}
            onChange={setSelectedGroups}
            label="Secondary Groups"
            fullWidth
            renderValue={(value, getItemProps) =>
              value.map((option, index) => {
                const itemProps = getItemProps({ index });
                const { key, ...chipProps } = itemProps;
                return (
                  <Chip
                    key={key}
                    label={option}
                    size="small"
                    variant="soft"
                    {...chipProps}
                  />
                );
              })
            }
          />
        </div>
      </AppDialogContent>
      <AppDialogActions>
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
    </GeneralDialog>
  );
};

export default EditUserDialog;
