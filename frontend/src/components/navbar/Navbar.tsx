import MenuIcon from "@mui/icons-material/Menu";
import {
  Grid,
  InputBase,
  AppBar,
  IconButton,
  Toolbar,
  Box,
  useTheme,
} from "@mui/material";
import SearchIcon from "lucide-react/dist/esm/icons/search";
import React from "react";

import Customizer from "./NavbarCustomizer";
import NavbarNotificationsDropdown from "./NavbarNotificationsDropdown";
import Settings from "./NavbarThemeToggle";
import NavbarUserDropdown from "./NavbarUserDropdown";

import { getHoverBackground } from "@/theme/components";

type NavbarProps = {
  onDrawerToggle: React.MouseEventHandler<HTMLElement>;
};

const Navbar: React.FC<NavbarProps> = ({ onDrawerToggle }) => {
  const theme = useTheme();

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
              size="large"
            >
              <MenuIcon sx={{ width: 22, height: 22 }} />
            </IconButton>
          </Grid>

          {/* Search Field (only desktop) */}
          <Grid>
            <Box
              sx={{
                position: "relative",
                borderRadius: 2,
                backgroundColor: theme.header.background,
                width: "100%",
                display: { xs: "none", md: "block" },
                "&:hover": {
                  backgroundColor: getHoverBackground(theme),
                },
              }}
            >
              <Box
                sx={{
                  position: "absolute",
                  width: 50,
                  height: "100%",
                  pointerEvents: "none",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                }}
              >
                <SearchIcon width={22} height={22} />
              </Box>
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
            </Box>
          </Grid>

          {/* Spacer */}
          <Grid sx={{ flexGrow: 1 }} />

          {/* User Actions */}
          <Grid>
            <NavbarNotificationsDropdown />
            <Settings />
            <Customizer />
            <NavbarUserDropdown />
          </Grid>
        </Grid>
      </Toolbar>
    </AppBar>
  );
};

export default Navbar;
