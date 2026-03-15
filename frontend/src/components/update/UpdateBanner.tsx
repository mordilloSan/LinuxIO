import { Icon } from "@iconify/react";
import { Alert, Button, IconButton, Link, useMediaQuery } from "@mui/material";
import { useTheme } from "@mui/material/styles";

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
  const theme = useTheme();
  const isSmallUp = useMediaQuery(theme.breakpoints.up("sm"));
  const {
    startUpdate,
    resetUpdate,
    phase,
    status,
    progress,
    output,
    error,
    isUpdating,
    updateComplete,
    updateSuccess,
    targetVersion,
  } = useLinuxIOUpdater();

  const handleUpdate = () => {
    if (
      !confirm(
        `Update LinuxIO from ${updateInfo.current_version} to ${updateInfo.latest_version}?\n\n` +
          "The service will restart automatically.",
      )
    ) {
      return;
    }

    startUpdate(updateInfo.latest_version);
  };

  const handleCloseDialog = () => {
    if (!isUpdating) resetUpdate();
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
        open={phase !== "idle"}
        status={error || status}
        progress={progress}
        output={output}
        onClose={handleCloseDialog}
        canClose={!isUpdating && !updateSuccess}
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
            <Icon icon="mdi:close" width={18} height={18} />
          </IconButton>
        }
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: theme.spacing(2),
            width: "100%",
            flexWrap: isSmallUp ? "nowrap" : "wrap",
          }}
        >
          <div style={{ minWidth: 0, flexGrow: 1 }}>
            <strong>Update Available</strong>
            <span>
              LinuxIO {updateInfo.latest_version} is available. You are on{" "}
              {updateInfo.current_version}.
            </span>
          </div>

          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: theme.spacing(1),
            }}
          >
            <Button
              variant="contained"
              size="small"
              startIcon={
                !isUpdating ? (
                  <Icon icon="mdi:download" width={20} height={20} />
                ) : null
              }
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
          </div>
        </div>
      </Alert>
    </>
  );
};

export default UpdateBanner;
