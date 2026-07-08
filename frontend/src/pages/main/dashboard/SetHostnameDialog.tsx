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

const DASHBOARD_TOAST_META = { href: "/", label: "Open dashboard" };

interface Props {
  current: string;
  onClose: () => void;
  open: boolean;
}

const SetHostnameDialog = ({ open, current, onClose }: Props) => {
  const toast = useScopedToast(DASHBOARD_TOAST_META);
  const [hostname, setHostname] = useState(current);

  const { mutate, isPending } = linuxio.hostname.set_hostname.useJobAction({
    success: () => {
      toast.success("Hostname updated successfully");
      onClose();
    },
    error: "Failed to update hostname",
    toast: DASHBOARD_TOAST_META,
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
