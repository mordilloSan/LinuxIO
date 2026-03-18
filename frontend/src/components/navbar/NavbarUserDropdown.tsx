import { Icon } from "@iconify/react";
import {
  Menu,
  MenuItem,
} from "@mui/material";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import React, { useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { linuxio } from "@/api";
import AppButton from "@/components/ui/AppButton";
import AppDivider from "@/components/ui/AppDivider";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { iconSize } from "@/constants";
import useAuth from "@/hooks/useAuth";
import usePowerAction from "@/hooks/usePowerAction";

function NavbarUserDropdown() {
  const ref = useRef(null);
  const navigate = useNavigate();
  const { user, signOut } = useAuth();
  const { triggerReboot, triggerPowerOff } = usePowerAction();

  const [anchorMenu, setAnchorMenu] = useState<null | HTMLElement>(null);
  const [confirm, setConfirm] = useState<"reboot" | "poweroff" | null>(null);

  // Mutations for power actions
  const { mutate: reboot } = linuxio.dbus.reboot.useMutation({
    onSuccess: () => {
      // Server may die before responding - this is expected
    },
    onError: (error: Error) => {
      // Server may die before responding - this is expected, so we don't show error
      console.warn("Reboot error (may be expected):", error);
    },
  });

  const { mutate: powerOff } = linuxio.dbus.power_off.useMutation({
    onSuccess: () => {
      // Server may die before responding - this is expected
    },
    onError: (error: Error) => {
      // Server may die before responding - this is expected, so we don't show error
      console.warn("Power off error (may be expected):", error);
    },
  });

  const toggleMenu = (event: React.SyntheticEvent<HTMLElement>) => {
    setAnchorMenu(event.currentTarget);
  };

  const closeMenu = () => setAnchorMenu(null);
  const closeConfirm = () => setConfirm(null);

  const handleSignOut = async () => {
    await signOut();
    navigate("/sign-in");
  };

  const handleConfirmedAction = () => {
    const action = confirm;
    closeMenu();
    closeConfirm();

    // Show overlay immediately
    if (action === "reboot") {
      triggerReboot();
      reboot([]);
    } else if (action === "poweroff") {
      triggerPowerOff();
      powerOff([]);
    }
  };

  return (
    <>
      <AppTooltip title="Account">
        <AppIconButton color="inherit" ref={ref} onClick={toggleMenu}>
          <Icon icon="mdi:power" width={iconSize.md} height={iconSize.md} />
        </AppIconButton>
      </AppTooltip>

      <Menu
        id="menu-appbar"
        anchorEl={anchorMenu}
        open={Boolean(anchorMenu)}
        onClose={closeMenu}
      >
        {user?.name && (
          <MenuItem disabled style={{ opacity: 0.7, fontWeight: 600 }}>
            Signed in as {user.name}
          </MenuItem>
        )}
        <AppDivider />
        <MenuItem onClick={() => setConfirm("reboot")}>Reboot</MenuItem>
        <MenuItem onClick={() => setConfirm("poweroff")}>Power Down</MenuItem>
        <AppDivider />
        <MenuItem onClick={handleSignOut}>Sign out</MenuItem>
      </Menu>

      <GeneralDialog open={confirm !== null} onClose={closeConfirm}>
        <AppDialogTitle>
          {confirm === "reboot" ? "Confirm Reboot" : "Confirm Power Down"}
        </AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            Are you sure you want to{" "}
            {confirm === "reboot" ? "reboot" : "power off"} the server? This
            action will terminate all services and disconnect users.
          </AppDialogContentText>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={closeConfirm}>Cancel</AppButton>
          <AppButton
            onClick={handleConfirmedAction}
            color="error"
            variant="contained"
            autoFocus
          >
            {confirm === "reboot" ? "Reboot" : "Power Down"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </>
  );
}

export default React.memo(NavbarUserDropdown);
