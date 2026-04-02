import { useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";
import { toast } from "sonner";

import { linuxio } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";
import { getMutationErrorMessage } from "@/utils/mutations";

interface Props {
  open: boolean;
  current: string;
  onClose: () => void;
}

const SetHostnameDialog: React.FC<Props> = ({ open, current, onClose }) => {
  const queryClient = useQueryClient();
  const [hostname, setHostname] = useState(current);

  const { mutate, isPending } = linuxio.dbus.set_hostname.useMutation({
    onSuccess: () => {
      toast.success("Hostname updated successfully");
      queryClient.invalidateQueries({
        queryKey: linuxio.system.get_host_info.queryKey(),
      });
      onClose();
    },
    onError: (error: Error) => {
      toast.error(getMutationErrorMessage(error, "Failed to update hostname"));
    },
  });

  const isValid = /^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/.test(
    hostname,
  );

  const handleSave = () => {
    if (isValid) mutate([hostname]);
  };

  return (
    <GeneralDialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <AppDialogTitle>Set Hostname</AppDialogTitle>
      <AppDialogContent>
        <AppTextField
          autoFocus
          label="Hostname"
          type="text"
          fullWidth
          variant="outlined"
          value={hostname}
          onChange={(e) => setHostname(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && isValid) handleSave();
          }}
          error={hostname.length > 0 && !isValid}
          helperText={
            hostname.length > 0 && !isValid
              ? "Only letters, numbers, and hyphens; cannot start or end with a hyphen"
              : undefined
          }
        />
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          onClick={handleSave}
          disabled={!isValid || isPending}
          variant="contained"
        >
          Save
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default SetHostnameDialog;
