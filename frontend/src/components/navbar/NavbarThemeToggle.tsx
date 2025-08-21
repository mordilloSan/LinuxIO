import { IconButton, Tooltip } from "@mui/material";
import { Sun, Moon } from "lucide-react";
import React from "react";

import { THEMES } from "@/constants";
import { useConfigValue } from "@/hooks/useConfig";
import { setDarkMode } from "@/utils/filebrowserCache";

function NavbarThemeToggle() {
  // useConfigValue gives us [value, setter]
  const [theme, setTheme] = useConfigValue("theme");
  const isDark = theme === THEMES.DARK;

  const isFBVisible = () => {
    const el = document.getElementById("filebrowser-layer");
    if (!el) return false;
    const cs = getComputedStyle(el);
    return cs.visibility === "visible" && parseFloat(cs.opacity || "0") > 0.5;
  };

  const tryClickIframeToggle = () => {
    const iframe = document.getElementById(
      "filebrowser-iframe",
    ) as HTMLIFrameElement | null;
    const doc = iframe?.contentDocument || iframe?.contentWindow?.document;
    if (!doc) return false;
    const icon = Array.from(
      doc.querySelectorAll(".quick-toggles .material-icons"),
    ).find((el) => el.textContent?.trim() === "dark_mode");
    if (icon) {
      (icon as HTMLElement).click();
      return true;
    }
    return false;
  };

  const bgReloadIfHidden = () => {
    const iframe = document.getElementById(
      "filebrowser-iframe",
    ) as HTMLIFrameElement | null;
    if (!iframe || isFBVisible()) return;
    try {
      iframe.contentWindow?.location?.reload();
    } catch {
      const current = iframe.src;
      iframe.src = "about:blank";
      setTimeout(() => {
        iframe.src = current;
      }, 0);
    }
  };

  const toggleTheme = async () => {
    const next = isDark ? THEMES.LIGHT : THEMES.DARK;
    const dark = next === THEMES.DARK;

    // 1) switch app theme immediately
    setTheme(next);

    // 2) persist to FileBrowser
    try {
      await setDarkMode(dark);
    } catch {
      // silent fail
    }

    // 3) update iframe
    if (isFBVisible()) {
      if (!tryClickIframeToggle()) {
        // fallback could be a visible reload (we skip to avoid flashing)
      }
    } else {
      bgReloadIfHidden();
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
