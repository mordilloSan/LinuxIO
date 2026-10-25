import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Outlet } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { RoutedTabLayout, type RoutedTab } from "@/components/tabbar";
import { hasAccessPolicy, useAccessContext } from "@/hooks/useCapabilities";
import { HardDriveIcon } from "@/icons/svg";

import { STORAGE_TABS } from "./-components/storageTabs";
import { Route as SubvolumesRoute } from "./subvolumes";

function StorageLayout() {
  const access = useAccessContext();
  const subvolumesAllowed = hasAccessPolicy(
    SubvolumesRoute.options.staticData?.access,
    access,
  );
  // Subvolumes works on mounted btrfs filesystems; without one it is empty.
  const { data: filesystems = [] } = useQuery({
    ...linuxio.storage.btrfs_list,
    enabled: subvolumesAllowed,
  });
  const tabs: readonly RoutedTab[] =
    subvolumesAllowed && filesystems.length > 0
      ? STORAGE_TABS
      : STORAGE_TABS.filter((tab) => tab.to !== "/storage/subvolumes");
  return (
    <RoutedTabLayout tabs={tabs}>
      <Outlet />
    </RoutedTabLayout>
  );
}

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
