import { Icon } from "@iconify/react";
import { memo } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useConfigValue } from "@/hooks/useConfig";
import { iconSize } from "@/theme/constants";

function NavbarThemeToggle() {
  const [theme, setTheme] = useConfigValue("theme");
  const isDark = theme === "DARK";

  const toggleTheme = () => {
    setTheme(isDark ? "LIGHT" : "DARK");
  };

  return (
    <AppTooltip title={isDark ? "Switch to light mode" : "Switch to dark mode"}>
      <AppIconButton
        aria-label={isDark ? "Switch to light mode" : "Switch to dark mode"}
        color="inherit"
        onClick={toggleTheme}
      >
        {isDark ? (
          <Icon
            height={iconSize.md}
            icon="mdi:weather-night"
            width={iconSize.md}
          />
        ) : (
          <Icon
            height={iconSize.md}
            icon="mdi:weather-sunny"
            width={iconSize.md}
          />
        )}
      </AppIconButton>
    </AppTooltip>
  );
}

export default memo(NavbarThemeToggle);
