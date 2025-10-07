import { Box, Fade } from "@mui/material";
import React, { useState } from "react";

import ContainerList from "./ContainerList";
import ImageList from "./ImageList";
import DockerNetworksTable from "./NetworkList";

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

      <Fade in={tab === "images"} timeout={300} unmountOnExit={false}>
        <Box
          sx={{
            display: tab === "images" ? "block" : "none",
            position: "relative",
            width: "100%",
          }}
        >
          <ImageList />
        </Box>
      </Fade>

      <Fade in={tab === "networks"} timeout={300} unmountOnExit={false}>
        <Box
          sx={{
            display: tab === "networks" ? "block" : "none",
            position: "relative",
            width: "100%",
          }}
        >
          <DockerNetworksTable />
        </Box>
      </Fade>
    </Box>
  );
};

export default DockerPage;
