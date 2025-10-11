import RefreshIcon from "@mui/icons-material/Refresh";
import { Box, Fade, Button } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React, { useMemo, useState } from "react";

import UpdateHistory from "./UpdateHistory";
import UpdateSettings from "./UpdateSettings";
import UpdateStatus from "./UpdateStatus";

import TabSelector from "@/components/tabbar/TabSelector";
import { Update } from "@/types/update";
import axios from "@/utils/axios";

const tabOptions = [
  { value: "updates", label: "Updates" },
  { value: "history", label: "History" },
  { value: "settings", label: "Settings" },
];

const Updates: React.FC = () => {
  const [tab, setTab] = useState("updates");

  // Query updates for the button
  const { data } = useQuery<{ updates: Update[] }>({
    queryKey: ["updateInfo"],
    queryFn: () => axios.get("/updates/packages").then((res) => res.data),
    enabled: tab === "updates", // Only fetch when on updates tab
    refetchInterval: 50000,
  });

  const updates = useMemo(() => data?.updates || [], [data]);

  // Determine what button to show based on active tab
  const getRightContent = () => {
    if (tab === "updates" && updates.length > 0) {
      return (
        <Button
          variant="contained"
          size="small"
          startIcon={<RefreshIcon />}
          onClick={() => {
            // We'll pass this function down to UpdateStatus
            // or trigger it via a ref/callback
          }}
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
            <UpdateStatus />
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
