import { useState } from "react";

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

const ACCOUNTS_TOAST_META = {
  label: "Open accounts",
  to: "/accounts",
} as const;

interface ChangePasswordDialogProps {
  onClose: () => void;
  open: boolean;
  username: string;
}

const ChangePasswordDialog = ({
  open,
  onClose,
  username,
}: ChangePasswordDialogProps) => {
  const toast = useScopedToast(ACCOUNTS_TOAST_META);
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");

  const { mutate: changePassword, isPending } =
    linuxio.accounts.change_password.useAction({
      success: () => {
        toast.success(`Password changed for "${username}"`);
        handleClose();
      },
      error: "Failed to change password",
      toast: ACCOUNTS_TOAST_META,
    });

  const handleClose = () => {
    setPassword("");
    setConfirmPassword("");
    onClose();
  };

  const handleSubmit = () => {
    if (!password) {
      toast.error("Password is required");
      return;
    }
    if (password !== confirmPassword) {
      toast.error("Passwords do not match");
      return;
    }

    changePassword({ username, password });
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
      <AppDialogTitle>Change Password: {username}</AppDialogTitle>
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
            label="New Password"
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
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isPending || !password || password !== confirmPassword}
          onClick={handleSubmit}
          variant="contained"
        >
          {isPending ? "Changing..." : "Change Password"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default ChangePasswordDialog;
