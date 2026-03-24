import React, { useState } from "react";

import InstallModule from "./InstallModule";
import ModulesList from "./ModulesList";

import TabPanel from "@/components/tabbar/TabPanel";
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
        <TabPanel value="installed" activeTab={tab} timeout={300}>
          <ModulesList />
        </TabPanel>

        <TabPanel value="install" activeTab={tab} timeout={300}>
          <InstallModule onInstalled={() => setTab("installed")} />
        </TabPanel>
      </div>
    </div>
  );
};

export default ModulesPage;
