import RefreshIcon from "@mui/icons-material/Refresh";
import { Box, Fade, Button } from "@mui/material";
import React, { useMemo, useState } from "react";

import UpdateHistory from "./UpdateHistory";
import UpdateSettings from "./UpdateSettings";
import UpdateStatus from "./UpdateStatus";

import linuxio from "@/api/react-query";
import TabSelector from "@/components/tabbar/TabSelector";
import { usePackageUpdater } from "@/hooks/usePackageUpdater";

const tabOptions = [
  { value: "updates", label: "Updates" },
  { value: "history", label: "History" },
  { value: "settings", label: "Settings" },
];

const Updates: React.FC = () => {
  const [tab, setTab] = useState("updates");

  // Query updates - use GetUpdatesBasic for fast initial load
  // This skips the slow GetUpdateDetail D-Bus call
  const {
    data: rawUpdates,
    isPending: isLoading,
    refetch,
  } = linuxio.dbus.GetUpdatesBasic.useQuery({
    enabled: tab === "updates", // Only fetch when on updates tab
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

  // Determine what button to show based on active tab
  const getRightContent = () => {
    if (tab === "updates" && updates.length > 0) {
      return (
        <Button
          variant="contained"
          size="small"
          startIcon={<RefreshIcon />}
          disabled={!!updatingPackage || isLoading}
          onClick={() => updateAll(updates.map((u) => u.package_id))}
        >
          Update All ({updates.length})
        </Button>
      );
    }

    // No buttons for history or settings
    return null;
  };

  return (
    <Box sx={{ px: 2 }}>
      <TabSelector
        value={tab}
        onChange={setTab}
        options={tabOptions}
        rightContent={getRightContent()}
      />
      <Box sx={{ position: "relative", minHeight: 400 }}>
        <Fade in={tab === "updates"} timeout={300} unmountOnExit={false}>
          <Box
            sx={{
              display: tab === "updates" ? "block" : "none",
              position: "absolute",
              width: "100%",
            }}
          >
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
          </Box>
        </Fade>

        <Fade in={tab === "history"} timeout={300} unmountOnExit={false}>
          <Box
            sx={{
              display: tab === "history" ? "block" : "none",
              position: "absolute",
              width: "100%",
            }}
          >
            <UpdateHistory />
          </Box>
        </Fade>

        <Fade in={tab === "settings"} timeout={300} unmountOnExit={false}>
          <Box
            sx={{
              display: tab === "settings" ? "block" : "none",
              position: "absolute",
              width: "100%",
            }}
          >
            <UpdateSettings />
          </Box>
        </Fade>
      </Box>
    </Box>
  );
};

export default Updates;
