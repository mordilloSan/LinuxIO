import { Box, Fade } from "@mui/material";
import React, { useState } from "react";

import UpdateHistory from "./UpdateHistory";
import UpdateSettings from "./UpdateSettings";
import UpdateStatus from "./UpdateStatus";

import TabSelector from "@/components/tabbar/TabSelector";

const tabOptions = [
  { value: "updates", label: "Updates" },
  { value: "history", label: "History" },
  { value: "settings", label: "Settings" },
];

const Updates: React.FC = () => {
  const [tab, setTab] = useState("updates");

  return (
    <Box sx={{ px: 2 }}>
      <TabSelector value={tab} onChange={setTab} options={tabOptions} />
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
