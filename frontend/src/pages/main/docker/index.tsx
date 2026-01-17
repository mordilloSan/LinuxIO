import { Add as AddIcon } from "@mui/icons-material";
import { Box, Button } from "@mui/material";
import React, { useState } from "react";

import ComposeStacksPage from "./ComposeStacksPage";
import ContainerList from "./ContainerList";
import ImageList from "./ImageList";
import DockerNetworksTable from "./NetworkList";
import VolumeList from "./VolumeList";

import { TabContainer } from "@/components/tabbar";

const DockerPage: React.FC = () => {
  const [createStackHandler, setCreateStackHandler] = useState<
    (() => void) | null
  >(null);
  const [createNetworkHandler, setCreateNetworkHandler] = useState<
    (() => void) | null
  >(null);

  return (
    <TabContainer
      tabs={[
        {
          value: "containers",
          label: "Containers",
          component: <ContainerList />,
        },
        {
          value: "compose",
          label: "Stacks",
          component: (
            <ComposeStacksPage
              onMountCreateHandler={(handler) =>
                setCreateStackHandler(() => handler)
              }
            />
          ),
          rightContent: createStackHandler ? (
            <Button
              variant="contained"
              size="small"
              startIcon={
                <AddIcon sx={{ display: { xs: "none", sm: "block" } }} />
              }
              onClick={createStackHandler}
              sx={{
                minWidth: { xs: "auto", sm: "auto" },
                px: { xs: 1, sm: 2 },
              }}
            >
              <Box
                component="span"
                sx={{ display: { xs: "none", sm: "inline" } }}
              >
                Create Stack
              </Box>
              <AddIcon sx={{ display: { xs: "block", sm: "none" } }} />
            </Button>
          ) : undefined,
        },
        {
          value: "networks",
          label: "Networks",
          component: (
            <DockerNetworksTable
              onMountCreateHandler={(handler) =>
                setCreateNetworkHandler(() => handler)
              }
            />
          ),
          rightContent: createNetworkHandler ? (
            <Button
              variant="contained"
              size="small"
              startIcon={
                <AddIcon sx={{ display: { xs: "none", sm: "block" } }} />
              }
              onClick={createNetworkHandler}
              sx={{
                minWidth: { xs: "auto", sm: "auto" },
                px: { xs: 1, sm: 2 },
              }}
            >
              <Box
                component="span"
                sx={{ display: { xs: "none", sm: "inline" } }}
              >
                Add Network
              </Box>
              <AddIcon sx={{ display: { xs: "block", sm: "none" } }} />
            </Button>
          ) : undefined,
        },
        {
          value: "volumes",
          label: "Volumes",
          component: <VolumeList />,
        },
        {
          value: "images",
          label: "Images",
          component: <ImageList />,
        },
      ]}
      defaultTab="containers"
      urlParam="dockerTab"
    />
  );
};

export default DockerPage;
