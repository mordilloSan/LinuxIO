import { Icon } from "@iconify/react";
import React from "react";

import NavbarNotificationsDropdown from "./NavbarNotificationsDropdown";
import NavbarSettingsDialogTrigger from "./NavbarSettingsDialogTrigger";
import Settings from "./NavbarThemeToggle";
import NavbarUserDropdown from "./NavbarUserDropdown";
import "./navbar.css";

import AppIconButton from "@/components/ui/AppIconButton";
import { shadowSm } from "@/constants";
import { iconSize } from "@/constants";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { getHoverBackground } from "@/theme/components";

interface NavbarProps {
  onDrawerToggle: React.MouseEventHandler<HTMLElement>;
}

const Navbar: React.FC<NavbarProps> = ({ onDrawerToggle }) => {
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
        {!isDesktop && (
          <AppIconButton
            color="inherit"
            aria-label="Open drawer"
            onClick={onDrawerToggle}
          >
            <Icon icon="mdi:menu" width={iconSize.md} height={iconSize.md} />
          </AppIconButton>
        )}

        {isDesktop && (
          <div className="app-navbar__search">
            <div
              className="app-navbar-search"
              style={
                {
                  "--app-navbar-search-bg": theme.header.background,
                  "--app-navbar-search-hover": getHoverBackground(theme),
                  "--app-navbar-search-text": theme.header.search.color,
                  "--app-navbar-search-icon": theme.header.color,
                  borderRadius: `${theme.shape.borderRadius * 2}px`,
                } as React.CSSProperties
              }
            >
              <div className="app-navbar-search__icon" aria-hidden="true">
                <Icon
                  icon="mdi:magnify"
                  width={iconSize.md}
                  height={iconSize.md}
                />
              </div>
              <input
                className="app-navbar-search__input"
                placeholder="Search"
                id="search-input"
                name="search"
                type="search"
                aria-label="Search containers or services"
              />
            </div>
          </div>
        )}

        <div className="app-navbar__actions">
          <NavbarNotificationsDropdown />
          <Settings />
          <NavbarSettingsDialogTrigger />
          <NavbarUserDropdown />
        </div>
      </div>
    </header>
  );
};

export default React.memo(Navbar);
