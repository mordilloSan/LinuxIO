import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { FB_BASE } from "@/utils/filebrowser";

export default function FilebrowserIframe() {
  const iframeRef = useRef<HTMLIFrameElement>(null);

  // handshake flag in state so effects can react to it
  const [fbReady, setFbReady] = useState(false);

  // remember the last suffix we sent to avoid echo loops
  const lastSentRef = useRef<string>("");

  const location = useLocation();
  const navigate = useNavigate();

  const isFBRoute = location.pathname.startsWith("/filebrowser");

  // Resolve the real origin of the FileBrowser app (works for absolute or relative FB_BASE)
  const FB_ORIGIN = useMemo(
    () => new URL(FB_BASE, window.location.href).origin,
    [],
  );

  // Keep always-fresh values without re-binding the event listener
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

  // The suffix we want the iframe to show (everything after /filebrowser)
  const urlSuffix = useMemo(
    () =>
      location.pathname.replace(/^\/filebrowser/, "") +
      location.search +
      location.hash,
    [location.pathname, location.search, location.hash],
  );

  // Build the iframe src once (stable after mount) to prevent reloads on route changes
  const initialSrcRef = useRef<string>("");
  if (!initialSrcRef.current) {
    const suffix = isFBRoute ? urlSuffix || "/" : "/";
    initialSrcRef.current = `${FB_BASE.replace(/\/+$/, "")}/${String(
      suffix,
    ).replace(/^\/+/, "")}`;
  }
  const iframeSrc = initialSrcRef.current;

  // Helper: send navigation to the iframe (postMessage), targeted to FB_ORIGIN
  const sendNavigate = useCallback(
    (suffix: string) => {
      const win = iframeRef.current?.contentWindow;
      if (!win) return;
      try {
        win.postMessage(
          { type: "linuxio:navigate", url: suffix || "/" },
          FB_ORIGIN,
        );
        lastSentRef.current = suffix;
      } catch {
        // no-op
      }
    },
    [FB_ORIGIN],
  );

  // Listen for messages from the iframe (handshake + internal navigation)
  useEffect(() => {
    function onMsg(ev: MessageEvent) {
      if (ev.origin !== FB_ORIGIN) return;
      const fromIframe =
        iframeRef.current?.contentWindow &&
        ev.source === iframeRef.current.contentWindow;
      if (!fromIframe) return;

      const d = ev.data;

      if (d?.type === "filebrowser:ready") {
        setFbReady(true);
        if (latestRef.current.isFBRoute) {
          const curSuffix =
            latestRef.current.path.replace(/^\/filebrowser/, "") +
            latestRef.current.search +
            latestRef.current.hash;
          if (curSuffix !== lastSentRef.current) {
            sendNavigate(curSuffix);
          }
        }
        return;
      }

      if (d?.type === "filebrowser:navigation") {
        const url = String(d.url || "/");
        const next = `/filebrowser${url}`;
        const cur =
          latestRef.current.path +
          latestRef.current.search +
          latestRef.current.hash;

        lastSentRef.current = url;

        if (latestRef.current.isFBRoute && next !== cur) {
          latestRef.current.navigate(next, { replace: true });
        }
      }
    }

    window.addEventListener("message", onMsg);
    return () => window.removeEventListener("message", onMsg);
  }, [FB_ORIGIN, sendNavigate]);

  // Parent route changed (or iframe became ready) → tell iframe to navigate
  useEffect(() => {
    if (!isFBRoute) return;
    if (!fbReady) return; // wait for handshake
    if (urlSuffix === lastSentRef.current) return; // avoid echo/bounce

    sendNavigate(urlSuffix);
  }, [isFBRoute, urlSuffix, fbReady, sendNavigate]);

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
        src={iframeSrc}
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
