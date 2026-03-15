import { Icon } from "@iconify/react";
import {
  Grid,
  InputBase,
  AppBar,
  IconButton,
  Toolbar,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import React, { useState } from "react";

import NavbarNotificationsDropdown from "./NavbarNotificationsDropdown";
import NavbarSettingsDialogTrigger from "./NavbarSettingsDialogTrigger";
import Settings from "./NavbarThemeToggle";
import NavbarUserDropdown from "./NavbarUserDropdown";

import { iconSize } from "@/constants";
import { getHoverBackground } from "@/theme/components";

interface NavbarProps {
  onDrawerToggle: React.MouseEventHandler<HTMLElement>;
}

const Navbar: React.FC<NavbarProps> = ({ onDrawerToggle }) => {
  const theme = useTheme();
  const isDesktop = useMediaQuery(theme.breakpoints.up("md"));
  const [isSearchHovered, setIsSearchHovered] = useState(false);
  const baseBorderRadius = parseFloat(String(theme.shape.borderRadius)) || 0;

  return (
    <AppBar
      position="sticky"
      elevation={1}
      sx={{
        background: theme.header.background,
        color: theme.header.color,
      }}
    >
      <Toolbar>
        <Grid container alignItems="center" sx={{ width: "100%" }}>
          {/* Mobile menu button */}
          <Grid sx={{ display: { xs: "block", md: "none" } }}>
            <IconButton
              color="inherit"
              aria-label="Open drawer"
              onClick={onDrawerToggle}
            >
              <Icon icon="mdi:menu" width={iconSize.md} height={iconSize.md} />
            </IconButton>
          </Grid>

          {/* Search Field (only desktop) */}
          {isDesktop && (
            <Grid>
              <div
                onMouseEnter={() => setIsSearchHovered(true)}
                onMouseLeave={() => setIsSearchHovered(false)}
                style={{
                  position: "relative",
                  borderRadius: `${baseBorderRadius * 2}px`,
                  backgroundColor: isSearchHovered
                    ? getHoverBackground(theme)
                    : theme.header.background,
                  width: "100%",
                }}
              >
                <div
                  style={{
                    position: "absolute",
                    width: 50,
                    height: "100%",
                    pointerEvents: "none",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                  }}
                >
                  <Icon
                    icon="mdi:magnify"
                    width={iconSize.md}
                    height={iconSize.md}
                  />
                </div>
                <InputBase
                  placeholder="Search"
                  id="search-input"
                  name="search"
                  inputProps={{ "aria-label": "Search containers or services" }}
                  sx={{
                    color: "inherit",
                    width: "100%",
                    input: {
                      color: theme.header.search.color,
                      p: theme.spacing(2.5, 2.5, 2.5, 12),
                      width: 160,
                    },
                  }}
                />
              </div>
            </Grid>
          )}

          {/* Spacer */}
          <Grid sx={{ flexGrow: 1 }} />

          {/* User Actions */}
          <Grid sx={{ display: "flex", alignItems: "center" }}>
            <NavbarNotificationsDropdown />
            <Settings />
            <NavbarSettingsDialogTrigger />
            <NavbarUserDropdown />
          </Grid>
        </Grid>
      </Toolbar>
    </AppBar>
  );
};

export default React.memo(Navbar);
