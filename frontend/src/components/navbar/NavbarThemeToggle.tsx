// src/components/NavbarThemeToggle.tsx
import { IconButton, Tooltip } from "@mui/material";
import { Sun, Moon } from "lucide-react";
import React from "react";

import { useConfigValue } from "@/hooks/useConfig";

function NavbarThemeToggle() {
  const [theme, setTheme] = useConfigValue("theme");
  const isDark = theme === "DARK";

  const toggleTheme = () => {
    setTheme(isDark ? "LIGHT" : "DARK");
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
