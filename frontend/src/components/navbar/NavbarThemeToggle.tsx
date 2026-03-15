// src/components/NavbarThemeToggle.tsx
import { Icon } from "@iconify/react";
import { IconButton, Tooltip } from "@mui/material";
import React from "react";

import { iconSize } from "@/constants";
import { useConfigValue } from "@/hooks/useConfig";

function NavbarThemeToggle() {
  const [theme, setTheme] = useConfigValue("theme");
  const isDark = theme === "DARK";

  const toggleTheme = () => {
    setTheme(isDark ? "LIGHT" : "DARK");
  };

  return (
    <Tooltip title={isDark ? "Switch to light mode" : "Switch to dark mode"}>
      <IconButton color="inherit" onClick={toggleTheme}>
        {isDark ? <Icon icon="mdi:weather-night" width={iconSize.md} height={iconSize.md} /> : <Icon icon="mdi:weather-sunny" width={iconSize.md} height={iconSize.md} />}
      </IconButton>
    </Tooltip>
  );
}

export default React.memo(NavbarThemeToggle);
