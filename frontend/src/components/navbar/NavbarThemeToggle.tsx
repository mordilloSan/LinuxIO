// NavbarThemeToggle.tsx
import { IconButton, Tooltip } from "@mui/material";
import { Sun, Moon } from "lucide-react";
import React from "react";

import { THEMES } from "@/constants";
import useTheme from "@/hooks/useAppTheme";
import { setDarkMode } from "@/utils/filebrowserCache";

function NavbarThemeToggle() {
  const { theme, setTheme } = useTheme();
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

    // 1) switch app theme immediately for instant UI feedback
    setTheme(next);

    // 2) persist to FileBrowser and WAIT for it to finish
    try {
      await setDarkMode(dark);
    } catch {
      // keep UX quiet; still attempt to refresh iframe below
    }

    // 3) live update the iframe:
    if (isFBVisible()) {
      // If the iframe is on screen, prefer the DOM toggle (no reload, no flash)
      if (!tryClickIframeToggle()) {
        // optional: last resort would be a visible reload, but we avoid flashing
      }
    } else {
      // If hidden, refresh it in the background so it’s correct next time
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
