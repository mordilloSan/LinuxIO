// src/components/NavbarThemeToggle.tsx
import { Icon } from "@iconify/react";
import { IconButton } from "@mui/material";
import React from "react";

import AppTooltip from "@/components/ui/AppTooltip";
import { iconSize } from "@/constants";
import { useConfigValue } from "@/hooks/useConfig";

function NavbarThemeToggle() {
  const [theme, setTheme] = useConfigValue("theme");
  const isDark = theme === "DARK";

  const toggleTheme = () => {
    setTheme(isDark ? "LIGHT" : "DARK");
  };

  return (
    <AppTooltip title={isDark ? "Switch to light mode" : "Switch to dark mode"}>
      <IconButton color="inherit" onClick={toggleTheme}>
        {isDark ? (
          <Icon
            icon="mdi:weather-night"
            width={iconSize.md}
            height={iconSize.md}
          />
        ) : (
          <Icon
            icon="mdi:weather-sunny"
            width={iconSize.md}
            height={iconSize.md}
          />
        )}
      </IconButton>
    </AppTooltip>
  );
}

export default React.memo(NavbarThemeToggle);
