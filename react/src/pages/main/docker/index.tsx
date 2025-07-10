import { Box, Fade } from "@mui/material";
import React, { useState } from "react";

import ContainerList from "./ContainerList";

import TabSelector from "@/components/tabbar/TabSelector";

const tabOptions = [
  { value: "containers", label: "Containers" },
  { value: "compose", label: "Stacks" },
  { value: "networks", label: "Networks" },
  { value: "volumes", label: "Volumes" },
  { value: "images", label: "Images" },
];

const DockerPage: React.FC = () => {
  const [tab, setTab] = useState("containers");

  return (
    <Box sx={{ px: 2, position: "relative" }}>
      <TabSelector value={tab} onChange={setTab} options={tabOptions} />
      <Fade in={tab === "containers"} timeout={300} unmountOnExit={false}>
        <Box
          sx={{
            display: tab === "containers" ? "block" : "none",
            position: "relative",
            width: "100%",
          }}
        >
          <ContainerList />
        </Box>
      </Fade>
    </Box>
  );
};

export default DockerPage;
