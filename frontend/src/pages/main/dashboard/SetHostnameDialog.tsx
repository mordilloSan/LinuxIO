import { useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";

import { linuxio } from "@/api";
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

interface Props {
  current: string;
  onClose: () => void;
  open: boolean;
}

const SetHostnameDialog: React.FC<Props> = ({ open, current, onClose }) => {
  const toast = useScopedToast({ href: "/", label: "Open dashboard" });
  const queryClient = useQueryClient();
  const [hostname, setHostname] = useState(current);

  const { mutate, isPending } = linuxio.hostname.set_hostname.useMutation({
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
    if (isValid) mutate({ hostname });
  };

  return (
    <GeneralDialog fullWidth maxWidth="xs" onClose={onClose} open={open}>
      <AppDialogTitle>Set Hostname</AppDialogTitle>
      <AppDialogContent>
        <AppTextField
          autoFocus
          error={hostname.length > 0 && !isValid}
          fullWidth
          helperText={
            hostname.length > 0 && !isValid
              ? "Only letters, numbers, and hyphens; cannot start or end with a hyphen"
              : undefined
          }
          label="Hostname"
          onChange={(e) => setHostname(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && isValid) handleSave();
          }}
          type="text"
          value={hostname}
          variant="outlined"
        />
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          disabled={!isValid || isPending}
          onClick={handleSave}
          variant="contained"
        >
          Save
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default SetHostnameDialog;
