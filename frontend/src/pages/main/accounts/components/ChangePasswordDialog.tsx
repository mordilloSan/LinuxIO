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
import { useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";

interface ChangePasswordDialogProps {
  open: boolean;
  onClose: () => void;
  username: string;
}

const ChangePasswordDialog: React.FC<ChangePasswordDialogProps> = ({
  open,
  onClose,
  username,
}) => {
  const theme = useAppTheme();
  const queryClient = useQueryClient();
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");

  const { mutate: changePassword, isPending } =
    linuxio.accounts.change_password.useMutation({
      onSuccess: () => {
        toast.success(`Password changed for "${username}"`);
        queryClient.invalidateQueries({
          queryKey: linuxio.accounts.list_users.queryKey(),
        });
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to change password"),
        );
      },
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

    changePassword([username, password]);
  };

  return (
    <GeneralDialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>Change Password: {username}</AppDialogTitle>
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
            label="New Password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            fullWidth
            required
            autoFocus
          />
          <AppTextField
            label="Confirm Password"
            type="password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            fullWidth
            required
            error={confirmPassword !== "" && password !== confirmPassword}
            helperText={
              confirmPassword !== "" && password !== confirmPassword
                ? "Passwords do not match"
                : ""
            }
          />
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={handleClose} disabled={isPending}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleSubmit}
          variant="contained"
          disabled={isPending || !password || password !== confirmPassword}
        >
          {isPending ? "Changing..." : "Change Password"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default ChangePasswordDialog;
