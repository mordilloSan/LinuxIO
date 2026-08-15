import { Icon } from "@iconify/react";
import { memo, type MouseEventHandler } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import { useConfigValue } from "@/hooks/useConfig";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { shadowSm } from "@/theme/constants";
import { iconSize } from "@/theme/constants";

import NavbarNotificationsDropdown from "./NavbarNotificationsDropdown";
import NavbarSettingsDialogTrigger from "./NavbarSettingsDialogTrigger";
import Settings from "./NavbarThemeToggle";
import NavbarUserDropdown from "./NavbarUserDropdown";
import Dock from "../dock/Dock";

import "./navbar.css";

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
          <Dock>
            <NavbarNotificationsDropdown />
            <Settings />
            <NavbarSettingsDialogTrigger />
            <NavbarUserDropdown />
          </Dock>
        ) : (
          <div className="app-navbar__actions">
            <NavbarNotificationsDropdown />
            <Settings />
            <NavbarSettingsDialogTrigger />
            <NavbarUserDropdown />
          </div>
        )}
      </div>
    </header>
  );
};

export default memo(Navbar);
