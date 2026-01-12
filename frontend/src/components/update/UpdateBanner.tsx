import CloseIcon from "@mui/icons-material/Close";
import DownloadIcon from "@mui/icons-material/Download";
import { Alert, Button, IconButton, Link, Stack } from "@mui/material";
import { useState, useEffect, useRef } from "react";

import UpdateDialog from "./UpdateDialog";

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
  const [targetVersion, setTargetVersion] = useState<string | null>(null);
  const {
    startUpdate,
    setProgress,
    status,
    progress,
    output,
    error,
    isUpdating,
  } = useLinuxIOUpdater();
  const pollingIntervalRef = useRef<ReturnType<typeof setInterval> | null>(
    null,
  );

  // Poll version endpoint to detect when server is back up after update
  useEffect(() => {
    return () => {
      if (pollingIntervalRef.current) {
        clearInterval(pollingIntervalRef.current);
        pollingIntervalRef.current = null;
      }
    };
  }, []);

  const startVersionPolling = () => {
    console.log("[UpdateBanner] Starting version polling in 5 seconds...");
    setProgress(80); // Waiting for server to come back up

    setTimeout(() => {
      console.log("[UpdateBanner] Polling /api/version for server recovery...");
      pollingIntervalRef.current = setInterval(async () => {
        try {
          const response = await fetch("/api/version", {
            method: "GET",
            headers: { "Content-Type": "application/json" },
          });

          if (response.ok) {
            const versions = await response.json();
            console.log(
              "[UpdateBanner] Server is back up! Installed versions:",
              versions,
            );

            if (pollingIntervalRef.current) {
              clearInterval(pollingIntervalRef.current);
              pollingIntervalRef.current = null;
            }

            // Check if update was successful by comparing versions
            // Look for any component with the target version
            const hasTargetVersion = targetVersion
              ? Object.values(versions).some((v) => v === targetVersion)
              : true;

            setProgress(100); // Update complete!
            setUpdateComplete(true);
            setUpdateSuccess(hasTargetVersion);

            if (!hasTargetVersion && targetVersion) {
              console.warn(
                "[UpdateBanner] Update may have failed - target version not found in:",
                versions,
              );
            }
          }
        } catch (err) {
          // Server still down, keep polling
          console.log("[UpdateBanner] Version check failed, retrying...", err);
        }
      }, 2000); // Poll every 2 seconds
    }, 5000); // Wait 5 seconds before starting
  };

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
    setTargetVersion(updateInfo.latest_version || null);

    try {
      await startUpdate(updateInfo.latest_version);

      // Start polling version endpoint to detect when server is back up
      startVersionPolling();
    } catch (err) {
      console.error("Update failed:", err);
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
