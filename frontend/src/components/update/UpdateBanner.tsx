import { Icon } from "@iconify/react";

import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import { useLinuxIOUpdater } from "@/hooks/useLinuxIOUpdater";
import { useAppMediaQuery, useAppTheme } from "@/theme";

import UpdateDialog from "./UpdateDialog";

interface UpdateInfo {
  available: boolean;
  current_version: string;
  latest_version?: string;
  release_url?: string;
}

interface UpdateBannerProps {
  onDismiss: () => void;
  updateInfo: UpdateInfo;
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
        canClose={!isUpdating && !updateSuccess}
        onClose={handleCloseDialog}
        onContinue={handleContinue}
        open={phase !== "idle"}
        output={output}
        progress={progress}
        status={error || status}
        targetVersion={targetVersion}
        updateComplete={updateComplete}
        updateSuccess={updateSuccess}
      />
      <AppAlert
        action={
          <AppIconButton
            aria-label="close"
            color="inherit"
            disabled={isUpdating}
            onClick={onDismiss}
            size="small"
          >
            <Icon height={18} icon="mdi:close" width={18} />
          </AppIconButton>
        }
        className="app-alert--centered"
        severity="info"
        style={{
          borderRadius: 16,
          alignItems: "center",
          backgroundColor: "var(--update-banner-bg)",
          color: "var(--update-banner-color)",
          padding: "3px 16px",
        }}
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
            {" — "}
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
              disabled={isUpdating}
              onClick={handleUpdate}
              size="small"
              startIcon={
                !isUpdating ? (
                  <Icon height={20} icon="mdi:download" width={20} />
                ) : null
              }
              style={{ whiteSpace: "nowrap" }}
              variant="contained"
            >
              {isUpdating ? "Updating..." : "Update Now"}
            </AppButton>

            {updateInfo.release_url && (
              <a
                href={updateInfo.release_url}
                rel="noopener noreferrer"
                style={{ textDecoration: "none" }}
                target="_blank"
              >
                <AppButton
                  disabled={isUpdating}
                  size="small"
                  style={{ whiteSpace: "nowrap" }}
                  variant="outlined"
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
