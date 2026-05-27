import { Icon } from "@iconify/react";
import React, { useMemo, useState } from "react";

import { linuxio } from "@/api";
import { TabContainer } from "@/components/tabbar";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useCapability } from "@/hooks/useCapabilities";
import { usePackageUpdater } from "@/hooks/usePackageUpdater";
import { useAppTheme } from "@/theme";

import UpdateHistory from "./UpdateHistory";
import UpdateSettingsDialog from "./UpdateSettingsDialog";
import UpdateStatus from "./UpdateStatus";

const Updates: React.FC = () => {
  const theme = useAppTheme();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const { status: packageKitStatus, reason: packageKitReason } = useCapability(
    "packageKitAvailable",
  );
  const packageKitUnavailable = packageKitStatus === "unavailable";
  const {
    data: rawUpdates,
    isPending: isLoading,
    refetch,
  } = linuxio.updates.get_updates_basic.useQuery({
    enabled: !packageKitUnavailable,
    refetchInterval: 50000,
  });

  const updates = useMemo(() => rawUpdates || [], [rawUpdates]);
  const {
    updateOne,
    updateAll,
    cancelUpdate,
    updatingPackage,
    progress,
    status,
    eventLog,
    error,
    clearError,
  } = usePackageUpdater(refetch);

  return (
    <>
      <TabContainer
        containerStyle={{ paddingInline: 0 }}
        defaultTab="updates"
        tabs={[
          {
            value: "updates",
            label: "Updates",
            component: packageKitUnavailable ? (
              <AppAlert severity="warning">
                <AppAlertTitle>PackageKit unavailable</AppAlertTitle>
                {packageKitReason}
              </AppAlert>
            ) : (
              <UpdateStatus
                error={error}
                eventLog={eventLog}
                isLoading={isLoading}
                onCancel={cancelUpdate}
                onClearError={clearError}
                onUpdateOne={updateOne}
                progress={progress}
                status={status}
                updates={updates}
                updatingPackage={updatingPackage}
              />
            ),
            rightContent: (
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: theme.spacing(1),
                }}
              >
                {!packageKitUnavailable ? (
                  <AppTooltip title="Update settings">
                    <AppIconButton
                      aria-label="Open update settings"
                      onClick={() => setSettingsOpen(true)}
                      size="small"
                    >
                      <Icon height={20} icon="mdi:cog" width={20} />
                    </AppIconButton>
                  </AppTooltip>
                ) : null}
                {!packageKitUnavailable && updates.length > 0 ? (
                  <AppButton
                    disabled={!!updatingPackage || isLoading}
                    onClick={() => updateAll(updates.map((u) => u.package_id))}
                    size="small"
                    startIcon={
                      <Icon height={20} icon="mdi:refresh" width={20} />
                    }
                    variant="contained"
                  >
                    Update All ({updates.length})
                  </AppButton>
                ) : null}
              </div>
            ),
          },
          {
            value: "history",
            label: "History",
            component: <UpdateHistory />,
          },
        ]}
        urlParam="updateTab"
      />

      <UpdateSettingsDialog
        onClose={() => setSettingsOpen(false)}
        open={settingsOpen}
      />
    </>
  );
};

export default Updates;
