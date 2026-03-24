import { Icon } from "@iconify/react";
import React, { useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

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
import AppTooltip from "@/components/ui/AppTooltip";
import { iconSize } from "@/constants";
import useAuth from "@/hooks/useAuth";
import { useDismissibleLayer } from "@/hooks/useDismissibleLayer";
import usePowerAction from "@/hooks/usePowerAction";

function NavbarUserDropdown() {
  const ref = useRef<HTMLButtonElement>(null);
  const navigate = useNavigate();
  const { user, signOut } = useAuth();
  const { triggerReboot, triggerPowerOff } = usePowerAction();

  const [menuOpen, setMenuOpen] = useState(false);
  const [confirm, setConfirm] = useState<"reboot" | "poweroff" | null>(null);
  const layerRef = useDismissibleLayer<HTMLDivElement>(menuOpen, () =>
    setMenuOpen(false),
  );

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

  const toggleMenu = () => {
    setMenuOpen((open) => !open);
  };

  const closeMenu = () => setMenuOpen(false);
  const closeConfirm = () => setConfirm(null);
  const openConfirm = (action: "reboot" | "poweroff") => {
    closeMenu();
    setConfirm(action);
  };

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
      <div ref={layerRef} className="app-navbar-dropdown">
        <AppTooltip title="Account">
          <AppIconButton
            color="inherit"
            ref={ref}
            onClick={toggleMenu}
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            aria-controls={menuOpen ? "navbar-account-menu" : undefined}
          >
            <Icon icon="mdi:power" width={iconSize.md} height={iconSize.md} />
          </AppIconButton>
        </AppTooltip>

        {menuOpen ? (
          <div
            id="navbar-account-menu"
            className="app-navbar-panel app-navbar-panel--compact"
            role="menu"
            aria-label="Account actions"
          >
            {user?.name ? (
              <div className="app-navbar-panel__header">
                <p className="app-navbar-panel__eyebrow">Signed in as</p>
                <p className="app-navbar-panel__title">{user.name}</p>
              </div>
            ) : null}

            {user?.name ? <AppDivider /> : null}

            <div className="app-navbar-menu">
              <button
                type="button"
                className="app-navbar-menu__item"
                role="menuitem"
                onClick={() => openConfirm("reboot")}
              >
                Reboot
              </button>
              <button
                type="button"
                className="app-navbar-menu__item"
                role="menuitem"
                onClick={() => openConfirm("poweroff")}
              >
                Power Down
              </button>
            </div>

            <AppDivider />

            <div className="app-navbar-menu">
              <button
                type="button"
                className="app-navbar-menu__item"
                role="menuitem"
                onClick={handleSignOut}
              >
                Sign out
              </button>
            </div>
          </div>
        ) : null}
      </div>

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
