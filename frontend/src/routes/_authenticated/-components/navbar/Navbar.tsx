import { Icon } from "@iconify/react";
import { memo, type MouseEventHandler } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import { useConfigValue } from "@/hooks/useConfig";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { shadowSm } from "@/theme/constants";
import { iconSize } from "@/theme/constants";

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

        {navigationMode === "dock" ? <Dock /> : null}

        {/* Power sits in the header corner in both navigation modes: it acts on
            the machine rather than the app, so it stays out of the dock's row
            of routes. */}
        <div className="app-navbar__actions">
          <NavbarUserDropdown />
        </div>
      </div>
    </header>
  );
};

export default memo(Navbar);
