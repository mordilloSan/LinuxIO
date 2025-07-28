import React, { useEffect, useRef, useState, useCallback } from "react";
import { useNavigate, useLocation } from "react-router-dom";

import { THEMES } from "@/constants";
import useTheme from "@/hooks/useAppTheme";
import axios from "@/utils/axios";

const FILEBROWSER_BASE = "/navigator";

const Filebrowser: React.FC = () => {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const navigate = useNavigate();
  const location = useLocation();
  const { theme } = useTheme();
  const isDark = theme === THEMES.DARK;

  const skipNextIframeSrcUpdate = useRef(false);
  const [iframeSrc, setIframeSrc] = useState(() => {
    const initialPath = location.pathname.replace(/^\/filebrowser/, "") || "/";
    return FILEBROWSER_BASE + initialPath + location.search + location.hash;
  });

  // Update iframeSrc if router path changes
  useEffect(() => {
    if (skipNextIframeSrcUpdate.current) {
      skipNextIframeSrcUpdate.current = false;
      return;
    }
    const targetPath = location.pathname.replace(/^\/filebrowser/, "") || "/";
    const expectedSrc =
      FILEBROWSER_BASE + targetPath + location.search + location.hash;
    if (iframeSrc !== expectedSrc) {
      setIframeSrc(expectedSrc);
    }
    // eslint-disable-next-line
  }, [location.pathname, location.search, location.hash]);

  // Listen for navigation messages from FileBrowser
  useEffect(() => {
    function handleMessage(event: MessageEvent) {
      const data = event.data;
      if (data && data.type === "filebrowser:navigation") {
        const url = data.url as string;
        const expectedPath = `/filebrowser${url}`;
        if (expectedPath !== location.pathname) {
          skipNextIframeSrcUpdate.current = true;
          navigate(expectedPath, { replace: true });
        }
      }
    }
    window.addEventListener("message", handleMessage);
    return () => window.removeEventListener("message", handleMessage);
  }, [location.pathname, navigate]);

  // --- THEME SYNC: API-based ---
  const syncFileBrowserThemeWithAPI = useCallback(async () => {
    try {
      // 1. Fetch FileBrowser theme via settings API
      const res = await axios.get("/navigator/api/settings", {
        withCredentials: true,
      });
      const fbIsDark = res.data?.userDefaults.darkMode;
      console.log("FileBrowser theme (API):", fbIsDark);
      console.log("MainAPI theme (API):", isDark);

      // 2. If themes differ, try toggling inside the iframe
      if (fbIsDark !== isDark) {
        console.log("Executing code to toggle theme in iframe");
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
              isDark,
            },
          }),
        });
        console.log("💾 Theme persisted via API");
      }
    } catch (e) {
      console.error("❌ Failed to sync FileBrowser theme:", e);
    }
  }, [isDark]);

  // Sync theme on load and theme change
  useEffect(() => {
    const iframe = iframeRef.current;
    if (iframe) {
      const onLoad = () => {
        syncFileBrowserThemeWithAPI();
      };
      iframe.addEventListener("load", onLoad);
      // Call immediately in case already loaded
      syncFileBrowserThemeWithAPI();
      return () => {
        iframe.removeEventListener("load", onLoad);
      };
    }
  }, [iframeSrc, syncFileBrowserThemeWithAPI]);

  // Watch for theme changes (re-sync if needed)
  useEffect(() => {
    syncFileBrowserThemeWithAPI();
  }, [theme, iframeSrc, syncFileBrowserThemeWithAPI]);

  return (
    <iframe
      id="filebrowser-iframe"
      ref={iframeRef}
      src={iframeSrc}
      style={{
        width: "100%",
        height: "90vh",
        border: "none",
      }}
      title="FileBrowser"
      allow="fullscreen"
    />
  );
};

export default Filebrowser;
