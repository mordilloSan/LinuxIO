import { Button } from "@mui/material";

/**
 * Dev-only tool to force an update notification for testing.
 * Only rendered when import.meta.env.DEV is true.
 */
export const ForceUpdateNotification = () => {
  // Check if update notification is currently shown
  const shown = !!sessionStorage.getItem("dev_update_forced");

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
    <div
      style={{
        position: "fixed",
        bottom: 20,
        right: 20,
        zIndex: 9999,
        background: "#1976d2",
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
  );
};
