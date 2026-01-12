import CloseIcon from "@mui/icons-material/Close";
import DownloadIcon from "@mui/icons-material/Download";
import { Alert, Button, IconButton, Link, Stack } from "@mui/material";
import { useState, useEffect, useRef } from "react";

import UpdateDialog from "./UpdateDialog";

import { getStreamMux, initStreamMux } from "@/api/linuxio";
import { useLinuxIOUpdater } from "@/hooks/useLinuxIOUpdater";

interface UpdateInfo {
  available: boolean;
  current_version: string;
  latest_version?: string;
  release_url?: string;
}

interface UpdateBannerProps {
  updateInfo: UpdateInfo;
  onDismiss: () => void;
}

const UpdateBanner: React.FC<UpdateBannerProps> = ({
  updateInfo,
  onDismiss,
}) => {
  const [showDialog, setShowDialog] = useState(false);
  const [updateComplete, setUpdateComplete] = useState(false);
  const [updateSuccess, setUpdateSuccess] = useState(false);
  const { startUpdate, status, progress, output, error, isUpdating } =
    useLinuxIOUpdater();
  const waitingForReconnectRef = useRef(false);
  const hasDisconnectedRef = useRef(false);

  // Auto-reload when WebSocket reconnects after update
  // With socket activation, we need to actively try to reconnect (not passively wait)
  useEffect(() => {
    let reconnectInterval: ReturnType<typeof setInterval> | null = null;
    let unsubscribe: (() => void) | null = null;

    const setupListener = () => {
      const mux = getStreamMux();
      if (!mux) return;

      unsubscribe = mux.addStatusListener((newStatus) => {
        if (!waitingForReconnectRef.current) return;

        console.log(
          `[UpdateBanner] WebSocket status: ${newStatus}, hasDisconnected=${hasDisconnectedRef.current}`,
        );

        // Track when WebSocket disconnects (service restarting)
        if (newStatus === "closed" || newStatus === "error") {
          hasDisconnectedRef.current = true;

          // Start actively polling for reconnection (triggers socket activation)
          // Wait a few seconds before polling to let the update script finish
          if (!reconnectInterval) {
            console.log(
              "[UpdateBanner] Will start reconnection polling in 5 seconds...",
            );
            setTimeout(() => {
              console.log("[UpdateBanner] Starting reconnection polling...");
              reconnectInterval = setInterval(() => {
                console.log("[UpdateBanner] Attempting to reconnect...");
                // initStreamMux creates a new WebSocket if current is closed
                const newMux = initStreamMux();

                // Listen for this mux to open
                const checkAndShowSuccess = (status: string) => {
                  if (status === "open") {
                    console.log(
                      "[UpdateBanner] Reconnected! Update successful.",
                    );
                    // Clear interval to stop further attempts
                    if (reconnectInterval) {
                      clearInterval(reconnectInterval);
                      reconnectInterval = null;
                    }
                    // Show success state - user clicks to continue
                    waitingForReconnectRef.current = false;
                    setUpdateComplete(true);
                    setUpdateSuccess(true);
                  }
                };

                // Check current status and also listen for changes
                if (newMux.status === "open") {
                  checkAndShowSuccess("open");
                } else {
                  newMux.addStatusListener(checkAndShowSuccess);
                }
              }, 2000); // Try every 2 seconds
            }, 5000); // Wait 5 seconds before starting to poll
          }
        }

        // When it reconnects after being disconnected, show success
        if (newStatus === "open" && hasDisconnectedRef.current) {
          console.log("[UpdateBanner] Service reconnected! Update successful.");
          waitingForReconnectRef.current = false;
          setUpdateComplete(true);
          setUpdateSuccess(true);
        }
      });
    };

    setupListener();

    return () => {
      if (unsubscribe) unsubscribe();
      if (reconnectInterval) clearInterval(reconnectInterval);
    };
  }, []);

  const handleUpdate = async () => {
    if (
      !confirm(
        `Update LinuxIO from ${updateInfo.current_version} to ${updateInfo.latest_version}?\n\n` +
          "The service will restart automatically.",
      )
    ) {
      return;
    }

    setShowDialog(true);
    setUpdateComplete(false);
    setUpdateSuccess(false);
    waitingForReconnectRef.current = false;
    hasDisconnectedRef.current = false;

    try {
      await startUpdate(updateInfo.latest_version);

      // Start monitoring for reconnection
      waitingForReconnectRef.current = true;

      // Fallback: if reconnection detection fails, show success after 30 seconds
      // (assuming update completed but WebSocket reconnection failed)
      setTimeout(() => {
        if (waitingForReconnectRef.current) {
          console.log(
            "[UpdateBanner] Fallback timeout reached, assuming success...",
          );
          waitingForReconnectRef.current = false;
          setUpdateComplete(true);
          setUpdateSuccess(true);
        }
      }, 30000);
    } catch (err) {
      console.error("Update failed:", err);
      waitingForReconnectRef.current = false;
      // Dialog will show the error, keep it open
    }
  };

  const handleCloseDialog = () => {
    if (!isUpdating) {
      setShowDialog(false);
    }
  };

  const handleContinue = () => {
    // Clear update info and reload to login page
    sessionStorage.removeItem("update_info");
    window.location.reload();
  };

  if (!updateInfo.available) {
    return null;
  }

  return (
    <>
      <UpdateDialog
        open={showDialog}
        status={error || status}
        progress={progress}
        output={output}
        onClose={handleCloseDialog}
        canClose={!isUpdating && !updateComplete}
        updateComplete={updateComplete}
        updateSuccess={updateSuccess}
        onContinue={handleContinue}
      />
      <Alert
        severity="info"
        sx={{ mx: { xs: 6, md: 8 }, mt: 0, mb: 0, borderRadius: 2 }}
        slotProps={{ message: { sx: { width: "100%", p: 0 } } }}
        action={
          <IconButton
            aria-label="close"
            color="inherit"
            size="small"
            onClick={onDismiss}
            disabled={isUpdating}
          >
            <CloseIcon fontSize="small" />
          </IconButton>
        }
      >
        <Stack
          direction="row"
          alignItems="center"
          spacing={2}
          sx={{ width: "100%", flexWrap: { xs: "wrap", sm: "nowrap" } }}
        >
          <Stack sx={{ minWidth: 0, flexGrow: 1 }}>
            <strong>Update Available</strong>
            <span>
              LinuxIO {updateInfo.latest_version} is available. You are on{" "}
              {updateInfo.current_version}.
            </span>
          </Stack>

          <Stack direction="row" spacing={1} alignItems="center">
            <Button
              variant="contained"
              size="small"
              startIcon={!isUpdating ? <DownloadIcon /> : null}
              onClick={handleUpdate}
              disabled={isUpdating}
              sx={{ whiteSpace: "nowrap" }}
            >
              {isUpdating ? "Updating..." : "Update Now"}
            </Button>

            {updateInfo.release_url && (
              <Button
                variant="outlined"
                size="small"
                component={Link}
                href={updateInfo.release_url}
                target="_blank"
                rel="noopener noreferrer"
                disabled={isUpdating}
                sx={{ whiteSpace: "nowrap" }}
              >
                Release Notes
              </Button>
            )}
          </Stack>
        </Stack>
      </Alert>
    </>
  );
};

export default UpdateBanner;
