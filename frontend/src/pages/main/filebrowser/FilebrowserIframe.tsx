import React, { useEffect, useMemo, useRef } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { FB_BASE } from "@/utils/filebrowser";

export default function FilebrowserIframe() {
  const iframeRef = useRef<HTMLIFrameElement>(null);

  // runtime flags for cross-window coordination
  const fbReadyRef = useRef(false);
  const lastSentRef = useRef<string>("");

  const location = useLocation();
  const navigate = useNavigate();
  const isFBRoute = location.pathname.startsWith("/filebrowser");

  // Keep latest values in a ref so the listener never goes stale
  const latestRef = useRef({
    isFBRoute,
    path: location.pathname,
    search: location.search,
    hash: location.hash,
    navigate,
  });
  latestRef.current = {
    isFBRoute,
    path: location.pathname,
    search: location.search,
    hash: location.hash,
    navigate,
  };

  // 1) Subscribe to window messages ONCE
  useEffect(() => {
    function onMsg(ev: MessageEvent) {
      if (ev.origin !== window.location.origin) return;
      const fromIframe =
        iframeRef.current?.contentWindow &&
        ev.source === iframeRef.current.contentWindow;
      if (!fromIframe) return;

      const d = ev.data;

      // Handshake: mark iframe as ready to receive navigation
      if (d?.type === "filebrowser:ready") {
        fbReadyRef.current = true;
        return;
      }

      // Sync parent route when FB navigates internally
      if (latestRef.current.isFBRoute && d?.type === "filebrowser:navigation") {
        const url = String(d.url || "/");
        const next = `/filebrowser${url}`;
        const cur =
          latestRef.current.path +
          latestRef.current.search +
          latestRef.current.hash;
        if (next !== cur) latestRef.current.navigate(next, { replace: true });
      }
    }

    window.addEventListener("message", onMsg);
    return () => window.removeEventListener("message", onMsg);
  }, []);

  // Precompute the suffix we want the iframe to navigate to
  const urlSuffix = useMemo(
    () =>
      location.pathname.replace(/^\/filebrowser/, "") +
      location.search +
      location.hash,
    [location.pathname, location.search, location.hash],
  );

  // 2) Push navigation to the iframe when our route changes under /filebrowser
  useEffect(() => {
    if (!isFBRoute) return;
    if (urlSuffix === lastSentRef.current) return;

    const win = iframeRef.current?.contentWindow;
    if (!win || !fbReadyRef.current) return; // wait for the iframe handshake

    try {
      win.postMessage(
        { type: "linuxio:navigate", url: urlSuffix || "/" },
        window.location.origin,
      );
      lastSentRef.current = urlSuffix;
    } catch {
      // no-op
    }
  }, [isFBRoute, urlSuffix]);

  return (
    <div
      id="filebrowser-layer"
      aria-hidden={!isFBRoute}
      style={{
        position: "absolute",
        inset: 0,
        opacity: isFBRoute ? 1 : 0,
        visibility: isFBRoute ? "visible" : "hidden",
        pointerEvents: isFBRoute ? "auto" : "none",
        transition: "none",
        zIndex: 1,
        backgroundColor:
          "var(--app-bg, var(--mui-palette-background-default, transparent))",
      }}
    >
      <iframe
        id="filebrowser-iframe"
        ref={iframeRef}
        src={`${FB_BASE}/`}
        title="FileBrowser"
        allow="fullscreen"
        loading="lazy"
        style={{
          width: "100%",
          height: "99%",
          border: "none",
          background:
            "var(--app-bg, var(--mui-palette-background-default, transparent))",
        }}
      />
    </div>
  );
}
