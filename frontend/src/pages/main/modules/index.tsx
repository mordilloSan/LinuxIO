import { Box, Fade } from "@mui/material";
import React, { useState } from "react";

import InstallModule from "./InstallModule";
import ModulesList from "./ModulesList";

import TabSelector from "@/components/tabbar/TabSelector";

const tabOptions = [
  { value: "installed", label: "Installed" },
  { value: "install", label: "Install" },
];

const ModulesPage: React.FC = () => {
  const [tab, setTab] = useState("installed");

  return (
    <Box sx={{ px: 2 }}>
      <TabSelector value={tab} onChange={setTab} options={tabOptions} />

      <Box sx={{ position: "relative", minHeight: 400 }}>
        <Fade in={tab === "installed"} timeout={300} unmountOnExit={false}>
          <Box
            sx={{
              display: tab === "installed" ? "block" : "none",
              position: "absolute",
              width: "100%",
            }}
          >
            <ModulesList />
          </Box>
        </Fade>

        <Fade in={tab === "install"} timeout={300} unmountOnExit={false}>
          <Box
            sx={{
              display: tab === "install" ? "block" : "none",
              position: "absolute",
              width: "100%",
            }}
          >
            <InstallModule onInstalled={() => setTab("installed")} />
          </Box>
        </Fade>
      </Box>
    </Box>
  );
};

export default ModulesPage;
