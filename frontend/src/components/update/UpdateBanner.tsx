import CloseIcon from "@mui/icons-material/Close";
import DownloadIcon from "@mui/icons-material/Download";
import { Alert, Button, IconButton, Link, Stack } from "@mui/material";
import { useState, useEffect, useRef } from "react";

import UpdateDialog from "./UpdateDialog";

import { getStreamMux, initStreamMux } from "@/api/linuxio";
import { call } from "@/api/linuxio-core";
import type { VersionResponse } from "@/api/linuxio-types";
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
  const [verifiedVersion, setVerifiedVersion] = useState<string | null>(null);
  const [targetVersion, setTargetVersion] = useState<string | null>(null);
  const { startUpdate, status, progress, output, error, isUpdating } =
    useLinuxIOUpdater();
  const waitingForReconnectRef = useRef(false);
  const hasDisconnectedRef = useRef(false);
  const targetVersionRef = useRef<string | null>(null); // Also keep ref for use in async callbacks

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

                // Listen for this mux to open and verify version
                const checkAndShowSuccess = async (status: string) => {
                  if (status === "open") {
                    console.log(
                      "[UpdateBanner] Reconnected! Verifying version...",
                    );
                    // Clear interval to stop further attempts
                    if (reconnectInterval) {
                      clearInterval(reconnectInterval);
                      reconnectInterval = null;
                    }
                    waitingForReconnectRef.current = false;

                    // Verify the version actually changed
                    try {
                      const versionInfo = await call<VersionResponse>(
                        "control",
                        "version",
                      );
                      const currentVersion = versionInfo.current_version;
                      const targetVersion = targetVersionRef.current;

                      console.log(
                        `[UpdateBanner] Version check: current=${currentVersion}, target=${targetVersion}`,
                      );
                      setVerifiedVersion(currentVersion);

                      if (targetVersion && currentVersion === targetVersion) {
                        console.log(
                          "[UpdateBanner] Version verified! Update successful.",
                        );
                        setUpdateComplete(true);
                        setUpdateSuccess(true);
                      } else if (
                        targetVersion &&
                        currentVersion !== targetVersion
                      ) {
                        console.warn(
                          `[UpdateBanner] Version mismatch: expected ${targetVersion}, got ${currentVersion}`,
                        );
                        setUpdateComplete(true);
                        setUpdateSuccess(false);
                      } else {
                        // No target version to compare, assume success
                        setUpdateComplete(true);
                        setUpdateSuccess(true);
                      }
                    } catch (err) {
                      console.error(
                        "[UpdateBanner] Failed to verify version:",
                        err,
                      );
                      // Version check failed, but service is up - show success with warning
                      setUpdateComplete(true);
                      setUpdateSuccess(true);
                    }
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

        // When it reconnects after being disconnected, verify version
        if (newStatus === "open" && hasDisconnectedRef.current) {
          console.log(
            "[UpdateBanner] Service reconnected! Verifying version...",
          );
          waitingForReconnectRef.current = false;

          // Verify the version actually changed
          (async () => {
            try {
              const versionInfo = await call<VersionResponse>(
                "control",
                "version",
              );
              const currentVersion = versionInfo.current_version;
              const targetVersion = targetVersionRef.current;

              console.log(
                `[UpdateBanner] Version check: current=${currentVersion}, target=${targetVersion}`,
              );
              setVerifiedVersion(currentVersion);

              if (targetVersion && currentVersion === targetVersion) {
                console.log(
                  "[UpdateBanner] Version verified! Update successful.",
                );
                setUpdateComplete(true);
                setUpdateSuccess(true);
              } else if (targetVersion && currentVersion !== targetVersion) {
                console.warn(
                  `[UpdateBanner] Version mismatch: expected ${targetVersion}, got ${currentVersion}`,
                );
                setUpdateComplete(true);
                setUpdateSuccess(false);
              } else {
                // No target version to compare, assume success
                setUpdateComplete(true);
                setUpdateSuccess(true);
              }
            } catch (err) {
              console.error("[UpdateBanner] Failed to verify version:", err);
              setUpdateComplete(true);
              setUpdateSuccess(true);
            }
          })();
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
    setVerifiedVersion(null);
    setTargetVersion(updateInfo.latest_version || null);
    waitingForReconnectRef.current = false;
    hasDisconnectedRef.current = false;
    targetVersionRef.current = updateInfo.latest_version || null;

    try {
      await startUpdate(updateInfo.latest_version);

      // Start monitoring for reconnection
      waitingForReconnectRef.current = true;

      // Fallback: if reconnection detection fails, try to verify after 30 seconds
      setTimeout(async () => {
        if (waitingForReconnectRef.current) {
          console.log(
            "[UpdateBanner] Fallback timeout reached, attempting to verify...",
          );
          waitingForReconnectRef.current = false;

          // Try to initialize connection and verify version
          try {
            const mux = initStreamMux();
            // Wait a bit for connection to establish
            await new Promise((resolve) => setTimeout(resolve, 2000));

            if (mux.status === "open") {
              const versionInfo = await call<VersionResponse>(
                "control",
                "version",
              );
              const currentVersion = versionInfo.current_version;
              const targetVersion = targetVersionRef.current;

              console.log(
                `[UpdateBanner] Fallback version check: current=${currentVersion}, target=${targetVersion}`,
              );
              setVerifiedVersion(currentVersion);

              if (targetVersion && currentVersion === targetVersion) {
                setUpdateComplete(true);
                setUpdateSuccess(true);
              } else if (targetVersion && currentVersion !== targetVersion) {
                setUpdateComplete(true);
                setUpdateSuccess(false);
              } else {
                setUpdateComplete(true);
                setUpdateSuccess(true);
              }
            } else {
              // Connection failed, show as failed
              setUpdateComplete(true);
              setUpdateSuccess(false);
            }
          } catch (err) {
            console.error("[UpdateBanner] Fallback verification failed:", err);
            setUpdateComplete(true);
            setUpdateSuccess(false);
          }
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
        targetVersion={targetVersion}
        verifiedVersion={verifiedVersion}
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
