import { Icon } from "@iconify/react";
import { useTheme } from "@mui/material/styles";

import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import React, { useMemo, useState } from "react";

import UpdateHistory from "./UpdateHistory";
import UpdateSettingsDialog from "./UpdateSettingsDialog";
import UpdateStatus from "./UpdateStatus";

import { linuxio } from "@/api";
import { TabContainer } from "@/components/tabbar";
import AppTooltip from "@/components/ui/AppTooltip";
import { usePackageUpdater } from "@/hooks/usePackageUpdater";

const Updates: React.FC = () => {
  const theme = useTheme();
  const [settingsOpen, setSettingsOpen] = useState(false);

  // Query updates - use GetUpdatesBasic for fast initial load
  // This skips the slow GetUpdateDetail D-Bus call
  const {
    data: rawUpdates,
    isPending: isLoading,
    refetch,
  } = linuxio.dbus.get_updates_basic.useQuery({
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
        tabs={[
          {
            value: "updates",
            label: "Updates",
            component: (
              <UpdateStatus
                updates={updates}
                isLoading={isLoading}
                onUpdateOne={updateOne}
                updatingPackage={updatingPackage}
                progress={progress}
                status={status}
                eventLog={eventLog}
                error={error}
                onClearError={clearError}
                onCancel={cancelUpdate}
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
                <AppTooltip title="Update settings">
                  <AppIconButton
                    size="small"
                    aria-label="Open update settings"
                    onClick={() => setSettingsOpen(true)}
                  >
                    <Icon icon="mdi:cog" width={20} height={20} />
                  </AppIconButton>
                </AppTooltip>
                {updates.length > 0 ? (
                  <AppButton
                    variant="contained"
                    size="small"
                    startIcon={
                      <Icon icon="mdi:refresh" width={20} height={20} />
                    }
                    disabled={!!updatingPackage || isLoading}
                    onClick={() => updateAll(updates.map((u) => u.package_id))}
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
        defaultTab="updates"
        urlParam="updateTab"
      />

      <UpdateSettingsDialog
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
      />
    </>
  );
};

export default Updates;
