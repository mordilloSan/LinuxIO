import CloseIcon from "@mui/icons-material/Close";
import DownloadIcon from "@mui/icons-material/Download";
import { Alert, Button, IconButton, Link, Stack } from "@mui/material";
import { useState, useEffect, useRef } from "react";

import UpdateDialog from "./UpdateDialog";

import { getStreamMux } from "@/api/linuxio";
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
  const { startUpdate, status, progress, output, error, isUpdating } =
    useLinuxIOUpdater();
  const waitingForReconnectRef = useRef(false);
  const hasDisconnectedRef = useRef(false);

  // Auto-reload when WebSocket reconnects after update
  useEffect(() => {
    if (!waitingForReconnectRef.current) return;

    const mux = getStreamMux();
    if (!mux) return;

    const unsubscribe = mux.addStatusListener((newStatus) => {
      console.log(
        `[UpdateBanner] WebSocket status: ${newStatus}, hasDisconnected=${hasDisconnectedRef.current}`,
      );

      // Track when WebSocket disconnects (service restarting)
      if (newStatus === "closed" || newStatus === "error") {
        hasDisconnectedRef.current = true;
      }

      // When it reconnects after being disconnected, reload the page
      if (
        newStatus === "open" &&
        hasDisconnectedRef.current &&
        waitingForReconnectRef.current
      ) {
        console.log("[UpdateBanner] Service reconnected, reloading page...");
        sessionStorage.removeItem("update_info");
        window.location.reload();
      }
    });

    return () => unsubscribe();
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
    waitingForReconnectRef.current = false;
    hasDisconnectedRef.current = false;

    try {
      await startUpdate(updateInfo.latest_version);

      // Start monitoring for reconnection
      waitingForReconnectRef.current = true;

      // Fallback: if reconnection detection fails, reload after 10 seconds
      setTimeout(() => {
        if (waitingForReconnectRef.current) {
          console.log(
            "[UpdateBanner] Fallback timeout reached, reloading page...",
          );
          sessionStorage.removeItem("update_info");
          window.location.reload();
        }
      }, 10000);
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
        canClose={!isUpdating}
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
