import RefreshIcon from "@mui/icons-material/Refresh";
import SettingsIcon from "@mui/icons-material/Settings";
import { Button, IconButton, Stack, Tooltip } from "@mui/material";
import React, { useMemo, useState } from "react";

import UpdateHistory from "./UpdateHistory";
import UpdateSettingsDialog from "./UpdateSettingsDialog";
import UpdateStatus from "./UpdateStatus";

import { linuxio } from "@/api";
import { TabContainer } from "@/components/tabbar";
import { usePackageUpdater } from "@/hooks/usePackageUpdater";

const Updates: React.FC = () => {
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
              <Stack direction="row" alignItems="center" sx={{ gap: 1 }}>
                <Tooltip title="Update settings">
                  <IconButton
                    size="small"
                    aria-label="Open update settings"
                    onClick={() => setSettingsOpen(true)}
                  >
                    <SettingsIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
                {updates.length > 0 ? (
                  <Button
                    variant="contained"
                    size="small"
                    startIcon={<RefreshIcon />}
                    disabled={!!updatingPackage || isLoading}
                    onClick={() => updateAll(updates.map((u) => u.package_id))}
                  >
                    Update All ({updates.length})
                  </Button>
                ) : null}
              </Stack>
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
