import { Box, Fade } from "@mui/material";
import React, { useState } from "react";

import ComposeStacksPage from "./ComposeStacksPage";
import ContainerList from "./ContainerList";
import ImageList from "./ImageList";
import DockerNetworksTable from "./NetworkList";
import VolumeList from "./VolumeList";

import ErrorBoundary from "@/components/errors/ErrorBoundary";
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
          <ErrorBoundary>
            <ContainerList />
          </ErrorBoundary>
        </Box>
      </Fade>

      <Fade in={tab === "compose"} timeout={300} unmountOnExit={false}>
        <Box
          sx={{
            display: tab === "compose" ? "block" : "none",
            position: "relative",
            width: "100%",
          }}
        >
          <ErrorBoundary>
            <ComposeStacksPage />
          </ErrorBoundary>
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
          <ErrorBoundary>
            <ImageList />
          </ErrorBoundary>
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
          <ErrorBoundary>
            <DockerNetworksTable />
          </ErrorBoundary>
        </Box>
      </Fade>

      <Fade in={tab === "volumes"} timeout={300} unmountOnExit={false}>
        <Box
          sx={{
            display: tab === "volumes" ? "block" : "none",
            position: "relative",
            width: "100%",
          }}
        >
          <ErrorBoundary>
            <VolumeList />
          </ErrorBoundary>
        </Box>
      </Fade>
    </Box>
  );
};

export default DockerPage;
