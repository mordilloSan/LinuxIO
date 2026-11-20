import { Icon } from "@iconify/react";
import { useNavigate } from "@tanstack/react-router";
import { memo, useRef, useState, type MouseEvent } from "react";

import { linuxio } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppDivider from "@/components/ui/AppDivider";
import AppIconButton from "@/components/ui/AppIconButton";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTooltip from "@/components/ui/AppTooltip";
import type { SettlePowerAction } from "@/contexts/PowerActionContext";
import useAuth from "@/hooks/useAuth";
import usePowerAction from "@/hooks/usePowerAction";
import { iconSize } from "@/theme/constants";

function NavbarUserDropdown() {
  const navigate = useNavigate();
  const { user, signOut, privileged } = useAuth();
  const { triggerReboot, triggerPowerOff } = usePowerAction();

  const [anchorEl, setAnchorEl] = useState<HTMLButtonElement | null>(null);
  const [confirm, setConfirm] = useState<"reboot" | "poweroff" | null>(null);
  const menuOpen = anchorEl !== null;

  // The server may die before it answers. The overlay decides what a lost
  // connection means; a refusal takes it down and shows the reason. Each
  // request settles the overlay it started, never a later one.
  const settlePowerAction = useRef<SettlePowerAction | undefined>(undefined);
  const { mutate: reboot } = linuxio.control.reboot.useMutation({
    error: (error) => settlePowerAction.current?.(error),
  });

  const { mutate: powerOff } = linuxio.control.power_off.useMutation({
    success: () => settlePowerAction.current?.(),
    error: (error) => settlePowerAction.current?.(error),
  });

  const toggleMenu = (event: MouseEvent<HTMLButtonElement>) => {
    setAnchorEl((current) => (current ? null : event.currentTarget));
  };

  const closeMenu = () => setAnchorEl(null);
  const closeConfirm = () => setConfirm(null);
  const openConfirm = (action: "reboot" | "poweroff") => {
    closeMenu();
    setConfirm(action);
  };

  const handleSignOut = async () => {
    closeMenu();
    await signOut();
    await navigate({ to: "/sign-in", search: {} });
  };

  const handleConfirmedAction = () => {
    const action = confirm;
    closeMenu();
    closeConfirm();

    // Show overlay immediately
    if (action === "reboot") {
      settlePowerAction.current = triggerReboot();
      reboot();
    } else if (action === "poweroff") {
      settlePowerAction.current = triggerPowerOff();
      powerOff();
    }
  };

  return (
    <>
      <div className="app-navbar-dropdown">
        <AppTooltip title="Account">
          <AppIconButton
            aria-label="Account"
            aria-controls={menuOpen ? "navbar-account-menu" : undefined}
            aria-expanded={menuOpen}
            aria-haspopup="menu"
            color="secondary"
            onClick={toggleMenu}
          >
            <Icon height={iconSize.md} icon="mdi:power" width={iconSize.md} />
          </AppIconButton>
        </AppTooltip>

        <AppMenu
          anchorEl={anchorEl}
          anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
          ariaLabel="Account actions"
          className="app-navbar-account-menu"
          id="navbar-account-menu"
          onClose={closeMenu}
          open={menuOpen}
          transformOrigin={{ vertical: "top", horizontal: "right" }}
        >
          {user?.name ? (
            <div className="app-navbar-panel__header">
              <p className="app-navbar-panel__eyebrow">Signed in as</p>
              <p className="app-navbar-panel__title">{user.name}</p>
            </div>
          ) : null}

          {user?.name ? <AppDivider /> : null}

          {/* control.reboot and control.power_off are admin-only routes. */}
          {privileged ? (
            <>
              <AppMenuItem onClick={() => openConfirm("reboot")}>
                Reboot
              </AppMenuItem>
              <AppMenuItem onClick={() => openConfirm("poweroff")}>
                Power Down
              </AppMenuItem>

              <AppDivider />
            </>
          ) : null}

          <AppMenuItem onClick={handleSignOut}>Sign out</AppMenuItem>
        </AppMenu>
      </div>

      <GeneralDialog onClose={closeConfirm} open={confirm !== null}>
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
            autoFocus
            color="error"
            onClick={handleConfirmedAction}
            variant="contained"
          >
            {confirm === "reboot" ? "Reboot" : "Power Down"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </>
  );
}

export default memo(NavbarUserDropdown);
