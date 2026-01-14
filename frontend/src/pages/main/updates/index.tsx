import RefreshIcon from "@mui/icons-material/Refresh";
import { Button } from "@mui/material";
import React, { useMemo } from "react";

import UpdateHistory from "./UpdateHistory";
import UpdateSettings from "./UpdateSettings";
import UpdateStatus from "./UpdateStatus";

import linuxio from "@/api/react-query";
import { TabContainer } from "@/components/tabbar";
import { usePackageUpdater } from "@/hooks/usePackageUpdater";

const Updates: React.FC = () => {
  // Query updates - use GetUpdatesBasic for fast initial load
  // This skips the slow GetUpdateDetail D-Bus call
  const {
    data: rawUpdates,
    isPending: isLoading,
    refetch,
  } = linuxio.dbus.GetUpdatesBasic.useQuery({
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
    error,
    clearError,
  } = usePackageUpdater(refetch);

  return (
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
              error={error}
              onClearError={clearError}
              onCancel={cancelUpdate}
              onComplete={refetch}
            />
          ),
          rightContent:
            updates.length > 0 ? (
              <Button
                variant="contained"
                size="small"
                startIcon={<RefreshIcon />}
                disabled={!!updatingPackage || isLoading}
                onClick={() => updateAll(updates.map((u) => u.package_id))}
              >
                Update All ({updates.length})
              </Button>
            ) : null,
        },
        {
          value: "history",
          label: "History",
          component: <UpdateHistory />,
        },
        {
          value: "settings",
          label: "Settings",
          component: <UpdateSettings />,
        },
      ]}
      defaultTab="updates"
      urlParam="updateTab"
    />
  );
};

export default Updates;
