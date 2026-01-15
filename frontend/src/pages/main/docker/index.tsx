import { Add as AddIcon } from "@mui/icons-material";
import { Button } from "@mui/material";
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
              startIcon={<AddIcon />}
              onClick={createStackHandler}
            >
              Create Stack
            </Button>
          ) : undefined,
        },
        {
          value: "networks",
          label: "Networks",
          component: <DockerNetworksTable />,
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
