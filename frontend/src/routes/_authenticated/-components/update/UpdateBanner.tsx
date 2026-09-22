import { Icon } from "@iconify/react";
import { useState } from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinkButton from "@/components/ui/AppLinkButton";
import { useLinuxIOUpdater } from "@/hooks/useLinuxIOUpdater";
import { useAppMediaQuery } from "@/theme";
import { up } from "@/theme/breakpoints";
import type { UpdateInfo } from "@/types/auth";

import UpdateDialog from "./UpdateDialog";

interface UpdateBannerProps {
  onDismiss: () => void;
  updateInfo: UpdateInfo;
}

const UpdateBanner = ({ updateInfo, onDismiss }: UpdateBannerProps) => {
  const isSmallUp = useAppMediaQuery(up("sm"));
  const [confirmOpen, setConfirmOpen] = useState(false);
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
    setConfirmOpen(true);
  };

  const confirmUpdate = () => {
    setConfirmOpen(false);
    startUpdate(updateInfo.latest_version);
  };

  const handleCloseDialog = () => {
    if (!isUpdating) resetUpdate();
  };

  const handleContinue = () => {
    // Clear update info and reload to login page
    try {
      sessionStorage.removeItem("update_info");
      sessionStorage.setItem("update_info_checked", "1");
    } catch {
      // Storage may be unavailable; the reload still completes the update flow.
    }
    window.location.reload();
  };

  if (!updateInfo.available) {
    return null;
  }

  return (
    <>
      <GeneralDialog
        fullWidth
        maxWidth="xs"
        onClose={() => setConfirmOpen(false)}
        open={confirmOpen}
      >
        <AppDialogTitle>Update LinuxIO</AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            Update LinuxIO from {updateInfo.current_version} to{" "}
            {updateInfo.latest_version}?
          </AppDialogContentText>
          <AppDialogContentText>
            The service will restart automatically.
          </AppDialogContentText>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={() => setConfirmOpen(false)}>Cancel</AppButton>
          <AppButton onClick={confirmUpdate} variant="contained">
            Update
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
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
            aria-label="Dismiss update notification"
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
          borderRadius: "var(--app-radius-card)",
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
            gap: "var(--app-space-8)",
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
              gap: "var(--app-space-4)",
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
              <AppLinkButton
                href={updateInfo.release_url}
                rel="noopener noreferrer"
                style={{ whiteSpace: "nowrap" }}
                target="_blank"
                disabled={isUpdating}
                size="small"
                variant="outlined"
              >
                Release Notes
              </AppLinkButton>
            )}
          </div>
        </div>
      </AppAlert>
    </>
  );
};

export default UpdateBanner;
