import { createFileRoute } from "@tanstack/react-router";

import { makeTabLayout } from "@/components/tabbar";
import { HardDriveIcon } from "@/icons/svg";

import { STORAGE_TABS } from "./-components/storageTabs";

const StorageLayout = makeTabLayout(STORAGE_TABS);

export const Route = createFileRoute("/_authenticated/storage")({
  component: StorageLayout,
  staticData: {
    navigation: {
      icon: HardDriveIcon,
      position: 40,
      title: "Storage",
    },
  },
});
