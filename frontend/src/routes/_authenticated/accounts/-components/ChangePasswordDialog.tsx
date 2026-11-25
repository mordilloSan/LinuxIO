import { useState } from "react";

import { linuxio } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppTextField from "@/components/ui/AppTextField";
import { useScopedToast } from "@/hooks/useScopedToast";

import { SMB_SIGN_IN_LABEL, useSambaSignIn } from "./useSambaSignIn";

const ACCOUNTS_TOAST_META = {
  label: "Open accounts",
  to: "/accounts",
} as const;

interface ChangePasswordDialogProps {
  onClose: () => void;
  onExited?: () => void;
  open: boolean;
  username: string;
}

const ChangePasswordDialog = ({
  open,
  onClose,
  onExited,
  username,
}: ChangePasswordDialogProps) => {
  const toast = useScopedToast(ACCOUNTS_TOAST_META);
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  // Sign-in to shared folders from Windows and a Mac: what the user has now,
  // until the box is touched.
  const signIn = useSambaSignIn(open);
  const sambaUser = signIn.users?.find((user) => user.username === username);
  const [signInChoice, setSignInChoice] = useState<boolean | null>(null);
  const signInOn = signInChoice ?? Boolean(sambaUser);

  const { mutate: turnOffSignIn, isPending: turningOff } =
    linuxio.shares.remove_samba_user.useMutation({
      success: () => {
        toast.success(`"${username}" can no longer open shared folders`);
        handleClose();
      },
      error: "Failed to turn off shared folder sign-in",
      toast: ACCOUNTS_TOAST_META,
    });

  const { mutate: changePassword, isPending } =
    linuxio.accounts.change_password.useMutation({
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
    setSignInChoice(null);
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

    // Before the list has arrived, nothing is said: the server keeps a user
    // who has sign-in in step, where "off" would take it away.
    const smbSignIn =
      signIn.offered && (signInChoice !== null || signIn.users)
        ? signInOn
        : undefined;
    changePassword({
      username,
      password,
      ...(smbSignIn === undefined ? {} : { smbSignIn }),
    });
  };

  return (
    <GeneralDialog
      fullWidth
      maxWidth="sm"
      onClose={handleClose}
      open={open}
      slotProps={{ transition: { onExited } }}
    >
      <AppDialogTitle>Change Password: {username}</AppDialogTitle>
      <AppDialogContent>
        <div className="app-dialog-fields">
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
          {signIn.offered && (
            <AppFormControlLabel
              control={
                <AppCheckbox
                  checked={signInOn}
                  onChange={(event) => setSignInChoice(event.target.checked)}
                />
              }
              label={SMB_SIGN_IN_LABEL}
            />
          )}
          {signIn.offered && sambaUser?.passwordStale && (
            <AppAlert severity="warning">
              This password was changed outside LinuxIO, so the old one still
              opens shared folders. Setting it here brings the two back in step.
            </AppAlert>
          )}
          {signIn.offered && sambaUser && (
            <AppButton
              disabled={isPending || turningOff}
              onClick={() => turnOffSignIn({ username })}
              size="small"
              style={{ alignSelf: "flex-start" }}
            >
              Turn off without changing the password
            </AppButton>
          )}
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
