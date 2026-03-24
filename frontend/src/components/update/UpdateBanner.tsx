import { Icon } from "@iconify/react";

import UpdateDialog from "./UpdateDialog";

import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import { useLinuxIOUpdater } from "@/hooks/useLinuxIOUpdater";
import { useAppTheme, useAppMediaQuery } from "@/theme";

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
  const theme = useAppTheme();
  const isSmallUp = useAppMediaQuery(theme.breakpoints.up("sm"));
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
      <AppAlert
        severity="info"
        style={{
          marginInline: 64,
          marginTop: 0,
          marginBottom: 0,
          borderRadius: 16,
          width: "100%",
          padding: 0,
        }}
        action={
          <AppIconButton
            aria-label="close"
            color="inherit"
            size="small"
            onClick={onDismiss}
            disabled={isUpdating}
          >
            <Icon icon="mdi:close" width={18} height={18} />
          </AppIconButton>
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
            <AppButton
              variant="contained"
              size="small"
              startIcon={
                !isUpdating ? (
                  <Icon icon="mdi:download" width={20} height={20} />
                ) : null
              }
              onClick={handleUpdate}
              disabled={isUpdating}
              style={{ whiteSpace: "nowrap" }}
            >
              {isUpdating ? "Updating..." : "Update Now"}
            </AppButton>

            {updateInfo.release_url && (
              <a
                href={updateInfo.release_url}
                target="_blank"
                rel="noopener noreferrer"
                style={{ textDecoration: "none" }}
              >
                <AppButton
                  variant="outlined"
                  size="small"
                  disabled={isUpdating}
                  style={{ whiteSpace: "nowrap" }}
                >
                  Release Notes
                </AppButton>
              </a>
            )}
          </div>
        </div>
      </AppAlert>
    </>
  );
};

export default UpdateBanner;
