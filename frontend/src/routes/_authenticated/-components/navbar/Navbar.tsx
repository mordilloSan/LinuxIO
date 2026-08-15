import { Icon } from "@iconify/react";
import { memo, type MouseEventHandler } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import { useConfigValue } from "@/hooks/useConfig";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { shadowSm } from "@/theme/constants";
import { iconSize } from "@/theme/constants";

import NavbarNotificationsDropdown from "./NavbarNotificationsDropdown";
import NavbarSettingsDialogTrigger from "./NavbarSettingsDialogTrigger";
import NavbarUserDropdown from "./NavbarUserDropdown";
import Dock, { type DockAction } from "../dock/Dock";

import "./navbar.css";

/* In dock mode the actions render as dock tiles: the dock's own hover label
   replaces each control's tooltip, so those are switched off. */
const DOCK_ACTIONS: readonly DockAction[] = [
  {
    label: "Notifications",
    node: <NavbarNotificationsDropdown tooltip={false} />,
  },
  { label: "Settings", node: <NavbarSettingsDialogTrigger tooltip={false} /> },
  { label: "Account", node: <NavbarUserDropdown tooltip={false} /> },
];

interface NavbarProps {
  onDrawerToggle?: MouseEventHandler<HTMLElement>;
}

const Navbar = ({ onDrawerToggle }: NavbarProps) => {
  const theme = useAppTheme();
  const isDesktop = useAppMediaQuery(theme.breakpoints.up("md"));
  const [navigationMode] = useConfigValue("navigationMode");

  return (
    <header
      className="app-navbar"
      style={{
        boxShadow: shadowSm,
      }}
    >
      <div className="app-navbar__inner">
        {!isDesktop && onDrawerToggle && (
          <AppIconButton
            aria-label="Open drawer"
            color="inherit"
            onClick={onDrawerToggle}
          >
            <Icon height={iconSize.md} icon="mdi:menu" width={iconSize.md} />
          </AppIconButton>
        )}

        {navigationMode === "dock" ? (
          <Dock actions={DOCK_ACTIONS} />
        ) : (
          <div className="app-navbar__actions">
            <NavbarNotificationsDropdown />
            <NavbarSettingsDialogTrigger />
            <NavbarUserDropdown />
          </div>
        )}
      </div>
    </header>
  );
};

export default memo(Navbar);
