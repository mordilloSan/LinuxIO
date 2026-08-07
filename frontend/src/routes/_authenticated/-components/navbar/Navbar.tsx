import { Icon } from "@iconify/react";
import { memo, type CSSProperties, type MouseEventHandler } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import AppSearchField from "@/components/ui/AppSearchField";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { shadowSm } from "@/theme/constants";
import { iconSize } from "@/theme/constants";

import NavbarNotificationsDropdown from "./NavbarNotificationsDropdown";
import NavbarSettingsDialogTrigger from "./NavbarSettingsDialogTrigger";
import Settings from "./NavbarThemeToggle";
import NavbarUserDropdown from "./NavbarUserDropdown";

import "./navbar.css";

interface NavbarProps {
  onDrawerToggle: MouseEventHandler<HTMLElement>;
}

const Navbar = ({ onDrawerToggle }: NavbarProps) => {
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
            aria-label="Open drawer"
            color="inherit"
            onClick={onDrawerToggle}
          >
            <Icon height={iconSize.md} icon="mdi:menu" width={iconSize.md} />
          </AppIconButton>
        )}

        {isDesktop && (
          <div className="app-navbar__search">
            <AppSearchField
              aria-label="Search containers or services"
              className="app-navbar-search"
              fullWidth
              id="search-input"
              name="search"
              placeholder="Search"
              startAdornment={
                <Icon
                  height={iconSize.md}
                  icon="mdi:magnify"
                  width={iconSize.md}
                />
              }
              style={
                {
                  "--app-navbar-search-bg": theme.header.background,
                  "--app-navbar-search-text": theme.header.search.color,
                  "--app-navbar-search-icon": theme.header.color,
                  borderRadius: `${theme.shape.borderRadius * 2}px`,
                } as CSSProperties
              }
              type="search"
            />
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

export default memo(Navbar);
