import React from "react";

import ComposeStacksPage from "./ComposeStacksPage";
import ContainerList from "./ContainerList";
import ImageList from "./ImageList";
import DockerNetworksTable from "./NetworkList";
import VolumeList from "./VolumeList";

import { TabContainer } from "@/components/tabbar";

const DockerPage: React.FC = () => {
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
          component: <ComposeStacksPage />,
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
