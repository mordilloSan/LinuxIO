import { Icon } from "@iconify/react";
import { memo, type MouseEventHandler } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import { HeaderActionSlotHost } from "@/contexts/HeaderActionSlotContext";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { shadowSm } from "@/theme/constants";
import { iconSize } from "@/theme/constants";

import NavbarUserDropdown from "./NavbarUserDropdown";
import Dock from "../dock/Dock";

import "./navbar.css";

interface NavbarProps {
  dockMode: boolean;
  onDrawerToggle?: MouseEventHandler<HTMLElement>;
}

const Navbar = ({ dockMode, onDrawerToggle }: NavbarProps) => {
  const theme = useAppTheme();
  const isDesktop = useAppMediaQuery(theme.breakpoints.up("md"));

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
            color="secondary"
            onClick={onDrawerToggle}
          >
            <Icon height={iconSize.md} icon="mdi:menu" width={iconSize.md} />
          </AppIconButton>
        )}

        {dockMode ? <Dock /> : null}

        {/* Power sits in the header corner in both navigation modes: it acts on
            the machine rather than the app, so it stays out of the dock's row
            of routes. On small screens the active route parks its condensed
            actions trigger to its left, so every header control shares one
            corner. */}
        <div className="app-navbar__actions">
          <HeaderActionSlotHost />
          <NavbarUserDropdown />
        </div>
      </div>
    </header>
  );
};

export default memo(Navbar);
