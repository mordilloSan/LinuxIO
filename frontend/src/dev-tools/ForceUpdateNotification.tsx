import { Button } from "@mui/material";
import { useState } from "react";
import { ReactQueryDevtoolsPanel } from "@tanstack/react-query-devtools";

/**
 * Dev-only tool to force an update notification for testing.
 * Only rendered when import.meta.env.DEV is true.
 */
export const ForceUpdateNotification = () => {
  // Check if update notification is currently shown
  const shown = !!sessionStorage.getItem("dev_update_forced");
  const [isDevtoolsOpen, setIsDevtoolsOpen] = useState(false);

  const forceUpdateNotification = () => {
    const fakeUpdateInfo = {
      available: true,
      current_version: "dev-v0.6.12",
      latest_version: "dev-v0.6.12",
      release_url: "https://github.com/mordilloSan/LinuxIO/releases",
    };

    sessionStorage.setItem("update_info", JSON.stringify(fakeUpdateInfo));
    sessionStorage.setItem("dev_update_forced", "true");
    window.location.reload();
  };

  const clearUpdateNotification = () => {
    sessionStorage.removeItem("update_info");
    sessionStorage.removeItem("dev_update_forced");
    window.location.reload();
  };

  if (!import.meta.env.DEV) {
    return null;
  }

  return (
    <>
      <div
        style={{
          position: "fixed",
          bottom: 20,
          right: 20,
          zIndex: 9999,
          color: "white",
          padding: "12px 16px",
          borderRadius: 8,
          boxShadow: "0 4px 6px rgba(0,0,0,0.3)",
          fontSize: 14,
          display: "flex",
          flexDirection: "column",
          gap: 8,
        }}
      >
        <div style={{ fontWeight: "bold", marginBottom: 4 }}>🛠️ Dev Tools</div>
        <Button
          variant="contained"
          color="primary"
          size="small"
          onClick={() => setIsDevtoolsOpen(!isDevtoolsOpen)}
          fullWidth
        >
          {isDevtoolsOpen ? "Close" : "Open"} React Query Devtools
        </Button>
        {!shown ? (
          <Button
            variant="contained"
            color="warning"
            size="small"
            onClick={forceUpdateNotification}
            fullWidth
          >
            Show Update Notification
          </Button>
        ) : (
          <Button
            variant="contained"
            color="secondary"
            size="small"
            onClick={clearUpdateNotification}
            fullWidth
          >
            Hide Update Notification
          </Button>
        )}
      </div>
      {isDevtoolsOpen && (
        <>
          {/* Backdrop */}
          <div
            style={{
              position: "fixed",
              top: 0,
              left: 0,
              width: "100%",
              height: "100%",
              backgroundColor: "rgba(0, 0, 0, 0.5)",
              zIndex: 9997,
            }}
            onClick={() => setIsDevtoolsOpen(false)}
          />
          {/* Devtools Panel */}
          <div
            style={{
              position: "fixed",
              top: "5%",
              left: "5%",
              width: "90%",
              height: "90%",
              zIndex: 9998,
              borderRadius: 12,
              overflow: "hidden",
              boxShadow: "0 25px 50px -12px rgba(0, 0, 0, 0.5)",
            }}
          >
            <ReactQueryDevtoolsPanel
              onClose={() => setIsDevtoolsOpen(false)}
            />
          </div>
        </>
      )}
    </>
  );
};
