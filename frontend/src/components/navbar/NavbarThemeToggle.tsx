import { IconButton, Tooltip } from "@mui/material";
import { Sun, Moon } from "lucide-react";
import React from "react";
import { useLocation } from "react-router-dom";

import { THEMES } from "@/constants";
import useTheme from "@/hooks/useAppTheme";

function NavbarThemeToggle() {
  const { theme, setTheme } = useTheme();
  const location = useLocation();
  const isDark = theme === THEMES.DARK;

  const toggleTheme = async () => {
    const newTheme = isDark ? THEMES.LIGHT : THEMES.DARK;
    const darkMode = newTheme === THEMES.DARK;
    setTheme(newTheme);

    const isOnFilebrowser = location.pathname.startsWith("/filebrowser");

    if (isOnFilebrowser) {
      // ✅ Simulate click inside iframe
      const iframe = document.getElementById(
        "filebrowser-iframe",
      ) as HTMLIFrameElement;
      const iframeDoc =
        iframe?.contentDocument || iframe?.contentWindow?.document;

      if (iframeDoc) {
        const icon = Array.from(
          iframeDoc.querySelectorAll(".quick-toggles .material-icons"),
        ).find((el) => el.textContent?.trim() === "dark_mode");

        if (icon) {
          (icon as HTMLElement).click();
          console.log("🌓 Theme toggle clicked in iframe");
          return;
        }
      }
      console.warn("Theme button not found — fallback to API");
    }

    // ❌ Not on /filebrowser, or iframe not ready → Fallback to API
    try {
      await fetch("/navigator/api/users?id=1", {
        method: "PUT",
        headers: {
          "Content-Type": "text/plain;charset=UTF-8",
          Accept: "*/*",
        },
        body: JSON.stringify({
          what: "user",
          which: ["darkMode"],
          data: {
            darkMode,
          },
        }),
      });
      console.log("💾 Theme persisted via API");
    } catch (err) {
      console.error("❌ Failed to persist FileBrowser theme:", err);
    }
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
