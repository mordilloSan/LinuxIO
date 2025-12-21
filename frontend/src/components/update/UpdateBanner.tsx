import CloseIcon from "@mui/icons-material/Close";
import DownloadIcon from "@mui/icons-material/Download";
import { Alert, Button, IconButton, Link, Stack } from "@mui/material";
import { useState } from "react";

import { streamApi } from "@/utils/streamApi";

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
  const [isUpdating, setIsUpdating] = useState(false);

  const handleUpdate = async () => {
    if (
      !confirm(
        `Update LinuxIO from ${updateInfo.current_version} to ${updateInfo.latest_version}?\n\n` +
          "The service will restart. This may take a minute.",
      )
    ) {
      return;
    }

    setIsUpdating(true);
    try {
      await streamApi.post("control", "update");
      sessionStorage.removeItem("update_info");

      // Wait before attempting reload
      setTimeout(() => {
        window.location.reload();
      }, 5000);
    } catch (error) {
      console.error("Update failed:", error);
      const msg = error instanceof Error ? error.message : "Unknown error";
      alert(
        "❌ Update failed. Please try manually:\n\n" +
          "sudo linuxio-update\n\n" +
          `Error: ${msg}`,
      );
      setIsUpdating(false);
    }
  };

  if (!updateInfo.available) {
    return null;
  }

  return (
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
  );
};

export default UpdateBanner;
