// src/components/PersistentFilebrowser.tsx
import React, { useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import axios from "@/utils/axios";

const FB_BASE = "/navigator";

export default function PersistentFilebrowser() {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [ready, setReady] = useState(false);

  const location = useLocation();
  const navigate = useNavigate();

  const isFBRoute = useMemo(
    () => location.pathname.startsWith("/filebrowser"),
    [location.pathname],
  );

  useEffect(() => {
    const controller = new AbortController();
    axios
      .get(FB_BASE + "/", {
        signal: controller.signal, // cancel on unmount
      })
      .catch(() => { });
    return () => controller.abort();
  }, []);

  // Mark ready after the FIRST load; stays mounted forever.
  useEffect(() => {
    const i = iframeRef.current;
    if (!i) return;
    const onLoad = () => setReady(true);
    i.addEventListener("load", onLoad);
    return () => i.removeEventListener("load", onLoad);
  }, []);

  // Keep parent router in sync with FileBrowser's internal navigation
  useEffect(() => {
    function onMsg(ev: MessageEvent) {
      if (!isFBRoute) return;
      if (ev.origin !== window.location.origin) return;

      const fromIframe =
        iframeRef.current?.contentWindow &&
        ev.source === iframeRef.current.contentWindow;
      if (!fromIframe) return;

      const d = ev.data;
      if (d && d.type === "filebrowser:navigation") {
        const url = String(d.url || "/");
        const next = `/filebrowser${url}`;
        const cur = location.pathname + location.search + location.hash;
        if (next !== cur) navigate(next, { replace: true });
      }
    }
    window.addEventListener("message", onMsg);
    return () => window.removeEventListener("message", onMsg);
  }, [isFBRoute, location.pathname, location.search, location.hash, navigate]);

  // When the app route changes under /filebrowser, ask FB to navigate WITHOUT reload.
  useEffect(() => {
    if (!isFBRoute || !ready) return;

    const urlSuffix =
      location.pathname.replace(/^\/filebrowser/, "") +
      location.search +
      location.hash;

    const win = iframeRef.current?.contentWindow;
    if (!win) return;

    try {
      win.postMessage(
        { type: "linuxio:navigate", url: urlSuffix || "/" },
        window.location.origin,
      );
    } catch (err) {
      console.log("[filebrowser] postMessage failed:", err);
    }
  }, [isFBRoute, ready, location.pathname, location.search, location.hash]);

  return (
    <div
      id="filebrowser-layer"
      style={{
        position: "absolute",
        inset: 0,
        opacity: isFBRoute ? 1 : 0,
        visibility: isFBRoute ? "visible" : "hidden",
        pointerEvents: isFBRoute ? "auto" : "none",
        transition: "none",
        zIndex: 1,
      }}
    >
      {!ready && (
        <div
          style={{
            position: "absolute",
            inset: 0,
            background:
              "var(--app-bg, var(--mui-palette-background-default, transparent))",
          }}
        />
      )}
      <iframe
        id="filebrowser-iframe"
        ref={iframeRef}
        src={`${FB_BASE}/`}
        title="FileBrowser"
        allow="fullscreen"
        loading="eager"
        style={{
          width: "100%",
          height: "100%",
          border: "none",
          background:
            "var(--app-bg, var(--mui-palette-background-default, transparent))",
        }}
      />
    </div>
  );
}
