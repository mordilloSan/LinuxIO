import { useState } from "react";

import { linuxio, useCallMutation } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";
import { useScopedToast } from "@/hooks/useScopedToast";

const DASHBOARD_TOAST_META = { label: "Open dashboard", to: "/" } as const;

interface Props {
  current: string;
  onClose: () => void;
  open: boolean;
}

interface HostnameSession {
  open: boolean;
  source: string;
  value: string;
}

const SetHostnameDialog = ({ open, current, onClose }: Props) => {
  const toast = useScopedToast(DASHBOARD_TOAST_META);
  const [storedSession, setStoredSession] = useState<HostnameSession>(() => ({
    open,
    source: current,
    value: current,
  }));
  const sessionIsCurrent =
    storedSession.open === open && storedSession.source === current;
  const hostname = sessionIsCurrent ? storedSession.value : current;

  // Reset synchronously when the backend-provided current value changes,
  // while retaining an in-progress edit across ordinary parent rerenders.
  if (!sessionIsCurrent) {
    setStoredSession({ open, source: current, value: current });
  }

  const { mutate, isPending } = useCallMutation(linuxio.hostname.set_hostname, {
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
    if (isValid && !isPending) mutate({ hostname });
  };

  const handleClose = () => {
    if (!isPending) onClose();
  };

  return (
    <GeneralDialog
      aria-busy={isPending || undefined}
      disableEscapeKeyDown={isPending}
      fullWidth
      maxWidth="xs"
      onClose={handleClose}
      open={open}
    >
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
          disabled={isPending}
          onChange={(e) =>
            setStoredSession({ open, source: current, value: e.target.value })
          }
          onKeyDown={(e) => {
            if (e.key === "Enter" && isValid && !isPending) handleSave();
          }}
          type="text"
          value={hostname}
          variant="outlined"
        />
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={!isValid || isPending}
          onClick={handleSave}
          variant="contained"
        >
          {isPending ? "Saving…" : "Save"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default SetHostnameDialog;
