import React, { useEffect, useRef, useState } from "react";
import { useNavigate, useLocation } from "react-router-dom";

const FILEBROWSER_BASE = "/navigator";

const Filebrowser: React.FC = () => {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const navigate = useNavigate();
  const location = useLocation();

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
