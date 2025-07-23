import { IconButton, Tooltip } from "@mui/material";
import { Sun, Moon } from "lucide-react";
import React from "react";

import { THEMES } from "@/constants";
import useTheme from "@/hooks/useAppTheme";

function NavbarThemeToggle() {
  const { theme, setTheme } = useTheme();
  const isDark = theme === THEMES.DARK;

  const toggleTheme = () => {
    setTheme(isDark ? THEMES.LIGHT : THEMES.DARK);
  };

  return (
    <Tooltip title={isDark ? "Switch to light mode" : "Switch to dark mode"}>
      <IconButton color="inherit" onClick={toggleTheme} size="large">
        {isDark ? <Moon /> : <Sun />}
      </IconButton>
    </Tooltip>
  );
}

export default NavbarThemeToggle;
