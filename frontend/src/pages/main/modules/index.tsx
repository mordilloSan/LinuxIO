import { Fade } from "@mui/material";
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
    <div style={{ paddingLeft: 8, paddingRight: 8 }}>
      <TabSelector value={tab} onChange={setTab} options={tabOptions} />

      <div style={{ position: "relative", minHeight: 400 }}>
        <Fade in={tab === "installed"} timeout={300} unmountOnExit={false}>
          <div
            style={{
              display: tab === "installed" ? "block" : "none",
              position: "absolute",
              width: "100%",
            }}
          >
            <ModulesList />
          </div>
        </Fade>

        <Fade in={tab === "install"} timeout={300} unmountOnExit={false}>
          <div
            style={{
              display: tab === "install" ? "block" : "none",
              position: "absolute",
              width: "100%",
            }}
          >
            <InstallModule onInstalled={() => setTab("installed")} />
          </div>
        </Fade>
      </div>
    </div>
  );
};

export default ModulesPage;
