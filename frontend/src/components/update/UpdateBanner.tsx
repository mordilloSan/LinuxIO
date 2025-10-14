import { Close, Download } from "@mui/icons-material";
import { Alert, Button, IconButton, Link, Stack } from "@mui/material";
import { useState } from "react";

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
      const response = await fetch("/control/update", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({}), // Empty for latest version
      });

      if (!response.ok) {
        throw new Error("Update request failed");
      }

      const result = await response.json();

      if (result.success) {
        alert("✅ Update complete! Please refresh the page.");
        sessionStorage.removeItem("update_info");
        window.location.reload();
      } else {
        throw new Error(result.error || "Update failed");
      }
    } catch (error) {
      console.error("Update failed:", error);
      alert(
        "❌ Update failed. Please try manually:\n\n" +
        "sudo linuxio-update\n\n" +
        `Error: ${error instanceof Error ? error.message : "Unknown error"}`,
      );
    } finally {
      setIsUpdating(false);
    }
  };

  if (!updateInfo.available) {
    console.log("No update available, not showing banner.");
    return null;
  }

  return (
    <Alert
      severity="info"
      sx={{
        mx: { xs: 6, md: 8 },
        mt: 0,
        mb: 0,
        borderRadius: 2,
      }}
      action={
        <Stack direction="row" spacing={1} alignItems="center">
          <Button
            variant="contained"
            size="small"
            startIcon={isUpdating ? null : <Download />}
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
              sx={{ whiteSpace: "nowrap" }}
            >
              Release Notes
            </Button>
          )}

          <IconButton
            aria-label="close"
            color="inherit"
            size="small"
            onClick={onDismiss}
            disabled={isUpdating}
          >
            <Close fontSize="small" />
          </IconButton>
        </Stack>
      }
    >
      <Stack>
        <strong>Update Available</strong>
        <span>
          LinuxIO {updateInfo.latest_version} is available. You are on{" "}
          {updateInfo.current_version}.
        </span>
      </Stack>
    </Alert>
  );
};

export default UpdateBanner;